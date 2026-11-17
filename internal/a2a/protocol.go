package a2a

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kelvins-io/eino-bot/internal/store"
)

// Envelope is the agent-to-agent message format.
type Envelope struct {
	ID        string         `json:"id"`
	FromBotID string         `json:"from_bot_id"`
	ToBotID   string         `json:"to_bot_id"`
	Intent    string         `json:"intent"`
	Payload   map[string]any `json:"payload"`
	Timestamp string         `json:"timestamp"`
	Signature string         `json:"signature"`
}

type DiscoverResponse struct {
	BotID       string   `json:"bot_id"`
	BotName     string   `json:"bot_name"`
	PublicURL   string   `json:"public_url"`
	Capabilities []string `json:"capabilities"`
	BotContact  string   `json:"bot_contact"`
}

type Client struct {
	BotID       string
	BotName     string
	PublicURL   string
	Secret      string
	HTTPClient  *http.Client
	DB          *store.DB
}

func NewClient(botID, botName, publicURL, secret string, db *store.DB) *Client {
	return &Client{
		BotID: botID, BotName: botName, PublicURL: publicURL, Secret: secret, DB: db,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Sign(e *Envelope) {
	e.Signature = SignPayload(c.Secret, e.ID, e.FromBotID, e.ToBotID, e.Intent, e.Timestamp)
}

func SignPayload(secret, id, from, to, intent, ts string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{id, from, to, intent, ts}, "|")))
	return hex.EncodeToString(mac.Sum(nil))
}

func (c *Client) Verify(e *Envelope) bool {
	expect := SignPayload(c.Secret, e.ID, e.FromBotID, e.ToBotID, e.Intent, e.Timestamp)
	return hmac.Equal([]byte(expect), []byte(e.Signature))
}

func (c *Client) Discover(ctx context.Context) DiscoverResponse {
	return DiscoverResponse{
		BotID: c.BotID, BotName: c.BotName, PublicURL: c.PublicURL,
		Capabilities: []string{"profile.exchange", "recruit.inquiry", "skill.match"},
		BotContact:   c.PublicURL + "/v1/a2a/message",
	}
}

func (c *Client) Send(ctx context.Context, endpoint, toBotID, intent string, payload map[string]any) (*Envelope, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	env := &Envelope{
		ID: uuid.NewString(), FromBotID: c.BotID, ToBotID: toBotID,
		Intent: intent, Payload: payload, Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	c.Sign(env)
	body, _ := json.Marshal(env)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/v1/a2a/message", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		_ = c.logMessage(ctx, "out", toBotID, endpoint, intent, string(body), "error:"+err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		_ = c.logMessage(ctx, "out", toBotID, endpoint, intent, string(body), fmt.Sprintf("http_%d", resp.StatusCode))
		return nil, fmt.Errorf("a2a peer status %d: %s", resp.StatusCode, string(raw))
	}
	var reply Envelope
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, err
	}
	_ = c.logMessage(ctx, "out", toBotID, endpoint, intent, string(body), "ok")
	_ = c.logMessage(ctx, "in", reply.FromBotID, endpoint, reply.Intent, string(raw), "ok")
	return &reply, nil
}

func (c *Client) HandleIncoming(ctx context.Context, env *Envelope, profileSummary string) (*Envelope, error) {
	if !c.Verify(env) {
		return nil, fmt.Errorf("invalid a2a signature")
	}
	raw, _ := json.Marshal(env)
	_ = c.logMessage(ctx, "in", env.FromBotID, "", env.Intent, string(raw), "ok")

	replyPayload := map[string]any{
		"accepted": true,
		"bot_id":   c.BotID,
		"bot_name": c.BotName,
	}
	switch env.Intent {
	case "profile.inquiry", "recruit.inquiry":
		replyPayload["profile_summary"] = profileSummary
		replyPayload["note"] = "仅返回对方授权范围内的摘要信息"
	case "handshake":
		replyPayload["capabilities"] = []string{"profile.exchange", "recruit.inquiry", "skill.match"}
	default:
		replyPayload["echo_intent"] = env.Intent
		replyPayload["received_keys"] = keysOf(env.Payload)
	}
	reply := &Envelope{
		ID: uuid.NewString(), FromBotID: c.BotID, ToBotID: env.FromBotID,
		Intent: env.Intent + ".reply", Payload: replyPayload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	c.Sign(reply)
	out, _ := json.Marshal(reply)
	_ = c.logMessage(ctx, "out", env.FromBotID, "", reply.Intent, string(out), "ok")
	return reply, nil
}

func (c *Client) logMessage(ctx context.Context, direction, peerID, endpoint, intent, payload, status string) error {
	if c.DB == nil {
		return nil
	}
	return c.DB.Gorm().WithContext(ctx).Create(&store.A2AMessageRow{
		ID: uuid.NewString(), Direction: direction, PeerBotID: peerID,
		PeerEndpoint: endpoint, Intent: intent, PayloadJSON: payload,
		Status: status, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}).Error
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
