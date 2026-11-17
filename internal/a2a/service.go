package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"

	"github.com/kelvins-io/eino-bot/internal/store"
)

const (
	metaIntentKey  = "eino.bot.intent"
	defaultA2APath = "/a2a"
	bearerScheme   = a2a.SecuritySchemeName("bearer")
)

// ProfileSummaryFunc returns the local owner's authorized profile summary for inbound A2A replies.
type ProfileSummaryFunc func(ctx context.Context) string

// Service exposes a standards-compliant A2A Agent Card + JSON-RPC surface and an outbound client.
type Service struct {
	BotID       string
	BotName     string
	PublicURL   string
	BearerToken string
	A2APath     string
	DB          *store.DB
	HTTPClient  *http.Client

	card           *a2a.AgentCard
	requestHandler a2asrv.RequestHandler
	jsonrpc        http.Handler
	cardHandler    http.Handler
	profileFn      ProfileSummaryFunc

	registry   *registryStore
	hostRegistry bool
	regCfg     RegistrationConfig
	regMu      sync.RWMutex
	regTargets []string
}

// Config configures the A2A service.
type Config struct {
	BotID        string
	BotName      string
	PublicURL    string
	BearerToken  string
	A2APath      string
	DB           *store.DB
	ProfileFn    ProfileSummaryFunc
	HostRegistry bool
	Registration RegistrationConfig
}

func NewService(cfg Config) *Service {
	path := cfg.A2APath
	if path == "" {
		path = defaultA2APath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base := strings.TrimRight(cfg.PublicURL, "/")
	invokeURL := base + path

	s := &Service{
		BotID: cfg.BotID, BotName: cfg.BotName, PublicURL: base,
		BearerToken: cfg.BearerToken, A2APath: path, DB: cfg.DB,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		profileFn:    cfg.ProfileFn,
		registry:     newRegistryStore(),
		hostRegistry: cfg.HostRegistry,
		regCfg:       cfg.Registration,
	}
	if s.regCfg.HeartbeatInterval <= 0 {
		s.regCfg.HeartbeatInterval = 60 * time.Second
	}

	card := &a2a.AgentCard{
		Name:        cfg.BotName,
		Description: "Persistent personal agent bot (memory, recruiting, profile exchange)",
		Version:     "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(invokeURL, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
		Skills: []a2a.AgentSkill{
			{
				ID: "profile.exchange", Name: "Profile Exchange",
				Description: "Share an authorized profile summary",
				Tags:        []string{"profile", "memory"},
				Examples:    []string{"profile.inquiry", "handshake"},
			},
			{
				ID: "recruit.inquiry", Name: "Recruiting Inquiry",
				Description: "Answer recruiting-related profile questions",
				Tags:        []string{"recruit", "hiring"},
				Examples:    []string{"recruit.inquiry"},
			},
		},
		Provider: &a2a.AgentProvider{Org: "eino-bot"},
	}
	if cfg.BearerToken != "" {
		card.SecuritySchemes = a2a.NamedSecuritySchemes{
			bearerScheme: a2a.HTTPAuthSecurityScheme{Scheme: "Bearer", BearerFormat: "opaque"},
		}
		card.SecurityRequirements = []a2a.SecurityRequirements{{bearerScheme: {}}}
	}
	s.card = card

	var opts []a2asrv.RequestHandlerOption
	if cfg.BearerToken != "" {
		opts = append(opts, a2asrv.WithCallInterceptors(&bearerAuthInterceptor{token: cfg.BearerToken}))
	}
	s.requestHandler = a2asrv.NewHandler(a2asrv.AgentExecutorFunc(s.execute), opts...)
	s.jsonrpc = a2asrv.NewJSONRPCHandler(s.requestHandler)
	s.cardHandler = a2asrv.NewStaticAgentCardHandler(card)
	return s
}

func (s *Service) AgentCard() *a2a.AgentCard { return s.card }

func (s *Service) JSONRPCHandler() http.Handler { return s.jsonrpc }

func (s *Service) CardHandler() http.Handler { return s.cardHandler }

func (s *Service) HostRegistry() bool { return s.hostRegistry }

// DiscoverSummary returns a compact discovery payload for compatibility endpoints.
func (s *Service) DiscoverSummary() map[string]any {
	return map[string]any{
		"protocol":         "a2a",
		"protocol_version": string(a2a.Version),
		"bot_id":           s.BotID,
		"bot_name":         s.BotName,
		"public_url":       s.PublicURL,
		"agent_card":       s.PublicURL + a2asrv.WellKnownAgentCardPath,
		"jsonrpc_endpoint": s.PublicURL + s.A2APath,
		"registry":         s.PublicURL + "/registry/agents",
		"host_registry":    s.hostRegistry,
		"auto_register":    s.regCfg.Enabled,
		"transport":        string(a2a.TransportProtocolJSONRPC),
		"skills":           []string{"profile.exchange", "recruit.inquiry"},
		"auth":             map[string]any{"bearer_required": s.BearerToken != ""},
		"registered":       len(s.ListRegisteredAgents()),
	}
}

// Send resolves the peer Agent Card and calls SendMessage over JSON-RPC.
func (s *Service) Send(ctx context.Context, peerBaseURL, toBotID, intent string, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	base := strings.TrimRight(peerBaseURL, "/")
	card, err := agentcard.DefaultResolver.Resolve(ctx, base)
	if err != nil {
		_ = s.logMessage(ctx, "out", toBotID, base, intent, "", "resolve_error:"+err.Error())
		return nil, fmt.Errorf("resolve agent card: %w", err)
	}

	factoryOpts := []a2aclient.FactoryOption{
		a2aclient.WithJSONRPCTransport(s.HTTPClient),
	}
	sessionID := a2aclient.SessionID("eino-bot")
	if s.BearerToken != "" {
		credStore := a2aclient.NewInMemoryCredentialsStore()
		credStore.Set(sessionID, bearerScheme, a2aclient.AuthCredential(s.BearerToken))
		factoryOpts = append(factoryOpts, a2aclient.WithCallInterceptors(&a2aclient.AuthInterceptor{Service: credStore}))
		ctx = a2aclient.AttachSessionID(ctx, sessionID)
	}

	client, err := a2aclient.NewFromCard(ctx, card, factoryOpts...)
	if err != nil {
		_ = s.logMessage(ctx, "out", toBotID, base, intent, "", "client_error:"+err.Error())
		return nil, err
	}

	msg := a2a.NewMessage(
		a2a.MessageRoleUser,
		a2a.NewTextPart(fmt.Sprintf("intent=%s", intent)),
		a2a.NewDataPart(map[string]any{
			"intent":   intent,
			"from_bot": s.BotID,
			"to_bot":   toBotID,
			"payload":  payload,
		}),
	)
	msg.SetMeta(metaIntentKey, intent)

	reqBody, _ := json.Marshal(map[string]any{"intent": intent, "payload": payload})
	resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		_ = s.logMessage(ctx, "out", toBotID, base, intent, string(reqBody), "error:"+err.Error())
		return nil, err
	}
	out := resultToMap(resp)
	raw, _ := json.Marshal(out)
	_ = s.logMessage(ctx, "out", toBotID, base, intent, string(reqBody), "ok")
	_ = s.logMessage(ctx, "in", card.Name, base, intent+".reply", string(raw), "ok")
	return out, nil
}

