package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelvins-io/eino-bot/internal/store"
)

// MarketRecruitTask is a delegated recruiting search on an A2A market agent.
type MarketRecruitTask struct {
	ID            string           `json:"id"`
	JobID         string           `json:"job_id"`
	AgentURL      string           `json:"agent_url"`
	AgentBotID    string           `json:"agent_bot_id"`
	Status        string           `json:"status"`
	CallbackToken string           `json:"callback_token,omitempty"`
	Results       []map[string]any `json:"results"`
	ErrorMsg      string           `json:"error_msg,omitempty"`
	CreatedAt     string           `json:"created_at"`
	UpdatedAt     string           `json:"updated_at"`
}

func (s *Service) CreateMarketRecruitTask(ctx context.Context, jobID, agentURL, agentBotID string) (*MarketRecruitTask, error) {
	if jobID == "" || agentURL == "" {
		return nil, fmt.Errorf("job_id and agent_url are required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row := store.MarketRecruitTaskRow{
		ID: uuid.NewString(), JobID: jobID, AgentURL: agentURL, AgentBotID: agentBotID,
		Status: "pending", CallbackToken: uuid.NewString(), ResultJSON: "[]",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.gorm().WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return marketTaskFromRow(row), nil
}

func (s *Service) GetMarketRecruitTask(ctx context.Context, taskID string) (*MarketRecruitTask, error) {
	var row store.MarketRecruitTaskRow
	err := s.gorm().WithContext(ctx).Where("id = ?", taskID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("market recruit task not found")
	}
	if err != nil {
		return nil, err
	}
	return marketTaskFromRow(row), nil
}

func (s *Service) GetMarketRecruitTaskByToken(ctx context.Context, token string) (*MarketRecruitTask, error) {
	var row store.MarketRecruitTaskRow
	err := s.gorm().WithContext(ctx).Where("callback_token = ?", token).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("market recruit task not found")
	}
	if err != nil {
		return nil, err
	}
	return marketTaskFromRow(row), nil
}

func (s *Service) ListMarketRecruitTasks(ctx context.Context, jobID string) ([]MarketRecruitTask, error) {
	q := s.gorm().WithContext(ctx).Order("updated_at DESC")
	if jobID != "" {
		q = q.Where("job_id = ?", jobID)
	}
	var rows []store.MarketRecruitTaskRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]MarketRecruitTask, 0, len(rows))
	for _, r := range rows {
		out = append(out, *marketTaskFromRow(r))
	}
	return out, nil
}

func (s *Service) MarkMarketRecruitTaskRunning(ctx context.Context, taskID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return s.gorm().WithContext(ctx).Model(&store.MarketRecruitTaskRow{}).Where("id = ?", taskID).
		Updates(map[string]any{"status": "running", "updated_at": now}).Error
}

func (s *Service) CompleteMarketRecruitTask(ctx context.Context, taskID string, results []map[string]any, errMsg string) (*MarketRecruitTask, error) {
	status := "completed"
	if errMsg != "" && len(results) == 0 {
		status = "failed"
	}
	if results == nil {
		results = []map[string]any{}
	}
	raw, _ := json.Marshal(results)
	now := time.Now().UTC().Format(time.RFC3339)
	err := s.gorm().WithContext(ctx).Model(&store.MarketRecruitTaskRow{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"status": status, "result_json": string(raw), "error_msg": errMsg, "updated_at": now,
		}).Error
	if err != nil {
		return nil, err
	}
	return s.GetMarketRecruitTask(ctx, taskID)
}

func marketTaskFromRow(row store.MarketRecruitTaskRow) *MarketRecruitTask {
	t := &MarketRecruitTask{
		ID: row.ID, JobID: row.JobID, AgentURL: row.AgentURL, AgentBotID: row.AgentBotID,
		Status: row.Status, CallbackToken: row.CallbackToken, ErrorMsg: row.ErrorMsg,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Results: []map[string]any{},
	}
	_ = json.Unmarshal([]byte(row.ResultJSON), &t.Results)
	return t
}
