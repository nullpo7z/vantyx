// Package metrics is a dependency-free Prometheus exposition: a handful
// of counters, a duration summary and scrape-time gauges rendered in the
// text format (version 0.0.4). It is deliberately small -- Vantyx does
// not need the full client library to answer "how busy is the gateway".
package metrics

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Labels is an ordered label set (name=value pairs) of a series.
type Labels [][2]string

func (l Labels) key() string {
	parts := make([]string, 0, len(l))
	for _, kv := range l {
		parts = append(parts, kv[0]+"="+kv[1])
	}
	return strings.Join(parts, ",")
}

func (l Labels) render() string {
	if len(l) == 0 {
		return ""
	}
	parts := make([]string, 0, len(l))
	for _, kv := range l {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, kv[0], escapeLabel(kv[1])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabel(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(v)
}

type series struct {
	labels Labels
	value  float64
}

type metric struct {
	name   string
	help   string
	kind   string // counter | gauge | summary
	series map[string]*series
}

// Registry holds counters and gauge callbacks.
type Registry struct {
	mu       sync.Mutex
	counters map[string]*metric
	gauges   []gaugeFunc
	started  time.Time
	// durations: name -> labelsKey -> (sum, count)
	durations map[string]*metric
}

type gaugeFunc struct {
	name, help string
	fn         func() []series
}

// New creates an empty registry.
func New() *Registry {
	return &Registry{
		counters:  map[string]*metric{},
		durations: map[string]*metric{},
		started:   time.Now(),
	}
}

// Default is the process-wide registry.
var Default = New()

// Inc adds delta to a counter series.
func (r *Registry) Inc(name, help string, labels Labels, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.counters[name]
	if !ok {
		m = &metric{name: name, help: help, kind: "counter", series: map[string]*series{}}
		r.counters[name] = m
	}
	k := labels.key()
	s, ok := m.series[k]
	if !ok {
		s = &series{labels: labels}
		m.series[k] = s
	}
	s.value += delta
}

// Observe records a duration into a summary-style pair (_sum / _count).
func (r *Registry) Observe(name, help string, labels Labels, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.durations[name]
	if !ok {
		m = &metric{name: name, help: help, kind: "summary", series: map[string]*series{}}
		r.durations[name] = m
	}
	k := labels.key()
	sum, ok := m.series[k+"|sum"]
	if !ok {
		sum = &series{labels: labels}
		m.series[k+"|sum"] = sum
		m.series[k+"|count"] = &series{labels: labels}
	}
	sum.value += d.Seconds()
	m.series[k+"|count"].value++
}

// Gauge registers a callback evaluated at scrape time. Each returned
// series becomes one sample of the named gauge.
func (r *Registry) Gauge(name, help string, fn func() []Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, gaugeFunc{name: name, help: help, fn: func() []series {
		out := fn()
		s := make([]series, 0, len(out))
		for _, o := range out {
			s = append(s, series{labels: o.Labels, value: o.Value})
		}
		return s
	}})
}

// Sample is one gauge value with its labels.
type Sample struct {
	Labels Labels
	Value  float64
}

// Write renders the registry in Prometheus text format.
func (r *Registry) Write(w io.Writer) {
	r.mu.Lock()
	counters := make([]*metric, 0, len(r.counters))
	for _, m := range r.counters {
		counters = append(counters, m)
	}
	durations := make([]*metric, 0, len(r.durations))
	for _, m := range r.durations {
		durations = append(durations, m)
	}
	gauges := append([]gaugeFunc(nil), r.gauges...)
	started := r.started
	r.mu.Unlock()

	sort.Slice(counters, func(i, j int) bool { return counters[i].name < counters[j].name })
	sort.Slice(durations, func(i, j int) bool { return durations[i].name < durations[j].name })

	for _, m := range counters {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", m.name, m.help, m.name)
		for _, s := range sortedSeries(m) {
			fmt.Fprintf(w, "%s%s %s\n", m.name, s.labels.render(), fmtFloat(s.value))
		}
	}
	for _, m := range durations {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s summary\n", m.name, m.help, m.name)
		keys := make([]string, 0, len(m.series))
		for k := range m.series {
			if strings.HasSuffix(k, "|sum") {
				keys = append(keys, strings.TrimSuffix(k, "|sum"))
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			sum := m.series[k+"|sum"]
			cnt := m.series[k+"|count"]
			fmt.Fprintf(w, "%s_sum%s %s\n", m.name, sum.labels.render(), fmtFloat(sum.value))
			fmt.Fprintf(w, "%s_count%s %s\n", m.name, cnt.labels.render(), fmtFloat(cnt.value))
		}
	}
	for _, g := range gauges {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
		for _, s := range g.fn() {
			fmt.Fprintf(w, "%s%s %s\n", g.name, s.labels.render(), fmtFloat(s.value))
		}
	}
	fmt.Fprintf(w, "# HELP process_uptime_seconds Seconds since the process started.\n# TYPE process_uptime_seconds gauge\nprocess_uptime_seconds %s\n", fmtFloat(time.Since(started).Seconds()))
	fmt.Fprintf(w, "# HELP go_goroutines Number of goroutines.\n# TYPE go_goroutines gauge\ngo_goroutines %d\n", runtime.NumGoroutine())
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(w, "# HELP go_memstats_heap_alloc_bytes Heap bytes allocated and in use.\n# TYPE go_memstats_heap_alloc_bytes gauge\ngo_memstats_heap_alloc_bytes %d\n", ms.HeapAlloc)
}

// WriteGauges renders only the gauge callbacks (used for per-App
// registries that hold live-state gauges next to the global counters).
func (r *Registry) WriteGauges(w io.Writer) {
	r.mu.Lock()
	gauges := append([]gaugeFunc(nil), r.gauges...)
	r.mu.Unlock()
	for _, g := range gauges {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
		for _, s := range g.fn() {
			fmt.Fprintf(w, "%s%s %s\n", g.name, s.labels.render(), fmtFloat(s.value))
		}
	}
}

func sortedSeries(m *metric) []*series {
	out := make([]*series, 0, len(m.series))
	for _, s := range m.series {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].labels.key() < out[j].labels.key() })
	return out
}

func fmtFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}
