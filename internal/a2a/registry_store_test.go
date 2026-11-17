package a2a

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestSearchBySkillTagPrefersInteractions(t *testing.T) {
	s := newRegistryStore()
	cardRecruit := &a2a.AgentCard{
		Name: "Market",
		Skills: []a2a.AgentSkill{{
			ID: "talent.recruit.market", Name: "人才招聘",
			Tags: []string{"人才招聘", "recruit"},
		}},
	}
	cardOther := &a2a.AgentCard{
		Name: "Other",
		Skills: []a2a.AgentSkill{{
			ID: "profile.exchange", Name: "Profile", Tags: []string{"profile"},
		}},
	}
	s.Upsert("http://a", "bot-a", cardRecruit)
	s.Upsert("http://b", "bot-b", cardRecruit)
	s.Upsert("http://c", "bot-c", cardOther)
	s.RecordInteraction("http://b")
	s.RecordInteraction("http://b")
	s.RecordInteraction("http://a")

	got := s.SearchBySkillTag("人才招聘", 10)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].URL != "http://b" {
		t.Fatalf("want highest interaction first, got %s", got[0].URL)
	}
	if got[0].InteractionCount != 2 {
		t.Fatalf("want count 2, got %d", got[0].InteractionCount)
	}
}
