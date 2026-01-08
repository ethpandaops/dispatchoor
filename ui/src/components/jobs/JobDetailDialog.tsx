import { useState, useEffect } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { Job, JobTemplate } from '../../types';
import { api } from '../../api/client';
import { useAuthStore } from '../../stores/authStore';
import { LabelsDisplay } from '../common/LabelBadge';

interface JobDetailDialogProps {
  job: Job;
  template?: JobTemplate;
  isOpen: boolean;
  onClose: () => void;
}

const statusColors: Record<string, { bg: string; text: string; dot: string }> = {
  pending: { bg: 'bg-amber-500/10', text: 'text-amber-400', dot: 'bg-amber-400' },
  triggered: { bg: 'bg-blue-500/10', text: 'text-blue-400', dot: 'bg-blue-400' },
  running: { bg: 'bg-green-500/10', text: 'text-green-400', dot: 'bg-green-400 animate-pulse' },
  completed: { bg: 'bg-emerald-500/10', text: 'text-emerald-400', dot: 'bg-emerald-400' },
  failed: { bg: 'bg-red-500/10', text: 'text-red-400', dot: 'bg-red-400' },
  cancelled: { bg: 'bg-zinc-500/10', text: 'text-zinc-400', dot: 'bg-zinc-400' },
};

