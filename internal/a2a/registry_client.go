package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

func (s *Service) registerOnce(ctx context.Context, registryBase string) error {
	if s.isLocalRegistry(registryBase) {
		s.registry.Upsert(s.PublicURL, s.BotID, s.card)
		return nil
	}
	return s.postRegistry(ctx, registryBase, "/registry/register", s.registerPayload())
}

func (s *Service) heartbeatOnce(ctx context.Context, registryBase string) error {
	if s.isLocalRegistry(registryBase) {
		if !s.registry.Touch(s.PublicURL) {
			s.registry.Upsert(s.PublicURL, s.BotID, s.card)
		}
		return nil
	}
	return s.postRegistry(ctx, registryBase, "/registry/heartbeat", s.registerPayload())
}

func (s *Service) unregisterOnce(ctx context.Context, registryBase string) error {
	if s.isLocalRegistry(registryBase) {
		_ = s.registry.Delete(s.PublicURL)
		return nil
	}
	return s.postRegistry(ctx, registryBase, "/registry/unregister", map[string]any{
		"bot_id": s.BotID,
		"url":    s.PublicURL,
	})
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
