// internal/storage/sqlite.go
package storage

import (
	"fmt"
	"log"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type SQLiteRepository struct {
	db *gorm.DB
}

func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database file: %w", err)
	}

	
	err = db.AutoMigrate(&DBJob{})
	if err != nil {
		return nil, fmt.Errorf("failed to execute db automigration: %w", err)
	}

	log.Println(" SQLite database successfully initialized and migrated.")
	return &SQLiteRepository{db: db}, nil
}

func (r *SQLiteRepository) Create(job *DBJob) error {
	return r.db.Create(job).Error
}

func (r *SQLiteRepository) Get(id string) (*DBJob, error) {
	var job DBJob
	err := r.db.First(&job, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *SQLiteRepository) UpdateStatus(id string, status string, title string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if title != "" {
		updates["result_title"] = title
	}
	return r.db.Model(&DBJob{}).Where("id = ?", id).Updates(updates).Error
}