// Command baseline measures product contracts through real HTTP, WebSocket and PTY I/O.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"
)

type sample struct {
	Value float64 `json:"value"`
	OK    bool    `json:"ok"`
	Error string  `json:"error,omitempty"`
	Case  string  `json:"case,omitempty"`
}
type metric struct {
	CorrectnessGate bool     `json:"correctness_gate"`
	Name            string   `json:"name"`
	Unit            string   `json:"unit"`
	Samples         []sample `json:"samples"`
	Summary         summary  `json:"summary"`
}
type summary struct {
	Attempts    int        `json:"attempts"`
	Passed      int        `json:"passed"`
	SuccessRate float64    `json:"success_rate"`
	Wilson95    [2]float64 `json:"wilson_95"`
	Mean        float64    `json:"mean"`
	P50         float64    `json:"p50"`
	P05         float64    `json:"p05"`
	P95         float64    `json:"p95"`
	Max         float64    `json:"max"`
	Min         float64    `json:"min"`
}

func summarize(samples []sample) summary {
	s := summary{Attempts: len(samples)}
	var values []float64
	for _, v := range samples {
		if v.OK {
			s.Passed++
			values = append(values, v.Value)
			s.Mean += v.Value
		}
	}
	if s.Attempts == 0 {
		return s
	}
	n := float64(s.Attempts)
	p := float64(s.Passed) / n
	s.SuccessRate = p
	z2 := 1.96 * 1.96
	center := (p + z2/(2*n)) / (1 + z2/n)
	half := 1.96 * math.Sqrt(p*(1-p)/n+z2/(4*n*n)) / (1 + z2/n)
	s.Wilson95 = [2]float64{math.Max(0, center-half), math.Min(1, center+half)}
	if len(values) > 0 {
		slices.Sort(values)
		s.Mean /= float64(len(values))
		s.P50 = values[int(math.Ceil(.50*float64(len(values))))-1]
		s.P05 = values[int(math.Ceil(.05*float64(len(values))))-1]
		s.P95 = values[int(math.Ceil(.95*float64(len(values))))-1]
		s.Max = values[len(values)-1]
		s.Min = values[0]
	}
	return s
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func checkf(ok bool, format string, args ...any) {
	if !ok {
		panic(fmt.Errorf(format, args...))
	}
}
func git(args ...string) string {
	b, err := exec.Command("git", args...).Output()
	if err != nil {
		return "unavailable"
	}
	return string(b)
}

type cleanupFailure struct {
	ID    string
	Cause error
}

func (e cleanupFailure) Error() string {
	return fmt.Sprintf("session %s cleanup failed: %v", e.ID, e.Cause)
}

type measurementAbort struct {
	Metric metric
	Cause  error
}

func (e measurementAbort) Error() string { return e.Cause.Error() }
func measure(name, unit string, n int, fn func(int) float64) metric {
	m := metric{Name: name, Unit: unit, CorrectnessGate: true}
	for i := 0; i < n; i++ {
		var fatal error
		func() {
			v := sample{}
			defer func() {
				if r := recover(); r != nil {
					v.Error = fmt.Sprint(r)
					if failure, ok := r.(cleanupFailure); ok {
						fatal = failure
					}
				}
				m.Samples = append(m.Samples, v)
			}()
			v.Value = fn(i)
			v.OK = true
		}()
		if fatal != nil {
			m.Summary = summarize(m.Samples)
			panic(measurementAbort{Metric: m, Cause: fatal})
		}
	}
	m.Summary = summarize(m.Samples)
	fmt.Fprintf(os.Stderr, "%s: %d/%d, p50=%.3f p95=%.3f %s\n", name, m.Summary.Passed, m.Summary.Attempts, m.Summary.P50, m.Summary.P95, unit)
	return m
}
func milliseconds(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

func run(n int, metrics *[]metric, f *fixture) {
	*metrics = append(*metrics, f.interaction(n, 0)...)
	*metrics = append(*metrics, f.reconnect(n))
	*metrics = append(*metrics, f.continuity(n))
	*metrics = append(*metrics, f.replay(n))
	*metrics = append(*metrics, f.streaming(n))
	*metrics = append(*metrics, f.interaction(n, 8)...)
	*metrics = append(*metrics, f.access())
	*metrics = append(*metrics, f.worktrees(n))
	*metrics = append(*metrics, f.status())
}
func main() {
	n := flag.Int("samples", 30, "measured repetitions per performance scenario (minimum 2)")
	out := flag.String("output", "baseline/results/latest.json", "JSON evidence path")
	flag.Parse()
	if *n < 2 {
		fmt.Fprintln(os.Stderr, "samples must be >= 2")
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	started := time.Now().UTC()
	report := map[string]any{"schema_version": 1, "started_at": started, "parameters": map[string]any{"samples": *n, "warmup_echoes": 3, "scrollback_bytes": capacity, "loaded_sessions": 8, "isolated_git_environment": true, "transport": "loopback HTTP + WebSocket, real bash PTY; no browser or network shaping"}}
	failed := false
	var metrics []metric
	func() {
		defer func() {
			if r := recover(); r != nil {
				if abort, ok := r.(measurementAbort); ok {
					metrics = append(metrics, abort.Metric)
				}
				report["fatal_error"] = fmt.Sprint(r)
				failed = true
			}
		}()
		f := newFixture()
		defer f.close()
		report["environment"] = map[string]any{"revision": git("rev-parse", "HEAD"), "dirty": git("status", "--porcelain") != "", "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0), "hostname": hostname(), "uname": command("uname", "-a"), "cpu": cpu(), "git": command("git", "--version")}
		run(*n, &metrics, f)
	}()
	report["metrics"] = metrics
	for _, m := range metrics {
		if m.CorrectnessGate && m.Summary.Passed != m.Summary.Attempts {
			failed = true
		}
	}
	report["duration_seconds"] = time.Since(started).Seconds()
	report["correctness_passed"] = !failed
	data, err := json.MarshalIndent(report, "", "  ")
	must(err)
	must(os.MkdirAll(filepath.Dir(*out), 0755))
	must(os.WriteFile(*out, append(data, '\n'), 0644))
	fmt.Fprintln(os.Stderr, "Evidence:", *out)
	if failed {
		os.Exit(1)
	}
}
func hostname() string { h, _ := os.Hostname(); return h }
func command(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "unavailable"
	}
	return string(b)
}
func cpu() string {
	if runtime.GOOS == "darwin" {
		return command("sysctl", "-n", "machdep.cpu.brand_string")
	}
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unavailable"
	}
	return string(b)
}
func timeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}
