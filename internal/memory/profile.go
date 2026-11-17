package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kelvins-io/eino-bot/internal/store"
)

// Fact is one atomic memory unit about a person.
type Fact struct {
	ID         int64   `json:"id"`
	OwnerID    string  `json:"owner_id"`
	Category   string  `json:"category"`
	Content    string  `json:"content"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	Duplicate  bool    `json:"duplicate,omitempty"`
}

// Profile is the aggregated person portrait.
type Profile struct {
	OwnerID     string            `json:"owner_id"`
	DisplayName string            `json:"display_name"`
	Summary     string            `json:"summary"`
	BotContact  string            `json:"bot_contact"`
	Meta        map[string]string `json:"meta"`
	Facts       []Fact            `json:"facts"`
	UpdatedAt   string            `json:"updated_at"`
}

type Service struct {
	db *store.DB
}

func New(db *store.DB) *Service {
	return &Service{db: db}
}

func (s *Service) gorm() *gorm.DB {
	return s.db.Gorm()
}

func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) || r == '，' || r == '。' || r == ',' || r == '.' || r == ';' || r == '；' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

func HashContent(norm string) string {
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

type UpsertResult struct {
	Inserted  bool   `json:"inserted"`
	Duplicate bool   `json:"duplicate"`
	FactID    int64  `json:"fact_id"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	Message   string `json:"message"`
}

func (s *Service) UpsertFact(ctx context.Context, ownerID, category, content, source string, confidence float64) (*UpsertResult, error) {
	content = strings.TrimSpace(content)
	if ownerID == "" || content == "" {
		return nil, fmt.Errorf("owner_id and content are required")
	}
	if category == "" {
		category = "general"
	}
	if source == "" {
		source = "chat"
	}
	if confidence <= 0 {
		confidence = 1
	}
	norm := Normalize(content)
	hash := HashContent(norm)
	now := time.Now().UTC().Format(time.RFC3339)

	row := store.ProfileFact{
		OwnerID: ownerID, Category: category, Content: content,
		ContentNorm: norm, ContentHash: hash, Source: source,
		Confidence: confidence, CreatedAt: now, UpdatedAt: now,
	}
	tx := s.gorm().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "owner_id"}, {Name: "content_hash"}},
		DoNothing: true,
	}).Create(&row)
	if tx.Error != nil {
		return nil, tx.Error
	}
	if tx.RowsAffected == 0 {
		var existing store.ProfileFact
		_ = s.gorm().WithContext(ctx).
			Where("owner_id = ? AND content_hash = ?", ownerID, hash).
			First(&existing).Error
		return &UpsertResult{
			Inserted: false, Duplicate: true, FactID: existing.ID,
			Content: content, Category: category, Message: "duplicate fact skipped",
		}, nil
	}
	if err := s.ensureProfile(ctx, ownerID); err != nil {
		return nil, err
	}
	return &UpsertResult{
		Inserted: true, Duplicate: false, FactID: row.ID,
		Content: content, Category: category, Message: "fact stored",
	}, nil
}

func (s *Service) ensureProfile(ctx context.Context, ownerID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	row := store.ProfileRow{
		OwnerID: ownerID, DisplayName: "", Summary: "", BotContact: "",
		MetaJSON: "{}", UpdatedAt: now,
	}
	return s.gorm().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "owner_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
	}).Create(&row).Error
}

func (s *Service) UpdateProfileMeta(ctx context.Context, ownerID, displayName, summary, botContact string, meta map[string]string) error {
	if err := s.ensureProfile(ctx, ownerID); err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]string{}
	}
	metaJSON, _ := json.Marshal(meta)
	now := time.Now().UTC().Format(time.RFC3339)

	updates := map[string]any{
		"meta_json":  string(metaJSON),
		"updated_at": now,
	}
	if displayName != "" {
		updates["display_name"] = displayName
	}
	if summary != "" {
		updates["summary"] = summary
	}
	if botContact != "" {
		updates["bot_contact"] = botContact
	}
	return s.gorm().WithContext(ctx).Model(&store.ProfileRow{}).
		Where("owner_id = ?", ownerID).Updates(updates).Error
}

func (s *Service) GetProfile(ctx context.Context, ownerID string) (*Profile, error) {
	p := &Profile{OwnerID: ownerID, Meta: map[string]string{}, Facts: []Fact{}}
	var row store.ProfileRow
	err := s.gorm().WithContext(ctx).Where("owner_id = ?", ownerID).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		p.DisplayName = row.DisplayName
		p.Summary = row.Summary
		p.BotContact = row.BotContact
		p.UpdatedAt = row.UpdatedAt
		if row.MetaJSON != "" {
			_ = json.Unmarshal([]byte(row.MetaJSON), &p.Meta)
		}
	}

	var facts []store.ProfileFact
	if err := s.gorm().WithContext(ctx).
		Where("owner_id = ?", ownerID).
		Order("category ASC, id ASC").
		Find(&facts).Error; err != nil {
		return nil, err
	}
	for _, f := range facts {
		p.Facts = append(p.Facts, Fact{
			ID: f.ID, OwnerID: f.OwnerID, Category: f.Category, Content: f.Content,
			Source: f.Source, Confidence: f.Confidence, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
		})
	}
	return p, nil
}

func (s *Service) SearchFacts(ctx context.Context, ownerID, query string, limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 20
	}
	q := "%" + Normalize(query) + "%"
	var rows []store.ProfileFact
	err := s.gorm().WithContext(ctx).
		Where("owner_id = ? AND (content_norm LIKE ? OR category LIKE ?)", ownerID, q, q).
		Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Fact, 0, len(rows))
	for _, f := range rows {
		out = append(out, Fact{
			ID: f.ID, OwnerID: f.OwnerID, Category: f.Category, Content: f.Content,
			Source: f.Source, Confidence: f.Confidence, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Service) AppendChat(ctx context.Context, sessionID, role, content string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return s.gorm().WithContext(ctx).Create(&store.ChatMessage{
		SessionID: sessionID, Role: role, Content: content, CreatedAt: now,
	}).Error
}

func (s *Service) ListChat(ctx context.Context, sessionID string, limit int) ([]map[string]string, error) {
	if limit <= 0 {
		limit = 40
	}
	var rows []store.ChatMessage
	err := s.gorm().WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	rev := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		rev = append(rev, map[string]string{
			"role": r.Role, "content": r.Content, "created_at": r.CreatedAt,
		})
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

func (s *Service) RebuildSummary(ctx context.Context, ownerID string) (string, error) {
	p, err := s.GetProfile(ctx, ownerID)
	if err != nil {
		return "", err
	}
	byCat := map[string][]string{}
	for _, f := range p.Facts {
		byCat[f.Category] = append(byCat[f.Category], f.Content)
	}
	var b strings.Builder
	if p.DisplayName != "" {
		b.WriteString("姓名/称呼: ")
		b.WriteString(p.DisplayName)
		b.WriteString("\n")
	}
	if p.BotContact != "" {
		b.WriteString("Bot联系方式: ")
		b.WriteString(p.BotContact)
		b.WriteString("\n")
	}
	for cat, items := range byCat {
		b.WriteString("- ")
		b.WriteString(cat)
		b.WriteString(": ")
		b.WriteString(strings.Join(items, "；"))
		b.WriteString("\n")
	}
	summary := strings.TrimSpace(b.String())
	_ = s.UpdateProfileMeta(ctx, ownerID, "", summary, "", p.Meta)
	return summary, nil
}
