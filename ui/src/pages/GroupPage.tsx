import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { api } from '../api/client';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuthStore } from '../stores/authStore';
import { JobCard } from '../components/jobs/JobCard';
import { AddJobDialog } from '../components/jobs/AddJobDialog';
import type { Job, JobTemplate, Runner } from '../types';

function SortableJobCard({ job, template }: { job: Job; template?: JobTemplate }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: job.id,
    disabled: job.status !== 'pending',
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  return (
    <div ref={setNodeRef} style={style}>
      <JobCard
        job={job}
        template={template}
        isDragging={isDragging}
        dragHandleProps={{ ...attributes, ...listeners }}
      />
    </div>
  );
}

function RunnerCard({ runner }: { runner: Runner }) {
  return (
    <div className="flex items-center justify-between rounded-sm border border-zinc-800 bg-zinc-900 px-3 py-2">
      <div className="flex items-center gap-2">
        <span
          className={`size-2 rounded-full ${
            runner.status === 'online'
              ? runner.busy
                ? 'bg-amber-500 animate-pulse'
                : 'bg-green-500'
              : 'bg-zinc-600'
          }`}
        />
        <span className="text-sm text-zinc-200">{runner.name}</span>
      </div>
      <span className={`text-xs ${runner.busy ? 'text-amber-400' : 'text-zinc-500'}`}>
        {runner.busy ? 'busy' : runner.status}
      </span>
    </div>
  );
}

