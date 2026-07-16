import { useMutation } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { Group } from '../../types';

interface DeleteGroupDialogProps {
  group: Group;
  isOpen: boolean;
  onClose: () => void;
  onDeleted: () => void;
}

export function DeleteGroupDialog({ group, isOpen, onClose, onDeleted }: DeleteGroupDialogProps) {
  const deleteMutation = useMutation({
    mutationFn: () => api.deleteGroup(group.id),
    onSuccess: () => {
      onDeleted();
    },
  });

  if (!isOpen) return null;

  const handleClose = () => {
    if (deleteMutation.isPending) return;
    deleteMutation.reset();
    onClose();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/60" onClick={handleClose} />

      {/* Dialog */}
      <div className="relative w-full max-w-md mx-4 flex flex-col rounded-sm border border-zinc-800 bg-zinc-900 shadow-xl">
        {/* Header */}
        <div className="flex items-start justify-between gap-2 border-b border-zinc-800 px-4 py-3">
          <h2 className="text-lg font-semibold text-zinc-100">Delete Group</h2>
          <button
            onClick={handleClose}
            className="mt-0.5 shrink-0 rounded-sm p-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
          >
            <svg className="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div className="p-4 space-y-3">
          <p className="text-sm text-zinc-300">
            Are you sure you want to delete{' '}
            <span className="font-semibold text-zinc-100">{group.name}</span>?
          </p>
          <div className="rounded-sm border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">
            This permanently deletes the group along with all of its job templates and run
            history. This action cannot be undone. Groups with pending or running jobs
            cannot be deleted.
          </div>
          {deleteMutation.isError && (
            <div className="rounded-sm border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">
              {deleteMutation.error instanceof Error
                ? deleteMutation.error.message
                : 'Failed to delete group'}
            </div>
          )}
        </div>

        {/* Actions */}
        <div className="flex justify-end gap-2 border-t border-zinc-800 px-4 py-3">
          <button
            onClick={handleClose}
            disabled={deleteMutation.isPending}
            className="rounded-sm border border-zinc-700 px-4 py-2 text-sm font-medium text-zinc-300 hover:bg-zinc-800 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={() => deleteMutation.mutate()}
            disabled={deleteMutation.isPending}
            className="flex items-center gap-2 rounded-sm bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
          >
            <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            {deleteMutation.isPending ? 'Deleting...' : 'Delete Group'}
          </button>
        </div>
      </div>
    </div>
  );
}
