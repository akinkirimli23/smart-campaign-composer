// Package metrics is a minimal counter registry with Prometheus text output.
// The project needs a handful of counters; that does not justify a client
// library dependency.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// Registry holds monotonically increasing counters, keyed by name and labels.
// The zero value is not usable; a nil *Registry ignores every call, so
// instrumented code does not have to check whether metrics are wired.
type Registry struct {
	mu       sync.Mutex
	counters map[string]map[string]int64 // name -> rendered labels -> value
}

func New() *Registry {
	return &Registry{counters: map[string]map[string]int64{}}
}

// Inc adds one to a counter. labels are name/value pairs: Inc("x", "channel", "sms").
func (r *Registry) Inc(name string, labels ...string) {
	if r == nil {
		return
	}
	key := Labels(labels...)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counters[name] == nil {
		r.counters[name] = map[string]int64{}
	}
	r.counters[name][key]++
}

// Value returns a counter's current value (0 if it was never incremented).
func (r *Registry) Value(name string, labels ...string) int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counters[name][Labels(labels...)]
}

// WriteCounters writes every counter in the text exposition format, sorted.
func (r *Registry) WriteCounters(w io.Writer) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.counters))
	for name := range r.counters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "# TYPE %s counter\n", name)
		series := r.counters[name]
		keys := make([]string, 0, len(series))
		for k := range series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			WriteSample(w, name, k, series[k])
		}
	}
}

// Labels renders name/value pairs as `a="b",c="d"`.
func Labels(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%q", pairs[i], pairs[i+1])
	}
	return b.String()
}

// WriteSample writes one sample line.
func WriteSample(w io.Writer, name, labels string, value int64) {
	if labels == "" {
		fmt.Fprintf(w, "%s %d\n", name, value)
		return
	}
	fmt.Fprintf(w, "%s{%s} %d\n", name, labels, value)
}
