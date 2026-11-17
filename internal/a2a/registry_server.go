package a2a

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

type registerBody struct {
	AgentCard *a2a.AgentCard `json:"agent_card"`
	BotID     string         `json:"bot_id"`
	URL       string         `json:"url"`
	// Also accept a bare AgentCard at top level (python-a2a style).
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Service) RegistryHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/registry/register", s.handleRegistryRegister)
	mux.HandleFunc("/registry/unregister", s.handleRegistryUnregister)
	mux.HandleFunc("/registry/heartbeat", s.handleRegistryHeartbeat)
	mux.HandleFunc("/registry/agents", s.handleRegistryAgents)
	// a2a-registry.dev-style alias
	mux.HandleFunc("/agents", s.handleRegistryAgentsAlias)
	return mux
}

func (s *Service) handleRegistryRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	card, botID, url, err := decodeRegisterPayload(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if url == "" {
		url = primaryCardURL(card)
	}
	if url == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing agent url"})
		return
	}
	entry := s.registry.Upsert(strings.TrimRight(url, "/"), botID, card)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent": entry})
}

func (s *Service) handleRegistryUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, _, url, err := decodeRegisterPayload(r.Body)
	if err != nil || url == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "url required"})
		return
	}
	ok := s.registry.Delete(strings.TrimRight(url, "/"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok})
}

func (s *Service) handleRegistryHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	card, botID, url, err := decodeRegisterPayload(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if url == "" {
		url = primaryCardURL(card)
	}
	url = strings.TrimRight(url, "/")
	if url == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "url required"})
		return
	}
	if !s.registry.Touch(url) {
		// First heartbeat may arrive before register; upsert instead.
		s.registry.Upsert(url, botID, card)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleRegistryAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	skillTag := strings.TrimSpace(r.URL.Query().Get("skill_tag"))
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			limit = n
		}
	}
	var agents []RegisteredAgent
	if skillTag != "" {
		agents = s.registry.SearchBySkillTag(skillTag, limit)
	} else {
		agents = s.registry.List()
		sortAgentsByInteraction(agents)
		if len(agents) > limit {
			agents = agents[:limit]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"agents":    agents,
		"count":     len(agents),
		"skill_tag": skillTag,
		"sorted_by": "interaction_count_desc",
	})
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadInt
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, errBadInt
	}
	return n, nil
}

var errBadInt = errString("bad int")

type errString string

func (e errString) Error() string { return string(e) }

func (s *Service) handleRegistryAgentsAlias(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRegistryAgents(w, r)
	case http.MethodPost:
		// a2a-registry.dev: POST /agents { "agent_card": {...} }
		s.handleRegistryRegister(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func decodeRegisterPayload(r io.Reader) (*a2a.AgentCard, string, string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, "", "", err
	}
	var body registerBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, "", "", err
	}
	card := body.AgentCard
	if card == nil {
		// Bare card JSON
		var bare a2a.AgentCard
		if err := json.Unmarshal(raw, &bare); err == nil && bare.Name != "" {
			card = &bare
		}
	}
	url := strings.TrimSpace(body.URL)
	if url == "" && card != nil {
		url = primaryCardURL(card)
	}
	return card, body.BotID, url, nil
}

func primaryCardURL(card *a2a.AgentCard) string {
	if card == nil {
		return ""
	}
	for _, iface := range card.SupportedInterfaces {
		if iface != nil && iface.URL != "" {
			// Prefer base URL without /a2a path for registry identity.
			u := iface.URL
			if i := strings.LastIndex(u, "/a2a"); i > 0 {
				return u[:i]
			}
			return u
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
