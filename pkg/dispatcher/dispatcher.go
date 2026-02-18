package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/github"
	"github.com/ethpandaops/dispatchoor/pkg/queue"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
)

// RunnerChangeCallback is called when a runner's status changes.
type RunnerChangeCallback func(runner *store.Runner)

// Dispatcher defines the interface for the job dispatch service.
type Dispatcher interface {
	Start(ctx context.Context) error
	Stop() error
	SetRunnerChangeCallback(cb RunnerChangeCallback)
}

// dispatcher implements Dispatcher.
type dispatcher struct {
	log      logrus.FieldLogger
	cfg      *config.Config
	store    store.Store
	queue    queue.Service
	ghClient github.Client

	interval         time.Duration
	trackingInterval time.Duration

	cancel               context.CancelFunc
	wg                   sync.WaitGroup
	mu                   sync.Mutex
	runnerChangeCallback RunnerChangeCallback

	// workflowLocks provides per-workflow-template locking to prevent race conditions
	// when multiple groups dispatch the same workflow. Key: "owner/repo/workflow_id".
	workflowLocks   map[string]*sync.Mutex
	workflowLocksMu sync.Mutex
}

// Ensure dispatcher implements Dispatcher.
var _ Dispatcher = (*dispatcher)(nil)

// NewDispatcher creates a new dispatcher.
func NewDispatcher(
	log logrus.FieldLogger,
	cfg *config.Config,
	st store.Store,
	q queue.Service,
	ghClient github.Client,
) Dispatcher {
	return &dispatcher{
		log:              log.WithField("component", "dispatcher"),
		cfg:              cfg,
		store:            st,
		queue:            q,
		ghClient:         ghClient,
		interval:         cfg.Dispatcher.Interval,
		trackingInterval: cfg.Dispatcher.TrackingInterval,
		workflowLocks:    make(map[string]*sync.Mutex),
	}
}

// Start begins the dispatch loop.
func (d *dispatcher) Start(ctx context.Context) error {
	if !d.cfg.Dispatcher.Enabled {
		d.log.Info("Dispatcher is disabled")

		return nil
	}

	d.log.WithField("interval", d.interval).Info("Starting dispatcher")

	ctx, d.cancel = context.WithCancel(ctx)

	// Start the dispatch loop.
	d.wg.Add(1)

	go d.dispatchLoop(ctx)

	// Start the run tracker loop.
	d.wg.Add(1)

	go d.trackRunsLoop(ctx)

	return nil
}

// Stop stops the dispatcher.
func (d *dispatcher) Stop() error {
	d.log.Info("Stopping dispatcher")

	if d.cancel != nil {
		d.cancel()
	}

	d.wg.Wait()

	return nil
}

// SetRunnerChangeCallback sets the callback for runner status changes.
func (d *dispatcher) SetRunnerChangeCallback(cb RunnerChangeCallback) {
	d.runnerChangeCallback = cb
}

// notifyRunnerChange calls the callback if set.
func (d *dispatcher) notifyRunnerChange(runner *store.Runner) {
	if d.runnerChangeCallback != nil {
		d.runnerChangeCallback(runner)
	}
}

// getWorkflowLock returns or creates a mutex for a specific workflow template.
// This ensures sequential dispatch for jobs targeting the same workflow.
func (d *dispatcher) getWorkflowLock(owner, repo, workflowID string) *sync.Mutex {
	key := fmt.Sprintf("%s/%s/%s", owner, repo, workflowID)

	d.workflowLocksMu.Lock()
	defer d.workflowLocksMu.Unlock()

	if lock, ok := d.workflowLocks[key]; ok {
		return lock
	}

	lock := &sync.Mutex{}
	d.workflowLocks[key] = lock

	return lock
}

