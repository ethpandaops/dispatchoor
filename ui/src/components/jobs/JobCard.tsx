import { useState, useEffect } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { Job, JobTemplate } from '../../types';
import { api } from '../../api/client';
import { useAuthStore } from '../../stores/authStore';

interface JobCardProps {
  job: Job;
  template?: JobTemplate;
  isDragging?: boolean;
  dragHandleProps?: Record<string, unknown>;
}

const statusColors: Record<string, { bg: string; text: string; dot: string }> = {
  pending: { bg: 'bg-amber-500/10', text: 'text-amber-400', dot: 'bg-amber-400' },
  triggered: { bg: 'bg-blue-500/10', text: 'text-blue-400', dot: 'bg-blue-400' },
  running: { bg: 'bg-green-500/10', text: 'text-green-400', dot: 'bg-green-400 animate-pulse' },
  completed: { bg: 'bg-emerald-500/10', text: 'text-emerald-400', dot: 'bg-emerald-400' },
  failed: { bg: 'bg-red-500/10', text: 'text-red-400', dot: 'bg-red-400' },
  cancelled: { bg: 'bg-zinc-500/10', text: 'text-zinc-400', dot: 'bg-zinc-400' },
};

export function JobCard({ job, template, isDragging, dragHandleProps }: JobCardProps) {
  const { user } = useAuthStore();
  const queryClient = useQueryClient();
  const isAdmin = user?.role === 'admin';
  const [showStopRequeueConfirm, setShowStopRequeueConfirm] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  const deleteMutation = useMutation({
    mutationFn: () => api.deleteJob(job.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });

  const pauseMutation = useMutation({
    mutationFn: () => api.pauseJob(job.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
    },
  });

  const unpauseMutation = useMutation({
    mutationFn: () => api.unpauseJob(job.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
    },
  });

  const disableRequeueMutation = useMutation({
    mutationFn: () => api.disableAutoRequeue(job.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
    },
  });

  const toggleRequeueMutation = useMutation({
    mutationFn: () => api.updateAutoRequeue(job.id, !job.auto_requeue, job.requeue_limit),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
    },
  });

  // Keyboard shortcuts for disable requeue confirmation modal
  useEffect(() => {
    if (!showStopRequeueConfirm) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setShowStopRequeueConfirm(false);
      } else if (e.key === 'Enter') {
        const isRunningOrTriggered = job.status === 'running' || job.status === 'triggered';
        const isPending = !isRunningOrTriggered && !toggleRequeueMutation.isPending;
        const canDisable = isRunningOrTriggered && !disableRequeueMutation.isPending;

        if (isPending) {
          toggleRequeueMutation.mutate();
          setShowStopRequeueConfirm(false);
        } else if (canDisable) {
          disableRequeueMutation.mutate();
          setShowStopRequeueConfirm(false);
        }
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [showStopRequeueConfirm, disableRequeueMutation, toggleRequeueMutation, job.status]);

  // Keyboard shortcuts for delete confirmation modal
  useEffect(() => {
    if (!showDeleteConfirm) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setShowDeleteConfirm(false);
      } else if (e.key === 'Enter' && !deleteMutation.isPending) {
        deleteMutation.mutate();
        setShowDeleteConfirm(false);
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [showDeleteConfirm, deleteMutation]);

  const colors = statusColors[job.status] || statusColors.pending;

  const getRequeueCountDisplay = () => {
    if (!job.auto_requeue && job.requeue_count === 0) return null;
    if (job.requeue_limit !== null) {
      return `${job.requeue_count}/${job.requeue_limit}`;
    }
    return job.requeue_count > 0 ? `${job.requeue_count}/∞` : '∞';
  };

  const formatTime = (dateStr: string | null) => {
    if (!dateStr) return null;
    const date = new Date(dateStr);
    return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
  };

  const getElapsedTime = () => {
    if (!job.triggered_at) return null;
    const start = new Date(job.triggered_at).getTime();
    const end = job.completed_at ? new Date(job.completed_at).getTime() : Date.now();
    const seconds = Math.floor((end - start) / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    if (hours > 0) return `${hours}h ${minutes % 60}m`;
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`;
    return `${seconds}s`;
  };

  return (
    <div
      className={`rounded-sm border border-zinc-800 bg-zinc-900 p-4 transition-shadow ${
        isDragging ? 'shadow-lg ring-2 ring-blue-500' : ''
      }`}
    >
      <div className="flex items-start gap-3">
        {/* Drag handle */}
        {isAdmin && job.status === 'pending' && (
          <button
            {...dragHandleProps}
            className="mt-1 cursor-grab text-zinc-600 hover:text-zinc-400 active:cursor-grabbing"
          >
            <svg className="size-4" fill="currentColor" viewBox="0 0 24 24">
              <path d="M8 6a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 8a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 8a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm8-16a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 8a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 8a2 2 0 1 0 0-4 2 2 0 0 0 0 4z" />
            </svg>
          </button>
        )}

        <div className="flex-1 min-w-0">
          {/* Header */}
          <div className="flex items-center gap-2 mb-2">
            <span className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-xs font-medium ${job.paused ? 'bg-zinc-500/10 text-zinc-400' : colors.bg + ' ' + colors.text}`}>
              <span className={`size-1.5 rounded-full ${job.paused ? 'bg-zinc-400' : colors.dot}`} />
              {job.paused ? 'paused' : job.status}
            </span>
            <span className="text-xs text-zinc-500">#{job.position}</span>
            {(job.auto_requeue || job.requeue_count > 0) && (
              <span className="inline-flex items-center gap-1 rounded-sm bg-purple-500/10 px-2 py-0.5 text-xs font-medium text-purple-400" title="Auto-requeue enabled">
                <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                {getRequeueCountDisplay()}
              </span>
            )}
          </div>

          {/* Job name */}
          <h4 className="text-sm font-medium text-zinc-200 truncate">
            {template?.name || job.template_id}
          </h4>

          {/* Metadata */}
          <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-zinc-500">
            {template && (
              <span className="flex items-center gap-1">
                <svg className="size-3.5" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/>
                </svg>
                {template.owner}/{template.repo}
              </span>
            )}
            <span className="flex items-center gap-1" title="Created at">
              <svg className="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              {formatTime(job.created_at)}
            </span>
            {job.triggered_at && (
              <span className="flex items-center gap-1" title="Started at">
                <svg className="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 3l14 9-14 9V3z" />
                </svg>
                {formatTime(job.triggered_at)}
              </span>
            )}
            {getElapsedTime() && (
              <span className="flex items-center gap-1">
                <svg className="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
                </svg>
                {getElapsedTime()}
              </span>
            )}
            {job.runner_name && (
              <span className="flex items-center gap-1">
                <svg className="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2" />
                </svg>
                {job.runner_name}
              </span>
            )}
          </div>

          {/* Error message */}
          {job.error_message && (
            <div className="mt-2 rounded-sm bg-red-500/10 px-2 py-1 text-xs text-red-400">
              {job.error_message}
            </div>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1">
          {/* Auto-requeue toggle for running/triggered jobs - appears before GitHub link */}
          {isAdmin && (job.status === 'triggered' || job.status === 'running') && (
            <button
              onClick={() => {
                if (job.auto_requeue) {
                  setShowStopRequeueConfirm(true);
                } else {
                  toggleRequeueMutation.mutate();
                }
              }}
              disabled={toggleRequeueMutation.isPending || disableRequeueMutation.isPending}
              className={`rounded-sm p-1.5 text-zinc-500 disabled:opacity-50 ${
                job.auto_requeue
                  ? 'bg-purple-500/10 text-purple-400 hover:bg-purple-500/20'
                  : 'hover:bg-purple-500/10 hover:text-purple-400'
              }`}
              title={job.auto_requeue ? 'Disable auto-requeue' : 'Enable auto-requeue'}
            >
              <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            </button>
          )}
          {job.run_url && (
            <a
              href={job.run_url}
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-sm p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-300"
              title="View run on GitHub"
            >
              <svg className="size-4" fill="currentColor" viewBox="0 0 24 24">
                <path d="M14 3v2h3.59l-9.83 9.83 1.41 1.41L19 6.41V10h2V3h-7z"/>
                <path d="M5 5v14h14v-7h-2v5H7V7h5V5H5z"/>
              </svg>
            </a>
          )}
          {isAdmin && job.status === 'pending' && !job.paused && (
            <button
              onClick={() => pauseMutation.mutate()}
              disabled={pauseMutation.isPending}
              className="rounded-sm p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-300 disabled:opacity-50"
              title="Pause job"
            >
              <svg className="size-4" fill="currentColor" viewBox="0 0 24 24">
                <path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/>
              </svg>
            </button>
          )}
          {isAdmin && job.status === 'pending' && job.paused && (
            <button
              onClick={() => unpauseMutation.mutate()}
              disabled={unpauseMutation.isPending}
              className="rounded-sm p-1.5 text-zinc-500 hover:bg-green-500/10 hover:text-green-400 disabled:opacity-50"
              title="Resume job"
            >
              <svg className="size-4" fill="currentColor" viewBox="0 0 24 24">
                <path d="M8 5v14l11-7z"/>
              </svg>
            </button>
          )}
          {/* Auto-requeue toggle for pending jobs */}
          {isAdmin && job.status === 'pending' && (
            <button
              onClick={() => {
                // Show confirmation modal when disabling auto-requeue
                if (job.auto_requeue) {
                  setShowStopRequeueConfirm(true);
                } else {
                  toggleRequeueMutation.mutate();
                }
              }}
              disabled={toggleRequeueMutation.isPending || disableRequeueMutation.isPending}
              className={`rounded-sm p-1.5 text-zinc-500 disabled:opacity-50 ${
                job.auto_requeue
                  ? 'bg-purple-500/10 text-purple-400 hover:bg-purple-500/20'
                  : 'hover:bg-purple-500/10 hover:text-purple-400'
              }`}
              title={job.auto_requeue ? 'Disable auto-requeue' : 'Enable auto-requeue'}
            >
              <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            </button>
          )}
          {isAdmin && (job.status === 'pending' || job.status === 'failed') && (
            <button
              onClick={() => setShowDeleteConfirm(true)}
              disabled={deleteMutation.isPending}
              className="rounded-sm p-1.5 text-zinc-500 hover:bg-red-500/10 hover:text-red-400 disabled:opacity-50"
              title="Remove job"
            >
              <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </button>
          )}
        </div>
      </div>

      {/* Disable auto-requeue confirmation modal */}
      {showStopRequeueConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60" onClick={() => setShowStopRequeueConfirm(false)} />
          <div className="relative w-full max-w-sm mx-4 rounded-sm border border-zinc-800 bg-zinc-900 shadow-xl">
            <div className="p-4">
              <h3 className="text-lg font-semibold text-zinc-100 mb-2">Disable Auto-Requeue?</h3>
              <div className="mb-3 rounded-sm bg-zinc-800 p-3">
                <div className="flex items-center gap-2 mb-1">
                  <span className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-xs font-medium ${colors.bg} ${colors.text}`}>
                    <span className={`size-1.5 rounded-full ${colors.dot}`} />
                    {job.status}
                  </span>
                  <span className="text-xs text-zinc-500">#{job.position}</span>
                </div>
                <p className="text-sm font-medium text-zinc-200 truncate">
                  {template?.name || job.template_id}
                </p>
                {getRequeueCountDisplay() && (
                  <p className="text-xs text-purple-400 mt-1">
                    Requeue count: {getRequeueCountDisplay()}
                  </p>
                )}
              </div>
              <p className="text-sm text-zinc-400 mb-4">
                {job.status === 'running' || job.status === 'triggered'
                  ? 'This job will complete its current run but will not automatically create a new job afterwards.'
                  : 'This job will no longer automatically create a new job after completion.'}
              </p>
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => setShowStopRequeueConfirm(false)}
                  className="rounded-sm px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-800"
                >
                  Cancel
                </button>
                <button
                  onClick={() => {
                    if (job.status === 'running' || job.status === 'triggered') {
                      disableRequeueMutation.mutate();
                    } else {
                      toggleRequeueMutation.mutate();
                    }
                    setShowStopRequeueConfirm(false);
                  }}
                  disabled={toggleRequeueMutation.isPending || disableRequeueMutation.isPending}
                  className="rounded-sm bg-purple-600 px-4 py-2 text-sm font-medium text-white hover:bg-purple-700 disabled:opacity-50"
                >
                  {(toggleRequeueMutation.isPending || disableRequeueMutation.isPending) ? 'Disabling...' : 'Disable'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Delete job confirmation modal */}
      {showDeleteConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60" onClick={() => setShowDeleteConfirm(false)} />
          <div className="relative w-full max-w-sm mx-4 rounded-sm border border-zinc-800 bg-zinc-900 shadow-xl">
            <div className="p-4">
              <h3 className="text-lg font-semibold text-zinc-100 mb-2">Remove Job?</h3>
              <div className="mb-3 rounded-sm bg-zinc-800 p-3">
                <div className="flex items-center gap-2 mb-1">
                  <span className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-xs font-medium ${colors.bg} ${colors.text}`}>
                    <span className={`size-1.5 rounded-full ${colors.dot}`} />
                    {job.status}
                  </span>
                  <span className="text-xs text-zinc-500">#{job.position}</span>
                </div>
                <p className="text-sm font-medium text-zinc-200 truncate">
                  {template?.name || job.template_id}
                </p>
              </div>
              <p className="text-sm text-zinc-400 mb-4">
                This job will be permanently removed from the queue. This action cannot be undone.
              </p>
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => setShowDeleteConfirm(false)}
                  className="rounded-sm px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-800"
                >
                  Cancel
                </button>
                <button
                  onClick={() => {
                    deleteMutation.mutate();
                    setShowDeleteConfirm(false);
                  }}
                  disabled={deleteMutation.isPending}
                  className="rounded-sm bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
                >
                  {deleteMutation.isPending ? 'Removing...' : 'Remove'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
