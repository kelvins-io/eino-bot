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
	"strings"
	"time"
)

// RegistrationConfig controls startup auto-registration.
type RegistrationConfig struct {
	// RegistryURLs is a list of registry bases to register with.
	// Empty means register to the local PublicURL (self catalog).
	RegistryURLs []string
	// HeartbeatInterval defaults to 60s.
	HeartbeatInterval time.Duration
	// Enabled defaults true when left zero-value with Enabled set via config loader.
	Enabled bool
}

// marketRegisterRequest matches agent-market POST /api/v1/agents/register.
type marketRegisterRequest struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Category    string         `json:"category"`
	Tags        []string       `json:"tags"`
	Lat         float64        `json:"lat"`
	Lng         float64        `json:"lng"`
	A2AURL      string         `json:"a2a_url"`
	HandlerPath string         `json:"handler_path"`
	AgentCard   map[string]any `json:"agent_card,omitempty"`
}

// StartAutoRegister registers this agent with configured registries and starts heartbeats.
// It is non-blocking and never prevents process startup.
func (s *Service) StartAutoRegister(ctx context.Context) {
	if !s.regCfg.Enabled {
		log.Printf("[a2a] auto-register disabled")
		return
	}
	urls := s.regCfg.RegistryURLs
	if len(urls) == 0 {
		urls = []string{s.PublicURL}
	}
	s.regMu.Lock()
	s.regTargets = append([]string{}, urls...)
	s.regMu.Unlock()

	go func() {
		// Wait briefly so remote registries / self HTTP are up before first POST.
		select {
		case <-ctx.Done():
			return
		case <-time.After(800 * time.Millisecond):
		}
		for _, u := range urls {
			if err := s.registerOnce(ctx, u); err != nil {
				log.Printf("[a2a] register %s failed: %v (will retry on heartbeat)", u, err)
			} else {
				log.Printf("[a2a] registered to %s", u)
			}
		}
		ticker := time.NewTicker(s.regCfg.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.regMu.RLock()
				targets := append([]string{}, s.regTargets...)
				s.regMu.RUnlock()
				for _, u := range targets {
					if err := s.heartbeatOnce(ctx, u); err != nil {
						log.Printf("[a2a] heartbeat %s failed: %v", u, err)
						_ = s.registerOnce(ctx, u)
					}
				}
			}
		}
	}()
}

// StopAutoRegister unregisters from all registries (best effort).
func (s *Service) StopAutoRegister(ctx context.Context) {
	s.regMu.RLock()
	targets := append([]string{}, s.regTargets...)
	s.regMu.RUnlock()
	for _, u := range targets {
		if err := s.unregisterOnce(ctx, u); err != nil {
			log.Printf("[a2a] unregister %s failed: %v", u, err)
		} else {
			log.Printf("[a2a] unregistered from %s", u)
		}
	}
}

// ListRegisteredAgents returns the local catalog snapshot.
func (s *Service) ListRegisteredAgents() []RegisteredAgent {
	if s.registry == nil {
		return nil
	}
	return s.registry.List()
}

func (s *Service) registerPayload() map[string]any {
	return map[string]any{
		"bot_id":     s.BotID,
		"url":        s.PublicURL,
		"agent_card": s.card,
	}
}

func (s *Service) marketPayload() marketRegisterRequest {
	category := "recruit"
	tags := []string{"招聘", "recruit", "hiring"}
	desc := ""
	if s.card != nil {
		desc = s.card.Description
		for _, sk := range s.card.Skills {
			tags = append(tags, sk.Tags...)
		}
	}
	if s.marketRecruitMode {
		category = "人才招聘"
		tags = append(tags, DefaultMarketSkillTag, "market")
	} else {
		tags = append(tags, "recruiter", "人才招聘")
	}
	tags = uniqueStrings(tags)

	lat, lng := s.MarketLat, s.MarketLng
	if lat == 0 && lng == 0 {
		lat, lng = 39.9042, 116.4074 // Beijing default (same as agent-market UI)
	}

	var cardMap map[string]any
	if s.card != nil {
		raw, _ := json.Marshal(s.card)
		_ = json.Unmarshal(raw, &cardMap)
	}
	return marketRegisterRequest{
		ID: s.BotID, Name: s.BotName, Description: desc,
		Category: category, Tags: tags, Lat: lat, Lng: lng,
		A2AURL: s.PublicURL, HandlerPath: s.A2APath, AgentCard: cardMap,
	}
}

func (s *Service) registerOnce(ctx context.Context, registryBase string) error {
	if s.isLocalRegistry(registryBase) {
		s.registry.Upsert(s.PublicURL, s.BotID, s.card)
		return nil
	}
	// Prefer agent-market API (e.g. http://127.0.0.1:13100).
	if err := s.postRegistry(ctx, registryBase, "/api/v1/agents/register", s.marketPayload()); err == nil {
		return nil
	} else {
		marketErr := err
		if err2 := s.postRegistry(ctx, registryBase, "/registry/register", s.registerPayload()); err2 != nil {
			return fmt.Errorf("market: %v; legacy: %v", marketErr, err2)
		}
		return nil
	}
}

func (s *Service) heartbeatOnce(ctx context.Context, registryBase string) error {
	if s.isLocalRegistry(registryBase) {
		if !s.registry.Touch(s.PublicURL) {
			s.registry.Upsert(s.PublicURL, s.BotID, s.card)
		}
		return nil
	}
	// agent-market treats re-register as heartbeat; also try legacy heartbeat.
	if err := s.registerOnce(ctx, registryBase); err == nil {
		return nil
	}
	return s.postRegistry(ctx, registryBase, "/registry/heartbeat", s.registerPayload())
}

func (s *Service) unregisterOnce(ctx context.Context, registryBase string) error {
	if s.isLocalRegistry(registryBase) {
		_ = s.registry.Delete(s.PublicURL)
		return nil
	}
	base := strings.TrimRight(registryBase, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/api/v1/agents/"+url.PathEscape(s.BotID), nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return s.postRegistry(ctx, registryBase, "/registry/unregister", map[string]any{
			"bot_id": s.BotID,
			"url":    s.PublicURL,
		})
	}
	return nil
}

func (s *Service) isLocalRegistry(registryBase string) bool {
	return s.hostRegistry && strings.TrimRight(registryBase, "/") == s.PublicURL
}

func (s *Service) postRegistry(ctx context.Context, registryBase, path string, body any) error {
	base := strings.TrimRight(registryBase, "/")
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.BearerToken)
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
