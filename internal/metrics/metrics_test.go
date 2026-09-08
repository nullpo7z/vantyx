package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestRegistry_TextFormat(t *testing.T) {
	r := New()
	r.Inc("vantyx_test_total", "A counter.", Labels{{"kind", "a"}}, 1)
	r.Inc("vantyx_test_total", "A counter.", Labels{{"kind", "a"}}, 2)
	r.Inc("vantyx_test_total", "A counter.", Labels{{"kind", `b"q`}}, 1)
	r.Observe("vantyx_test_seconds", "A summary.", Labels{{"m", "GET"}}, 1500*time.Millisecond)
	r.Observe("vantyx_test_seconds", "A summary.", Labels{{"m", "GET"}}, 500*time.Millisecond)
	r.Gauge("vantyx_test_gauge", "A gauge.", func() []Sample { return []Sample{{Value: 7}, {Labels: Labels{{"x", "y"}}, Value: 0.5}} })
	var sb strings.Builder
	r.Write(&sb)
	out := sb.String()
	for _, want := range []string{
		"# TYPE vantyx_test_total counter",
		`vantyx_test_total{kind="a"} 3`,
		`vantyx_test_total{kind="b\"q"} 1`,
		"# TYPE vantyx_test_seconds summary",
		`vantyx_test_seconds_sum{m="GET"} 2`,
		`vantyx_test_seconds_count{m="GET"} 2`,
		"# TYPE vantyx_test_gauge gauge",
		"vantyx_test_gauge 7",
		`vantyx_test_gauge{x="y"} 0.5`,
		"process_uptime_seconds",
		"go_goroutines",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
