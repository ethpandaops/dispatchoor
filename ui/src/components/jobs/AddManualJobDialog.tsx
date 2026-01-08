import { useState, useEffect } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { JobTemplate } from '../../types';
import { api } from '../../api/client';
import { LabelsDisplay } from '../common/LabelBadge';

interface AddManualJobDialogProps {
  groupId: string;
  templates: JobTemplate[];
  isOpen: boolean;
  onClose: () => void;
}

export function AddManualJobDialog({ groupId, templates, isOpen, onClose }: AddManualJobDialogProps) {
  // Form state
  const [name, setName] = useState('');
  const [owner, setOwner] = useState('');
  const [repo, setRepo] = useState('');
  const [workflowId, setWorkflowId] = useState('');
  const [ref, setRef] = useState('');
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [newInputKey, setNewInputKey] = useState('');
  const [newInputValue, setNewInputValue] = useState('');
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [newLabelKey, setNewLabelKey] = useState('');
  const [newLabelValue, setNewLabelValue] = useState('');

  // Auto-requeue
  const [autoRequeue, setAutoRequeue] = useState(false);
  const [hasRequeueLimit, setHasRequeueLimit] = useState(false);
  const [requeueLimit, setRequeueLimit] = useState(5);

  // Prepopulate from template
  const [prepopulateTemplateId, setPrepopulateTemplateId] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [lastOpenState, setLastOpenState] = useState(false);

  const queryClient = useQueryClient();

  // Handle dialog open/close - reset state when opening
  if (isOpen && !lastOpenState) {
    setName('');
    setOwner('');
    setRepo('');
    setWorkflowId('');
    setRef('');
    setInputs({});
    setNewInputKey('');
    setNewInputValue('');
    setLabels({});
    setNewLabelKey('');
    setNewLabelValue('');
    setAutoRequeue(false);
    setHasRequeueLimit(false);
    setRequeueLimit(5);
    setPrepopulateTemplateId('');
    setSearchQuery('');
  }
  if (isOpen !== lastOpenState) {
    setLastOpenState(isOpen);
  }

  // Filter templates by search query
  const filteredTemplates = templates.filter((t) => {
    const query = searchQuery.toLowerCase();
    if (t.name.toLowerCase().includes(query)) return true;
    if (t.labels) {
      for (const [key, value] of Object.entries(t.labels)) {
        if (key.toLowerCase().includes(query) || value.toLowerCase().includes(query)) {
          return true;
        }
      }
    }
    return false;
  });

  // Handle prepopulate from template
  const handlePrepopulate = (templateId: string) => {
    if (templateId) {
      const template = templates.find(t => t.id === templateId);
      if (template) {
        // Don't set name - keep it user-defined
        setOwner(template.owner);
        setRepo(template.repo);
        setWorkflowId(template.workflow_id);
        setRef(template.ref);
        setInputs({ ...template.default_inputs });
        setLabels(template.labels ? { ...template.labels } : {});
      }
    } else {
      // Reset to empty
      setOwner('');
      setRepo('');
      setWorkflowId('');
      setRef('');
      setInputs({});
      setLabels({});
    }
  };

  const isValid = owner.trim() && repo.trim() && workflowId.trim() && ref.trim();

  const createMutation = useMutation({
    mutationFn: () => api.createJob(
      groupId,
      null, // No template_id for manual jobs
      Object.keys(inputs).length > 0 ? inputs : undefined,
      autoRequeue,
      hasRequeueLimit ? requeueLimit : null,
      {
        name: name || undefined,
        owner,
        repo,
        workflow_id: workflowId,
        ref,
        labels: Object.keys(labels).length > 0 ? labels : undefined,
      }
    ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', groupId] });
      queryClient.invalidateQueries({ queryKey: ['groups'] });
      onClose();
    },
  });

  // Keyboard shortcuts: ESC to close, Enter to submit
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't trigger if user is in a textarea
      if (e.target instanceof HTMLTextAreaElement) return;

      if (e.key === 'Escape') {
        onClose();
      } else if (e.key === 'Enter' && !e.shiftKey) {
        // Submit if valid and not pending
        if (isValid && !createMutation.isPending) {
          e.preventDefault();
          createMutation.mutate();
        }
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose, isValid, createMutation]);

  const handleAddInput = () => {
    if (newInputKey.trim()) {
      setInputs(prev => ({ ...prev, [newInputKey.trim()]: newInputValue }));
      setNewInputKey('');
      setNewInputValue('');
    }
  };

  const handleRemoveInput = (key: string) => {
    setInputs(prev => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  };

  const handleInputChange = (key: string, value: string) => {
    setInputs(prev => ({ ...prev, [key]: value }));
  };

  const handleAddLabel = () => {
    if (newLabelKey.trim()) {
      setLabels(prev => ({ ...prev, [newLabelKey.trim()]: newLabelValue }));
      setNewLabelKey('');
      setNewLabelValue('');
    }
  };

  const handleRemoveLabel = (key: string) => {
    setLabels(prev => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />

      {/* Dialog */}
      <div className="relative w-full max-w-3xl max-h-[85vh] mx-4 flex flex-col rounded-sm border border-zinc-800 bg-zinc-900 shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3 shrink-0">
          <h2 className="text-lg font-semibold text-zinc-100">Add Manual Job</h2>
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
          {/* Prepopulate from template */}
          {templates.length > 0 && (
            <div className="rounded-sm border border-zinc-700 p-3">
              <label className="block text-sm font-medium text-zinc-300 mb-2">
                Prepopulate from Template (Optional)
              </label>
              <p className="text-xs text-zinc-500 mb-2">
                Copy values from an existing template. The job will not be linked to this template.
              </p>

              {/* Search input */}
              <div className="relative mb-2">
                <svg
                  className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-zinc-500"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
                  />
                </svg>
                <input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="Search templates..."
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 py-2 pl-10 pr-3 text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                />
              </div>

              {/* Template list */}
              <div className="max-h-32 overflow-y-auto rounded-sm border border-zinc-700 bg-zinc-800">
                <button
                  type="button"
                  onClick={() => {
                    setPrepopulateTemplateId('');
                    handlePrepopulate('');
                  }}
                  className={`w-full px-3 py-2 text-left text-sm transition-colors ${
                    !prepopulateTemplateId
                      ? 'bg-blue-600/20 text-blue-400'
                      : 'text-zinc-400 hover:bg-zinc-700'
                  }`}
                >
                  <span className="italic">None - Start fresh</span>
                </button>
                {filteredTemplates.map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => {
                      setPrepopulateTemplateId(template.id);
                      handlePrepopulate(template.id);
                    }}
                    className={`w-full px-3 py-2 text-left text-sm transition-colors ${
                      prepopulateTemplateId === template.id
                        ? 'bg-blue-600/20 text-blue-400'
                        : 'text-zinc-300 hover:bg-zinc-700'
                    }`}
                  >
                    <div className="font-medium">{template.name}</div>
                    <div className="flex items-center gap-2 mt-0.5">
                      <span className="text-xs text-zinc-500">
                        {template.owner}/{template.repo}
                      </span>
                      {template.labels && Object.keys(template.labels).length > 0 && (
                        <LabelsDisplay labels={template.labels} maxDisplay={2} />
                      )}
                    </div>
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Workflow fields */}
          <div>
            <h3 className="text-sm font-medium text-zinc-300 mb-3">Workflow</h3>

            {/* Name (optional) */}
            <div className="mb-3">
              <label className="block text-xs font-medium text-zinc-400 mb-1">
                Name (optional)
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Custom job name..."
                className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
              />
            </div>

            {/* Owner and Repo */}
            <div className="grid grid-cols-2 gap-3 mb-3">
              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">
                  Owner <span className="text-red-400">*</span>
                </label>
                <input
                  type="text"
                  value={owner}
                  onChange={(e) => setOwner(e.target.value)}
                  placeholder="e.g., ethpandaops"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">
                  Repository <span className="text-red-400">*</span>
                </label>
                <input
                  type="text"
                  value={repo}
                  onChange={(e) => setRepo(e.target.value)}
                  placeholder="e.g., ethereum-package"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                />
              </div>
            </div>

            {/* Workflow file and Ref */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">
                  Workflow File <span className="text-red-400">*</span>
                </label>
                <input
                  type="text"
                  value={workflowId}
                  onChange={(e) => setWorkflowId(e.target.value)}
                  placeholder="e.g., test.yml"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">
                  Branch / Ref <span className="text-red-400">*</span>
                </label>
                <input
                  type="text"
                  value={ref}
                  onChange={(e) => setRef(e.target.value)}
                  placeholder="e.g., main"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                />
              </div>
            </div>
          </div>

          {/* Workflow Inputs */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-medium text-zinc-300">Workflow Inputs</h3>
            </div>

            {/* Existing inputs */}
            {Object.keys(inputs).length > 0 && (
              <div className="space-y-2 mb-3">
                {Object.entries(inputs).map(([key, value]) => (
                  <div key={key} className="flex items-start gap-2">
                    <div className="flex-1">
                      <label className="block text-xs font-medium text-zinc-400 mb-1">{key}</label>
                      {value.length > 80 || value.includes('\n') ? (
                        <textarea
                          value={value}
                          onChange={(e) => handleInputChange(key, e.target.value)}
                          rows={3}
                          className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 font-mono focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                        />
                      ) : (
                        <input
                          type="text"
                          value={value}
                          onChange={(e) => handleInputChange(key, e.target.value)}
                          className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                        />
                      )}
                    </div>
                    <button
                      onClick={() => handleRemoveInput(key)}
                      className="mt-5 rounded-sm p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-red-400"
                      title="Remove input"
                    >
                      <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* Add new input */}
            <div className="flex items-end gap-2 p-2 rounded-sm bg-zinc-800/50 border border-zinc-700">
              <div className="flex-1">
                <label className="block text-xs text-zinc-500 mb-1">Key</label>
                <input
                  type="text"
                  value={newInputKey}
                  onChange={(e) => setNewInputKey(e.target.value)}
                  placeholder="input_name"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      handleAddInput();
                    }
                  }}
                />
              </div>
              <div className="flex-1">
                <label className="block text-xs text-zinc-500 mb-1">Value</label>
                <input
                  type="text"
                  value={newInputValue}
                  onChange={(e) => setNewInputValue(e.target.value)}
                  placeholder="value"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      handleAddInput();
                    }
                  }}
                />
              </div>
              <button
                onClick={handleAddInput}
                disabled={!newInputKey.trim()}
                className="rounded-sm bg-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-600 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Add
              </button>
            </div>
          </div>

          {/* Labels */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-medium text-zinc-300">Labels</h3>
              <span className="text-xs text-zinc-500">Optional tags for filtering</span>
            </div>

            {/* Existing labels */}
            {Object.keys(labels).length > 0 && (
              <div className="flex flex-wrap gap-2 mb-3">
                {Object.entries(labels).map(([key, value]) => (
                  <span
                    key={key}
                    className="inline-flex items-center gap-1 rounded-sm bg-zinc-700 px-2 py-1 text-sm"
                  >
                    <span className="text-zinc-400">{key}:</span>
                    <span className="text-zinc-200">{value}</span>
                    <button
                      onClick={() => handleRemoveLabel(key)}
                      className="ml-1 text-zinc-500 hover:text-red-400"
                    >
                      <svg className="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  </span>
                ))}
              </div>
            )}

            {/* Add new label */}
            <div className="flex items-end gap-2 p-2 rounded-sm bg-zinc-800/50 border border-zinc-700">
              <div className="flex-1">
                <label className="block text-xs text-zinc-500 mb-1">Key</label>
                <input
                  type="text"
                  value={newLabelKey}
                  onChange={(e) => setNewLabelKey(e.target.value)}
                  placeholder="label_key"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      handleAddLabel();
                    }
                  }}
                />
              </div>
              <div className="flex-1">
                <label className="block text-xs text-zinc-500 mb-1">Value</label>
                <input
                  type="text"
                  value={newLabelValue}
                  onChange={(e) => setNewLabelValue(e.target.value)}
                  placeholder="value"
                  className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      handleAddLabel();
                    }
                  }}
                />
              </div>
              <button
                onClick={handleAddLabel}
                disabled={!newLabelKey.trim()}
                className="rounded-sm bg-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-600 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Add
              </button>
            </div>
          </div>

          {/* Auto-requeue options */}
          <div className="rounded-sm border border-zinc-700 p-3 space-y-3">
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={autoRequeue}
                onChange={(e) => setAutoRequeue(e.target.checked)}
                className="size-4 rounded-sm border-zinc-600 bg-zinc-800 text-blue-500 focus:ring-blue-500 focus:ring-offset-zinc-900"
              />
              <span className="text-sm text-zinc-300">Auto-requeue after completion</span>
            </label>
            {autoRequeue && (
              <div className="ml-6 space-y-2">
                <p className="text-xs text-zinc-500">
                  Job will automatically re-queue itself after finishing (completed, failed, or cancelled).
                </p>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={hasRequeueLimit}
                    onChange={(e) => setHasRequeueLimit(e.target.checked)}
                    className="size-4 rounded-sm border-zinc-600 bg-zinc-800 text-blue-500 focus:ring-blue-500 focus:ring-offset-zinc-900"
                  />
                  <span className="text-sm text-zinc-400">Limit number of requeues</span>
                </label>
                {hasRequeueLimit && (
                  <div className="flex items-center gap-2">
                    <input
                      type="number"
                      min={1}
                      max={1000}
                      value={requeueLimit}
                      onChange={(e) => setRequeueLimit(parseInt(e.target.value) || 1)}
                      className="w-24 rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-sm text-zinc-100 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
                    />
                    <span className="text-sm text-zinc-500">times</span>
                  </div>
                )}
              </div>
            )}
          </div>

          {createMutation.error && (
            <div className="rounded-sm bg-red-500/10 border border-red-500/20 px-3 py-2 text-sm text-red-400">
              {createMutation.error instanceof Error ? createMutation.error.message : 'Failed to create job'}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-2 border-t border-zinc-800 px-4 py-3 shrink-0">
          <button
            onClick={onClose}
            className="rounded-sm px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-800"
          >
            Cancel
          </button>
          <button
            onClick={() => createMutation.mutate()}
            disabled={!isValid || createMutation.isPending}
            className="rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:bg-blue-600/50 disabled:cursor-not-allowed"
          >
            {createMutation.isPending ? 'Adding...' : 'Add to Queue'}
          </button>
        </div>
      </div>
    </div>
  );
}
