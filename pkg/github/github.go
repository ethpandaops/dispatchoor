package github

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// Client defines the interface for GitHub API operations.
type Client interface {
	Start(ctx context.Context) error
	Stop() error

	// Connection status.
	IsConnected() bool
	ConnectionError() string

	// Runners.
	ListOrgRunners(ctx context.Context, org string) ([]*Runner, error)
	ListRepoRunners(ctx context.Context, owner, repo string) ([]*Runner, error)

	// Workflows.
	TriggerWorkflowDispatch(
		ctx context.Context,
		owner, repo, workflowID, ref string,
		inputs map[string]string,
	) error
	GetWorkflowRun(ctx context.Context, owner, repo string, runID int64) (*WorkflowRun, error)
	ListWorkflowRuns(ctx context.Context, owner, repo, workflowID string, opts ListWorkflowRunsOpts) ([]*WorkflowRun, error)
	ListWorkflowRunJobs(ctx context.Context, owner, repo string, runID int64) ([]*WorkflowJob, error)
	CancelWorkflowRun(ctx context.Context, owner, repo string, runID int64) error

	// Rate limiting.
	RateLimitRemaining() int
	RateLimitReset() time.Time
}

// ListWorkflowRunsOpts contains options for listing workflow runs.
type ListWorkflowRunsOpts struct {
	Branch    string
	Event     string
	Status    string
	CreatedAt *time.Time
	PerPage   int
}

// Runner represents a GitHub Actions runner.
type Runner struct {
	ID     int64
	Name   string
	OS     string
	Status string // online, offline
	Busy   bool
	Labels []string
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID         int64
	Name       string
	Status     string // queued, in_progress, completed
	Conclusion string // success, failure, cancelled, etc.
	HTMLURL    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// WorkflowJob represents a job within a GitHub Actions workflow run.
type WorkflowJob struct {
	ID         int64
	Name       string
	Status     string // queued, in_progress, completed
	Conclusion string // success, failure, cancelled, etc.
	RunnerID   int64
	RunnerName string
	StartedAt  time.Time
}

// client implements Client.
type client struct {
	log             logrus.FieldLogger
	token           string
	gh              *github.Client
	mu              sync.RWMutex
	rateRemaining   int
	rateReset       time.Time
	connected       bool
	connectionError string
}

// Ensure client implements Client.
var _ Client = (*client)(nil)

// NewClient creates a new GitHub client.
func NewClient(log logrus.FieldLogger, token string) Client {
	return &client{
		log:   log.WithField("component", "github"),
		token: token,
	}
}

// Start initializes the GitHub client.
// If authentication fails, the client will be marked as disconnected but no error is returned.
// Use IsConnected() and ConnectionError() to check the connection status.
func (c *client) Start(ctx context.Context) error {
	c.log.Info("Initializing GitHub client")

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: c.token})
	tc := oauth2.NewClient(ctx, ts)

	c.gh = github.NewClient(tc)

	// Test authentication by getting rate limit.
	rate, _, err := c.gh.RateLimit.Get(ctx)
	if err != nil {
		c.mu.Lock()
		c.connected = false
		c.connectionError = fmt.Sprintf("authentication failed: %v", err)
		c.mu.Unlock()

		c.log.WithError(err).Warn("GitHub authentication failed - client will operate in disconnected mode")

		return nil
	}

	c.mu.Lock()
	c.rateRemaining = rate.Core.Remaining
	c.rateReset = rate.Core.Reset.Time
	c.connected = true
	c.connectionError = ""
	c.mu.Unlock()

	c.log.WithFields(logrus.Fields{
		"rate_remaining": rate.Core.Remaining,
		"rate_limit":     rate.Core.Limit,
		"rate_reset":     rate.Core.Reset.Time,
	}).Info("GitHub client initialized")

	return nil
}

// Stop shuts down the GitHub client.
func (c *client) Stop() error {
	c.log.Info("Stopping GitHub client")

	return nil
}

// updateRateLimit updates rate limit info from response headers.
func (c *client) updateRateLimit(resp *github.Response) {
	if resp == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.rateRemaining = resp.Rate.Remaining
	c.rateReset = resp.Rate.Reset.Time
}

// RateLimitRemaining returns the remaining API calls.
func (c *client) RateLimitRemaining() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.rateRemaining
}

