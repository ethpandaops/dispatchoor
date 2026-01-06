package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// JobChangeCallback is called when a job state changes.
type JobChangeCallback func(job *store.Job)

// Service defines the interface for queue operations.
type Service interface {
	Start(ctx context.Context) error
	Stop() error

	// Queue operations.
	Enqueue(ctx context.Context, groupID, templateID, createdBy string, inputs map[string]string) (*store.Job, error)
	Dequeue(ctx context.Context, groupID string) (*store.Job, error)
	Peek(ctx context.Context, groupID string) (*store.Job, error)
	Remove(ctx context.Context, jobID string) error
	Reorder(ctx context.Context, groupID string, jobIDs []string) error

	// Queries.
	GetJob(ctx context.Context, jobID string) (*store.Job, error)
	ListPending(ctx context.Context, groupID string) ([]*store.Job, error)
	ListByStatus(ctx context.Context, groupID string, statuses ...store.JobStatus) ([]*store.Job, error)
	ListHistory(ctx context.Context, groupID string, limit int) ([]*store.Job, error)

	// State transitions.
	MarkTriggered(ctx context.Context, jobID string, runID int64, runURL string) error
	MarkRunning(ctx context.Context, jobID, runnerName string) error
	MarkCompleted(ctx context.Context, jobID string) error
	MarkFailed(ctx context.Context, jobID, errMsg string) error
	MarkCancelled(ctx context.Context, jobID string) error

	// Pause/Unpause.
	Pause(ctx context.Context, jobID string) (*store.Job, error)
	Unpause(ctx context.Context, jobID string) (*store.Job, error)

	// Update.
	UpdateInputs(ctx context.Context, jobID string, inputs map[string]string) error

	// Callbacks.
	SetJobChangeCallback(cb JobChangeCallback)
}

// service implements Service.
type service struct {
	log               logrus.FieldLogger
	store             store.Store
	mu                sync.Mutex
	jobChangeCallback JobChangeCallback
}

// Ensure service implements Service.
var _ Service = (*service)(nil)

// NewService creates a new queue service.
func NewService(log logrus.FieldLogger, st store.Store) Service {
	return &service{
		log:   log.WithField("component", "queue"),
		store: st,
	}
}

// Start initializes the queue service.
func (s *service) Start(ctx context.Context) error {
	s.log.Info("Starting queue service")

	return nil
}

// Stop shuts down the queue service.
func (s *service) Stop() error {
	s.log.Info("Stopping queue service")

	return nil
}

// SetJobChangeCallback sets the callback for job state changes.
func (s *service) SetJobChangeCallback(cb JobChangeCallback) {
	s.jobChangeCallback = cb
}

// notifyJobChange calls the callback if set.
func (s *service) notifyJobChange(job *store.Job) {
	if s.jobChangeCallback != nil {
		s.jobChangeCallback(job)
	}
}

// Enqueue adds a new job to the queue.
func (s *service) Enqueue(
	ctx context.Context,
	groupID, templateID, createdBy string,
	inputs map[string]string,
) (*store.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify template exists.
	template, err := s.store.GetJobTemplate(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("getting template: %w", err)
	}

	if template == nil {
		return nil, fmt.Errorf("template not found: %s", templateID)
	}

	if template.GroupID != groupID {
		return nil, fmt.Errorf("template %s does not belong to group %s", templateID, groupID)
	}

	// Get max position.
	maxPos, err := s.store.GetMaxPosition(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("getting max position: %w", err)
	}

	// Merge inputs with template defaults.
	mergedInputs := make(map[string]string, len(template.DefaultInputs))
	for k, v := range template.DefaultInputs {
		mergedInputs[k] = v
	}

	for k, v := range inputs {
		mergedInputs[k] = v
	}

	now := time.Now()

	job := &store.Job{
		ID:         uuid.New().String(),
		GroupID:    groupID,
		TemplateID: templateID,
		Priority:   0,
		Position:   maxPos + 1,
		Status:     store.JobStatusPending,
		Inputs:     mergedInputs,
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.store.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("creating job: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"job_id":      job.ID,
		"group_id":    groupID,
		"template_id": templateID,
		"position":    job.Position,
	}).Info("Job enqueued")

	s.notifyJobChange(job)

	return job, nil
}

// Dequeue removes and returns the next pending job from the queue.
func (s *service) Dequeue(ctx context.Context, groupID string) (*store.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetNextPendingJob(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("getting next pending job: %w", err)
	}

	return job, nil
}

// Peek returns the next pending job without removing it.
func (s *service) Peek(ctx context.Context, groupID string) (*store.Job, error) {
	return s.store.GetNextPendingJob(ctx, groupID)
}

// Remove removes a job from the queue.
func (s *service) Remove(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Only allow removing pending or failed jobs.
	if job.Status != store.JobStatusPending && job.Status != store.JobStatusFailed {
		return fmt.Errorf("cannot remove job with status %s", job.Status)
	}

	if err := s.store.DeleteJob(ctx, jobID); err != nil {
		return fmt.Errorf("deleting job: %w", err)
	}

	s.log.WithField("job_id", jobID).Info("Job removed from queue")

	return nil
}

