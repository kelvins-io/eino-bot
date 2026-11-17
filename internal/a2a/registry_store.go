package a2a

import (
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// RegisteredAgent is one entry in the local A2A catalog.
type RegisteredAgent struct {
	BotID       string         `json:"bot_id,omitempty"`
	URL         string         `json:"url"`
	AgentCard   *a2a.AgentCard `json:"agent_card"`
	RegisteredAt string        `json:"registered_at"`
	LastSeenAt  string         `json:"last_seen_at"`
}

type registryStore struct {
	mu   sync.RWMutex
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

func (s *registryStore) List() []RegisteredAgent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RegisteredAgent, 0, len(s.byURL))
	for _, e := range s.byURL {
		out = append(out, *cloneRegistered(e))
	}
	return out
}

func cloneRegistered(e *RegisteredAgent) *RegisteredAgent {
	cp := *e
	return &cp
}
