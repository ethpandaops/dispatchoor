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

interface EditState {
  inputs: Record<string, string>;
  owner: string;
  repo: string;
  workflowId: string;
  ref: string;
}

export function JobDetailDialog({ job, template, isOpen, onClose }: JobDetailDialogProps) {
  const { user } = useAuthStore();
  const queryClient = useQueryClient();
  const isAdmin = user?.role === 'admin';
  const canEdit = isAdmin && job.status === 'pending';

  // Initialize edit state with job overrides or template defaults
  const getInitialState = (): EditState => ({
    inputs: job.inputs || {},
    owner: job.owner ?? template?.owner ?? '',
    repo: job.repo ?? template?.repo ?? '',
    workflowId: job.workflow_id ?? template?.workflow_id ?? '',
    ref: job.ref ?? template?.ref ?? '',
  });

  const [editState, setEditState] = useState<EditState>(getInitialState);
  const [hasChanges, setHasChanges] = useState(false);

  // Reset edit state when job changes or dialog opens
  useEffect(() => {
    if (isOpen) {
      setEditState(getInitialState());
      setHasChanges(false);
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, job.id, job.inputs, job.owner, job.repo, job.workflow_id, job.ref, template]);

  const updateMutation = useMutation({
    mutationFn: () => {
      // Build update payload - only include overrides that differ from template
      const updates: Parameters<typeof api.updateJob>[1] = {
        inputs: editState.inputs,
      };

      // Check each field - if it differs from template, include the override
      // Empty string means "clear override, use template"
      if (editState.owner !== (template?.owner ?? '')) {
        updates.owner = editState.owner || undefined;
      } else if (job.owner !== undefined) {
        // Had an override before, clear it
        updates.owner = undefined;
      }

      if (editState.repo !== (template?.repo ?? '')) {
        updates.repo = editState.repo || undefined;
      } else if (job.repo !== undefined) {
        updates.repo = undefined;
      }

      if (editState.workflowId !== (template?.workflow_id ?? '')) {
        updates.workflow_id = editState.workflowId || undefined;
      } else if (job.workflow_id !== undefined) {
        updates.workflow_id = undefined;
      }

      if (editState.ref !== (template?.ref ?? '')) {
        updates.ref = editState.ref || undefined;
      } else if (job.ref !== undefined) {
        updates.ref = undefined;
      }

      return api.updateJob(job.id, updates);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
      setHasChanges(false);
    },
  });

  if (!isOpen) return null;

  const colors = statusColors[job.status] || statusColors.pending;

  const handleInputChange = (key: string, value: string) => {
    setEditState((prev) => ({ ...prev, inputs: { ...prev.inputs, [key]: value } }));
    setHasChanges(true);
  };

  const handleFieldChange = (field: keyof Omit<EditState, 'inputs'>, value: string) => {
    setEditState((prev) => ({ ...prev, [field]: value }));
    setHasChanges(true);
  };

  const handleSave = () => {
    updateMutation.mutate();
  };

  const handleCancel = () => {
    setEditState(getInitialState());
    setHasChanges(false);
  };

  // Check if a field has been overridden from template
  const isOverridden = (field: 'owner' | 'repo' | 'workflowId' | 'ref') => {
    const templateValue = field === 'workflowId' ? template?.workflow_id : template?.[field];
    return editState[field] !== (templateValue ?? '') && editState[field] !== '';
  };

  // Get effective value (edit state for editing, job override or template for display)
  const getEffectiveValue = (field: 'owner' | 'repo' | 'workflowId' | 'ref'): string => {
    if (canEdit) return editState[field];
    // Get job override value
    let jobValue: string | undefined;
    switch (field) {
      case 'owner': jobValue = job.owner; break;
      case 'repo': jobValue = job.repo; break;
      case 'workflowId': jobValue = job.workflow_id; break;
      case 'ref': jobValue = job.ref; break;
    }
    // Get template value
    let templateValue: string | undefined;
    switch (field) {
      case 'owner': templateValue = template?.owner; break;
      case 'repo': templateValue = template?.repo; break;
      case 'workflowId': templateValue = template?.workflow_id; break;
      case 'ref': templateValue = template?.ref; break;
    }
    return jobValue ?? templateValue ?? '';
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
    if (value.startsWith('{') || value.startsWith('[')) {
      try {
        return JSON.stringify(JSON.parse(value), null, 2);
      } catch {
        return value;
      }
    }
    return value;
  };

  const inputEntries = Object.entries(canEdit ? editState.inputs : (job.inputs || {}));

  // Get effective owner/repo for links
  const effectiveOwner = getEffectiveValue('owner');
  const effectiveRepo = getEffectiveValue('repo');
  const effectiveWorkflowId = getEffectiveValue('workflowId');
  const effectiveRef = getEffectiveValue('ref');

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
              {job.name ?? template?.name ?? job.template_id}
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
          <div className="rounded-sm border border-zinc-800 bg-zinc-800/30 p-3 space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-xs font-medium text-zinc-400 uppercase tracking-wide">Workflow</h3>
              {canEdit && (
                <span className="text-xs text-zinc-500">Edit fields to override template</span>
              )}
            </div>
            <div className="grid grid-cols-2 gap-3 text-sm">
              {/* Owner */}
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-zinc-500 text-xs">Owner</span>
                  {isOverridden('owner') && <span className="text-xs text-amber-400">overridden</span>}
                </div>
                {canEdit ? (
                  <input
                    type="text"
                    value={editState.owner}
                    onChange={(e) => handleFieldChange('owner', e.target.value)}
                    placeholder={template?.owner || ''}
                    className={`w-full rounded-sm border px-2 py-1 text-sm font-mono focus:outline-hidden focus:ring-1 focus:ring-blue-500 ${
                      isOverridden('owner')
                        ? 'border-amber-500/50 bg-amber-500/10 text-amber-200'
                        : 'border-zinc-700 bg-zinc-800 text-zinc-200'
                    }`}
                  />
                ) : (
                  <p className={`text-sm font-mono ${job.owner ? 'text-amber-200' : 'text-zinc-200'}`}>
                    {effectiveOwner || '-'}
                  </p>
                )}
                {isOverridden('owner') && template?.owner && (
                  <p className="text-xs text-zinc-500 mt-1">Template: {template.owner}</p>
                )}
              </div>
              {/* Repository */}
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-zinc-500 text-xs">Repository</span>
                  {isOverridden('repo') && <span className="text-xs text-amber-400">overridden</span>}
                </div>
                {canEdit ? (
                  <input
                    type="text"
                    value={editState.repo}
                    onChange={(e) => handleFieldChange('repo', e.target.value)}
                    placeholder={template?.repo || ''}
                    className={`w-full rounded-sm border px-2 py-1 text-sm font-mono focus:outline-hidden focus:ring-1 focus:ring-blue-500 ${
                      isOverridden('repo')
                        ? 'border-amber-500/50 bg-amber-500/10 text-amber-200'
                        : 'border-zinc-700 bg-zinc-800 text-zinc-200'
                    }`}
                  />
                ) : (
                  <p className={`text-sm font-mono ${job.repo ? 'text-amber-200' : 'text-zinc-200'}`}>
                    {effectiveRepo || '-'}
                  </p>
                )}
                {isOverridden('repo') && template?.repo && (
                  <p className="text-xs text-zinc-500 mt-1">Template: {template.repo}</p>
                )}
              </div>
              {/* Workflow File */}
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-zinc-500 text-xs">Workflow File</span>
                  {isOverridden('workflowId') && <span className="text-xs text-amber-400">overridden</span>}
                </div>
                {canEdit ? (
                  <input
                    type="text"
                    value={editState.workflowId}
                    onChange={(e) => handleFieldChange('workflowId', e.target.value)}
                    placeholder={template?.workflow_id || ''}
                    className={`w-full rounded-sm border px-2 py-1 text-sm font-mono focus:outline-hidden focus:ring-1 focus:ring-blue-500 ${
                      isOverridden('workflowId')
                        ? 'border-amber-500/50 bg-amber-500/10 text-amber-200'
                        : 'border-zinc-700 bg-zinc-800 text-zinc-200'
                    }`}
                  />
                ) : (
                  <p className={`text-sm font-mono ${job.workflow_id ? 'text-amber-200' : 'text-zinc-200'}`}>
                    {effectiveWorkflowId || '-'}
                  </p>
                )}
                {isOverridden('workflowId') && template?.workflow_id && (
                  <p className="text-xs text-zinc-500 mt-1">Template: {template.workflow_id}</p>
                )}
              </div>
              {/* Branch / Ref */}
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-zinc-500 text-xs">Branch / Ref</span>
                  {isOverridden('ref') && <span className="text-xs text-amber-400">overridden</span>}
                </div>
                {canEdit ? (
                  <input
                    type="text"
                    value={editState.ref}
                    onChange={(e) => handleFieldChange('ref', e.target.value)}
                    placeholder={template?.ref || ''}
                    className={`w-full rounded-sm border px-2 py-1 text-sm font-mono focus:outline-hidden focus:ring-1 focus:ring-blue-500 ${
                      isOverridden('ref')
                        ? 'border-amber-500/50 bg-amber-500/10 text-amber-200'
                        : 'border-zinc-700 bg-zinc-800 text-zinc-200'
                    }`}
                  />
                ) : (
                  <p className={`text-sm font-mono ${job.ref ? 'text-amber-200' : 'text-zinc-200'}`}>
                    {effectiveRef || '-'}
                  </p>
                )}
                {isOverridden('ref') && template?.ref && (
                  <p className="text-xs text-zinc-500 mt-1">Template: {template.ref}</p>
                )}
              </div>
            </div>
            {/* GitHub links - only show when not editing */}
            {!canEdit && effectiveOwner && effectiveRepo && (
              <div className="flex gap-4 pt-2 border-t border-zinc-700/50">
                <a
                  href={`https://github.com/${effectiveOwner}/${effectiveRepo}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1 text-xs text-zinc-400 hover:text-blue-400"
                >
                  View Repository
                  <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                  </svg>
                </a>
                {effectiveWorkflowId && effectiveRef && (
                  <a
                    href={`https://github.com/${effectiveOwner}/${effectiveRepo}/blob/${effectiveRef}/.github/workflows/${effectiveWorkflowId}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 text-xs text-zinc-400 hover:text-blue-400"
                  >
                    View Workflow
                    <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                    </svg>
                  </a>
                )}
              </div>
            )}
            {job.run_url && (
              <div className="pt-2 border-t border-zinc-700/50">
                <span className="text-zinc-500 text-xs">GitHub Run</span>
                <a
                  href={job.run_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1 text-sm text-zinc-200 hover:text-blue-400"
                >
                  Run #{job.run_id}
                  <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                  </svg>
                </a>
              </div>
            )}
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
                {inputEntries.map(([key, value]) => {
                  const templateDefault = template?.default_inputs?.[key];
                  const currentValue = canEdit ? editState.inputs[key] : value;
                  const isInputOverridden = templateDefault !== undefined && currentValue !== templateDefault;

                  return (
                    <div key={key}>
                      <div className="flex items-center gap-2 mb-1">
                        <label className="text-xs font-medium text-zinc-400">{key}</label>
                        {isInputOverridden && (
                          <span className="text-xs text-amber-400">overridden</span>
                        )}
                      </div>
                      {canEdit ? (
                        value.length > 100 || value.includes('\n') || value.startsWith('{') || value.startsWith('[') ? (
                          <textarea
                            value={editState.inputs[key]}
                            onChange={(e) => handleInputChange(key, e.target.value)}
                            rows={Math.min(8, Math.max(3, formatInputValue(value).split('\n').length))}
                            className={`w-full rounded-sm border px-3 py-2 text-sm font-mono focus:outline-hidden focus:ring-1 focus:ring-blue-500 ${
                              isInputOverridden
                                ? 'border-amber-500/50 bg-amber-500/10 text-amber-200'
                                : 'border-zinc-700 bg-zinc-800 text-zinc-100'
                            }`}
                          />
                        ) : (
                          <input
                            type="text"
                            value={editState.inputs[key]}
                            onChange={(e) => handleInputChange(key, e.target.value)}
                            className={`w-full rounded-sm border px-3 py-2 text-sm focus:outline-hidden focus:ring-1 focus:ring-blue-500 ${
                              isInputOverridden
                                ? 'border-amber-500/50 bg-amber-500/10 text-amber-200'
                                : 'border-zinc-700 bg-zinc-800 text-zinc-100'
                            }`}
                          />
                        )
                      ) : (
                        value.length > 80 || value.includes('\n') || value.startsWith('{') || value.startsWith('[') ? (
                          <pre className={`max-h-48 overflow-auto rounded-sm p-2 text-sm font-mono ${
                            isInputOverridden ? 'bg-amber-500/10 text-amber-200' : 'bg-zinc-800 text-zinc-300'
                          }`}>
                            {formatInputValue(value)}
                          </pre>
                        ) : (
                          <p className={`text-sm font-mono rounded-sm px-2 py-1 ${
                            isInputOverridden ? 'bg-amber-500/10 text-amber-200' : 'bg-zinc-800 text-zinc-200'
                          }`}>
                            {value}
                          </p>
                        )
                      )}
                      {isInputOverridden && templateDefault && (
                        <p className="text-xs text-zinc-500 mt-1 truncate" title={templateDefault}>
                          Template: {templateDefault.length > 60 ? templateDefault.substring(0, 60) + '...' : templateDefault}
                        </p>
                      )}
                    </div>
                  );
                })}
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
