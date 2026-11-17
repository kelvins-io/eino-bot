package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultMarketSkillTag is the A2A skill tag used to find talent-recruit marketplace agents.
	DefaultMarketSkillTag = "人才招聘"
	intentTaskCreate      = "recruit.task.create"
	intentTaskStatus      = "recruit.task.status"
	intentTaskResult      = "recruit.task.result"
)

// SearchMarketAgents finds catalog agents by skill tag, preferring higher interaction counts.
func (s *Service) SearchMarketAgents(skillTag string, limit int) []RegisteredAgent {
	if skillTag == "" {
		skillTag = DefaultMarketSkillTag
	}
	if s.registry == nil {
		return nil
	}
	return s.registry.SearchBySkillTag(skillTag, limit)
}

// FetchRemoteRegistryAgents pulls /registry/agents from a remote registry base URL.
func (s *Service) FetchRemoteRegistryAgents(ctx context.Context, registryBase, skillTag string, limit int) ([]RegisteredAgent, error) {
	base := strings.TrimRight(registryBase, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/registry/agents?skill_tag="+urlQueryEscape(skillTag), nil)
	if err != nil {
		return nil, err
	}
	if s.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.BearerToken)
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry status %d: %s", resp.StatusCode, string(b))
	}
	var body struct {
		Agents []RegisteredAgent `json:"agents"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, err
	}
	if skillTag != "" {
		filtered := make([]RegisteredAgent, 0, len(body.Agents))
		tag := strings.ToLower(skillTag)
		for _, a := range body.Agents {
			if agentMatchesSkillTag(a.AgentCard, tag) {
				filtered = append(filtered, a)
			}
		}
		body.Agents = filtered
	}
	// Prefer high interaction; remote list may already be sorted, re-sort locally.
	sortAgentsByInteraction(body.Agents)
	if limit > 0 && len(body.Agents) > limit {
		body.Agents = body.Agents[:limit]
	}
	return body.Agents, nil
}

func sortAgentsByInteraction(agents []RegisteredAgent) {
	for i := 0; i < len(agents); i++ {
		for j := i + 1; j < len(agents); j++ {
			if agents[j].InteractionCount > agents[i].InteractionCount {
				agents[i], agents[j] = agents[j], agents[i]
			}
		}
	}
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}

// MarketTaskCreatePayload is sent to a marketplace recruit agent.
type MarketTaskCreatePayload struct {
	TaskID        string         `json:"task_id"`
	JobID         string         `json:"job_id"`
	Title         string         `json:"title"`
	Requirements  map[string]any `json:"requirements"`
	CallbackURL   string         `json:"callback_url"`
	CallbackToken string         `json:"callback_token"`
	FromBotID     string         `json:"from_bot_id"`
	FromURL       string         `json:"from_url"`
}

// DelegateRecruitTask asks a market agent to start a talent search and return/callback results.
func (s *Service) DelegateRecruitTask(ctx context.Context, agentURL, agentBotID string, payload MarketTaskCreatePayload) (map[string]any, error) {
	reply, err := s.Send(ctx, agentURL, agentBotID, intentTaskCreate, map[string]any{
		"task_id":        payload.TaskID,
		"job_id":         payload.JobID,
		"title":          payload.Title,
		"requirements":   payload.Requirements,
		"callback_url":   payload.CallbackURL,
		"callback_token": payload.CallbackToken,
		"from_bot_id":    payload.FromBotID,
		"from_url":       payload.FromURL,
	})
	if err != nil {
		return nil, err
	}
	return reply, nil
}

// PollRecruitTask asks the market agent for task status/results.
func (s *Service) PollRecruitTask(ctx context.Context, agentURL, agentBotID, taskID string) (map[string]any, error) {
	return s.Send(ctx, agentURL, agentBotID, intentTaskStatus, map[string]any{
		"task_id": taskID,
	})
}

// fulfillMarketRecruitAsProvider runs when THIS bot acts as a 人才招聘 market agent.
func (s *Service) fulfillMarketRecruitAsProvider(ctx context.Context, payload map[string]any) {
	taskID, _ := payload["task_id"].(string)
	callbackURL, _ := payload["callback_url"].(string)
	callbackToken, _ := payload["callback_token"].(string)
	title, _ := payload["title"].(string)
	jobID, _ := payload["job_id"].(string)

	candidates := s.scanLocalResumesAsMarketHits(title, payload)
	// Persist locally so poll works even without callback.
	s.marketMu.Lock()
	if s.marketTasks == nil {
		s.marketTasks = map[string]marketProviderTask{}
	}
	s.marketTasks[taskID] = marketProviderTask{
		TaskID: taskID, JobID: jobID, Status: "completed", Results: candidates, UpdatedAt: time.Now().UTC(),
	}
	s.marketMu.Unlock()

	if callbackURL == "" {
		log.Printf("[a2a-market] task %s completed without callback (%d hits)", taskID, len(candidates))
		return
	}
	body := map[string]any{
		"task_id":    taskID,
		"job_id":     jobID,
		"status":     "completed",
		"token":      callbackToken,
		"candidates": candidates,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(raw))
	if err != nil {
		log.Printf("[a2a-market] callback build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if callbackToken != "" {
		req.Header.Set("X-Callback-Token", callbackToken)
	}
	if s.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.BearerToken)
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		log.Printf("[a2a-market] callback %s failed: %v", callbackURL, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[a2a-market] callback %s status=%d task=%s hits=%d", callbackURL, resp.StatusCode, taskID, len(candidates))
}

type marketProviderTask struct {
	TaskID    string
	JobID     string
	Status    string
	Results   []map[string]any
	UpdatedAt time.Time
}

func (s *Service) getProviderTask(taskID string) (marketProviderTask, bool) {
	s.marketMu.RLock()
	defer s.marketMu.RUnlock()
	t, ok := s.marketTasks[taskID]
	return t, ok
}

func (s *Service) scanLocalResumesAsMarketHits(title string, payload map[string]any) []map[string]any {
	root := os.Getenv("BOT_RESUME_ROOT")
	if root == "" {
		root = "./data/resumes"
	}
	q := strings.ToLower(title)
	if reqs, ok := payload["requirements"].(map[string]any); ok {
		if skills, ok := reqs["skills"].([]any); ok {
			for _, sk := range skills {
				if ss, ok := sk.(string); ok {
					q += " " + strings.ToLower(ss)
				}
			}
		}
	}
	terms := strings.Fields(q)
	var hits []map[string]any
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(raw)
		score := 40.0
		lower := strings.ToLower(text)
		for _, t := range terms {
			if t != "" && strings.Contains(lower, t) {
				score += 8
			}
		}
		if score > 95 {
			score = 95
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		preview := text
		if len(preview) > 800 {
			preview = preview[:800] + "..."
		}
		// Extract bot endpoint if present.
		botURL := ""
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "http://") || strings.Contains(line, "https://") {
				for _, p := range strings.Fields(line) {
					if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
						botURL = strings.TrimRight(p, "/).,")
						break
					}
				}
			}
		}
		hits = append(hits, map[string]any{
			"name":         name,
			"source":       "a2a_market",
			"score":        score,
			"bot_endpoint": botURL,
			"preview":      preview,
			"path":         path,
		})
		return nil
	})
	return hits
}
