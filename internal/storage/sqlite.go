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

	log.Println("SQLite database successfully initialized and migrated.")
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

func (r *SQLiteRepository) List(page, limit int, status string) ([]*DBJob, int64, error) {
	var jobs []*DBJob
	var total int64

	query := r.db.Model(&DBJob{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count jobs: %w", err)
	}

	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&jobs).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to fetch jobs: %w", err)
	}

	return jobs, total, nil
}

func (r *SQLiteRepository) Delete(id string) error {
	result := r.db.Delete(&DBJob{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete job: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("job %s not found", id)
	}
	return nil
}

func (r *SQLiteRepository) DeleteAll() (int64, error) {
	result := r.db.Where("1 = 1").Delete(&DBJob{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete all jobs: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (r *SQLiteRepository) ExportAll() ([]*DBJob, error) {
	var jobs []*DBJob
	if err := r.db.Order("created_at DESC").Find(&jobs).Error; err != nil {
		return nil, fmt.Errorf("failed to export jobs: %w", err)
	}
	return jobs, nil
}

func (r *SQLiteRepository) Retry(id string) (*DBJob, error) {
	var job DBJob
	if err := r.db.First(&job, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}
	if job.Status != "failed" {
		return nil, fmt.Errorf("only failed jobs can be retried, current status: %s", job.Status)
	}
	if err := r.db.Model(&DBJob{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":       "pending",
		"result_title": "",
	}).Error; err != nil {
		return nil, fmt.Errorf("failed to reset job status: %w", err)
	}
	r.db.First(&job, "id = ?", id)
	return &job, nil
}

func (r *SQLiteRepository) ResetDB() error {
	if err := r.db.Migrator().DropTable(&DBJob{}); err != nil {
		return fmt.Errorf("failed to drop table: %w", err)
	}
	if err := r.db.AutoMigrate(&DBJob{}); err != nil {
		return fmt.Errorf("failed to remigrate: %w", err)
	}
	log.Println("Database reset — all tables dropped and recreated.")
	return nil
}

func (r *SQLiteRepository) Metrics() (map[string]interface{}, error) {
	var total, pending, processing, completed, failed int64

	if err := r.db.Model(&DBJob{}).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("failed to count total: %w", err)
	}
	r.db.Model(&DBJob{}).Where("status = ?", "pending").Count(&pending)
	r.db.Model(&DBJob{}).Where("status = ?", "processing").Count(&processing)
	r.db.Model(&DBJob{}).Where("status = ?", "completed").Count(&completed)
	r.db.Model(&DBJob{}).Where("status = ?", "failed").Count(&failed)

	return map[string]interface{}{
		"total":      total,
		"pending":    pending,
		"processing": processing,
		"completed":  completed,
		"failed":     failed,
	}, nil
}
