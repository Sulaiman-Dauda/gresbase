package job

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/tools/cron"
	"github.com/rs/zerolog/log"
)

// Job represents a scheduled task.
type Job struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CronExpr  string     `json:"cron_expr"`
	Handler   string     `json:"handler"`
	Data      string     `json:"data"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// Runtime fields
	schedule *cron.Schedule
}

// Scheduler manages cron jobs with database persistence.
type Scheduler struct {
	db       *database.DB
	jobs     map[string]*Job
	handlers map[string]func(ctx context.Context, data map[string]any) error
	mu       sync.RWMutex
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewScheduler creates a new job scheduler.
func NewScheduler(db *database.DB) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		db:       db,
		jobs:     make(map[string]*Job),
		handlers: make(map[string]func(ctx context.Context, data map[string]any) error),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// EnsureTable creates the jobs table if it doesn't exist.
func (s *Scheduler) EnsureTable(ctx context.Context) error {
	return s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _jobs (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			cron_expr   TEXT NOT NULL,
			handler     TEXT NOT NULL,
			data        TEXT DEFAULT '',
			enabled     BOOLEAN DEFAULT TRUE,
			last_run_at TIMESTAMPTZ,
			next_run_at TIMESTAMPTZ,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS _job_runs (
			id          BIGSERIAL PRIMARY KEY,
			job_id      TEXT REFERENCES _jobs(id) ON DELETE CASCADE,
			status      TEXT DEFAULT 'running',
			output      TEXT DEFAULT '',
			started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			finished_at TIMESTAMPTZ,
			error       TEXT DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_job_runs_job_id ON _job_runs(job_id);
	`)
}

// RegisterHandler registers a named handler function.
func (s *Scheduler) RegisterHandler(name string, fn func(ctx context.Context, data map[string]any) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[name] = fn
}

