package a2a

import (
	"context"
	"encoding/json"
	"iter"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func (s *Service) execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		intent, payload := parseInbound(execCtx.Message)
		summary := ""
		if s.profileFn != nil {
			summary = s.profileFn(ctx)
		}
		replyData := map[string]any{
			"accepted": true,
			"bot_id":   s.BotID,
			"bot_name": s.BotName,
			"intent":   intent,
		}
		switch intent {
		case "profile.inquiry", "recruit.inquiry":
			replyData["profile_summary"] = summary
			replyData["note"] = "仅返回对方授权范围内的摘要信息"
		case "handshake", "":
			replyData["capabilities"] = []string{"profile.exchange", "recruit.inquiry"}
			if intent == "" {
				replyData["intent"] = "handshake"
			}
		default:
			replyData["echo_intent"] = intent
			replyData["received_payload"] = payload
		}
		raw, _ := json.Marshal(replyData)
		_ = s.logMessage(ctx, "in", "", "", intent, string(raw), "ok")

		msg := a2a.NewMessage(
			a2a.MessageRoleAgent,
			a2a.NewTextPart("a2a reply: "+intent),
			a2a.NewDataPart(replyData),
		)
		msg.SetMeta(metaIntentKey, intent+".reply")
		yield(msg, nil)
	}
}

func parseInbound(msg *a2a.Message) (intent string, payload map[string]any) {
	payload = map[string]any{}
	if msg == nil {
		return "handshake", payload
	}
	if msg.Metadata != nil {
		if v, ok := msg.Metadata[metaIntentKey].(string); ok && v != "" {
			intent = v
		}
	}
	for _, p := range msg.Parts {
		if d := p.Data(); d != nil {
			if m, ok := d.(map[string]any); ok {
				if intent == "" {
					if v, ok := m["intent"].(string); ok {
						intent = v
					}
				}
				if nested, ok := m["payload"].(map[string]any); ok {
					payload = nested
				} else {
					payload = m
				}
			}
		}
		if t := p.Text(); t != "" && intent == "" {
			if strings.HasPrefix(t, "intent=") {
				intent = strings.TrimPrefix(t, "intent=")
			}
		}
	}
	if intent == "" {
		intent = "handshake"
	}
	return intent, payload
}