// RateLimitReset returns when the rate limit resets.
func (c *client) RateLimitReset() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.rateReset
}

// IsConnected returns true if the GitHub client is connected and authenticated.
func (c *client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.connected
}

// ConnectionError returns the connection error message, if any.
func (c *client) ConnectionError() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.connectionError
}

// ListOrgRunners lists all self-hosted runners for an organization.
func (c *client) ListOrgRunners(ctx context.Context, org string) ([]*Runner, error) {
	c.log.WithField("org", org).Debug("Listing organization runners")

	var allRunners []*Runner

	opts := &github.ListOptions{PerPage: 100}

	for {
		runners, resp, err := c.gh.Actions.ListOrganizationRunners(ctx, org, opts)
		if err != nil {
			return nil, fmt.Errorf("listing org runners: %w", err)
		}

		c.updateRateLimit(resp)

		for _, r := range runners.Runners {
			allRunners = append(allRunners, convertRunner(r))
		}

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	c.log.WithFields(logrus.Fields{
		"org":   org,
		"count": len(allRunners),
	}).Debug("Listed organization runners")

	return allRunners, nil
}

// ListRepoRunners lists all self-hosted runners for a repository.
func (c *client) ListRepoRunners(ctx context.Context, owner, repo string) ([]*Runner, error) {
	c.log.WithFields(logrus.Fields{
		"owner": owner,
		"repo":  repo,
	}).Debug("Listing repository runners")

	var allRunners []*Runner

	opts := &github.ListOptions{PerPage: 100}

	for {
		runners, resp, err := c.gh.Actions.ListRunners(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("listing repo runners: %w", err)
		}

		c.updateRateLimit(resp)

		for _, r := range runners.Runners {
			allRunners = append(allRunners, convertRunner(r))
		}

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	c.log.WithFields(logrus.Fields{
		"owner": owner,
		"repo":  repo,
		"count": len(allRunners),
	}).Debug("Listed repository runners")

	return allRunners, nil
}

// convertRunner converts a GitHub runner to our Runner type.
func convertRunner(r *github.Runner) *Runner {
	labels := make([]string, 0, len(r.Labels))
	for _, l := range r.Labels {
		if l.Name != nil {
			labels = append(labels, *l.Name)
		}
	}

	runner := &Runner{
		ID:     r.GetID(),
		Name:   r.GetName(),
		OS:     r.GetOS(),
		Status: r.GetStatus(),
		Busy:   r.GetBusy(),
		Labels: labels,
	}

	return runner
}

// TriggerWorkflowDispatch triggers a workflow_dispatch event.
func (c *client) TriggerWorkflowDispatch(
	ctx context.Context,
	owner, repo, workflowID, ref string,
	inputs map[string]string,
) error {
	c.log.WithFields(logrus.Fields{
		"owner":    owner,
		"repo":     repo,
		"workflow": workflowID,
		"ref":      ref,
	}).Info("Triggering workflow dispatch")

	// Convert inputs to interface{} map as required by the API.
	inputsMap := make(map[string]interface{}, len(inputs))
	for k, v := range inputs {
		inputsMap[k] = v
	}

	event := github.CreateWorkflowDispatchEventRequest{
		Ref:    ref,
		Inputs: inputsMap,
	}

	resp, err := c.gh.Actions.CreateWorkflowDispatchEventByFileName(ctx, owner, repo, workflowID, event)
	if err != nil {
		return fmt.Errorf("triggering workflow dispatch: %w", err)
	}

	c.updateRateLimit(resp)

	// workflow_dispatch returns 204 No Content on success.
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	c.log.WithFields(logrus.Fields{
		"owner":    owner,
		"repo":     repo,
		"workflow": workflowID,
	}).Info("Workflow dispatch triggered successfully")

	return nil
}

// GetWorkflowRun gets details of a workflow run.
func (c *client) GetWorkflowRun(ctx context.Context, owner, repo string, runID int64) (*WorkflowRun, error) {
	c.log.WithFields(logrus.Fields{
		"owner":  owner,
		"repo":   repo,
		"run_id": runID,
	}).Debug("Getting workflow run")

	run, resp, err := c.gh.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
	if err != nil {
		return nil, fmt.Errorf("getting workflow run: %w", err)
	}

	c.updateRateLimit(resp)

	return &WorkflowRun{
		ID:         run.GetID(),
		Name:       run.GetName(),
		Status:     run.GetStatus(),
		Conclusion: run.GetConclusion(),
		HTMLURL:    run.GetHTMLURL(),
		CreatedAt:  run.GetCreatedAt().Time,
		UpdatedAt:  run.GetUpdatedAt().Time,
	}, nil
}

// ListWorkflowRuns lists workflow runs for a specific workflow.
func (c *client) ListWorkflowRuns(
	ctx context.Context,
	owner, repo, workflowID string,
	opts ListWorkflowRunsOpts,
) ([]*WorkflowRun, error) {
	c.log.WithFields(logrus.Fields{
		"owner":    owner,
		"repo":     repo,
		"workflow": workflowID,
	}).Debug("Listing workflow runs")

	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 10
	}

	listOpts := &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: perPage},
	}

	if opts.Branch != "" {
		listOpts.Branch = opts.Branch
	}

	if opts.Event != "" {
		listOpts.Event = opts.Event
	}

	if opts.Status != "" {
		listOpts.Status = opts.Status
	}

	if opts.CreatedAt != nil {
		listOpts.Created = ">=" + opts.CreatedAt.Format(time.RFC3339)
	}

	runs, resp, err := c.gh.Actions.ListWorkflowRunsByFileName(ctx, owner, repo, workflowID, listOpts)
	if err != nil {
		return nil, fmt.Errorf("listing workflow runs: %w", err)
	}

	c.updateRateLimit(resp)

	result := make([]*WorkflowRun, 0, len(runs.WorkflowRuns))

	for _, run := range runs.WorkflowRuns {
		result = append(result, &WorkflowRun{
			ID:         run.GetID(),
			Name:       run.GetName(),
			Status:     run.GetStatus(),
			Conclusion: run.GetConclusion(),
			HTMLURL:    run.GetHTMLURL(),
			CreatedAt:  run.GetCreatedAt().Time,
			UpdatedAt:  run.GetUpdatedAt().Time,
		})
	}

	c.log.WithFields(logrus.Fields{
		"owner":    owner,
		"repo":     repo,
		"workflow": workflowID,
		"count":    len(result),
	}).Debug("Listed workflow runs")

	return result, nil
}

