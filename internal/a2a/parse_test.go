package a2a

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestParseInbound(t *testing.T) {
	msg := a2a.NewMessage(
		a2a.MessageRoleUser,
		a2a.NewTextPart("intent=recruit.inquiry"),
		a2a.NewDataPart(map[string]any{
			"intent":  "recruit.inquiry",
			"payload": map[string]any{"job_id": "j1"},
		}),
	)
	msg.SetMeta(metaIntentKey, "recruit.inquiry")
	intent, payload := parseInbound(msg)
	if intent != "recruit.inquiry" {
		t.Fatalf("intent=%q", intent)
	}
	if payload["job_id"] != "j1" {
		t.Fatalf("payload=%v", payload)
	}
}
