// internal/storage/memory.go
package storage

import (
	"sync"
	"time"
)

type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

type Job struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Status    JobStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type MemoryStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs: make(map[string]*Job),
	}
}

func (s *MemoryStore) Create(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

func (s *MemoryStore) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, exists := s.jobs[id]
	return job, exists
}

func (s *MemoryStore) UpdateStatus(id string, status JobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if job, exists := s.jobs[id]; exists {
		job.Status = status
	}
}