func (s *Service) logMessage(ctx context.Context, direction, peerID, endpoint, intent, payload, status string) error {
	if s.DB == nil {
		return nil
	}
	return s.DB.Gorm().WithContext(ctx).Create(&store.A2AMessageRow{
		ID: uuid.NewString(), Direction: direction, PeerBotID: peerID,
		PeerEndpoint: endpoint, Intent: intent, PayloadJSON: payload,
		Status: status, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}).Error
}

func resultToMap(resp a2a.SendMessageResult) map[string]any {
	out := map[string]any{"protocol": "a2a"}
	switch v := resp.(type) {
	case *a2a.Message:
		out["kind"] = "message"
		out["message"] = messageToMap(v)
	case *a2a.Task:
		out["kind"] = "task"
		out["task_id"] = string(v.ID)
		out["state"] = string(v.Status.State)
		if v.Status.Message != nil {
			out["message"] = messageToMap(v.Status.Message)
		}
		if len(v.Artifacts) > 0 && len(v.Artifacts[0].Parts) > 0 {
			parts := make([]any, 0, len(v.Artifacts[0].Parts))
			for _, p := range v.Artifacts[0].Parts {
				parts = append(parts, partToAny(p))
			}
			out["artifacts"] = parts
		}
	default:
		out["raw_type"] = fmt.Sprintf("%T", resp)
	}
	return out
}

func messageToMap(m *a2a.Message) map[string]any {
	if m == nil {
		return nil
	}
	parts := make([]any, 0, len(m.Parts))
	var texts []string
	var data any
	for _, p := range m.Parts {
		parts = append(parts, partToAny(p))
		if t := p.Text(); t != "" {
			texts = append(texts, t)
		}
		if d := p.Data(); d != nil {
			data = d
		}
	}
	return map[string]any{
		"message_id": m.ID,
		"role":       string(m.Role),
		"text":       strings.Join(texts, "\n"),
		"data":       data,
		"parts":      parts,
		"metadata":   m.Metadata,
	}
}

func partToAny(p *a2a.Part) any {
	if p == nil {
		return nil
	}
	if t := p.Text(); t != "" {
		return map[string]any{"text": t}
	}
	if d := p.Data(); d != nil {
		return map[string]any{"data": d}
	}
	return map[string]any{}
}
