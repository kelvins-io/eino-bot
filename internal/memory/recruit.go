package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kelvins-io/eino-bot/internal/store"
)

type JobRequirement struct {
	ID           string         `json:"id"`
	OwnerID      string         `json:"owner_id"`
	Title        string         `json:"title"`
	Requirements map[string]any `json:"requirements"`
	Status       string         `json:"status"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

type Candidate struct {
	ID          string         `json:"id"`
	JobID       string         `json:"job_id"`
	Name        string         `json:"name"`
	Source      string         `json:"source"`
	GitHubLogin string         `json:"github_login"`
	BotEndpoint string         `json:"bot_endpoint"`
	Profile     map[string]any `json:"profile"`
	Score       float64        `json:"score"`
	RankPos     int            `json:"rank_pos"`
	Notes       string         `json:"notes"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func (s *Service) SaveJob(ctx context.Context, ownerID, title string, requirements map[string]any) (*JobRequirement, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if requirements == nil {
		requirements = map[string]any{}
	}
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(requirements)
	row := store.JobRequirementRow{
		ID: id, OwnerID: ownerID, Title: title,
		RequirementsJSON: string(raw), Status: "open",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.gorm().WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &JobRequirement{
		ID: id, OwnerID: ownerID, Title: title, Requirements: requirements,
		Status: "open", CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) GetJob(ctx context.Context, jobID string) (*JobRequirement, error) {
	var row store.JobRequirementRow
	err := s.gorm().WithContext(ctx).Where("id = ?", jobID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("job not found")
	}
	if err != nil {
		return nil, err
	}
	j := &JobRequirement{
		ID: row.ID, OwnerID: row.OwnerID, Title: row.Title,
		Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Requirements: map[string]any{},
	}
	_ = json.Unmarshal([]byte(row.RequirementsJSON), &j.Requirements)
	return j, nil
}

func (s *Service) UpdateJob(ctx context.Context, jobID string, requirements map[string]any, status string) (*JobRequirement, error) {
	j, err := s.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if requirements != nil {
		j.Requirements = requirements
	}
	if status != "" {
		j.Status = status
	}
	now := time.Now().UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(j.Requirements)
	err = s.gorm().WithContext(ctx).Model(&store.JobRequirementRow{}).Where("id = ?", jobID).
		Updates(map[string]any{
			"requirements_json": string(raw),
			"status":            j.Status,
			"updated_at":        now,
		}).Error
	if err != nil {
		return nil, err
	}
	j.UpdatedAt = now
	return j, nil
}

func (s *Service) UpsertCandidate(ctx context.Context, c *Candidate) (*Candidate, error) {
	if c.JobID == "" || c.Name == "" {
		return nil, fmt.Errorf("job_id and name are required")
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Profile == nil {
		c.Profile = map[string]any{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c.CreatedAt = now
	c.UpdatedAt = now
	raw, _ := json.Marshal(c.Profile)
	row := store.CandidateRow{
		ID: c.ID, JobID: c.JobID, Name: c.Name, Source: c.Source,
		GitHubLogin: c.GitHubLogin, BotEndpoint: c.BotEndpoint,
		ProfileJSON: string(raw), Score: c.Score, RankPos: c.RankPos,
		Notes: c.Notes, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	err := s.gorm().WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "source", "github_login", "bot_endpoint",
			"profile_json", "score", "notes", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) ListCandidates(ctx context.Context, jobID string) ([]Candidate, error) {
	var rows []store.CandidateRow
	err := s.gorm().WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("score DESC, updated_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(rows))
	for _, r := range rows {
		c := Candidate{
			ID: r.ID, JobID: r.JobID, Name: r.Name, Source: r.Source,
			GitHubLogin: r.GitHubLogin, BotEndpoint: r.BotEndpoint,
			Score: r.Score, RankPos: r.RankPos, Notes: r.Notes,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			Profile: map[string]any{},
		}
		_ = json.Unmarshal([]byte(r.ProfileJSON), &c.Profile)
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) RankTopCandidates(ctx context.Context, jobID string, topN int) ([]Candidate, error) {
	if topN <= 0 {
		topN = 3
	}
	list, err := s.ListCandidates(ctx, jobID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].Score > list[j].Score
	})
	if len(list) > topN {
		list = list[:topN]
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range list {
		list[i].RankPos = i + 1
		_ = s.gorm().WithContext(ctx).Model(&store.CandidateRow{}).Where("id = ?", list[i].ID).
			Updates(map[string]any{"rank_pos": list[i].RankPos, "updated_at": now}).Error
	}
	return list, nil
}