// waitForRunID polls GitHub to find and match the run ID for a just-triggered job.
// This blocks until the run ID is found or timeout is reached.
func (d *dispatcher) waitForRunID(
	ctx context.Context,
	job *store.Job,
	owner, repo, workflowID string,
) error {
	const (
		timeout      = 60 * time.Second
		pollInterval = 5 * time.Second
	)

	deadline := time.Now().Add(timeout)
	log := d.log.WithField("job_id", job.ID)

	for time.Now().Before(deadline) {
		// Build fresh claimed set each iteration so we see newly assigned runs.
		claimedRunIDs, claimErr := d.buildClaimedRunIDs(ctx)
		if claimErr != nil {
			log.WithError(claimErr).Warn("Failed to build claimed run IDs, proceeding without exclusion")
		}

		runID, runURL, err := d.findWorkflowRun(ctx, owner, repo, workflowID, job, claimedRunIDs)
		if err == nil && runID != 0 {
			job.RunID = &runID
			job.RunURL = runURL

			if err := d.store.UpdateJob(ctx, job); err != nil {
				return fmt.Errorf("updating job with run ID: %w", err)
			}

			log.WithFields(logrus.Fields{
				"run_id":  runID,
				"run_url": runURL,
			}).Info("Found workflow run inline")

			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
			log.Debug("Polling for workflow run...")
		}
	}

	return fmt.Errorf("timeout waiting for run ID after %v", timeout)
}

// getEffectiveWorkflowParams returns the effective workflow parameters,
// preferring job overrides over template defaults.
// For manual jobs (template == nil), only job fields are used.
func getEffectiveWorkflowParams(job *store.Job, template *store.JobTemplate) (owner, repo, workflowID, ref string) {
	// Start with template defaults if available.
	if template != nil {
		owner = template.Owner
		repo = template.Repo
		workflowID = template.WorkflowID
		ref = template.Ref
	}

	// Job overrides take precedence.
	if job.Owner != nil && *job.Owner != "" {
		owner = *job.Owner
	}

	if job.Repo != nil && *job.Repo != "" {
		repo = *job.Repo
	}

	if job.WorkflowID != nil && *job.WorkflowID != "" {
		workflowID = *job.WorkflowID
	}

	if job.Ref != nil && *job.Ref != "" {
		ref = *job.Ref
	}

	return
}

// dispatchLoop is the main dispatch loop that matches pending jobs to idle runners.
func (d *dispatcher) dispatchLoop(ctx context.Context) {
	defer d.wg.Done()

	// Do an initial dispatch immediately.
	if err := d.dispatch(ctx); err != nil {
		d.log.WithError(err).Error("Initial dispatch failed")
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.dispatch(ctx); err != nil {
				d.log.WithError(err).Error("Dispatch failed")
			}
		}
	}
}

// dispatch performs a single dispatch cycle.
func (d *dispatcher) dispatch(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get all enabled groups.
	groups, err := d.store.ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("listing groups: %w", err)
	}

	for _, group := range groups {
		if !group.Enabled {
			continue
		}

		if group.Paused {
			d.log.WithField("group", group.ID).Debug("Group is paused, skipping dispatch")

			continue
		}

		if err := d.dispatchForGroup(ctx, group); err != nil {
			d.log.WithError(err).WithField("group", group.ID).Error("Failed to dispatch for group")
		}
	}

	return nil
}

