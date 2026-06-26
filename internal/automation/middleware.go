package automation

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ExecuteFunc is the core execution signature threaded through the middleware chain.
type ExecuteFunc func(ctx context.Context, w Workflow, opts Options) (Result, error)

// Middleware wraps an ExecuteFunc to add cross-cutting behaviour.
// Middlewares are applied innermost-first: Chain(A, B, C) produces A(B(C(core))).
type Middleware func(next ExecuteFunc) ExecuteFunc

// Chain composes a slice of middlewares into a single middleware.
// The first middleware is the outermost (runs first on entry, last on exit).
func Chain(middlewares ...Middleware) Middleware {
	return func(next ExecuteFunc) ExecuteFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// ─── WithRetry ────────────────────────────────────────────────────────────────

// WithRetry retries a failed workflow up to maxAttempts additional times,
// waiting backoff (doubled each attempt) between attempts.
// Overrides opts.MaxRetries and opts.RetryBackoff when both are non-zero.
func WithRetry(maxAttempts int, backoff time.Duration) Middleware {
	return func(next ExecuteFunc) ExecuteFunc {
		return func(ctx context.Context, w Workflow, opts Options) (Result, error) {
			attempts := maxAttempts
			wait := backoff
			if opts.MaxRetries > 0 {
				attempts = opts.MaxRetries
			}
			if opts.RetryBackoff > 0 {
				wait = opts.RetryBackoff
			}
			if wait == 0 {
				wait = 5 * time.Second
			}

			var (
				result Result
				err    error
			)
			for i := 0; i <= attempts; i++ {
				result, err = next(ctx, w, opts)
				if err == nil {
					return result, nil
				}
				if i < attempts {
					select {
					case <-ctx.Done():
						return result, ctx.Err()
					case <-time.After(wait):
					}
					wait *= 2
				}
			}
			return result, fmt.Errorf("workflow %s failed after %d attempt(s): %w", w.Name(), attempts+1, err)
		}
	}
}

// ─── WithTimeout ──────────────────────────────────────────────────────────────

// WithTimeout cancels the workflow if it exceeds d.
// Opts.Timeout takes precedence when non-zero.
func WithTimeout(d time.Duration) Middleware {
	return func(next ExecuteFunc) ExecuteFunc {
		return func(ctx context.Context, w Workflow, opts Options) (Result, error) {
			timeout := d
			if opts.Timeout > 0 {
				timeout = opts.Timeout
			}
			if timeout <= 0 {
				return next(ctx, w, opts)
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next(ctx, w, opts)
		}
	}
}

// ─── WithLogging ──────────────────────────────────────────────────────────────

// WithLogging logs workflow start, completion, and errors to logger.
// Pass nil to use the default log package.
func WithLogging(logger *log.Logger) Middleware {
	logf := log.Printf
	if logger != nil {
		logf = logger.Printf
	}
	return func(next ExecuteFunc) ExecuteFunc {
		return func(ctx context.Context, w Workflow, opts Options) (Result, error) {
			logf("[automation] starting %s (dry_run=%v)", w.Name(), opts.DryRun)
			result, err := next(ctx, w, opts)
			if err != nil {
				logf("[automation] %s failed in %s: %v", w.Name(), result.Duration.Round(time.Millisecond), err)
			} else {
				logf("[automation] %s finished in %s", w.Name(), result.Duration.Round(time.Millisecond))
			}
			return result, err
		}
	}
}

// ─── WithRecovery ─────────────────────────────────────────────────────────────

// WithRecovery catches panics from the workflow, converts them to errors,
// and ensures the Result is always populated.
func WithRecovery() Middleware {
	return func(next ExecuteFunc) ExecuteFunc {
		return func(ctx context.Context, w Workflow, opts Options) (result Result, err error) {
			defer func() {
				if p := recover(); p != nil {
					err = fmt.Errorf("workflow %s panicked: %v", w.Name(), p)
					if result.WorkflowName == "" {
						result.WorkflowName = w.Name()
					}
				}
			}()
			return next(ctx, w, opts)
		}
	}
}

// ─── WithMetrics ──────────────────────────────────────────────────────────────

// WithMetrics calls onResult after every execution (success or failure).
// Use this to feed Prometheus counters, Datadog metrics, or custom logging.
func WithMetrics(onResult func(Result)) Middleware {
	return func(next ExecuteFunc) ExecuteFunc {
		return func(ctx context.Context, w Workflow, opts Options) (Result, error) {
			result, err := next(ctx, w, opts)
			if onResult != nil {
				onResult(result)
			}
			return result, err
		}
	}
}