export function JobDetailDialog({ job, template, isOpen, onClose }: JobDetailDialogProps) {
  const { user } = useAuthStore();
  const queryClient = useQueryClient();
  const isAdmin = user?.role === 'admin';
  const canEdit = isAdmin && job.status === 'pending';

  const [editedInputs, setEditedInputs] = useState<Record<string, string>>(job.inputs || {});
  const [hasChanges, setHasChanges] = useState(false);

  // Reset edited inputs when job changes or dialog opens
  useEffect(() => {
    if (isOpen) {
      setEditedInputs(job.inputs || {});
      setHasChanges(false);
    }
  }, [isOpen, job.inputs]);

  const updateMutation = useMutation({
    mutationFn: () => api.updateJob(job.id, editedInputs),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
      setHasChanges(false);
    },
  });

  if (!isOpen) return null;

  const colors = statusColors[job.status] || statusColors.pending;

  const handleInputChange = (key: string, value: string) => {
    setEditedInputs((prev) => ({ ...prev, [key]: value }));
    setHasChanges(true);
  };

  const handleSave = () => {
    updateMutation.mutate();
  };

  const handleCancel = () => {
    setEditedInputs(job.inputs || {});
    setHasChanges(false);
  };

  const formatDateTime = (dateStr: string | null) => {
    if (!dateStr) return null;
    const date = new Date(dateStr);
    return date.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    });
  };

  const getElapsedTime = () => {
    if (!job.triggered_at) return null;
    const start = new Date(job.triggered_at).getTime();
    const end = job.completed_at ? new Date(job.completed_at).getTime() : Date.now();
    const seconds = Math.floor((end - start) / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    if (hours > 0) return `${hours}h ${minutes % 60}m ${seconds % 60}s`;
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`;
    return `${seconds}s`;
  };

  const formatInputValue = (value: string): string => {
    // Try to pretty-print JSON
    if (value.startsWith('{') || value.startsWith('[')) {
      try {
        return JSON.stringify(JSON.parse(value), null, 2);
      } catch {
        return value;
      }
    }
    return value;
  };

  const inputEntries = Object.entries(editedInputs);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />

      {/* Dialog */}
      <div className="relative w-full max-w-2xl max-h-[85vh] mx-4 flex flex-col rounded-sm border border-zinc-800 bg-zinc-900 shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3 shrink-0">
          <div className="flex items-center gap-3">
            <span className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-xs font-medium ${job.paused ? 'bg-zinc-500/10 text-zinc-400' : colors.bg + ' ' + colors.text}`}>
              <span className={`size-1.5 rounded-full ${job.paused ? 'bg-zinc-400' : colors.dot}`} />
              {job.paused ? 'paused' : job.status}
            </span>
            <h2 className="text-lg font-semibold text-zinc-100">
              {template?.name || job.template_id}
            </h2>
            <span className="text-sm text-zinc-500">#{job.position}</span>
          </div>
          <button
            onClick={onClose}
            className="rounded-sm p-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
          >
            <svg className="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div className="p-4 space-y-4 overflow-y-auto flex-1">
          {/* Labels */}
          {template?.labels && Object.keys(template.labels).length > 0 && (
            <div>
              <LabelsDisplay labels={template.labels} maxDisplay={0} />
            </div>
          )}

          {/* Workflow Info */}
          <div className="rounded-sm border border-zinc-800 bg-zinc-800/30 p-3 space-y-2">
            <h3 className="text-xs font-medium text-zinc-400 uppercase tracking-wide">Workflow</h3>
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <span className="text-zinc-500">Repository</span>
                {template ? (
                  <a
                    href={`https://github.com/${template.owner}/${template.repo}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 text-zinc-200 hover:text-blue-400"
                  >
                    {template.owner}/{template.repo}
                    <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                    </svg>
                  </a>
                ) : (
                  <p className="text-zinc-400">-</p>
                )}
              </div>
              <div>
                <span className="text-zinc-500">Workflow File</span>
                {template ? (
                  <a
                    href={`https://github.com/${template.owner}/${template.repo}/blob/${template.ref}/.github/workflows/${template.workflow_id}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 text-zinc-200 hover:text-blue-400"
                  >
                    {template.workflow_id}
                    <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                    </svg>
                  </a>
                ) : (
                  <p className="text-zinc-400">-</p>
                )}
              </div>
              <div>
                <span className="text-zinc-500">Branch / Ref</span>
                <p className="text-zinc-200 font-mono">{template?.ref || '-'}</p>
              </div>
              {job.run_url && (
                <div>
                  <span className="text-zinc-500">GitHub Run</span>
                  <a
                    href={job.run_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 text-zinc-200 hover:text-blue-400"
                  >
                    Run #{job.run_id}
                    <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                    </svg>
                  </a>
                </div>
              )}
            </div>
          </div>

          {/* Timing Info */}
          <div className="rounded-sm border border-zinc-800 bg-zinc-800/30 p-3 space-y-2">
            <h3 className="text-xs font-medium text-zinc-400 uppercase tracking-wide">Timing</h3>
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <span className="text-zinc-500">Created</span>
                <p className="text-zinc-200">{formatDateTime(job.created_at)}</p>
              </div>
              {job.created_by && (
                <div>
                  <span className="text-zinc-500">Created By</span>
                  <p className="text-zinc-200">{job.created_by}</p>
                </div>
              )}
              {job.triggered_at && (
                <div>
                  <span className="text-zinc-500">Started</span>
                  <p className="text-zinc-200">{formatDateTime(job.triggered_at)}</p>
                </div>
              )}
              {job.completed_at && (
                <div>
                  <span className="text-zinc-500">Completed</span>
                  <p className="text-zinc-200">{formatDateTime(job.completed_at)}</p>
                </div>
              )}
              {getElapsedTime() && (
                <div>
                  <span className="text-zinc-500">Duration</span>
                  <p className="text-zinc-200">{getElapsedTime()}</p>
                </div>
              )}
              {job.runner_name && (
                <div>
                  <span className="text-zinc-500">Runner</span>
                  <p className="text-zinc-200">{job.runner_name}</p>
                </div>
              )}
            </div>
          </div>

          {/* Auto-requeue Info */}
          {(job.auto_requeue || job.requeue_count > 0) && (
            <div className="rounded-sm border border-purple-500/30 bg-purple-500/10 p-3 space-y-1">
              <h3 className="text-xs font-medium text-purple-400 uppercase tracking-wide">Auto-requeue</h3>
              <div className="text-sm text-purple-300">
                {job.auto_requeue ? 'Enabled' : 'Disabled'}
                {job.requeue_limit !== null && ` (limit: ${job.requeue_limit})`}
                {job.requeue_count > 0 && ` - ${job.requeue_count} requeue${job.requeue_count > 1 ? 's' : ''} so far`}
              </div>
            </div>
          )}

          {/* Error Message */}
          {job.error_message && (
            <div className="rounded-sm border border-red-500/30 bg-red-500/10 p-3 space-y-1">
              <h3 className="text-xs font-medium text-red-400 uppercase tracking-wide">Error</h3>
              <p className="text-sm text-red-300">{job.error_message}</p>
            </div>
          )}

          {/* Workflow Inputs */}
          <div className="rounded-sm border border-zinc-800 bg-zinc-800/30 p-3 space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-xs font-medium text-zinc-400 uppercase tracking-wide">Workflow Inputs</h3>
              {canEdit && hasChanges && (
                <span className="text-xs text-amber-400">Unsaved changes</span>
              )}
            </div>
            {inputEntries.length > 0 ? (
              <div className="space-y-3">
                {inputEntries.map(([key, value]) => (
                  <div key={key}>
                    <label className="block text-xs font-medium text-zinc-400 mb-1">{key}</label>
                    {canEdit ? (
                      // Editable input
                      value.length > 100 || value.includes('\n') || value.startsWith('{') || value.startsWith('[') ? (
                        <textarea
                          value={editedInputs[key]}
                          onChange={(e) => handleInputChange(key, e.target.value)}
                          rows={Math.min(8, Math.max(3, formatInputValue(value).split('\n').length))}
                          className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 font-mono focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                        />
                      ) : (
                        <input
                          type="text"
                          value={editedInputs[key]}
                          onChange={(e) => handleInputChange(key, e.target.value)}
                          className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                        />
                      )
                    ) : (
                      // Read-only display
                      value.length > 80 || value.includes('\n') || value.startsWith('{') || value.startsWith('[') ? (
                        <pre className="max-h-48 overflow-auto rounded-sm bg-zinc-800 p-2 text-sm font-mono text-zinc-300">
                          {formatInputValue(value)}
                        </pre>
                      ) : (
                        <p className="text-sm text-zinc-200 font-mono bg-zinc-800 rounded-sm px-2 py-1">
                          {value}
                        </p>
                      )
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-sm text-zinc-500">No inputs</p>
            )}
          </div>

          {/* Mutation error */}
          {updateMutation.error && (
            <div className="rounded-sm bg-red-500/10 border border-red-500/20 px-3 py-2 text-sm text-red-400">
              {updateMutation.error instanceof Error ? updateMutation.error.message : 'Failed to update job'}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-2 border-t border-zinc-800 px-4 py-3 shrink-0">
          {canEdit && hasChanges ? (
            <>
              <button
                onClick={handleCancel}
                className="rounded-sm px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-800"
              >
                Discard Changes
              </button>
              <button
                onClick={handleSave}
                disabled={updateMutation.isPending}
                className="rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              >
                {updateMutation.isPending ? 'Saving...' : 'Save Changes'}
              </button>
            </>
          ) : (
            <button
              onClick={onClose}
              className="rounded-sm px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-800"
            >
              Close
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