export function GroupPage() {
  const { id } = useParams<{ id: string }>();
  const { user } = useAuthStore();
  const queryClient = useQueryClient();
  const isAdmin = user?.role === 'admin';
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [preselectedTemplateId, setPreselectedTemplateId] = useState<string | undefined>();
  const [activeTab, setActiveTab] = useState<'queue' | 'history' | 'templates'>('queue');
  const { subscribe, unsubscribe } = useWebSocket();

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  // Subscribe to group updates
  useEffect(() => {
    if (id) {
      subscribe(id);
      return () => unsubscribe(id);
    }
  }, [id, subscribe, unsubscribe]);

  const { data: group, isLoading: groupLoading } = useQuery({
    queryKey: ['group', id],
    queryFn: () => api.getGroup(id!),
    enabled: !!id,
  });

  const { data: templates = [] } = useQuery({
    queryKey: ['templates', id],
    queryFn: () => api.getJobTemplates(id!),
    enabled: !!id,
  });

  const { data: queue = [], isLoading: queueLoading } = useQuery({
    queryKey: ['queue', id],
    queryFn: () => api.getQueue(id!),
    enabled: !!id,
  });

  const { data: history = [] } = useQuery({
    queryKey: ['history', id],
    queryFn: () => api.getHistory(id!),
    enabled: !!id && activeTab === 'history',
  });

  const { data: runners = [] } = useQuery({
    queryKey: ['runners', id],
    queryFn: () => api.getRunners(id!),
    enabled: !!id,
  });

  const reorderMutation = useMutation({
    mutationFn: (jobIds: string[]) => api.reorderQueue(id!, jobIds),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queue', id] });
    },
  });

  const getTemplateForJob = (job: Job) => templates.find((t) => t.id === job.template_id);

  const pendingJobs = queue.filter((j) => j.status === 'pending').sort((a, b) => a.position - b.position);
  const activeJobs = queue.filter((j) => j.status === 'triggered' || j.status === 'running');

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = pendingJobs.findIndex((j) => j.id === active.id);
    const newIndex = pendingJobs.findIndex((j) => j.id === over.id);
    const reordered = arrayMove(pendingJobs, oldIndex, newIndex);
    const jobIds = reordered.map((j) => j.id);
    reorderMutation.mutate(jobIds);
  };

  const idleRunners = runners.filter((r: Runner) => r.status === 'online' && !r.busy);
  const busyRunners = runners.filter((r: Runner) => r.busy);
  const offlineRunners = runners.filter((r: Runner) => r.status === 'offline');

  if (groupLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-zinc-400">Loading...</div>
      </div>
    );
  }

  if (!group) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-zinc-400">Group not found</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-100">{group.name}</h1>
          {group.description && (
            <p className="mt-1 text-sm text-zinc-400">{group.description}</p>
          )}
          <div className="mt-2 flex flex-wrap gap-1.5">
            {group.runner_labels.map((label) => (
              <span
                key={label}
                className="inline-flex rounded-sm bg-zinc-800 px-2 py-0.5 text-xs text-zinc-400"
              >
                {label}
              </span>
            ))}
          </div>
        </div>
        {isAdmin && (
          <button
            onClick={() => setShowAddDialog(true)}
            className="flex items-center gap-2 rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
          >
            <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            Add Job
          </button>
        )}
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <div className="rounded-sm border border-zinc-800 bg-zinc-900 p-4">
          <div className="text-2xl font-bold text-zinc-100">{pendingJobs.length}</div>
          <div className="text-sm text-zinc-400">Queued</div>
        </div>
        <div className="rounded-sm border border-zinc-800 bg-zinc-900 p-4">
          <div className="text-2xl font-bold text-green-400">{activeJobs.length}</div>
          <div className="text-sm text-zinc-400">Running</div>
        </div>
        <div className="rounded-sm border border-zinc-800 bg-zinc-900 p-4">
          <div className="text-2xl font-bold text-blue-400">{idleRunners.length}</div>
          <div className="text-sm text-zinc-400">Idle Runners</div>
        </div>
        <div className="rounded-sm border border-zinc-800 bg-zinc-900 p-4">
          <div className="text-2xl font-bold text-amber-400">{busyRunners.length}</div>
          <div className="text-sm text-zinc-400">Busy Runners</div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Queue/History Panel */}
        <div className="lg:col-span-2 space-y-4">
          {/* Tabs */}
          <div className="border-b border-zinc-800">
            <nav className="flex gap-4">
              <button
                onClick={() => setActiveTab('queue')}
                className={`border-b-2 pb-3 text-sm font-medium transition-colors ${
                  activeTab === 'queue'
                    ? 'border-blue-500 text-blue-400'
                    : 'border-transparent text-zinc-400 hover:text-zinc-200'
                }`}
              >
                Queue ({queue.length})
              </button>
              <button
                onClick={() => setActiveTab('history')}
                className={`border-b-2 pb-3 text-sm font-medium transition-colors ${
                  activeTab === 'history'
                    ? 'border-blue-500 text-blue-400'
                    : 'border-transparent text-zinc-400 hover:text-zinc-200'
                }`}
              >
                History
              </button>
              <button
                onClick={() => setActiveTab('templates')}
                className={`border-b-2 pb-3 text-sm font-medium transition-colors ${
                  activeTab === 'templates'
                    ? 'border-blue-500 text-blue-400'
                    : 'border-transparent text-zinc-400 hover:text-zinc-200'
                }`}
              >
                Templates ({templates.length})
              </button>
            </nav>
          </div>

          {/* Content */}
          {activeTab === 'queue' ? (
            <div className="space-y-6">
              {/* Active jobs */}
              {activeJobs.length > 0 && (
                <div>
                  <h3 className="mb-3 text-sm font-medium text-zinc-300">Running</h3>
                  <div className="space-y-2">
                    {activeJobs.map((job) => (
                      <JobCard key={job.id} job={job} template={getTemplateForJob(job)} />
                    ))}
                  </div>
                </div>
              )}

              {/* Pending jobs with drag-and-drop */}
              <div>
                <h3 className="mb-3 text-sm font-medium text-zinc-300">
                  Pending
                  {isAdmin && pendingJobs.length > 1 && (
                    <span className="ml-2 text-xs text-zinc-500">(drag to reorder)</span>
                  )}
                </h3>
                {queueLoading ? (
                  <div className="text-zinc-500">Loading...</div>
                ) : pendingJobs.length > 0 ? (
                  <DndContext
                    sensors={sensors}
                    collisionDetection={closestCenter}
                    onDragEnd={handleDragEnd}
                  >
                    <SortableContext items={pendingJobs.map((j) => j.id)} strategy={verticalListSortingStrategy}>
                      <div className="space-y-2">
                        {pendingJobs.map((job) => (
                          <SortableJobCard key={job.id} job={job} template={getTemplateForJob(job)} />
                        ))}
                      </div>
                    </SortableContext>
                  </DndContext>
                ) : (
                  <div className="rounded-sm border border-dashed border-zinc-800 py-8 text-center text-zinc-500">
                    No pending jobs
                  </div>
                )}
              </div>
            </div>
          ) : activeTab === 'history' ? (
            <div className="space-y-2">
              {history.length > 0 ? (
                history.map((job) => (
                  <JobCard key={job.id} job={job} template={getTemplateForJob(job)} />
                ))
              ) : (
                <div className="rounded-sm border border-dashed border-zinc-800 py-8 text-center text-zinc-500">
                  No job history
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-3">
              {templates.length > 0 ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  {templates.map((template) => (
                    <div
                      key={template.id}
                      className="rounded-sm border border-zinc-800 bg-zinc-900 p-3"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0 flex-1">
                          <h4 className="truncate text-sm font-medium text-zinc-200">
                            {template.name}
                          </h4>
                          <div className="mt-1 flex items-center gap-1.5 text-xs text-zinc-500">
                            <svg className="size-3.5 shrink-0" fill="currentColor" viewBox="0 0 24 24">
                              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/>
                            </svg>
                            <span className="truncate">{template.owner}/{template.repo}</span>
                          </div>
                          <div className="mt-1 flex items-center gap-1.5 text-xs text-zinc-500">
                            <svg className="size-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
                            </svg>
                            <span className="truncate">{template.workflow_id}</span>
                            <span className="text-zinc-600">@</span>
                            <span>{template.ref}</span>
                          </div>
                        </div>
                        {isAdmin && (
                          <button
                            onClick={() => {
                              setPreselectedTemplateId(template.id);
                              setShowAddDialog(true);
                            }}
                            className="shrink-0 rounded-sm bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700"
                          >
                            Add
                          </button>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-sm border border-dashed border-zinc-800 py-8 text-center text-zinc-500">
                  No templates configured
                </div>
              )}
            </div>
          )}
        </div>

        {/* Runners Panel */}
        <div className="space-y-4">
          <h2 className="text-sm font-medium text-zinc-300">
            Runners ({runners.length})
          </h2>

          {runners.length > 0 ? (
            <div className="space-y-4">
              {idleRunners.length > 0 && (
                <div>
                  <h3 className="mb-2 text-xs font-medium text-green-400">
                    Idle ({idleRunners.length})
                  </h3>
                  <div className="space-y-1.5">
                    {idleRunners.map((runner) => (
                      <RunnerCard key={runner.id} runner={runner} />
                    ))}
                  </div>
                </div>
              )}

              {busyRunners.length > 0 && (
                <div>
                  <h3 className="mb-2 text-xs font-medium text-amber-400">
                    Busy ({busyRunners.length})
                  </h3>
                  <div className="space-y-1.5">
                    {busyRunners.map((runner) => (
                      <RunnerCard key={runner.id} runner={runner} />
                    ))}
                  </div>
                </div>
              )}

              {offlineRunners.length > 0 && (
                <div>
                  <h3 className="mb-2 text-xs font-medium text-zinc-500">
                    Offline ({offlineRunners.length})
                  </h3>
                  <div className="space-y-1.5">
                    {offlineRunners.map((runner) => (
                      <RunnerCard key={runner.id} runner={runner} />
                    ))}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="rounded-sm border border-dashed border-zinc-800 py-8 text-center text-zinc-500 text-sm">
              No runners found
            </div>
          )}
        </div>
      </div>

      {/* Add Job Dialog */}
      <AddJobDialog
        groupId={id!}
        templates={templates}
        isOpen={showAddDialog}
        onClose={() => {
          setShowAddDialog(false);
          setPreselectedTemplateId(undefined);
        }}
        preselectedTemplateId={preselectedTemplateId}
      />
    </div>
  );
}
