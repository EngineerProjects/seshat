package automation

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─── Schedule interface ───────────────────────────────────────────────────────

// Schedule computes the next trigger time after a given reference point.
type Schedule interface {
	// Next returns the next time the schedule fires after from.
	// Returns the zero Time if the schedule never fires again.
	Next(from time.Time) time.Time
	// String returns a human-readable description of the schedule.
	String() string
}

// ─── IntervalSchedule ─────────────────────────────────────────────────────────

// IntervalSchedule fires every fixed Interval starting from first use.
type IntervalSchedule struct {
	Interval time.Duration
}

func Every(d time.Duration) *IntervalSchedule { return &IntervalSchedule{Interval: d} }

func (s *IntervalSchedule) Next(from time.Time) time.Time { return from.Add(s.Interval) }
func (s *IntervalSchedule) String() string                { return fmt.Sprintf("every %s", s.Interval) }

// ─── OnceSchedule ─────────────────────────────────────────────────────────────

// OnceSchedule fires exactly once at At.
type OnceSchedule struct {
	At time.Time
}

func Once(at time.Time) *OnceSchedule { return &OnceSchedule{At: at} }

func (s *OnceSchedule) Next(from time.Time) time.Time {
	if s.At.After(from) {
		return s.At
	}
	return time.Time{} // already fired
}

func (s *OnceSchedule) String() string { return fmt.Sprintf("once at %s", s.At.Format(time.RFC3339)) }

// ─── CronSchedule ─────────────────────────────────────────────────────────────

// CronSchedule fires according to a standard 5-field cron expression:
//
//	┌───────────── minute  (0–59)
//	│ ┌─────────── hour    (0–23)
//	│ │ ┌───────── dom     (1–31)
//	│ │ │ ┌─────── month   (1–12)
//	│ │ │ │ ┌───── weekday (0–6, 0=Sun)
//	│ │ │ │ │
//	* * * * *
//
// Supports: *, N, N-M, */N, N,M,…, and combinations thereof.
type CronSchedule struct {
	expr   string
	minute cronField
	hour   cronField
	dom    cronField
	month  cronField
	dow    cronField
}

// Cron parses a 5-field cron expression. Returns an error on invalid syntax.
func Cron(expr string) (*CronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields, got %d in %q", len(fields), expr)
	}
	c := &CronSchedule{expr: expr}
	var err error
	if c.minute, err = parseCronField(fields[0], 0, 59); err != nil {
		return nil, fmt.Errorf("cron minute: %w", err)
	}
	if c.hour, err = parseCronField(fields[1], 0, 23); err != nil {
		return nil, fmt.Errorf("cron hour: %w", err)
	}
	if c.dom, err = parseCronField(fields[2], 1, 31); err != nil {
		return nil, fmt.Errorf("cron dom: %w", err)
	}
	if c.month, err = parseCronField(fields[3], 1, 12); err != nil {
		return nil, fmt.Errorf("cron month: %w", err)
	}
	if c.dow, err = parseCronField(fields[4], 0, 6); err != nil {
		return nil, fmt.Errorf("cron dow: %w", err)
	}
	return c, nil
}

// MustCron parses expr and panics on error.
func MustCron(expr string) *CronSchedule {
	c, err := Cron(expr)
	if err != nil {
		panic(err)
	}
	return c
}

