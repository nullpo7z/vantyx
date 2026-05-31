package recording

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultResourceLimit  = 0.8
	envResourceLimit      = "VANTYX_RECORDING_RESOURCE_LIMIT"
	memoryWaitMaxDuration = 30 * time.Second
	memoryPollInterval    = 2 * time.Second
)

// ErrResourcePressure is returned when memory use exceeds the configured limit.
var ErrResourcePressure = fmt.Errorf("recording: system memory above resource limit")

// Governor limits concurrent heavy recording work (ffmpeg/agg exports and
// live screen capture) so the server stays responsive.
type Governor struct {
	limit               float64
	maxLiveConcurrent   int
	maxExportConcurrent int
	ffmpegThreads       int
	liveSem             chan struct{}
	exportSem           chan struct{}
}

// NewGovernor builds a governor from VANTYX_RECORDING_RESOURCE_LIMIT (default 0.8).
func NewGovernor() *Governor {
	limit := envFloat(envResourceLimit, defaultResourceLimit)
	if limit <= 0 || limit > 1 {
		limit = defaultResourceLimit
	}
	nCPU := runtime.NumCPU()
	if nCPU < 1 {
		nCPU = 1
	}
	liveSlots := max(1, int(float64(nCPU)*limit))
	exportSlots := max(1, int(float64(nCPU)*limit))
	threads := max(1, int(float64(nCPU)*limit))
	return &Governor{
		limit:               limit,
		maxLiveConcurrent:   liveSlots,
		maxExportConcurrent: exportSlots,
		ffmpegThreads:       threads,
		liveSem:             make(chan struct{}, liveSlots),
		exportSem:           make(chan struct{}, exportSlots),
	}
}

// LimitFraction returns the configured fraction (e.g. 0.8).
func (g *Governor) LimitFraction() float64 {
	if g == nil {
		return defaultResourceLimit
	}
	return g.limit
}

// MaxConcurrent returns the concurrent live-capture slot count.
func (g *Governor) MaxConcurrent() int {
	if g == nil {
		return 1
	}
	return g.maxLiveConcurrent
}

// MaxExportConcurrent returns the concurrent export slot count.
func (g *Governor) MaxExportConcurrent() int {
	if g == nil {
		return 1
	}
	return g.maxExportConcurrent
}

// FFmpegThreads returns the -threads value for ffmpeg child processes.
func (g *Governor) FFmpegThreads() int {
	if g == nil {
		return 1
	}
	return g.ffmpegThreads
}

// Acquire waits until memory is below the limit and a live-capture slot is free.
func (g *Governor) Acquire(ctx context.Context) error {
	return g.acquire(ctx, g.liveSem, true)
}

// AcquireExport waits for an export worker slot. Exports use a separate pool
// from live capture so a long RDP/VNC recording does not block finished
// recording downloads. Memory pressure is not gated for exports.
func (g *Governor) AcquireExport(ctx context.Context) error {
	return g.acquire(ctx, g.exportSem, false)
}

// Release frees a live-capture slot acquired with Acquire.
func (g *Governor) Release() {
	g.release(g.liveSem)
}

// ReleaseExport frees an export slot acquired with AcquireExport.
func (g *Governor) ReleaseExport() {
	g.release(g.exportSem)
}

func (g *Governor) acquire(ctx context.Context, sem chan struct{}, checkMemory bool) error {
	if g == nil {
		return nil
	}
	memoryDeadline := time.Now().Add(memoryWaitMaxDuration)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if checkMemory && !g.memoryWithinLimit() && time.Now().Before(memoryDeadline) {
			select {
			case <-time.After(memoryPollInterval):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case sem <- struct{}{}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (g *Governor) release(sem chan struct{}) {
	if g == nil || sem == nil {
		return
	}
	select {
	case <-sem:
	default:
	}
}

func (g *Governor) memoryWithinLimit() bool {
	total, available, err := linuxMemInfo()
	if err != nil || total == 0 {
		return true
	}
	used := total - available
	return float64(used)/float64(total) < g.limit
}

func linuxMemInfo() (total, available uint64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			total, _ = parseMeminfoKB(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			available, _ = parseMeminfoKB(line)
		}
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("MemTotal not found")
	}
	return total, available, sc.Err()
}

func parseMeminfoKB(line string) (uint64, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, fmt.Errorf("invalid meminfo line")
	}
	kb, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return kb * 1024, nil
}

func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

var defaultGovernorOnce sync.Once
var defaultGovernor *Governor

// DefaultGovernor returns the process-wide recording resource governor.
func DefaultGovernor() *Governor {
	defaultGovernorOnce.Do(func() {
		defaultGovernor = NewGovernor()
	})
	return defaultGovernor
}
