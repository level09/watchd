package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/level09/watchd/internal/service"
	"github.com/level09/watchd/internal/store"
)

var errAgentRunning = errors.New("agent already running")

type runJob struct {
	ID     string     `json:"job_id"`
	Agent  string     `json:"agent"`
	Status string     `json:"status"`
	RunID  string     `json:"run_id,omitempty"`
	Error  string     `json:"error,omitempty"`
	Run    *store.Run `json:"run,omitempty"`
}

type jobManager struct {
	mu      sync.Mutex
	jobs    map[string]*runJob
	running map[string]string
	service *service.Service
	store   *store.Store
}

func newJobManager(root string) *jobManager {
	return &jobManager{
		jobs:    make(map[string]*runJob),
		running: make(map[string]string),
		service: service.New(root),
		store:   store.New(root),
	}
}

func (m *jobManager) Start(agentName string) (*runJob, error) {
	if _, err := m.service.LoadAgent(agentName); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.running[agentName]; existing != "" {
		return nil, fmt.Errorf("%w: %q", errAgentRunning, agentName)
	}
	id, err := newJobID()
	if err != nil {
		return nil, err
	}
	job := &runJob{ID: id, Agent: agentName, Status: "queued"}
	m.jobs[id] = job
	m.running[agentName] = id
	go m.execute(job)
	return cloneJob(job), nil
}

func (m *jobManager) execute(job *runJob) {
	m.mu.Lock()
	job.Status = "running"
	m.mu.Unlock()
	run, err := m.service.RunAgent(job.Agent)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.running, job.Agent)
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		return
	}
	job.Status = "complete"
	job.RunID = run.ID
	job.Run = run
}

func (m *jobManager) Get(id string) (*runJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	copy := cloneJob(job)
	if copy.RunID != "" {
		if run, err := m.store.GetRun(copy.RunID); err == nil {
			copy.Run = run
		}
	}
	return copy, true
}

func cloneJob(job *runJob) *runJob {
	copy := *job
	return &copy
}

func newJobID() (string, error) {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("create run job id: %w", err)
	}
	return "job_" + hex.EncodeToString(data), nil
}
