package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CheckPointStore implements compose.CheckPointStore on GORM/SQLite.
type CheckPointStore struct {
	db *DB
}

func NewCheckPointStore(db *DB) *CheckPointStore {
	return &CheckPointStore{db: db}
}

func (c *CheckPointStore) Set(ctx context.Context, key string, value []byte) error {
	row := Checkpoint{
		CheckpointID: key,
		Payload:      value,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	return c.db.gorm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "checkpoint_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"payload", "updated_at"}),
	}).Create(&row).Error
}

func (c *CheckPointStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var row Checkpoint
	err := c.db.gorm.WithContext(ctx).Where("checkpoint_id = ?", key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return row.Payload, true, nil
}

func (c *CheckPointStore) Delete(ctx context.Context, key string) error {
	return c.db.gorm.WithContext(ctx).Where("checkpoint_id = ?", key).Delete(&Checkpoint{}).Error
}
