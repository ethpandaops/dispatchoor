import { useState, useEffect } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { JobTemplate } from '../../types';
import { api } from '../../api/client';
import { LabelsDisplay } from '../common/LabelBadge';

interface AddJobDialogProps {
  groupId: string;
  templates: JobTemplate[];
  isOpen: boolean;
  onClose: () => void;
  preselectedTemplateId?: string;
  autoRequeueTemplateIds?: Set<string>;
}

export function AddJobDialog({ groupId, templates, isOpen, onClose, preselectedTemplateId, autoRequeueTemplateIds }: AddJobDialogProps) {
  // Track which template was selected for confirmation tracking
  const [selectedTemplateId, setSelectedTemplateId] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [inputOverrides, setInputOverrides] = useState<Record<string, string>>({});
  const [autoRequeue, setAutoRequeue] = useState(false);
  const [hasRequeueLimit, setHasRequeueLimit] = useState(false);
  const [requeueLimit, setRequeueLimit] = useState(5);
  const [confirmedDuplicateForTemplate, setConfirmedDuplicateForTemplate] = useState<string | null>(null);
  const [lastOpenState, setLastOpenState] = useState(false);
  const queryClient = useQueryClient();

  // Handle dialog open/close - reset state when opening
  if (isOpen && !lastOpenState) {
    const initialTemplateId = preselectedTemplateId || (templates.length > 0 ? templates[0].id : '');
    setSelectedTemplateId(initialTemplateId);
    setSearchQuery('');
    setInputOverrides({});
    setAutoRequeue(false);
    setHasRequeueLimit(false);
    setRequeueLimit(5);
    setConfirmedDuplicateForTemplate(null);
  }
  if (isOpen !== lastOpenState) {
    setLastOpenState(isOpen);
  }

  const selectedTemplate = templates.find((t) => t.id === selectedTemplateId);
  const hasExistingAutoRequeue = autoRequeueTemplateIds?.has(selectedTemplateId) ?? false;
  // Confirmation is only valid for the specific template it was confirmed for
  const confirmedDuplicate = confirmedDuplicateForTemplate === selectedTemplateId;

  // Derive inputs from template defaults merged with user overrides
  const inputs = selectedTemplate
    ? { ...selectedTemplate.default_inputs, ...inputOverrides }
    : inputOverrides;

  // Filter templates by search query (searches name and labels)
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

  const createMutation = useMutation({
    mutationFn: () => api.createJob(
      groupId,
      selectedTemplateId,
      inputs,
      autoRequeue,
      hasRequeueLimit ? requeueLimit : null
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
        // Submit if valid
        const canSubmit = selectedTemplateId && (!hasExistingAutoRequeue || !autoRequeue || confirmedDuplicate);
        if (canSubmit && !createMutation.isPending) {
          e.preventDefault();
          createMutation.mutate();
        }
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose, selectedTemplateId, hasExistingAutoRequeue, autoRequeue, confirmedDuplicate, createMutation]);

  if (!isOpen) return null;

  const handleInputChange = (key: string, value: string) => {
    setInputOverrides((prev) => ({ ...prev, [key]: value }));
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />

      {/* Dialog */}
      <div className="relative w-full max-w-3xl max-h-[85vh] mx-4 flex flex-col rounded-sm border border-zinc-800 bg-zinc-900 shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3 shrink-0">
          <h2 className="text-lg font-semibold text-zinc-100">Add Job to Queue</h2>
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
          {/* Template selector with search */}
          <div>
            <label className="block text-sm font-medium text-zinc-300 mb-1">
              Job Template
            </label>
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
              {searchQuery && (
                <button
                  onClick={() => setSearchQuery('')}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-zinc-500 hover:text-zinc-300"
                >
                  <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              )}
            </div>
            {/* Template list */}
            <div className="max-h-48 overflow-y-auto rounded-sm border border-zinc-700 bg-zinc-800">
              {filteredTemplates.length > 0 ? (
                filteredTemplates.map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => {
                      setSelectedTemplateId(template.id);
                      setInputOverrides({});
                    }}
                    className={`w-full px-3 py-2 text-left text-sm transition-colors ${
                      selectedTemplateId === template.id
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
                        <LabelsDisplay labels={template.labels} maxDisplay={3} />
                      )}
                    </div>
                  </button>
                ))
              ) : (
                <div className="px-3 py-4 text-center text-sm text-zinc-500">
                  No templates found
                </div>
              )}
            </div>
          </div>

          {/* Template info */}
          {selectedTemplate && (
            <div className="rounded-sm bg-zinc-800/50 p-3 text-sm">
              <div className="flex items-center gap-2 text-zinc-400">
                <svg className="size-4 shrink-0" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/>
                </svg>
                <span>{selectedTemplate.owner}/{selectedTemplate.repo}</span>
              </div>
              <div className="flex items-center gap-2 text-zinc-400 mt-1">
                <svg className="size-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
                </svg>
                <span>{selectedTemplate.workflow_id}</span>
                <span className="text-zinc-600">@</span>
                <span>{selectedTemplate.ref}</span>
              </div>
              {selectedTemplate.labels && Object.keys(selectedTemplate.labels).length > 0 && (
                <div className="mt-2">
                  <LabelsDisplay labels={selectedTemplate.labels} maxDisplay={0} />
                </div>
              )}
            </div>
          )}

          {/* Inputs */}
          {Object.keys(inputs).length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-zinc-300 mb-2">Workflow Inputs</h3>
              <div className="space-y-3">
                {Object.entries(inputs).map(([key, value]) => (
                  <div key={key}>
                    <label className="block text-xs font-medium text-zinc-400 mb-1">{key}</label>
                    {value.length > 100 ? (
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
                ))}
              </div>
            </div>
          )}

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

          {/* Warning for existing auto-requeue job */}
          {hasExistingAutoRequeue && (
            <div className="rounded-sm bg-amber-500/10 border border-amber-500/20 p-3 space-y-2">
              <div className="flex items-start gap-2">
                <svg className="size-5 shrink-0 text-amber-400 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
                <div>
                  <p className="text-sm font-medium text-amber-400">Auto-requeue job already exists</p>
                  <p className="text-xs text-amber-400/80 mt-0.5">
                    This template already has an active auto-requeue job in the queue. Adding another job may result in duplicate runs.
                  </p>
                </div>
              </div>
              <label className="flex items-center gap-2 cursor-pointer ml-7">
                <input
                  type="checkbox"
                  checked={confirmedDuplicate}
                  onChange={(e) => setConfirmedDuplicateForTemplate(e.target.checked ? selectedTemplateId : null)}
                  className="size-4 rounded-sm border-amber-600 bg-zinc-800 text-amber-500 focus:ring-amber-500 focus:ring-offset-zinc-900"
                />
                <span className="text-sm text-amber-300">I understand, add anyway</span>
              </label>
            </div>
          )}

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
            disabled={!selectedTemplateId || createMutation.isPending || (hasExistingAutoRequeue && !confirmedDuplicate)}
            className="rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:bg-blue-600/50 disabled:cursor-not-allowed"
          >
            {createMutation.isPending ? 'Adding...' : 'Add to Queue'}
          </button>
        </div>
      </div>
    </div>
  );
}
