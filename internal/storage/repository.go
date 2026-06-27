package storage

import (
	"encoding/json"
	"time"
)

type DBJob struct {
	ID          string    `gorm:"primaryKey;type:varchar(50)" json:"id"`
	URL         string    `gorm:"type:text;not null" json:"url"`
	Status      string    `gorm:"type:varchar(20);default:'pending'" json:"status"`
	ResultTitle string    `gorm:"type:text" json:"result_title,omitempty"`
	Strategy    string    `gorm:"type:varchar(50);default:'title'" json:"strategy"`
	Selectors   string    `gorm:"type:text" json:"selectors,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type JobRepository interface {
	Create(job *DBJob) error
	Get(id string) (*DBJob, error)
	UpdateStatus(id string, status string, title string) error
	List(page, limit int, status string) ([]*DBJob, int64, error)
	Delete(id string) error
	DeleteAll() (int64, error)
	ExportAll() ([]*DBJob, error)
	Retry(id string) (*DBJob, error)
	ResetDB() error
	Metrics() (map[string]interface{}, error)
}

func (j *DBJob) ParseSelectors() map[string]string {
	if j.Selectors == "" {
		return nil
	}
	var selectors map[string]string
	if err := json.Unmarshal([]byte(j.Selectors), &selectors); err != nil {
		return nil
	}
	return selectors
}

func EncodeSelectors(selectors map[string]string) string {
	if len(selectors) == 0 {
		return ""
	}
	b, err := json.Marshal(selectors)
	if err != nil {
		return ""
	}
	return string(b)
}