// dispatchForGroup handles dispatching for a single group.
func (d *dispatcher) dispatchForGroup(ctx context.Context, group *store.Group) error {
	log := d.log.WithField("group", group.ID)

	// Check if there are already triggered jobs waiting to start.
	// We should wait for them to move to "running" before dispatching new ones.
	triggeredJobs, err := d.queue.ListByStatus(ctx, group.ID, store.JobStatusTriggered)
	if err != nil {
		return fmt.Errorf("listing triggered jobs: %w", err)
	}

	if len(triggeredJobs) > 0 {
		log.WithField("triggered_count", len(triggeredJobs)).
			Debug("Waiting for triggered jobs to start before dispatching new ones")

		return nil
	}

	// Get the next pending job.
	job, err := d.queue.Peek(ctx, group.ID)
	if err != nil {
		return fmt.Errorf("peeking queue: %w", err)
	}

	if job == nil {
		log.Debug("No pending jobs")

		return nil
	}

	// Get runners for this group's labels.
	runners, err := d.store.ListRunnersByLabels(ctx, group.RunnerLabels)
	if err != nil {
		return fmt.Errorf("listing runners: %w", err)
	}

	// Find an idle runner.
	var idleRunner *store.Runner

	for _, runner := range runners {
		if runner.Status == store.RunnerStatusOnline && !runner.Busy {
			idleRunner = runner

			break
		}
	}

	if idleRunner == nil {
		log.Debug("No idle runners available")

		return nil
	}

	// Get the job template (may be nil for manual jobs).
	var template *store.JobTemplate

	if job.TemplateID != "" {
		var err error

		template, err = d.store.GetJobTemplate(ctx, job.TemplateID)
		if err != nil {
			return fmt.Errorf("getting job template: %w", err)
		}

		if template == nil {
			return fmt.Errorf("template not found: %s", job.TemplateID)
		}
	}

	// Get effective workflow parameters (job override or template default).
	owner, repo, workflowID, ref := getEffectiveWorkflowParams(job, template)

	// Validate we have all required params (should be set for manual jobs).
	if owner == "" || repo == "" || workflowID == "" || ref == "" {
		return fmt.Errorf("missing required workflow params: owner=%q repo=%q workflow=%q ref=%q",
			owner, repo, workflowID, ref)
	}

	// Acquire per-workflow lock to prevent race conditions when multiple groups
	// dispatch the same workflow. This ensures sequential dispatch and run ID matching.
	workflowLock := d.getWorkflowLock(owner, repo, workflowID)
	workflowLock.Lock()
	defer workflowLock.Unlock()

	logFields := logrus.Fields{
		"job_id":   job.ID,
		"runner":   idleRunner.Name,
		"owner":    owner,
		"repo":     repo,
		"workflow": workflowID,
		"ref":      ref,
	}
	if template != nil {
		logFields["template"] = template.Name
	} else {
		logFields["manual"] = true
	}

	log.WithFields(logFields).Info("Dispatching job")

	// Trigger the workflow dispatch.
	if err := d.ghClient.TriggerWorkflowDispatch(
		ctx,
		owner,
		repo,
		workflowID,
		ref,
		job.Inputs,
	); err != nil {
		// Mark the job as failed if we can't trigger.
		if markErr := d.queue.MarkFailed(ctx, job.ID, fmt.Sprintf("Failed to trigger: %v", err)); markErr != nil {
			log.WithError(markErr).Error("Failed to mark job as failed")
		}

		return fmt.Errorf("triggering workflow dispatch: %w", err)
	}

	// Mark as triggered without a run ID initially.
	// workflow_dispatch returns 204 No Content with no run ID.
	if err := d.queue.MarkTriggered(ctx, job.ID, 0, ""); err != nil {
		return fmt.Errorf("marking job as triggered: %w", err)
	}

	// Wait inline for the run ID to be found while holding the workflow lock.
	// This prevents race conditions when multiple jobs trigger the same workflow.
	if err := d.waitForRunID(ctx, job, owner, repo, workflowID); err != nil {
		// Log warning but don't fail - the tracking loop will continue trying.
		log.WithError(err).Warn("Failed to match run ID inline, tracking loop will retry")
	}

	log.WithField("job_id", job.ID).Info("Job dispatched successfully")

	return nil
}

// trackRunsLoop polls GitHub for workflow run status updates.
func (d *dispatcher) trackRunsLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.trackingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.trackRuns(ctx); err != nil {
				d.log.WithError(err).Error("Track runs failed")
			}
		}
	}
}

// trackRuns updates the status of triggered/running jobs.
func (d *dispatcher) trackRuns(ctx context.Context) error {
	// Get all triggered and running jobs.
	jobs, err := d.store.ListJobsByStatus(ctx, store.JobStatusTriggered, store.JobStatusRunning)
	if err != nil {
		return fmt.Errorf("listing jobs: %w", err)
	}

	// Build the set of already-claimed run IDs from the fetched jobs so that
	// trackJob won't assign the same GitHub run to multiple jobs.
	claimedRunIDs := make(map[int64]struct{}, len(jobs))

	for _, j := range jobs {
		if j.RunID != nil && *j.RunID != 0 {
			claimedRunIDs[*j.RunID] = struct{}{}
		}
	}

	for _, job := range jobs {
		if err := d.trackJob(ctx, job, claimedRunIDs); err != nil {
			d.log.WithError(err).WithField("job_id", job.ID).Error("Failed to track job")
		}
	}

	return nil
}

