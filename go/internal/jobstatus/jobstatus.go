// Package jobstatus mirrors src/job_status.py: an in-memory registry that
// intentionally does not survive a restart.
package jobstatus

import (
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusFailed  Status = "failed"
)

type Job struct {
	CriadoEm     string  `json:"criado_em"`
	DataFim      string  `json:"data_fim"`
	DataInicio   string  `json:"data_inicio"`
	Estado       string  `json:"estado"`
	IDRequisicao string  `json:"id_requisicao"`
	Mensagem     *string `json:"mensagem"`
	Status       Status  `json:"status"`
	Tipo         string  `json:"tipo"`
}

type Registry struct {
	mu   sync.Mutex
	jobs map[string]Job
}

func NewRegistry() *Registry {
	return &Registry{jobs: make(map[string]Job)}
}

func nowIsoSeconds() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05-07:00")
}

func (r *Registry) Register(requestID, reportType string, states []string, startDate, endDate string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[requestID] = Job{
		CriadoEm:     nowIsoSeconds(),
		DataFim:      endDate,
		DataInicio:   startDate,
		Estado:       strings.Join(states, " · "),
		IDRequisicao: requestID,
		Mensagem:     nil,
		Status:       StatusQueued,
		Tipo:         reportType,
	}
}

func (r *Registry) MarkRunning(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[requestID]; ok {
		job.Status = StatusRunning
		r.jobs[requestID] = job
	}
}

func (r *Registry) MarkFailed(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[requestID]; ok {
		message := "Não foi possível gerar este relatório."
		job.Status = StatusFailed
		job.Mensagem = &message
		r.jobs[requestID] = job
	}
}

func (r *Registry) MarkSucceeded(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.jobs, requestID)
}

func (r *Registry) List() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		out = append(out, job)
	}
	return out
}
