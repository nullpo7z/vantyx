package filetransfer

import (
	"context"
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

// DefaultMaxHistoryPerUser is the per-user retention cap for terminal-state jobs.
const DefaultMaxHistoryPerUser = 500

// progressPersistInterval throttles how often progress writes hit the database.
// The notifier (used for SSE) is still called on every change.
const progressPersistInterval = 200 * time.Millisecond

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
	Progress   int64
	Total      int64
	Error      string
	TempPath   string
	CreatedAt  time.Time
	UpdatedAt  time.Time

	mu              sync.Mutex
	mgr             *Manager
	lastPersistedAt time.Time
}

// liveHandle holds the runtime resources for a job currently executing in this process.
type liveHandle struct {
	cancel func()
}

// Manager owns persistence (via Store) and the in-memory handles for running jobs.
type Manager struct {
	mu         sync.RWMutex
	store      *Store
	live       map[string]*liveHandle
	tempDir    string
	now        func() time.Time
	notify     func(JobSnapshot, string)
	maxHistory int
}

// NewManager creates a Manager that persists jobs via store. tempDir must exist and be writable.
// store may be nil for tests that only need in-memory behavior, but production callers should
// always pass a real Store.
func NewManager(tempDir string, store *Store) *Manager {
	return &Manager{
		store:      store,
		live:       make(map[string]*liveHandle),
		tempDir:    tempDir,
		now:        time.Now,
		maxHistory: DefaultMaxHistoryPerUser,
	}
}

