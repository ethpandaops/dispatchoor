package queue

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// newTestService creates a queue service on a temporary SQLite database.
// It returns the service and the ID of a group that jobs can go into.
func newTestService(t *testing.T) (Service, string) {
	t.Helper()

	ctx := context.Background()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	st := store.NewSQLiteStore(log, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, st.Start(ctx))
	require.NoError(t, st.Migrate(ctx))

	t.Cleanup(func() {
		require.NoError(t, st.Stop())
	})

	group := &store.Group{
		ID:           "group-1",
		Name:         "group-1",
		RunnerLabels: []string{"self-hosted"},
		Enabled:      true,
	}
	require.NoError(t, st.CreateGroup(ctx, group))

	return NewService(log, &config.Config{}, st), group.ID
}

// enqueueManualJob adds a manual job to the queue.
func enqueueManualJob(t *testing.T, svc Service, groupID string, autoRequeue bool) *store.Job {
	t.Helper()

	job, err := svc.Enqueue(context.Background(), groupID, "", "tester", nil, &EnqueueOptions{
		AutoRequeue: autoRequeue,
		Name:        "test-job",
		Owner:       "ethpandaops",
		Repo:        "dispatchoor",
		WorkflowID:  "test.yaml",
		Ref:         "master",
	})
	require.NoError(t, err)

	return job
}

// runJob moves a job to the running status.
func runJob(t *testing.T, svc Service, jobID string) {
	t.Helper()

	ctx := context.Background()

	require.NoError(t, svc.MarkTriggered(ctx, jobID, 1234, "https://example.com/run/1234"))
	require.NoError(t, svc.MarkRunning(ctx, jobID, 1, "runner-1"))
}

// requeuedJob returns the pending job that auto-requeue created.
func requeuedJob(t *testing.T, svc Service, groupID, originalJobID string) *store.Job {
	t.Helper()

	pending, err := svc.ListPending(context.Background(), groupID)
	require.NoError(t, err)

	for _, job := range pending {
		if job.ID != originalJobID {
			return job
		}
	}

	t.Fatalf("no requeued job found for %s", originalJobID)

	return nil
}

func TestPauseAndUnpauseStatuses(t *testing.T) {
	tests := []struct {
		name        string
		autoRequeue bool
		run         bool
		wantErr     string
	}{
		{
			name: "pending job",
		},
		{
			name:        "pending job with auto-requeue",
			autoRequeue: true,
		},
		{
			name:        "running job with auto-requeue",
			autoRequeue: true,
			run:         true,
		},
		{
			name:    "running job without auto-requeue",
			run:     true,
			wantErr: "cannot pause a running job without auto-requeue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			svc, groupID := newTestService(t)

			job := enqueueManualJob(t, svc, groupID, tt.autoRequeue)
			if tt.run {
				runJob(t, svc, job.ID)
			}

			paused, err := svc.Pause(ctx, job.ID)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				_, err = svc.Unpause(ctx, job.ID)
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.True(t, paused.Paused)

			// A pause never changes the status of the job.
			if tt.run {
				require.Equal(t, store.JobStatusRunning, paused.Status)
			}

			resumed, err := svc.Unpause(ctx, job.ID)
			require.NoError(t, err)
			require.False(t, resumed.Paused)
		})
	}
}

func TestPauseRunningJobRequeuesPaused(t *testing.T) {
	ctx := context.Background()

	svc, groupID := newTestService(t)

	job := enqueueManualJob(t, svc, groupID, true)
	runJob(t, svc, job.ID)

	_, err := svc.Pause(ctx, job.ID)
	require.NoError(t, err)

	require.NoError(t, svc.MarkCompleted(ctx, job.ID))

	requeued := requeuedJob(t, svc, groupID, job.ID)
	require.True(t, requeued.Paused)
	require.True(t, requeued.AutoRequeue)
	require.Equal(t, 1, requeued.RequeueCount)

	// The dispatcher gets nothing, because the requeued job is paused.
	next, err := svc.Peek(ctx, groupID)
	require.NoError(t, err)
	require.Nil(t, next)

	// A resume puts the requeued job back in front of the dispatcher.
	_, err = svc.Unpause(ctx, requeued.ID)
	require.NoError(t, err)

	next, err = svc.Peek(ctx, groupID)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Equal(t, requeued.ID, next.ID)
}

func TestUnpauseRunningJobRequeuesUnpaused(t *testing.T) {
	ctx := context.Background()

	svc, groupID := newTestService(t)

	job := enqueueManualJob(t, svc, groupID, true)
	runJob(t, svc, job.ID)

	_, err := svc.Pause(ctx, job.ID)
	require.NoError(t, err)

	_, err = svc.Unpause(ctx, job.ID)
	require.NoError(t, err)

	require.NoError(t, svc.MarkCompleted(ctx, job.ID))

	requeued := requeuedJob(t, svc, groupID, job.ID)
	require.False(t, requeued.Paused)
}

func TestDisableAutoRequeueClearsPauseOnRunningJob(t *testing.T) {
	tests := []struct {
		name    string
		disable func(t *testing.T, svc Service, jobID string) *store.Job
	}{
		{
			name: "DisableAutoRequeue",
			disable: func(t *testing.T, svc Service, jobID string) *store.Job {
				t.Helper()

				job, err := svc.DisableAutoRequeue(context.Background(), jobID)
				require.NoError(t, err)

				return job
			},
		},
		{
			name: "UpdateAutoRequeue",
			disable: func(t *testing.T, svc Service, jobID string) *store.Job {
				t.Helper()

				job, err := svc.UpdateAutoRequeue(context.Background(), jobID, false, nil)
				require.NoError(t, err)

				return job
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, groupID := newTestService(t)

			job := enqueueManualJob(t, svc, groupID, true)
			runJob(t, svc, job.ID)

			_, err := svc.Pause(context.Background(), job.ID)
			require.NoError(t, err)

			// The pause only applied to the requeue, so it goes away with it.
			updated := tt.disable(t, svc, job.ID)
			require.False(t, updated.AutoRequeue)
			require.False(t, updated.Paused)
		})
	}
}

func TestDisableAutoRequeueKeepsPauseOnPendingJob(t *testing.T) {
	ctx := context.Background()

	svc, groupID := newTestService(t)

	job := enqueueManualJob(t, svc, groupID, true)

	_, err := svc.Pause(ctx, job.ID)
	require.NoError(t, err)

	updated, err := svc.DisableAutoRequeue(ctx, job.ID)
	require.NoError(t, err)
	require.False(t, updated.AutoRequeue)
	require.True(t, updated.Paused)
}

func TestPauseCompletedJobFails(t *testing.T) {
	ctx := context.Background()

	svc, groupID := newTestService(t)

	job := enqueueManualJob(t, svc, groupID, false)
	runJob(t, svc, job.ID)
	require.NoError(t, svc.MarkCompleted(ctx, job.ID))

	_, err := svc.Pause(ctx, job.ID)
	require.ErrorContains(t, err, "cannot pause job with status completed")
}