// Reorder updates the position of jobs in the queue.
func (s *service) Reorder(ctx context.Context, groupID string, jobIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify all jobs exist and belong to the group.
	for _, jobID := range jobIDs {
		job, err := s.store.GetJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("getting job %s: %w", jobID, err)
		}

		if job == nil {
			return fmt.Errorf("job not found: %s", jobID)
		}

		if job.GroupID != groupID {
			return fmt.Errorf("job %s does not belong to group %s", jobID, groupID)
		}

		if job.Status != store.JobStatusPending {
			return fmt.Errorf("cannot reorder job %s with status %s", jobID, job.Status)
		}
	}

	if err := s.store.ReorderJobs(ctx, groupID, jobIDs); err != nil {
		return fmt.Errorf("reordering jobs: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"group_id": groupID,
		"count":    len(jobIDs),
	}).Info("Jobs reordered")

	return nil
}

// GetJob retrieves a job by ID.
func (s *service) GetJob(ctx context.Context, jobID string) (*store.Job, error) {
	return s.store.GetJob(ctx, jobID)
}

// ListPending returns all pending jobs for a group.
func (s *service) ListPending(ctx context.Context, groupID string) ([]*store.Job, error) {
	return s.store.ListJobsByGroup(ctx, groupID, store.JobStatusPending)
}

// ListByStatus returns jobs with the given statuses.
func (s *service) ListByStatus(
	ctx context.Context, groupID string, statuses ...store.JobStatus,
) ([]*store.Job, error) {
	return s.store.ListJobsByGroup(ctx, groupID, statuses...)
}

// ListHistory returns completed/failed/cancelled jobs for a group.
func (s *service) ListHistory(ctx context.Context, groupID string, limit int) ([]*store.Job, error) {
	jobs, err := s.store.ListJobsByGroup(
		ctx, groupID,
		store.JobStatusCompleted, store.JobStatusFailed, store.JobStatusCancelled,
	)
	if err != nil {
		return nil, err
	}

	// Sort by completed_at descending and limit.
	// Note: In a real implementation, this should be done in the SQL query.
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}

	return jobs, nil
}

// MarkTriggered marks a job as triggered.
func (s *service) MarkTriggered(ctx context.Context, jobID string, runID int64, runURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != store.JobStatusPending {
		return fmt.Errorf("cannot mark job as triggered: current status is %s", job.Status)
	}

	now := time.Now()
	job.Status = store.JobStatusTriggered
	job.TriggeredAt = &now
	job.RunID = &runID
	job.RunURL = runURL
	job.UpdatedAt = now

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"job_id": jobID,
		"run_id": runID,
	}).Info("Job marked as triggered")

	s.notifyJobChange(job)

	return nil
}

// MarkRunning marks a job as running.
func (s *service) MarkRunning(ctx context.Context, jobID, runnerName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != store.JobStatusTriggered {
		return fmt.Errorf("cannot mark job as running: current status is %s", job.Status)
	}

	job.Status = store.JobStatusRunning
	job.RunnerName = runnerName
	job.UpdatedAt = time.Now()

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"job_id": jobID,
		"runner": runnerName,
	}).Info("Job marked as running")

	s.notifyJobChange(job)

	return nil
}

// MarkCompleted marks a job as completed.
func (s *service) MarkCompleted(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	now := time.Now()
	job.Status = store.JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	s.log.WithField("job_id", jobID).Info("Job marked as completed")

	s.notifyJobChange(job)

	return nil
}

// MarkFailed marks a job as failed.
func (s *service) MarkFailed(ctx context.Context, jobID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	now := time.Now()
	job.Status = store.JobStatusFailed
	job.CompletedAt = &now
	job.ErrorMessage = errMsg
	job.UpdatedAt = now

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"job_id": jobID,
		"error":  errMsg,
	}).Info("Job marked as failed")

	s.notifyJobChange(job)

	return nil
}

// MarkCancelled marks a job as cancelled.
func (s *service) MarkCancelled(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Can only cancel pending or triggered jobs.
	if job.Status != store.JobStatusPending && job.Status != store.JobStatusTriggered {
		return fmt.Errorf("cannot cancel job with status %s", job.Status)
	}

	now := time.Now()
	job.Status = store.JobStatusCancelled
	job.CompletedAt = &now
	job.UpdatedAt = now

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	s.log.WithField("job_id", jobID).Info("Job marked as cancelled")

	s.notifyJobChange(job)

	return nil
}

// Pause pauses a pending job so it won't be scheduled.
func (s *service) Pause(ctx context.Context, jobID string) (*store.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != store.JobStatusPending {
		return nil, fmt.Errorf("cannot pause job with status %s", job.Status)
	}

	if job.Paused {
		return job, nil // Already paused
	}

	job.Paused = true
	job.UpdatedAt = time.Now()

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("updating job: %w", err)
	}

	s.log.WithField("job_id", jobID).Info("Job paused")

	s.notifyJobChange(job)

	return job, nil
}

// Unpause unpauses a paused job so it can be scheduled.
func (s *service) Unpause(ctx context.Context, jobID string) (*store.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != store.JobStatusPending {
		return nil, fmt.Errorf("cannot unpause job with status %s", job.Status)
	}

	if !job.Paused {
		return job, nil // Already unpaused
	}

	job.Paused = false
	job.UpdatedAt = time.Now()

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("updating job: %w", err)
	}

	s.log.WithField("job_id", jobID).Info("Job unpaused")

	s.notifyJobChange(job)

	return job, nil
}

// UpdateInputs updates the inputs for a pending job.
func (s *service) UpdateInputs(ctx context.Context, jobID string, inputs map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("getting job: %w", err)
	}

	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != store.JobStatusPending {
		return fmt.Errorf("cannot update inputs for job with status %s", job.Status)
	}

	job.Inputs = inputs
	job.UpdatedAt = time.Now()

	if err := s.store.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	s.log.WithField("job_id", jobID).Info("Job inputs updated")

	return nil
}