// AddJob creates a new job with proper cron parsing.
func (s *Scheduler) AddJob(ctx context.Context, name, cronExpr, handler string, data map[string]any) (*Job, error) {
	// Parse the cron expression
	schedule, err := cron.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	nextRun := schedule.Next(time.Now())

	dataJSON, _ := json.Marshal(data)
	job := &Job{
		ID:        uuid.New().String(),
		Name:      name,
		CronExpr:  cronExpr,
		Handler:   handler,
		Data:      string(dataJSON),
		Enabled:   true,
		NextRunAt: &nextRun,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		schedule:  schedule,
	}

	err = s.db.Exec(ctx, `
		INSERT INTO _jobs (id, name, cron_expr, handler, data, enabled, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		job.ID, job.Name, job.CronExpr, job.Handler, job.Data, job.Enabled, job.NextRunAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	log.Info().Str("name", name).Str("id", job.ID).Str("next", nextRun.Format(time.RFC3339)).Msg("Job created")
	return job, nil
}

// LoadJobs loads all jobs from database into memory.
func (s *Scheduler) LoadJobs(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, cron_expr, handler, data, enabled, last_run_at, next_run_at, created_at, updated_at
		FROM _jobs WHERE enabled = TRUE ORDER BY name`)
	if err != nil {
		return err
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	for rows.Next() {
		job := &Job{}
		if err := rows.Scan(&job.ID, &job.Name, &job.CronExpr, &job.Handler, &job.Data,
			&job.Enabled, &job.LastRunAt, &job.NextRunAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return err
		}
		// Parse schedule
		schedule, err := cron.Parse(job.CronExpr)
		if err != nil {
			log.Warn().Err(err).Str("job", job.Name).Msg("Invalid cron expression, skipping")
			continue
		}
		job.schedule = schedule
		s.jobs[job.ID] = job
	}
	return nil
}

// ListJobs returns all jobs.
func (s *Scheduler) ListJobs(ctx context.Context) ([]*Job, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, cron_expr, handler, data, enabled, last_run_at, next_run_at, created_at, updated_at
		FROM _jobs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		if err := rows.Scan(&job.ID, &job.Name, &job.CronExpr, &job.Handler, &job.Data,
			&job.Enabled, &job.LastRunAt, &job.NextRunAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		// Recalculate next run
		if sched, err := cron.Parse(job.CronExpr); err == nil {
			next := sched.Next(time.Now())
			job.NextRunAt = &next
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// SetJobEnabled toggles a job. Disabled jobs are removed from the in-memory
// schedule; re-enabling reloads and re-arms them.
func (s *Scheduler) SetJobEnabled(ctx context.Context, id string, enabled bool) error {
	if err := s.db.Exec(ctx,
		"UPDATE _jobs SET enabled = $1, updated_at = NOW() WHERE id = $2", enabled, id); err != nil {
		return err
	}

	if !enabled {
		s.mu.Lock()
		delete(s.jobs, id)
		s.mu.Unlock()
		return nil
	}

	job := &Job{}
	err := s.db.QueryRow(ctx, `
		SELECT id, name, cron_expr, handler, data, enabled, last_run_at, next_run_at, created_at, updated_at
		FROM _jobs WHERE id = $1`, id).
		Scan(&job.ID, &job.Name, &job.CronExpr, &job.Handler, &job.Data,
			&job.Enabled, &job.LastRunAt, &job.NextRunAt, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return err
	}
	schedule, err := cron.Parse(job.CronExpr)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", job.CronExpr, err)
	}
	job.schedule = schedule
	next := schedule.Next(time.Now())
	job.NextRunAt = &next
	_ = s.db.Exec(ctx, "UPDATE _jobs SET next_run_at = $1 WHERE id = $2", next, id)

	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	return nil
}

// JobRun is a single execution record of a job.
type JobRun struct {
	ID         int64      `json:"id"`
	JobID      string     `json:"job_id"`
	Status     string     `json:"status"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ListRuns returns the most recent executions of a job.
func (s *Scheduler) ListRuns(ctx context.Context, jobID string, limit int) ([]*JobRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, job_id, status, output, error, started_at, finished_at
		FROM _job_runs WHERE job_id = $1 ORDER BY started_at DESC LIMIT $2`, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []*JobRun{}
	for rows.Next() {
		run := &JobRun{}
		if err := rows.Scan(&run.ID, &run.JobID, &run.Status, &run.Output, &run.Error,
			&run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// DeleteJob removes a job.
func (s *Scheduler) DeleteJob(ctx context.Context, id string) error {
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()

	return s.db.Exec(ctx, "DELETE FROM _jobs WHERE id = $1", id)
}

// RunJob triggers a job manually.
func (s *Scheduler) RunJob(ctx context.Context, id string) error {
	s.mu.RLock()
	job, ok := s.jobs[id]
	var handlerFn func(ctx context.Context, data map[string]any) error
	if ok {
		handlerFn = s.handlers[job.Handler]
	}
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	now := time.Now()
	var data map[string]any
	// stored JSON; defaults to nil map if malformed
	_ = json.Unmarshal([]byte(job.Data), &data)

	// Record run start
	var runID int64
	if err := s.db.QueryRow(ctx,
		"INSERT INTO _job_runs (job_id, status) VALUES ($1, 'running') RETURNING id", id).Scan(&runID); err != nil {
		return fmt.Errorf("create job run: %w", err)
	}

	var runErr error
	if handlerFn != nil {
		runErr = safeCallHandler(ctx, job.Name, handlerFn, data)
	} else {
		log.Warn().Str("handler", job.Handler).Msg("No handler registered for job")
	}

	// Update run record
	status := "completed"
	errMsg := ""
	if runErr != nil {
		status = "failed"
		errMsg = runErr.Error()
	}
	if err := s.db.Exec(ctx,
		"UPDATE _job_runs SET status = $1, error = $2, finished_at = NOW() WHERE id = $3",
		status, errMsg, runID); err != nil {
		return fmt.Errorf("update job run: %w", err)
	}

	// Schedule next run
	if job.schedule != nil {
		next := job.schedule.Next(now)
		if err := s.db.Exec(ctx, "UPDATE _jobs SET last_run_at = $1, next_run_at = $2, updated_at = NOW() WHERE id = $3",
			now, next, id); err != nil {
			return fmt.Errorf("update job schedule: %w", err)
		}
	}

	if runErr != nil {
		log.Error().Err(runErr).Str("name", job.Name).Str("id", id).Msg("Job failed")
		return runErr
	}

	log.Info().Str("name", job.Name).Str("id", id).Msg("Job executed")
	return nil
}

// Start begins the scheduler loop with proper cron evaluation.
func (s *Scheduler) Start() {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	go s.runLoop()
	log.Info().Msg("Job scheduler started")
}

// Stop halts the scheduler.
func (s *Scheduler) Stop() {
	s.cancel()
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	log.Info().Msg("Job scheduler stopped")
}

func (s *Scheduler) runLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkAndRun()
		}
	}
}

func (s *Scheduler) checkAndRun() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, job := range s.jobs {
		if !job.Enabled || job.NextRunAt == nil || job.schedule == nil {
			continue
		}
		if now.After(*job.NextRunAt) || now.Equal(*job.NextRunAt) {
			go func(j *Job) {
				// Last line of defense: a panic escaping RunJob (e.g. from the
				// persistence layer) must never kill the scheduler or server.
				defer func() {
					if r := recover(); r != nil {
						log.Error().
							Str("name", j.Name).
							Str("id", j.ID).
							Interface("panic", r).
							Bytes("stack", debug.Stack()).
							Msg("Job run panicked; scheduler recovered")
					}
				}()
				ctx := context.Background()
				if err := s.RunJob(ctx, j.ID); err != nil {
					log.Warn().Err(err).Str("name", j.Name).Str("id", j.ID).Msg("Scheduled job run failed")
				}
			}(job)
		}
	}
}

// GetNextRuns returns the next N run times for a cron expression.
func (s *Scheduler) GetNextRuns(cronExpr string, n int) ([]time.Time, error) {
	sched, err := cron.Parse(cronExpr)
	if err != nil {
		return nil, err
	}
	return sched.NextN(time.Now(), n), nil
}

// ValidateCron checks if a cron expression is valid.
func (s *Scheduler) ValidateCron(cronExpr string) error {
	_, err := cron.Parse(cronExpr)
	return err
}

// safeCallHandler invokes a job handler and converts panics into errors so a
// panicking job can never take down the scheduler (or the whole server). The
// panic is logged with its stack trace and surfaces as a regular handler
// error, which marks the job run as failed.
func safeCallHandler(ctx context.Context, jobName string, fn func(ctx context.Context, data map[string]any) error, data map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Str("name", jobName).
				Interface("panic", r).
				Bytes("stack", debug.Stack()).
				Msg("Job handler panicked; scheduler recovered")
			err = fmt.Errorf("job handler panicked: %v", r)
		}
	}()
	return fn(ctx, data)
}