// trackJob updates the status of a single job.
// claimedRunIDs is the set of run IDs already assigned to other jobs in this tracking cycle.
func (d *dispatcher) trackJob(ctx context.Context, job *store.Job, claimedRunIDs map[int64]struct{}) error {
	log := d.log.WithField("job_id", job.ID)

	// Get the template to know which repo to query (may be nil for manual jobs).
	var template *store.JobTemplate

	if job.TemplateID != "" {
		var err error

		template, err = d.store.GetJobTemplate(ctx, job.TemplateID)
		if err != nil {
			return fmt.Errorf("getting job template: %w", err)
		}

		if template == nil {
			return fmt.Errorf("template not found: %s", job.TemplateID)
		}
	}

	// Get effective workflow parameters (job override or template default).
	owner, repo, workflowID, _ := getEffectiveWorkflowParams(job, template)

	// If we don't have a run ID, we need to find it.
	// Acquire the per-workflow lock to prevent races with the dispatch path
	// (waitForRunID) which also calls findWorkflowRun under the same lock.
	if job.RunID == nil || *job.RunID == 0 {
		workflowLock := d.getWorkflowLock(owner, repo, workflowID)
		workflowLock.Lock()

		runID, runURL, err := d.findWorkflowRun(ctx, owner, repo, workflowID, job, claimedRunIDs)

		if err != nil {
			workflowLock.Unlock()

			log.WithError(err).Debug("Could not find workflow run yet")

			// Check if the job has been triggered for too long without a run.
			// If so, mark it as failed.
			if job.TriggeredAt != nil && time.Since(*job.TriggeredAt) > 5*time.Minute {
				if markErr := d.queue.MarkFailed(ctx, job.ID, "Workflow run not found after 5 minutes"); markErr != nil {
					log.WithError(markErr).Error("Failed to mark job as failed")
				}
			}

			return nil
		}

		// Update the job with the run ID.
		job.RunID = &runID
		job.RunURL = runURL

		if err := d.store.UpdateJob(ctx, job); err != nil {
			workflowLock.Unlock()

			return fmt.Errorf("updating job with run ID: %w", err)
		}

		// Mark this run as claimed so other jobs in the same tracking cycle won't steal it.
		claimedRunIDs[runID] = struct{}{}

		workflowLock.Unlock()

		log.WithFields(logrus.Fields{
			"run_id":  runID,
			"run_url": runURL,
		}).Info("Found workflow run")
	}

	// Get the workflow run status.
	run, err := d.ghClient.GetWorkflowRun(ctx, owner, repo, *job.RunID)
	if err != nil {
		return fmt.Errorf("getting workflow run: %w", err)
	}

	// Update job status based on run status.
	switch run.Status {
	case "queued":
		// Still waiting, nothing to do.
		log.Debug("Workflow run is queued")

	case "in_progress":
		if job.Status == store.JobStatusTriggered {
			// Extract runner info from the workflow jobs.
			var runnerID int64

			var runnerName string

			jobs, err := d.ghClient.ListWorkflowRunJobs(ctx, owner, repo, *job.RunID)
			if err != nil {
				log.WithError(err).Warn("Failed to get workflow jobs for runner info")
			} else if len(jobs) > 0 {
				// Get runner info from the first job with a runner assigned.
				for _, j := range jobs {
					if j.RunnerID != 0 {
						runnerID = j.RunnerID
						runnerName = j.RunnerName

						break
					}
				}
			}

			if err := d.queue.MarkRunning(ctx, job.ID, runnerID, runnerName); err != nil {
				return fmt.Errorf("marking job as running: %w", err)
			}

			// Update runner busy status and notify.
			if runnerID != 0 {
				runner, err := d.store.GetRunner(ctx, runnerID)
				if err != nil {
					log.WithError(err).WithField("runner_id", runnerID).Warn("Failed to get runner by ID")
				} else if runner == nil {
					log.WithField("runner_id", runnerID).Warn("Runner not found by ID")
				} else if !runner.Busy {
					runner.Busy = true
					if err := d.store.UpsertRunner(ctx, runner); err != nil {
						log.WithError(err).Warn("Failed to update runner busy status")
					} else {
						d.notifyRunnerChange(runner)
					}
				}
			}

			log.WithFields(logrus.Fields{
				"runner_id":   runnerID,
				"runner_name": runnerName,
			}).Info("Job is now running")
		}

	case "completed":
		switch run.Conclusion {
		case "success":
			if err := d.queue.MarkCompleted(ctx, job.ID); err != nil {
				return fmt.Errorf("marking job as completed: %w", err)
			}

			log.Info("Job completed successfully")

		case "failure", "timed_out":
			if err := d.queue.MarkFailed(ctx, job.ID, fmt.Sprintf("Workflow %s", run.Conclusion)); err != nil {
				return fmt.Errorf("marking job as failed: %w", err)
			}

			log.WithField("conclusion", run.Conclusion).Info("Job failed")

		case "cancelled":
			if err := d.queue.MarkCancelled(ctx, job.ID); err != nil {
				return fmt.Errorf("marking job as cancelled: %w", err)
			}

			log.Info("Job was cancelled")

		default:
			log.WithField("conclusion", run.Conclusion).Warn("Unknown run conclusion")
		}
	}

	return nil
}