// SetNotifier registers a callback invoked when a job state or progress changes.
// The callback receives a snapshot and the owning userID. The callback should be
// non-blocking; perform any I/O in a goroutine.
func (m *Manager) SetNotifier(fn func(JobSnapshot, string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notify = fn
}

// SetMaxHistoryPerUser overrides the retention cap (terminal-state rows per user).
func (m *Manager) SetMaxHistoryPerUser(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	m.maxHistory = n
	m.mu.Unlock()
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

// Create registers a new job. cancel is invoked when the job is cancelled.
func (m *Manager) Create(opts CreateOpts, cancel func()) (*Job, error) {
	id, err := newJobID()
	if err != nil {
		return nil, err
	}
	now := m.now()
	state := opts.InitialState
	if state == "" {
		state = StateRunning
	}
	j := &Job{
		ID:         id,
		UserID:     opts.UserID,
		TargetID:   opts.TargetID,
		TargetName: opts.TargetName,
		Backend:    opts.Backend,
		Direction:  opts.Direction,
		RemotePath: opts.RemotePath,
		FileName:   opts.FileName,
		State:      state,
		Total:      opts.Total,
		CreatedAt:  now,
		UpdatedAt:  now,
		mgr:        m,
	}
	if m.store != nil {
		if err := m.store.Insert(context.Background(), j.toRecord()); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	m.live[id] = &liveHandle{cancel: cancel}
	m.mu.Unlock()
	m.emit(j)
	return j, nil
}

// Get returns a job by ID. The returned *Job is a fresh copy from the database.
func (m *Manager) Get(id string) (*Job, bool) {
	if m.store == nil {
		return nil, false
	}
	rec, err := m.store.Get(context.Background(), id)
	if err != nil {
		return nil, false
	}
	return m.recordToJob(rec), true
}

// ListByUser returns jobs for userID, newest first (by updated_at).
func (m *Manager) ListByUser(userID string) []*Job {
	if m.store == nil {
		return nil
	}
	m.mu.RLock()
	limit := m.maxHistory
	m.mu.RUnlock()
	recs, err := m.store.ListByUser(context.Background(), userID, limit)
	if err != nil {
		return nil
	}
	out := make([]*Job, 0, len(recs))
	for _, rec := range recs {
		out = append(out, m.recordToJob(rec))
	}
	return out
}

// ListByUserFiltered returns jobs matching filter, newest first (by updated_at).
// The caller is responsible for setting filter.UserID and (optionally) filter.Limit.
func (m *Manager) ListByUserFiltered(ctx context.Context, filter ListFilter) ([]*Job, error) {
	if m.store == nil {
		return nil, nil
	}
	if filter.Limit <= 0 {
		m.mu.RLock()
		filter.Limit = m.maxHistory
		m.mu.RUnlock()
	}
	recs, err := m.store.ListByUserFiltered(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]*Job, 0, len(recs))
	for _, rec := range recs {
		out = append(out, m.recordToJob(rec))
	}
	return out, nil
}

// Remove deletes a job from the manager and DB, dropping any live handle too.
// Caller is responsible for removing staging files on disk.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	delete(m.live, id)
	m.mu.Unlock()
	if m.store == nil {
		return
	}
	rec, err := m.store.Get(context.Background(), id)
	if err != nil {
		return
	}
	_, _, _ = m.store.Delete(context.Background(), id, rec.UserID)
}

// Cancel requests cancellation of a running job. It invokes the registered cancel
// function (if any) and transitions the job to cancelled state.
func (m *Manager) Cancel(id, userID string) error {
	if m.store == nil {
		return ErrNotFound
	}
	rec, err := m.store.Get(context.Background(), id)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if rec.UserID != userID {
		return ErrForbidden
	}
	switch rec.State {
	case StateCompleted, StateFailed, StateCancelled:
		return nil
	}
	m.mu.Lock()
	lh := m.live[id]
	m.mu.Unlock()
	if lh != nil && lh.cancel != nil {
		lh.cancel()
	}
	now := m.now()
	if err := m.store.UpdateState(context.Background(), id, StateCancelled, "", now); err != nil {
		return err
	}
	j := m.recordToJob(rec)
	j.State = StateCancelled
	j.UpdatedAt = now
	m.emit(j)
	m.releaseLive(id)
	m.trimHistory(userID)
	return nil
}

// SetState updates job state and optional error message and persists the change.
func (j *Job) SetState(state State, errMsg string) {
	if j == nil || j.mgr == nil {
		return
	}
	now := j.mgr.now()
	j.mu.Lock()
	j.State = state
	j.Error = errMsg
	j.UpdatedAt = now
	j.mu.Unlock()
	if j.mgr.store != nil {
		_ = j.mgr.store.UpdateState(context.Background(), j.ID, state, errMsg, now)
	}
	j.mgr.emit(j)
	if state == StateCompleted || state == StateFailed || state == StateCancelled {
		j.mgr.releaseLive(j.ID)
		j.mgr.trimHistory(j.UserID)
	}
}

// SetTempPath records the staging file path and persists it.
func (j *Job) SetTempPath(path string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	j.TempPath = path
	j.mu.Unlock()
	if j.mgr != nil && j.mgr.store != nil {
		_ = j.mgr.store.UpdateTempPath(context.Background(), j.ID, path)
	}
}

// SetProgress updates progress bytes (and optionally total). DB writes are
// throttled to avoid hammering SQLite; the notifier is always called.
func (j *Job) SetProgress(done, total int64) {
	if j == nil || j.mgr == nil {
		return
	}
	now := j.mgr.now()
	j.mu.Lock()
	j.Progress = done
	if total > 0 {
		j.Total = total
	}
	j.UpdatedAt = now
	persist := now.Sub(j.lastPersistedAt) >= progressPersistInterval
	if persist {
		j.lastPersistedAt = now
	}
	totalCopy := j.Total
	j.mu.Unlock()
	if persist && j.mgr.store != nil {
		_ = j.mgr.store.UpdateProgress(context.Background(), j.ID, done, totalCopy, now)
	}
	j.mgr.emit(j)
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
		UserID:     j.UserID,
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
	UserID     string `json:"user_id,omitempty"`
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

// ReapOrphans transitions any receiving/running jobs in the database to failed.
// Call once on startup to clean up jobs that were interrupted by a previous process exit.
func (m *Manager) ReapOrphans(ctx context.Context, message string) (int64, error) {
	if m.store == nil {
		return 0, nil
	}
	return m.store.MarkOrphansFailed(ctx, m.now(), message)
}

// emit invokes the notifier with the current snapshot. Safe to call after unlock.
func (m *Manager) emit(j *Job) {
	m.mu.RLock()
	fn := m.notify
	m.mu.RUnlock()
	if fn == nil {
		return
	}
	fn(j.Snapshot(), j.UserID)
}

// releaseLive removes the runtime handle for a job that has reached terminal state.
// The DB record (and its history) is preserved.
func (m *Manager) releaseLive(id string) {
	m.mu.Lock()
	delete(m.live, id)
	m.mu.Unlock()
}

func (m *Manager) trimHistory(userID string) {
	if m.store == nil {
		return
	}
	m.mu.RLock()
	limit := m.maxHistory
	m.mu.RUnlock()
	_, _ = m.store.TrimUserHistory(context.Background(), userID, limit)
}

func (j *Job) toRecord() JobRecord {
	return JobRecord{
		ID:         j.ID,
		UserID:     j.UserID,
		TargetID:   j.TargetID,
		TargetName: j.TargetName,
		Backend:    j.Backend,
		Direction:  j.Direction,
		RemotePath: j.RemotePath,
		FileName:   j.FileName,
		State:      j.State,
		Progress:   j.Progress,
		Total:      j.Total,
		Error:      j.Error,
		TempPath:   j.TempPath,
		CreatedAt:  j.CreatedAt,
		UpdatedAt:  j.UpdatedAt,
	}
}

func (m *Manager) recordToJob(rec JobRecord) *Job {
	return &Job{
		ID:         rec.ID,
		UserID:     rec.UserID,
		TargetID:   rec.TargetID,
		TargetName: rec.TargetName,
		Backend:    rec.Backend,
		Direction:  rec.Direction,
		RemotePath: rec.RemotePath,
		FileName:   rec.FileName,
		State:      rec.State,
		Progress:   rec.Progress,
		Total:      rec.Total,
		Error:      rec.Error,
		TempPath:   rec.TempPath,
		CreatedAt:  rec.CreatedAt,
		UpdatedAt:  rec.UpdatedAt,
		mgr:        m,
	}
}

func newJobID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
