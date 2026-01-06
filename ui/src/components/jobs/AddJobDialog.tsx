import { useState, useEffect } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { JobTemplate } from '../../types';
import { api } from '../../api/client';

interface AddJobDialogProps {
  groupId: string;
  templates: JobTemplate[];
  isOpen: boolean;
  onClose: () => void;
}

export function AddJobDialog({ groupId, templates, isOpen, onClose }: AddJobDialogProps) {
  const [selectedTemplateId, setSelectedTemplateId] = useState('');
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const queryClient = useQueryClient();

  const selectedTemplate = templates.find((t) => t.id === selectedTemplateId);

  useEffect(() => {
    if (selectedTemplate) {
      setInputs({ ...selectedTemplate.default_inputs });
    } else {
      setInputs({});
    }
  }, [selectedTemplate]);

  useEffect(() => {
    if (isOpen && templates.length > 0 && !selectedTemplateId) {
      setSelectedTemplateId(templates[0].id);
    }
  }, [isOpen, templates, selectedTemplateId]);

  const createMutation = useMutation({
    mutationFn: () => api.createJob(groupId, selectedTemplateId, inputs),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', groupId] });
      queryClient.invalidateQueries({ queryKey: ['groups'] });
      onClose();
      setSelectedTemplateId('');
      setInputs({});
    },
  });

  if (!isOpen) return null;

  const handleInputChange = (key: string, value: string) => {
    setInputs((prev) => ({ ...prev, [key]: value }));
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
          {/* Template selector */}
          <div>
            <label htmlFor="template" className="block text-sm font-medium text-zinc-300 mb-1">
              Job Template
            </label>
            <select
              id="template"
              value={selectedTemplateId}
              onChange={(e) => setSelectedTemplateId(e.target.value)}
              className="w-full rounded-sm border border-zinc-700 bg-zinc-800 px-3 py-2 text-zinc-100 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500"
            >
              {templates.map((template) => (
                <option key={template.id} value={template.id}>
                  {template.name}
                </option>
              ))}
            </select>
          </div>

          {/* Template info */}
          {selectedTemplate && (
            <div className="rounded-sm bg-zinc-800/50 p-3 text-sm">
              <div className="flex items-center gap-2 text-zinc-400">
                <svg className="size-4" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/>
                </svg>
                <span>{selectedTemplate.owner}/{selectedTemplate.repo}</span>
              </div>
              <div className="flex items-center gap-2 text-zinc-400 mt-1">
                <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
                </svg>
                <span>{selectedTemplate.workflow_id}</span>
                <span className="text-zinc-600">@</span>
                <span>{selectedTemplate.ref}</span>
              </div>
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
            disabled={!selectedTemplateId || createMutation.isPending}
            className="rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:bg-blue-600/50 disabled:cursor-not-allowed"
          >
            {createMutation.isPending ? 'Adding...' : 'Add to Queue'}
          </button>
        </div>
      </div>
    </div>
  );
}