// buildClaimedRunIDs returns the set of run IDs currently assigned to triggered/running jobs.
// This is used to prevent multiple jobs from claiming the same GitHub workflow run.
func (d *dispatcher) buildClaimedRunIDs(ctx context.Context) (map[int64]struct{}, error) {
	jobs, err := d.store.ListJobsByStatus(ctx, store.JobStatusTriggered, store.JobStatusRunning)
	if err != nil {
		return nil, fmt.Errorf("listing jobs for claimed run IDs: %w", err)
	}

	claimed := make(map[int64]struct{}, len(jobs))

	for _, j := range jobs {
		if j.RunID != nil && *j.RunID != 0 {
			claimed[*j.RunID] = struct{}{}
		}
	}

	return claimed, nil
}

// findWorkflowRun searches for a recently created workflow run that matches our job.
// claimedRunIDs contains run IDs already assigned to other jobs; these are skipped.
// A nil map is safe and disables exclusion (degrades to previous behavior).
func (d *dispatcher) findWorkflowRun(
	ctx context.Context,
	owner, repo, workflowID string,
	job *store.Job,
	claimedRunIDs map[int64]struct{},
) (int64, string, error) {
	// We need to list recent workflow runs and find one that was created
	// around the time we triggered the job.
	// This is not ideal but workflow_dispatch doesn't return the run ID.

	if job.TriggeredAt == nil {
		return 0, "", fmt.Errorf("job has no triggered_at time")
	}

	// List recent workflow runs created after our trigger time.
	// Give a small buffer before the trigger time to account for clock drift.
	searchTime := job.TriggeredAt.Add(-30 * time.Second)

	runs, err := d.ghClient.ListWorkflowRuns(ctx, owner, repo, workflowID, github.ListWorkflowRunsOpts{
		Event:     "workflow_dispatch",
		CreatedAt: &searchTime,
		PerPage:   10,
	})
	if err != nil {
		return 0, "", fmt.Errorf("listing workflow runs: %w", err)
	}

	if len(runs) == 0 {
		return 0, "", fmt.Errorf("no workflow runs found")
	}

	// Find the oldest unclaimed run created after our trigger time.
	// Dispatches are serialized by the per-workflow lock, so the oldest
	// unclaimed run after the trigger time is the most likely match.
	var bestRun *github.WorkflowRun

	for i, run := range runs {
		// The run must have been created after we triggered (minus buffer).
		if run.CreatedAt.Before(searchTime) {
			continue
		}

		// Skip runs already claimed by other jobs.
		if _, claimed := claimedRunIDs[run.ID]; claimed {
			continue
		}

		// Take the oldest unclaimed matching run.
		if bestRun == nil || run.CreatedAt.Before(bestRun.CreatedAt) {
			bestRun = runs[i]
		}
	}

	if bestRun == nil {
		return 0, "", fmt.Errorf("no matching workflow run found")
	}

	return bestRun.ID, bestRun.HTMLURL, nil
}