func (c *CronSchedule) Next(from time.Time) time.Time {
	// Start searching from the next whole minute after from.
	t := from.Add(time.Minute).Truncate(time.Minute)

	// Safety: search at most 1 year ahead (cron expressions always fire within a year).
	limit := from.Add(366 * 24 * time.Hour)
	for t.Before(limit) {
		if !c.month.contains(int(t.Month())) {
			// Skip to first day of next month.
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !c.dom.contains(t.Day()) || !c.dow.contains(int(t.Weekday())) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !c.hour.contains(t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if !c.minute.contains(t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

func (c *CronSchedule) String() string { return fmt.Sprintf("cron(%s)", c.expr) }

// ─── cron field parser ────────────────────────────────────────────────────────

// cronField is a bitmask over a range of integer values.
// Bit N is set when value N is active.
type cronField struct {
	bits uint64
	min  int
	max  int
}

func (f cronField) contains(v int) bool {
	if v < f.min || v > f.max {
		return false
	}
	return f.bits&(1<<uint(v)) != 0
}

// parseCronField parses a single cron field supporting:
// *, N, N-M, */step, N-M/step, and comma-separated combinations.
func parseCronField(expr string, min, max int) (cronField, error) {
	f := cronField{min: min, max: max}
	for _, part := range strings.Split(expr, ",") {
		if err := f.applyPart(strings.TrimSpace(part), min, max); err != nil {
			return f, err
		}
	}
	return f, nil
}

func (f *cronField) applyPart(part string, min, max int) error {
	// Handle */step
	if strings.HasPrefix(part, "*/") {
		step, err := strconv.Atoi(part[2:])
		if err != nil || step <= 0 {
			return fmt.Errorf("invalid step %q", part)
		}
		for v := min; v <= max; v += step {
			f.bits |= 1 << uint(v)
		}
		return nil
	}

	// Handle *
	if part == "*" {
		for v := min; v <= max; v++ {
			f.bits |= 1 << uint(v)
		}
		return nil
	}

	// Handle N-M or N-M/step
	if idx := strings.Index(part, "-"); idx >= 0 {
		rangePart := part[:idx]
		rest := part[idx+1:]
		step := 1
		if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
			s, err := strconv.Atoi(rest[slashIdx+1:])
			if err != nil || s <= 0 {
				return fmt.Errorf("invalid step in %q", part)
			}
			step = s
			rest = rest[:slashIdx]
		}
		lo, err1 := strconv.Atoi(rangePart)
		hi, err2 := strconv.Atoi(rest)
		if err1 != nil || err2 != nil || lo < min || hi > max || lo > hi {
			return fmt.Errorf("invalid range %q (valid: %d-%d)", part, min, max)
		}
		for v := lo; v <= hi; v += step {
			f.bits |= 1 << uint(v)
		}
		return nil
	}

	// Handle plain number N
	v, err := strconv.Atoi(part)
	if err != nil || v < min || v > max {
		return fmt.Errorf("invalid value %q (valid: %d-%d)", part, min, max)
	}
	f.bits |= 1 << uint(v)
	return nil
}

// ─── Scheduler ────────────────────────────────────────────────────────────────

// scheduledJob pairs a Workflow with its Schedule and execution Options.
type scheduledJob struct {
	workflow Workflow
	schedule Schedule
	opts     Options
	nextRun  time.Time
}

// Scheduler runs registered workflows according to their schedules.
// It uses a single goroutine and a timer-based loop; it does not spawn a
// goroutine per job. Concurrent job execution is not supported by design —
// use multiple Schedulers if you need parallel pipelines.
type Scheduler struct {
	mu       sync.Mutex
	jobs     []*scheduledJob
	executor *Executor
}

// NewScheduler creates a Scheduler backed by executor.
func NewScheduler(executor *Executor) *Scheduler {
	return &Scheduler{executor: executor}
}

// Add registers a workflow with its schedule and options.
// Returns the Scheduler to allow method chaining.
func (s *Scheduler) Add(w Workflow, schedule Schedule, opts Options) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, &scheduledJob{
		workflow: w,
		schedule: schedule,
		opts:     opts,
		nextRun:  schedule.Next(time.Now()),
	})
	return s
}

// Run blocks and runs scheduled jobs until ctx is cancelled.
// It ticks at most once per second to avoid busy-wait.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			s.tick(ctx, now)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	due := make([]*scheduledJob, 0)
	for _, job := range s.jobs {
		if !job.nextRun.IsZero() && !now.Before(job.nextRun) {
			due = append(due, job)
		}
	}
	s.mu.Unlock()

	for _, job := range due {
		// Run synchronously — one job at a time.
		s.executor.Run(ctx, job.workflow, job.opts) //nolint:errcheck

		s.mu.Lock()
		job.nextRun = job.schedule.Next(now)
		s.mu.Unlock()
	}
}

// RunNow immediately executes the workflow registered under name, bypassing
// the schedule. Returns ErrNotFound if the name is not registered.
func (s *Scheduler) RunNow(ctx context.Context, name string, opts Options) (Result, error) {
	s.mu.Lock()
	var job *scheduledJob
	for _, j := range s.jobs {
		if j.workflow.Name() == name {
			job = j
			break
		}
	}
	s.mu.Unlock()

	if job == nil {
		return Result{}, fmt.Errorf("scheduler: workflow %q not found", name)
	}
	return s.executor.Run(ctx, job.workflow, opts)
}

// Next returns a snapshot of the upcoming scheduled runs, sorted by time.
func (s *Scheduler) Next() []ScheduledRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs := make([]ScheduledRun, 0, len(s.jobs))
	for _, j := range s.jobs {
		runs = append(runs, ScheduledRun{
			WorkflowName: j.workflow.Name(),
			Schedule:     j.schedule.String(),
			NextRun:      j.nextRun,
		})
	}
	sort.Slice(runs, func(i, k int) bool {
		return runs[i].NextRun.Before(runs[k].NextRun)
	})
	return runs
}

// ScheduledRun is a read-only view of a single job's next execution.
type ScheduledRun struct {
	WorkflowName string
	Schedule     string
	NextRun      time.Time
}
