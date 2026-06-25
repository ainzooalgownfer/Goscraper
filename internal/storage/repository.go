// internal/storage/repository.go
package storage

import (
	"time"
)


type DBJob struct {
	ID          string    `gorm:"primaryKey;type:varchar(50)" json:"id"`
	URL         string    `gorm:"type:text;not null" json:"url"`
	Status      string    `gorm:"type:varchar(20);default:'pending'" json:"status"`
	ResultTitle string    `gorm:"type:text" json:"result_title,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}


type JobRepository interface {
	Create(job *DBJob) error
	Get(id string) (*DBJob, error)
	UpdateStatus(id string, status string, title string) error
}