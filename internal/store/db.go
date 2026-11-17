package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps a GORM connection for profiles, sessions and A2A logs.
type DB struct {
	gorm *gorm.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data: %w", err)
	}
	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := gdb.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := gdb.Exec("PRAGMA foreign_keys=ON").Error; err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	db := &DB{gorm: gdb}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error {
	sqlDB, err := d.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Gorm exposes the underlying *gorm.DB.
func (d *DB) Gorm() *gorm.DB {
	return d.gorm
}

func (d *DB) migrate() error {
	return d.gorm.AutoMigrate(
		&ProfileFact{},
		&ProfileRow{},
		&ChatMessage{},
		&Checkpoint{},
		&JobRequirementRow{},
		&CandidateRow{},
		&A2AMessageRow{},
		&MarketRecruitTaskRow{},
	)
}
