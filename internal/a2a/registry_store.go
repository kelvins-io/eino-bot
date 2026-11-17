package a2a

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// RegisteredAgent is one entry in the local A2A catalog.
type RegisteredAgent struct {
	BotID            string         `json:"bot_id,omitempty"`
	URL              string         `json:"url"`
	AgentCard        *a2a.AgentCard `json:"agent_card"`
	RegisteredAt     string         `json:"registered_at"`
	LastSeenAt       string         `json:"last_seen_at"`
	InteractionCount int64          `json:"interaction_count"`
}

type registryStore struct {
	mu    sync.RWMutex
	byURL map[string]*RegisteredAgent
}

func newRegistryStore() *registryStore {
	return &registryStore{byURL: map[string]*RegisteredAgent{}}
}

func (s *registryStore) Upsert(url string, botID string, card *a2a.AgentCard) *RegisteredAgent {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	entry, ok := s.byURL[url]
	if !ok {
		entry = &RegisteredAgent{
			URL: url, BotID: botID, AgentCard: card,
			RegisteredAt: now, LastSeenAt: now,
		}
		s.byURL[url] = entry
		return cloneRegistered(entry)
	}
	entry.BotID = botID
	entry.AgentCard = card
	entry.LastSeenAt = now
	return cloneRegistered(entry)
}

func (s *registryStore) Touch(url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.byURL[url]
	if !ok {
		return false
	}
	entry.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	return true
}

func (s *registryStore) Delete(url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byURL[url]; !ok {
		return false
	}
	delete(s.byURL, url)
	return true
}

func (s *registryStore) RecordInteraction(url string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	url = strings.TrimRight(url, "/")
	entry, ok := s.byURL[url]
	if !ok {
		return 0
	}
	entry.InteractionCount++
	entry.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	return entry.InteractionCount
}

func (s *registryStore) List() []RegisteredAgent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RegisteredAgent, 0, len(s.byURL))
	for _, e := range s.byURL {
		out = append(out, *cloneRegistered(e))
	}
	return out
}

// SearchBySkillTag returns agents whose skills match tag (name/description/tags),
// sorted by interaction_count descending.
func (s *registryStore) SearchBySkillTag(tag string, limit int) []RegisteredAgent {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if limit <= 0 {
		limit = 10
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RegisteredAgent, 0)
	for _, e := range s.byURL {
		if tag == "" || agentMatchesSkillTag(e.AgentCard, tag) {
			out = append(out, *cloneRegistered(e))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].InteractionCount != out[j].InteractionCount {
			return out[i].InteractionCount > out[j].InteractionCount
		}
		return out[i].LastSeenAt > out[j].LastSeenAt
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func agentMatchesSkillTag(card *a2a.AgentCard, tag string) bool {
	if card == nil {
		return false
	}
	for _, sk := range card.Skills {
		if strings.Contains(strings.ToLower(sk.ID), tag) ||
			strings.Contains(strings.ToLower(sk.Name), tag) ||
			strings.Contains(strings.ToLower(sk.Description), tag) {
			return true
		}
		for _, t := range sk.Tags {
			if strings.Contains(strings.ToLower(t), tag) {
				return true
			}
		}
	}
	return false
}

func cloneRegistered(e *RegisteredAgent) *RegisteredAgent {
	cp := *e
	return &cp
}
