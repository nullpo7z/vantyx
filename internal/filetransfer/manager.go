package filetransfer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Backend identifies where files are stored or transferred.
type Backend string

const (
	BackendRemote     Backend = "remote"      // SFTP/FTP/TFTP client to target
	BackendTFTPServer Backend = "tftp_server" // Vantyx embedded TFTP directory
)

// Direction is upload or download relative to the user's machine.
type Direction string

const (
	DirectionUpload   Direction = "upload"
	DirectionDownload Direction = "download"
)

// State is the lifecycle of a transfer job.
type State string

const (
	StateReceiving State = "receiving" // browser → Vantyx staging (upload only)
	StateRunning   State = "running"   // Vantyx ↔ remote
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Job is a background file transfer tracked server-side.
type Job struct {
	ID         string
	UserID     string
	TargetID   string
	TargetName string
	Backend    Backend
	Direction  Direction
	RemotePath string
	FileName   string
	State      State
	Progress   int64 // bytes transferred in running phase (or received in receiving)
	Total      int64 // 0 if unknown
	Error      string
	TempPath   string // staging file path; set when staging is ready
	CreatedAt  time.Time
	UpdatedAt  time.Time

	cancel func()
	mu     sync.Mutex
	notify func(JobSnapshot, string)
}

// Manager tracks in-memory transfer jobs per process.
type Manager struct {
	mu      sync.RWMutex
	jobs    map[string]*Job
	now     func() time.Time
	tempDir string
	notify  func(JobSnapshot, string)
}

// SetNotifier registers a callback invoked when a job state or progress changes.
// The callback receives a snapshot and the owning userID. The callback should be
// non-blocking; perform any I/O in a goroutine.
func (m *Manager) SetNotifier(fn func(JobSnapshot, string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notify = fn
}

// NewManager creates a Manager. tempDir must exist and be writable.
func NewManager(tempDir string) *Manager {
	return &Manager{
		jobs:    make(map[string]*Job),
		now:     time.Now,
		tempDir: tempDir,
	}
}

// TempDir returns the directory used for staging files.
func (m *Manager) TempDir() string {
	return m.tempDir
}

var (
	ErrNotFound  = errors.New("file transfer job not found")
	ErrForbidden = errors.New("file transfer job forbidden")
	ErrNotReady  = errors.New("file transfer not ready")
	ErrJobExists = errors.New("file transfer job already exists")
)

// Create registers a new job. cancel is invoked when the job is cancelled.
func (m *Manager) Create(opts CreateOpts, cancel func()) (*Job, error) {
	id, err := newJobID()
	if err != nil {
		return nil, err
	}
	now := m.now()
	m.mu.Lock()
	notify := m.notify
	m.mu.Unlock()
	j := &Job{
		ID:         id,
		UserID:     opts.UserID,
		TargetID:   opts.TargetID,
		TargetName: opts.TargetName,
		Backend:    opts.Backend,
		Direction:  opts.Direction,
		RemotePath: opts.RemotePath,
		FileName:   opts.FileName,
		State:      opts.InitialState,
		Total:      opts.Total,
		CreatedAt:  now,
		UpdatedAt:  now,
		cancel:     cancel,
		notify:     notify,
	}
	if j.State == "" {
		j.State = StateRunning
	}
	m.mu.Lock()
	m.jobs[id] = j
	m.mu.Unlock()
	j.emit()
	return j, nil
}

// CreateOpts holds metadata for a new job.
type CreateOpts struct {
	UserID       string
	TargetID     string
	TargetName   string
	Backend      Backend
	Direction    Direction
	RemotePath   string
	FileName     string
	InitialState State
	Total        int64
}

// Get returns a job by ID.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	return j, ok
}

// ListByUser returns jobs for userID, newest first.
func (m *Manager) ListByUser(userID string) []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Job
	for _, j := range m.jobs {
		if j.UserID == userID {
			out = append(out, j)
		}
	}
	// sort by CreatedAt desc
	for i := 0; i < len(out); i++ {
		for k := i + 1; k < len(out); k++ {
			if out[k].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[k] = out[k], out[i]
			}
		}
	}
	return out
}

// Remove deletes a job from the manager (caller removes temp file).
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	delete(m.jobs, id)
	m.mu.Unlock()
}

// Cancel requests cancellation of a running job.
func (m *Manager) Cancel(id, userID string) error {
	j, ok := m.Get(id)
	if !ok {
		return ErrNotFound
	}
	if j.UserID != userID {
		return ErrForbidden
	}
	j.mu.Lock()
	switch j.State {
	case StateCompleted, StateFailed, StateCancelled:
		j.mu.Unlock()
		return nil
	}
	if j.cancel != nil {
		j.cancel()
	}
	j.State = StateCancelled
	j.UpdatedAt = m.now()
	j.mu.Unlock()
	j.emit()
	return nil
}

// SetState updates job state and optional error message.
func (j *Job) SetState(state State, errMsg string) {
	j.mu.Lock()
	j.State = state
	j.Error = errMsg
	j.UpdatedAt = time.Now()
	j.mu.Unlock()
	j.emit()
}

// SetTempPath records the staging file path.
func (j *Job) SetTempPath(path string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.TempPath = path
}

// SetProgress updates progress bytes (and optionally total).
func (j *Job) SetProgress(done, total int64) {
	j.mu.Lock()
	j.Progress = done
	if total > 0 {
		j.Total = total
	}
	j.UpdatedAt = time.Now()
	j.mu.Unlock()
	j.emit()
}

// emit invokes the notifier with the current snapshot. Safe to call after unlock.
func (j *Job) emit() {
	if j.notify == nil {
		return
	}
	snap := j.Snapshot()
	j.notify(snap, j.UserID)
}

// GetTempPath returns the staging file path.
func (j *Job) GetTempPath() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.TempPath
}

// GetFileName returns the display file name.
func (j *Job) GetFileName() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.FileName
}

// Snapshot returns a copy of job fields safe for JSON.
func (j *Job) Snapshot() JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobSnapshot{
		ID:         j.ID,
		TargetID:   j.TargetID,
		TargetName: j.TargetName,
		Backend:    string(j.Backend),
		Direction:  string(j.Direction),
		RemotePath: j.RemotePath,
		FileName:   j.FileName,
		State:      string(j.State),
		Progress:   j.Progress,
		Total:      j.Total,
		Error:      j.Error,
		CreatedAt:  j.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  j.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// JobSnapshot is the JSON representation of a job.
type JobSnapshot struct {
	ID         string `json:"id"`
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name,omitempty"`
	Backend    string `json:"backend"`
	Direction  string `json:"direction"`
	RemotePath string `json:"remote_path"`
	FileName   string `json:"file_name"`
	State      string `json:"state"`
	Progress   int64  `json:"progress"`
	Total      int64  `json:"total"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func newJobID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