// ListWorkflowRunJobs lists jobs for a specific workflow run.
func (c *client) ListWorkflowRunJobs(ctx context.Context, owner, repo string, runID int64) ([]*WorkflowJob, error) {
	c.log.WithFields(logrus.Fields{
		"owner":  owner,
		"repo":   repo,
		"run_id": runID,
	}).Debug("Listing workflow run jobs")

	var allJobs []*WorkflowJob

	opts := &github.ListWorkflowJobsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		jobs, resp, err := c.gh.Actions.ListWorkflowJobs(ctx, owner, repo, runID, opts)
		if err != nil {
			return nil, fmt.Errorf("listing workflow jobs: %w", err)
		}

		c.updateRateLimit(resp)

		for _, job := range jobs.Jobs {
			wj := &WorkflowJob{
				ID:         job.GetID(),
				Name:       job.GetName(),
				Status:     job.GetStatus(),
				Conclusion: job.GetConclusion(),
				RunnerID:   job.GetRunnerID(),
				RunnerName: job.GetRunnerName(),
			}

			if job.StartedAt != nil {
				wj.StartedAt = job.StartedAt.Time
			}

			allJobs = append(allJobs, wj)
		}

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	c.log.WithFields(logrus.Fields{
		"owner":  owner,
		"repo":   repo,
		"run_id": runID,
		"count":  len(allJobs),
	}).Debug("Listed workflow run jobs")

	return allJobs, nil
}

// CancelWorkflowRun cancels a workflow run.
func (c *client) CancelWorkflowRun(ctx context.Context, owner, repo string, runID int64) error {
	c.log.WithFields(logrus.Fields{
		"owner":  owner,
		"repo":   repo,
		"run_id": runID,
	}).Info("Cancelling workflow run")

	resp, err := c.gh.Actions.CancelWorkflowRunByID(ctx, owner, repo, runID)
	if err != nil {
		return fmt.Errorf("cancelling workflow run: %w", err)
	}

	c.updateRateLimit(resp)

	c.log.WithFields(logrus.Fields{
		"owner":  owner,
		"repo":   repo,
		"run_id": runID,
	}).Info("Workflow run cancelled successfully")

	return nil
}
