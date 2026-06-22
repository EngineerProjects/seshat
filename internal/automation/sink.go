package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Sink receives a completed Result after a workflow finishes.
// Sinks are called sequentially by the Executor; use MultiSink for fan-out.
type Sink interface {
	Write(ctx context.Context, r Result) error
}

// ─── StdoutSink ───────────────────────────────────────────────────────────────

// StdoutSink prints a human-readable summary of the result to stdout.
// The workflow's streaming output is already printed in real time via StreamFn;
// StdoutSink adds a completion footer (duration, status).
type StdoutSink struct {
	// Quiet suppresses the footer when true.
	Quiet bool
}

func (s *StdoutSink) Write(_ context.Context, r Result) error {
	if s.Quiet {
		return nil
	}
	status := "ok"
	if !r.Success() {
		status = "error: " + r.Error.Error()
	}
	fmt.Printf("\n[seshat-auto] %s finished in %s — %s\n", r.WorkflowName, r.Duration.Round(time.Millisecond), status)
	return nil
}

// ─── FileSink ─────────────────────────────────────────────────────────────────

// FileSink writes the result to a file.
// The filename is derived from the workflow name and the run timestamp.
type FileSink struct {
	// Dir is the directory where output files are written. Created if absent.
	Dir string
	// Format is "text" (default) or "json".
	Format string
}

func (s *FileSink) Write(ctx context.Context, r Result) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("file sink: mkdir %s: %w", s.Dir, err)
	}

	ts := r.StartedAt.UTC().Format("20060102-150405")
	name := fmt.Sprintf("%s-%s", sanitizeName(r.WorkflowName), ts)

	format := s.Format
	if format == "" {
		format = "text"
	}

	switch format {
	case "json":
		return s.writeJSON(r, filepath.Join(s.Dir, name+".json"))
	default:
		return s.writeText(r, filepath.Join(s.Dir, name+".md"))
	}
}

func (s *FileSink) writeText(r Result, path string) error {
	var b strings.Builder
	b.WriteString("# " + r.WorkflowName + "\n\n")
	b.WriteString(fmt.Sprintf("**Started:** %s  \n", r.StartedAt.UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("**Duration:** %s  \n", r.Duration.Round(time.Millisecond)))
	if !r.Success() {
		b.WriteString(fmt.Sprintf("**Error:** %s  \n", r.Error.Error()))
	}
	b.WriteString("\n---\n\n")
	b.WriteString(r.Output)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func (s *FileSink) writeJSON(r Result, path string) error {
	payload := map[string]any{
		"workflow":    r.WorkflowName,
		"started_at":  r.StartedAt.UTC(),
		"finished_at": r.FinishedAt.UTC(),
		"duration_ms": r.Duration.Milliseconds(),
		"success":     r.Success(),
		"output":      r.Output,
		"metadata":    r.Metadata,
	}
	if r.Error != nil {
		payload["error"] = r.Error.Error()
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("file sink: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// ─── WebhookSink ──────────────────────────────────────────────────────────────

// WebhookSink sends the result as a JSON POST to a URL.
type WebhookSink struct {
	URL     string
	Headers map[string]string
	// Timeout for the HTTP request. Defaults to 15s.
	Timeout time.Duration
	client  *http.Client
}

func NewWebhookSink(url string) *WebhookSink {
	return &WebhookSink{URL: url, Timeout: 15 * time.Second}
}

func (s *WebhookSink) Write(ctx context.Context, r Result) error {
	payload := map[string]any{
		"workflow":    r.WorkflowName,
		"started_at":  r.StartedAt.UTC(),
		"finished_at": r.FinishedAt.UTC(),
		"duration_ms": r.Duration.Milliseconds(),
		"success":     r.Success(),
		"output":      r.Output,
		"metadata":    r.Metadata,
	}
	if r.Error != nil {
		payload["error"] = r.Error.Error()
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook sink: marshal: %w", err)
	}

	timeout := s.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	if s.client == nil {
		s.client = &http.Client{Timeout: timeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("webhook sink: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.Headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook sink: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook sink: server returned %d", resp.StatusCode)
	}
	return nil
}

// ─── MultiSink ────────────────────────────────────────────────────────────────

// MultiSink fans a Result out to multiple sinks.
// All sinks are called even if one errors; errors are joined.
type MultiSink struct {
	Sinks []Sink
}

func NewMultiSink(sinks ...Sink) *MultiSink {
	return &MultiSink{Sinks: sinks}
}

func (m *MultiSink) Write(ctx context.Context, r Result) error {
	var errs []string
	for _, s := range m.Sinks {
		if err := s.Write(ctx, r); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("multi sink: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ─── DiscardSink ──────────────────────────────────────────────────────────────

// DiscardSink silently drops every result. Useful in tests.
type DiscardSink struct{}

func (DiscardSink) Write(_ context.Context, _ Result) error { return nil }

// ─── helpers ──────────────────────────────────────────────────────────────────

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '/' || r == '\\' {
			b.WriteRune('-')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
