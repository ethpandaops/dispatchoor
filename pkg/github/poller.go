package github

import (
	"context"
	"sync"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
)

// RunnerChangeCallback is called when runner status changes.
type RunnerChangeCallback func(runner *store.Runner)

// Poller periodically fetches runner status from GitHub.
type Poller interface {
	Start(ctx context.Context) error
	Stop() error
	ForceRefresh(ctx context.Context) error
	SetRunnerChangeCallback(cb RunnerChangeCallback)
}

// poller implements Poller.
type poller struct {
	log                  logrus.FieldLogger
	cfg                  *config.Config
	client               Client
	store                store.Store
	metrics              Metrics
	interval             time.Duration
	rateLimitBuffer      int
	cancel               context.CancelFunc
	wg                   sync.WaitGroup
	mu                   sync.Mutex
	lastPoll             time.Time
	runnerChangeCallback RunnerChangeCallback
}

// Metrics interface for rate limit tracking.
type Metrics interface {
	SetGitHubRateLimit(remaining float64)
}

// Ensure poller implements Poller.
var _ Poller = (*poller)(nil)

// NewPoller creates a new runner poller.
func NewPoller(
	log logrus.FieldLogger,
	cfg *config.Config,
	client Client,
	st store.Store,
	m Metrics,
) Poller {
	return &poller{
		log:             log.WithField("component", "poller"),
		cfg:             cfg,
		client:          client,
		store:           st,
		metrics:         m,
		interval:        cfg.GitHub.PollInterval,
		rateLimitBuffer: cfg.GitHub.RateLimitBuffer,
	}
}

// Start begins the polling loop.
func (p *poller) Start(ctx context.Context) error {
	p.log.WithField("interval", p.interval).Info("Starting runner poller")

	ctx, p.cancel = context.WithCancel(ctx)

	// Do an initial poll.
	if err := p.poll(ctx); err != nil {
		p.log.WithError(err).Warn("Initial poll failed")
	}

	// Start the polling loop.
	p.wg.Add(1)

	go p.loop(ctx)

	return nil
}

// Stop stops the polling loop.
func (p *poller) Stop() error {
	p.log.Info("Stopping runner poller")

	if p.cancel != nil {
		p.cancel()
	}

	p.wg.Wait()

	return nil
}

// ForceRefresh triggers an immediate poll.
func (p *poller) ForceRefresh(ctx context.Context) error {
	p.log.Info("Force refreshing runners")

	return p.poll(ctx)
}

// SetRunnerChangeCallback sets the callback for runner status changes.
func (p *poller) SetRunnerChangeCallback(cb RunnerChangeCallback) {
	p.runnerChangeCallback = cb
}

// notifyRunnerChange calls the callback if set.
func (p *poller) notifyRunnerChange(runner *store.Runner) {
	if p.runnerChangeCallback != nil {
		p.runnerChangeCallback(runner)
	}
}

// loop runs the polling loop.
func (p *poller) loop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.log.WithError(err).Error("Poll failed")
			}
		}
	}
}

// poll fetches runner status from GitHub and updates the store.
func (p *poller) poll(ctx context.Context) error {
	p.mu.Lock()
	p.lastPoll = time.Now()
	p.mu.Unlock()

	// Check rate limit before polling.
	remaining := p.client.RateLimitRemaining()
	p.metrics.SetGitHubRateLimit(float64(remaining))

	if remaining < p.rateLimitBuffer {
		resetAt := p.client.RateLimitReset()
		p.log.WithFields(logrus.Fields{
			"remaining": remaining,
			"buffer":    p.rateLimitBuffer,
			"reset_at":  resetAt,
		}).Warn("Rate limit too low, skipping poll")

		return nil
	}

	// Get current runner state before polling (for change detection).
	existingRunners, err := p.store.ListRunners(ctx)
	if err != nil {
		p.log.WithError(err).Warn("Failed to list existing runners for change detection")
	}

	previousState := make(map[int64]struct {
		Status store.RunnerStatus
		Busy   bool
	}, len(existingRunners))

	for _, r := range existingRunners {
		previousState[r.ID] = struct {
			Status store.RunnerStatus
			Busy   bool
		}{r.Status, r.Busy}
	}

	// Collect unique orgs/repos from groups to poll.
	orgs := make(map[string]bool)

	for _, group := range p.cfg.Groups.GitHub {
		for _, tmpl := range group.WorkflowDispatchTemplates {
			// For now, assume runners are at org level.
			// Could be extended to support repo-level runners.
			orgs[tmpl.Owner] = true
		}
	}

	p.log.WithField("orgs", len(orgs)).Debug("Polling runners")

	// Poll each org.
	var allRunners []*Runner

	for org := range orgs {
		runners, err := p.client.ListOrgRunners(ctx, org)
		if err != nil {
			p.log.WithError(err).WithField("org", org).Error("Failed to list org runners")

			continue
		}

		allRunners = append(allRunners, runners...)
	}

	p.log.WithField("count", len(allRunners)).Debug("Fetched runners from GitHub")

	// Update store with runner status.
	now := time.Now()

	for _, r := range allRunners {
		status := store.RunnerStatusOnline
		if r.Status != "online" {
			status = store.RunnerStatusOffline
		}

		runner := &store.Runner{
			ID:         r.ID,
			Name:       r.Name,
			Labels:     r.Labels,
			Status:     status,
			Busy:       r.Busy,
			OS:         r.OS,
			LastSeenAt: now,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := p.store.UpsertRunner(ctx, runner); err != nil {
			p.log.WithError(err).WithField("runner", r.Name).Error("Failed to upsert runner")

			continue
		}

		// Check if runner state changed and notify.
		prev, existed := previousState[r.ID]
		if !existed || prev.Status != status || prev.Busy != r.Busy {
			p.log.WithFields(logrus.Fields{
				"runner":      r.Name,
				"status":      status,
				"busy":        r.Busy,
				"prev_status": prev.Status,
				"prev_busy":   prev.Busy,
				"new":         !existed,
			}).Debug("Runner state changed")

			p.notifyRunnerChange(runner)
		}
	}

	// Clean up stale runners (not seen in 24 hours).
	staleThreshold := now.Add(-24 * time.Hour)
	if err := p.store.DeleteStaleRunners(ctx, staleThreshold); err != nil {
		p.log.WithError(err).Error("Failed to delete stale runners")
	}

	// Update rate limit metric after poll.
	remaining = p.client.RateLimitRemaining()
	p.metrics.SetGitHubRateLimit(float64(remaining))

	p.log.WithFields(logrus.Fields{
		"runners":        len(allRunners),
		"rate_remaining": remaining,
	}).Info("Poll completed")

	return nil
}
