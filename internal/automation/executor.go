package automation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ExecutorConfig configures the Executor.
type ExecutorConfig struct {
	RunnerConfig RunnerConfig
	// Middleware is applied in order (first = outermost wrapper).
	Middleware []Middleware
	// Sinks receive the completed Result after each execution.
	Sinks []Sink
	// State persists execution history. May be nil (no persistence).
	State StateStore
}

// Executor orchestrates workflow execution: it combines the Runner with
// middleware, output sinks, and state management.
type Executor struct {
	runner *Runner
	chain  ExecuteFunc
	sinks  []Sink
	state  StateStore
}

// NewExecutor builds an Executor from cfg.
// WithRecovery() is always prepended so panics never escape.
func NewExecutor(cfg ExecutorConfig) (*Executor, error) {
	runner, err := NewRunner(cfg.RunnerConfig)
	if err != nil {
		return nil, fmt.Errorf("executor: %w", err)
	}

	e := &Executor{
		runner: runner,
		sinks:  cfg.Sinks,
		state:  cfg.State,
	}

	// Build the middleware chain with recovery always outermost.
	mw := append([]Middleware{WithRecovery()}, cfg.Middleware...)
	e.chain = Chain(mw...)(e.core)

	return e, nil
}

// Run executes w with opts, runs all middleware, feeds sinks, and updates state.
func (e *Executor) Run(ctx context.Context, w Workflow, opts Options) (Result, error) {
	result, err := e.chain(ctx, w, opts)

	// Feed sinks regardless of success/failure.
	for _, s := range e.sinks {
		if sinkErr := s.Write(ctx, result); sinkErr != nil {
			// Sink errors are non-fatal; they don't override the workflow error.
			_ = sinkErr
		}
	}

	// Persist state.
	if e.state != nil {
		_ = e.updateState(ctx, result)
	}

	return result, err
}

// core is the innermost ExecuteFunc: it runs the actual workflow via the Runner.
func (e *Executor) core(ctx context.Context, w Workflow, opts Options) (Result, error) {
	result := Result{
		WorkflowName: w.Name(),
		StartedAt:    time.Now(),
		Metadata:     make(map[string]any),
	}

	if opts.DryRun {
		result.FinishedAt = time.Now()
		result.Duration = result.FinishedAt.Sub(result.StartedAt)
		result.Output = fmt.Sprintf("[dry-run] workflow %q would execute here", w.Name())
		return result, nil
	}

	ec := ExecuteConfig{ModelOverride: opts.Model}

	// If the workflow defines its own system prompt, use it.
	if sp, ok := w.(SystemPrompter); ok {
		ec.SystemPrompt = sp.SystemPrompt()
	}

	var buf strings.Builder
	ec.StreamFn = func(delta string) { buf.WriteString(delta) }

	err := e.runner.Execute(ctx, w, ec)

	result.Output = buf.String()
	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)
	result.Error = err

	return result, err
}

func (e *Executor) updateState(ctx context.Context, r Result) error {
	state, _ := e.state.Load(ctx, r.WorkflowName)
	if state == nil {
		state = &ExecutionState{WorkflowName: r.WorkflowName}
	}

	state.LastRunAt = r.StartedAt
	state.RunCount++

	if r.Success() {
		state.LastStatus = "success"
		state.LastError = ""
		state.SuccessCount++
		state.ConsecErrors = 0
	} else {
		state.LastStatus = "error"
		state.LastError = r.Error.Error()
		state.FailureCount++
		state.ConsecErrors++
	}

	return e.state.Save(ctx, *state)
}

// Close releases the underlying Runner and all its resources.
func (e *Executor) Close() {
	e.runner.Close()
}
