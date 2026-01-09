import { useState, useEffect, useMemo, useCallback } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
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
import { motion, AnimatePresence, LayoutGroup } from 'framer-motion';
import { api } from '../api/client';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuthStore } from '../stores/authStore';
import { JobCard } from '../components/jobs/JobCard';
import { AddJobDialog } from '../components/jobs/AddJobDialog';
import { AddManualJobDialog } from '../components/jobs/AddManualJobDialog';
import { LabelsDisplay } from '../components/common/LabelBadge';
import { HistoryChart } from '../components/charts/HistoryChart';
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

function RunnerCard({ runner, job, template }: { runner: Runner; job?: Job; template?: JobTemplate }) {
  const hasOurJob = !!job;

  return (
    <div className="flex items-center justify-between rounded-sm border border-zinc-800 bg-zinc-900 px-3 py-2">
      <div className="flex items-center gap-2">
        <span
          className={`size-2 shrink-0 rounded-full ${
            runner.status === 'online'
              ? runner.busy
                ? 'bg-amber-500 animate-pulse'
                : 'bg-green-500'
              : 'bg-zinc-600'
          }`}
        />
        <div className="min-w-0">
          <div className="text-sm text-zinc-200">{runner.name}</div>
          {runner.busy && hasOurJob && (
            <div className="flex items-center gap-1">
              <span className="text-xs text-amber-400 truncate" title={template?.name}>
                {template?.name || 'running job'}
              </span>
              {job.run_url && (
                <a
                  href={job.run_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="shrink-0 text-zinc-500 hover:text-zinc-300"
                  title="View run on GitHub"
                  onClick={(e) => e.stopPropagation()}
                >
                  <svg className="size-3" fill="currentColor" viewBox="0 0 24 24">
                    <path d="M14 3v2h3.59l-9.83 9.83 1.41 1.41L19 6.41V10h2V3h-7z"/>
                    <path d="M5 5v14h14v-7h2v9H3V3h9v2H5z"/>
                  </svg>
                </a>
              )}
            </div>
          )}
        </div>
      </div>
      {runner.busy ? (
        !hasOurJob && <span className="text-xs text-zinc-500 italic">external job</span>
      ) : (
        <span className="text-xs text-zinc-500">{runner.status}</span>
      )}
    </div>
  );
}

type TabType = 'queue' | 'history' | 'templates';
type HistoryViewType = 'linear' | 'grouped';

const validTabs: TabType[] = ['queue', 'history', 'templates'];
const validHistoryViews: HistoryViewType[] = ['linear', 'grouped'];

export function GroupPage() {
  const { id } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useAuthStore();
  const queryClient = useQueryClient();
  const isAdmin = user?.role === 'admin';
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [showManualJobDialog, setShowManualJobDialog] = useState(false);
  const [showAddJobDropdown, setShowAddJobDropdown] = useState(false);
  const [preselectedTemplateId, setPreselectedTemplateId] = useState<string | undefined>();

  // History pagination state
  const [historyJobs, setHistoryJobs] = useState<Job[]>([]);
  const [historyCursor, setHistoryCursor] = useState<string | undefined>();
  const [hasMoreHistory, setHasMoreHistory] = useState(false);
  const [isLoadingMore, setIsLoadingMore] = useState(false);

  // Get active tab from URL, default to 'queue'
  const tabParam = searchParams.get('tab');
  const activeTab: TabType = validTabs.includes(tabParam as TabType) ? (tabParam as TabType) : 'queue';

  // Get history view mode from URL, default to 'linear'
  const viewParam = searchParams.get('view');
  const historyViewMode: HistoryViewType = validHistoryViews.includes(viewParam as HistoryViewType)
    ? (viewParam as HistoryViewType)
    : 'linear';

  const setActiveTab = (tab: TabType) => {
    setSearchParams((prev) => {
      if (tab === 'queue') {
        prev.delete('tab');
      } else {
        prev.set('tab', tab);
      }
      return prev;
    });
  };

  const setHistoryViewMode = (view: HistoryViewType) => {
    setSearchParams((prev) => {
      if (view === 'linear') {
        prev.delete('view');
      } else {
        prev.set('view', view);
      }
      return prev;
    });
  };

  // History filter state from URL
  type HistoryStatus = 'completed' | 'failed' | 'cancelled';
  const validHistoryStatuses: HistoryStatus[] = ['completed', 'failed', 'cancelled'];

  const getHistoryFiltersFromURL = () => {
    const statusParam = searchParams.get('hstatus');
    const statuses = statusParam
      ? statusParam.split(',').filter((s): s is HistoryStatus =>
          validHistoryStatuses.includes(s as HistoryStatus))
      : [];

    const labels: Record<string, string> = {};
    searchParams.forEach((value, key) => {
      if (key.startsWith('hlabel.')) {
        labels[key.replace('hlabel.', '')] = value;
      }
    });

    return { statuses, labels };
  };

  const historyFilters = getHistoryFiltersFromURL();

  const updateHistoryFilters = (newFilters: { statuses: HistoryStatus[]; labels: Record<string, string> }) => {
    setSearchParams((prev) => {
      // Clear existing filter params
      const keysToDelete = Array.from(prev.keys()).filter(
        k => k === 'hstatus' || k.startsWith('hlabel.')
      );
      keysToDelete.forEach(k => prev.delete(k));

      // Set new status filter
      if (newFilters.statuses.length > 0) {
        prev.set('hstatus', newFilters.statuses.join(','));
      }

      // Set new label filters
      for (const [key, value] of Object.entries(newFilters.labels)) {
        prev.set(`hlabel.${key}`, value);
      }

      return prev;
    });

    // Reset pagination when filters change
    setHistoryCursor(undefined);
    setHistoryJobs([]);
  };

  const toggleHistoryStatusFilter = (status: HistoryStatus) => {
    const newStatuses = historyFilters.statuses.includes(status)
      ? historyFilters.statuses.filter(s => s !== status)
      : [...historyFilters.statuses, status];
    updateHistoryFilters({ ...historyFilters, statuses: newStatuses });
  };

  const toggleHistoryLabelFilter = (key: string, value: string) => {
    const newLabels = { ...historyFilters.labels };
    if (newLabels[key] === value) {
      delete newLabels[key];
    } else {
      newLabels[key] = value;
    }
    updateHistoryFilters({ ...historyFilters, labels: newLabels });
  };

  const clearHistoryFilters = () => {
    updateHistoryFilters({ statuses: [], labels: {} });
  };

  const hasActiveHistoryFilters = historyFilters.statuses.length > 0 || Object.keys(historyFilters.labels).length > 0;

  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
  const { subscribe, unsubscribe } = useWebSocket();

  // Bulk selection state
  const [templateSelectionMode, setTemplateSelectionMode] = useState(false);
  const [selectedTemplateIds, setSelectedTemplateIds] = useState<Set<string>>(new Set());
  const [runningSelectionMode, setRunningSelectionMode] = useState(false);
  const [selectedRunningIds, setSelectedRunningIds] = useState<Set<string>>(new Set());
  const [pendingSelectionMode, setPendingSelectionMode] = useState(false);
  const [selectedPendingIds, setSelectedPendingIds] = useState<Set<string>>(new Set());
  const [bulkProgress, setBulkProgress] = useState<{ current: number; total: number; action: string } | null>(null);
  const [bulkActionConfirm, setBulkActionConfirm] = useState<{
    title: string;
    message: string;
    items?: { name: string; warning?: boolean; id?: string }[];
    warning?: string;
    showDuplicateCheckbox?: boolean;
    buttonLabel: string;
    buttonClass: string;
    onConfirm: (includeDuplicates: boolean) => void;
  } | null>(null);
  const [confirmDuplicates, setConfirmDuplicates] = useState(false);

  // Template filter state from URL
  type TemplateStatusFilter = 'unlabeled' | 'auto-requeue' | 'no-auto-requeue';
  const validTemplateStatusFilters: TemplateStatusFilter[] = ['unlabeled', 'auto-requeue', 'no-auto-requeue'];

  const getTemplateFiltersFromURL = () => {
    const statusParam = searchParams.get('tstatus');
    const status = validTemplateStatusFilters.includes(statusParam as TemplateStatusFilter)
      ? (statusParam as TemplateStatusFilter)
      : null;

    const labels: Record<string, string> = {};
    searchParams.forEach((value, key) => {
      if (key.startsWith('tlabel.')) {
        labels[key.replace('tlabel.', '')] = value;
      }
    });

    return { status, labels };
  };

  const templateFilters = getTemplateFiltersFromURL();
  const labelFilters = templateFilters.labels;
  const showUnlabeled = templateFilters.status === 'unlabeled';
  const showAutoRequeue = templateFilters.status === 'auto-requeue';
  const showNoAutoRequeue = templateFilters.status === 'no-auto-requeue';

  const updateTemplateFilters = (newFilters: { status: TemplateStatusFilter | null; labels: Record<string, string> }) => {
    setSearchParams((prev) => {
      // Clear existing template filter params
      const keysToDelete = Array.from(prev.keys()).filter(
        k => k === 'tstatus' || k.startsWith('tlabel.')
      );
      keysToDelete.forEach(k => prev.delete(k));

      // Set new status filter
      if (newFilters.status) {
        prev.set('tstatus', newFilters.status);
      }

      // Set new label filters
      for (const [key, value] of Object.entries(newFilters.labels)) {
        prev.set(`tlabel.${key}`, value);
      }

      return prev;
    });
  };

  const toggleGroupExpanded = (templateId: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(templateId)) {
        next.delete(templateId);
      } else {
        next.add(templateId);
      }
      return next;
    });
  };

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

  // Build filter object for API calls
  const historyFilterParams = {
    statuses: historyFilters.statuses.length > 0 ? historyFilters.statuses : undefined,
    labels: Object.keys(historyFilters.labels).length > 0 ? historyFilters.labels : undefined,
  };

  const { data: historyData, isLoading: historyLoading } = useQuery({
    queryKey: ['history', id, historyFilters.statuses, historyFilters.labels],
    queryFn: () => api.getHistory(id!, 50, undefined, historyFilterParams),
    enabled: !!id,
  });

  // Update history state when data changes or tab becomes active
  useEffect(() => {
    if (activeTab === 'history' && historyData && !historyCursor) {
      setHistoryJobs(historyData.jobs);
      setHasMoreHistory(historyData.has_more);
    }
  }, [historyData, historyCursor, activeTab]);

  // Reset history pagination when switching groups
  useEffect(() => {
    setHistoryCursor(undefined);
    setHistoryJobs([]);
    setHasMoreHistory(false);
  }, [id]);

  const loadMoreHistory = async () => {
    if (!historyData?.next_cursor || isLoadingMore) return;

    setIsLoadingMore(true);
    try {
      const moreData = await api.getHistory(id!, 50, historyData.next_cursor, historyFilterParams);
      setHistoryJobs(prev => [...prev, ...moreData.jobs]);
      setHasMoreHistory(moreData.has_more);
      setHistoryCursor(moreData.next_cursor);
    } finally {
      setIsLoadingMore(false);
    }
  };

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

  const pauseGroupMutation = useMutation({
    mutationFn: () => api.pauseGroup(id!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group', id] });
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });

  const unpauseGroupMutation = useMutation({
    mutationFn: () => api.unpauseGroup(id!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group', id] });
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });

  const getTemplateForJob = useCallback((job: Job) => templates.find((t) => t.id === job.template_id), [templates]);

  // Extract unique labels from all templates
  const availableLabels = useMemo(() => {
    const labelsMap = new Map<string, Set<string>>();
    for (const template of templates) {
      if (template.labels) {
        for (const [key, value] of Object.entries(template.labels)) {
          if (!labelsMap.has(key)) {
            labelsMap.set(key, new Set());
          }
          labelsMap.get(key)!.add(value);
        }
      }
    }
    // Convert to array of { key, values } for rendering
    return Array.from(labelsMap.entries()).map(([key, values]) => ({
      key,
      values: Array.from(values).sort(),
    }));
  }, [templates]);

  // Count unlabeled templates
  const unlabeledCount = useMemo(() => {
    return templates.filter((t) => !t.labels || Object.keys(t.labels).length === 0).length;
  }, [templates]);

  // Compute set of template IDs that have active auto-requeue jobs
  const autoRequeueTemplateIds = useMemo(() => {
    const ids = new Set<string>();
    for (const job of queue) {
      if (job.auto_requeue) {
        ids.add(job.template_id);
      }
    }
    return ids;
  }, [queue]);

  // Filter templates based on selected labels (AND logic), unlabeled, or auto-requeue filter
  const filteredTemplates = useMemo(() => {
    if (showAutoRequeue) {
      return templates.filter((t) => autoRequeueTemplateIds.has(t.id));
    }
    if (showNoAutoRequeue) {
      return templates.filter((t) => !autoRequeueTemplateIds.has(t.id));
    }
    if (showUnlabeled) {
      return templates.filter((t) => !t.labels || Object.keys(t.labels).length === 0);
    }
    if (Object.keys(labelFilters).length === 0) return templates;
    return templates.filter((t) => {
      if (!t.labels) return false;
      return Object.entries(labelFilters).every(
        ([key, value]) => t.labels?.[key] === value
      );
    });
  }, [templates, labelFilters, showUnlabeled, showAutoRequeue, showNoAutoRequeue, autoRequeueTemplateIds]);

  const toggleLabelFilter = (key: string, value: string) => {
    const newLabels = { ...labelFilters };
    if (newLabels[key] === value) {
      delete newLabels[key];
    } else {
      newLabels[key] = value;
    }
    updateTemplateFilters({ status: null, labels: newLabels });
  };

  const toggleUnlabeled = () => {
    updateTemplateFilters({ status: showUnlabeled ? null : 'unlabeled', labels: {} });
  };

  const toggleAutoRequeue = () => {
    updateTemplateFilters({ status: showAutoRequeue ? null : 'auto-requeue', labels: {} });
  };

  const toggleNoAutoRequeue = () => {
    updateTemplateFilters({ status: showNoAutoRequeue ? null : 'no-auto-requeue', labels: {} });
  };

  const clearLabelFilters = () => {
    updateTemplateFilters({ status: null, labels: {} });
  };

  // Selection helpers
  const toggleSelection = (set: Set<string>, id: string) => {
    const next = new Set(set);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  };

  // Clear selections when switching tabs or filters change
  useEffect(() => {
    setSelectedTemplateIds(new Set());
    setTemplateSelectionMode(false);
    setSelectedRunningIds(new Set());
    setRunningSelectionMode(false);
    setSelectedPendingIds(new Set());
    setPendingSelectionMode(false);
  }, [activeTab]);

  // Bulk action executor
  const executeBulkAction = async (
    ids: Set<string>,
    action: (id: string) => Promise<unknown>,
    actionName: string,
    onComplete: () => void
  ) => {
    const idArray = [...ids];
    setBulkProgress({ current: 0, total: idArray.length, action: actionName });

    let completed = 0;
    const results = await Promise.allSettled(
      idArray.map(async (id) => {
        const result = await action(id);
        completed++;
        setBulkProgress({ current: completed, total: idArray.length, action: actionName });
        return result;
      })
    );

    setBulkProgress(null);
    onComplete();
    queryClient.invalidateQueries({ queryKey: ['queue', id] });
    queryClient.invalidateQueries({ queryKey: ['groups'] });

    const failures = results.filter((r) => r.status === 'rejected');
    if (failures.length > 0) {
      console.error(`${failures.length} operations failed`);
    }
  };

  // Bulk add to queue - execute
  const executeBulkAddToQueue = async (withAutoRequeue: boolean, includeDuplicates: boolean) => {
    let templatesToAdd = [...selectedTemplateIds];

    // Skip templates that already have auto-requeue unless confirmed
    if (!includeDuplicates) {
      templatesToAdd = templatesToAdd.filter((tid) => !autoRequeueTemplateIds.has(tid));
    }

    if (templatesToAdd.length === 0) {
      setBulkActionConfirm(null);
      setConfirmDuplicates(false);
      setSelectedTemplateIds(new Set());
      setTemplateSelectionMode(false);
      return;
    }

    const actionLabel = withAutoRequeue ? 'Adding to queue with auto-requeue' : 'Adding to queue';
    setBulkProgress({ current: 0, total: templatesToAdd.length, action: actionLabel });

    let completed = 0;
    const results = await Promise.allSettled(
      templatesToAdd.map(async (templateId) => {
        const template = templates.find((t) => t.id === templateId);
        const result = await api.createJob(id!, templateId, template?.default_inputs, withAutoRequeue);
        completed++;
        setBulkProgress({ current: completed, total: templatesToAdd.length, action: actionLabel });
        return result;
      })
    );

    setBulkProgress(null);
    setSelectedTemplateIds(new Set());
    setTemplateSelectionMode(false);
    setBulkActionConfirm(null);
    setConfirmDuplicates(false);
    queryClient.invalidateQueries({ queryKey: ['queue', id] });
    queryClient.invalidateQueries({ queryKey: ['groups'] });

    const failures = results.filter((r) => r.status === 'rejected');
    if (failures.length > 0) {
      console.error(`${failures.length} jobs failed to create`);
    }
  };

  // Bulk add to queue - show confirmation
  const handleBulkAddToQueue = () => {
    const selectedTemplates = [...selectedTemplateIds].map((tid) => {
      const template = templates.find((t) => t.id === tid);
      return {
        id: tid,
        name: template?.name || tid,
        warning: autoRequeueTemplateIds.has(tid),
      };
    });
    const withExistingAutoRequeue = selectedTemplates.filter((t) => t.warning);
    const hasExistingAutoRequeue = withExistingAutoRequeue.length > 0;
    const templatesWithoutDuplicates = selectedTemplates.filter((t) => !t.warning);

    const warning = hasExistingAutoRequeue
      ? `${withExistingAutoRequeue.length} template(s) marked below already have auto-requeue jobs and will be skipped to avoid duplicates.`
      : undefined;

    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Add to Queue',
      message: hasExistingAutoRequeue
        ? `Add ${templatesWithoutDuplicates.length} of ${selectedTemplateIds.size} template(s) to the queue?`
        : `Add ${selectedTemplateIds.size} template(s) to the queue?`,
      items: selectedTemplates,
      warning,
      showDuplicateCheckbox: hasExistingAutoRequeue,
      buttonLabel: 'Add to Queue',
      buttonClass: 'bg-blue-600 hover:bg-blue-700',
      onConfirm: (includeDuplicates) => executeBulkAddToQueue(false, includeDuplicates),
    });
  };

  // Bulk add to queue with auto-requeue - show confirmation
  const handleBulkAddToQueueWithAutoRequeue = () => {
    const selectedTemplates = [...selectedTemplateIds].map((tid) => {
      const template = templates.find((t) => t.id === tid);
      return {
        id: tid,
        name: template?.name || tid,
        warning: autoRequeueTemplateIds.has(tid),
      };
    });
    const withExistingAutoRequeue = selectedTemplates.filter((t) => t.warning);
    const hasExistingAutoRequeue = withExistingAutoRequeue.length > 0;
    const templatesWithoutDuplicates = selectedTemplates.filter((t) => !t.warning);

    const warning = hasExistingAutoRequeue
      ? `${withExistingAutoRequeue.length} template(s) marked below already have auto-requeue jobs and will be skipped to avoid duplicates.`
      : undefined;

    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Add to Queue with Auto-requeue',
      message: hasExistingAutoRequeue
        ? `Add ${templatesWithoutDuplicates.length} of ${selectedTemplateIds.size} template(s) to the queue with auto-requeue enabled?`
        : `Add ${selectedTemplateIds.size} template(s) to the queue with auto-requeue enabled?`,
      items: selectedTemplates,
      warning,
      showDuplicateCheckbox: hasExistingAutoRequeue,
      buttonLabel: 'Add with Auto-requeue',
      buttonClass: 'bg-purple-600 hover:bg-purple-700',
      onConfirm: (includeDuplicates) => executeBulkAddToQueue(true, includeDuplicates),
    });
  };

  // Running jobs bulk actions
  const handleBulkCancel = () => {
    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Cancel Jobs',
      message: `Cancel ${selectedRunningIds.size} running job(s)?`,
      warning: 'This will attempt to cancel the GitHub workflow runs. Jobs that have already completed may not be affected.',
      buttonLabel: 'Cancel Jobs',
      buttonClass: 'bg-red-600 hover:bg-red-700',
      onConfirm: () => {
        executeBulkAction(
          selectedRunningIds,
          (jobId) => api.cancelJob(jobId),
          'Cancelling jobs',
          () => {
            setSelectedRunningIds(new Set());
            setRunningSelectionMode(false);
            setBulkActionConfirm(null);
          }
        );
      },
    });
  };

  const handleBulkEnableAutoRequeue = () => {
    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Enable Auto-requeue',
      message: `Enable auto-requeue for ${selectedRunningIds.size} job(s)?`,
      buttonLabel: 'Enable Auto-requeue',
      buttonClass: 'bg-purple-600 hover:bg-purple-700',
      onConfirm: () => {
        executeBulkAction(
          selectedRunningIds,
          (jobId) => api.updateAutoRequeue(jobId, true, null),
          'Enabling auto-requeue',
          () => {
            setSelectedRunningIds(new Set());
            setRunningSelectionMode(false);
            setBulkActionConfirm(null);
          }
        );
      },
    });
  };

  const handleBulkDisableAutoRequeue = () => {
    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Disable Auto-requeue',
      message: `Disable auto-requeue for ${selectedRunningIds.size} job(s)?`,
      buttonLabel: 'Disable Auto-requeue',
      buttonClass: 'bg-zinc-600 hover:bg-zinc-500',
      onConfirm: () => {
        executeBulkAction(
          selectedRunningIds,
          (jobId) => api.updateAutoRequeue(jobId, false, null),
          'Disabling auto-requeue',
          () => {
            setSelectedRunningIds(new Set());
            setRunningSelectionMode(false);
            setBulkActionConfirm(null);
          }
        );
      },
    });
  };

  // Pending jobs bulk actions
  const handleBulkPause = () => {
    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Pause Jobs',
      message: `Pause ${selectedPendingIds.size} pending job(s)?`,
      buttonLabel: 'Pause Jobs',
      buttonClass: 'bg-amber-600 hover:bg-amber-700',
      onConfirm: () => {
        executeBulkAction(
          selectedPendingIds,
          (jobId) => api.pauseJob(jobId),
          'Pausing jobs',
          () => {
            setSelectedPendingIds(new Set());
            setPendingSelectionMode(false);
            setBulkActionConfirm(null);
          }
        );
      },
    });
  };

  const handleBulkUnpause = () => {
    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Resume Jobs',
      message: `Resume ${selectedPendingIds.size} paused job(s)?`,
      buttonLabel: 'Resume Jobs',
      buttonClass: 'bg-green-600 hover:bg-green-700',
      onConfirm: () => {
        executeBulkAction(
          selectedPendingIds,
          (jobId) => api.unpauseJob(jobId),
          'Resuming jobs',
          () => {
            setSelectedPendingIds(new Set());
            setPendingSelectionMode(false);
            setBulkActionConfirm(null);
          }
        );
      },
    });
  };

  const handleBulkRemove = () => {
    setConfirmDuplicates(false);
    setBulkActionConfirm({
      title: 'Remove Jobs',
      message: `Remove ${selectedPendingIds.size} pending job(s) from the queue?`,
      warning: 'This action cannot be undone.',
      buttonLabel: 'Remove Jobs',
      buttonClass: 'bg-red-600 hover:bg-red-700',
      onConfirm: () => {
        executeBulkAction(
          selectedPendingIds,
          (jobId) => api.deleteJob(jobId),
          'Removing jobs',
          () => {
            setSelectedPendingIds(new Set());
            setPendingSelectionMode(false);
            setBulkActionConfirm(null);
          }
        );
      },
    });
  };

  // Group history jobs by template for grouped view
  const groupedHistory = useMemo(() => {
    const groups = new Map<string, { template: JobTemplate | undefined; jobs: Job[] }>();
    for (const job of historyJobs) {
      const template = getTemplateForJob(job);
      const key = job.template_id;
      if (!groups.has(key)) {
        groups.set(key, { template, jobs: [] });
      }
      groups.get(key)!.jobs.push(job);
    }
    return Array.from(groups.values());
  }, [historyJobs, getTemplateForJob]);

  const pendingJobs = queue.filter((j) => j.status === 'pending').sort((a, b) => a.position - b.position);
  const activeJobs = queue.filter((j) => j.status === 'triggered' || j.status === 'running').reverse();

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

  // Find which job a runner is executing by matching runner_id
  const getJobForRunner = (runnerId: number) => {
    return queue.find(
      (job) => job.runner_id === runnerId &&
               (job.status === 'running' || job.status === 'triggered')
    );
  };

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
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-bold text-zinc-100">{group.name}</h1>
            {group.paused && (
              <span className="rounded-sm bg-amber-500/20 px-2 py-0.5 text-xs font-medium text-amber-400">
                Paused
              </span>
            )}
          </div>
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
          <div className="flex gap-2">
            {group.paused ? (
              <button
                onClick={() => unpauseGroupMutation.mutate()}
                disabled={unpauseGroupMutation.isPending}
                className="flex items-center gap-2 rounded-sm bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-700 disabled:opacity-50"
              >
                <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                Resume Queue
              </button>
            ) : (
              <button
                onClick={() => pauseGroupMutation.mutate()}
                disabled={pauseGroupMutation.isPending}
                className="flex items-center gap-2 rounded-sm bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700 disabled:opacity-50"
              >
                <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                Pause Queue
              </button>
            )}
            <div className="relative">
              <button
                onClick={() => setShowAddJobDropdown(!showAddJobDropdown)}
                className="flex items-center gap-2 rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
              >
                <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                </svg>
                Add Job
                <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
              </button>
              {showAddJobDropdown && (
                <>
                  {/* Backdrop to close dropdown */}
                  <div
                    className="fixed inset-0 z-10"
                    onClick={() => setShowAddJobDropdown(false)}
                  />
                  {/* Dropdown menu */}
                  <div className="absolute right-0 z-20 mt-1 w-48 rounded-sm border border-zinc-700 bg-zinc-800 py-1 shadow-lg">
                    <button
                      onClick={() => {
                        setShowAddJobDropdown(false);
                        setShowAddDialog(true);
                      }}
                      className="flex w-full items-center gap-2 px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-700"
                    >
                      <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
                      </svg>
                      From Template
                    </button>
                    <button
                      onClick={() => {
                        setShowAddJobDropdown(false);
                        setShowManualJobDialog(true);
                      }}
                      className="flex w-full items-center gap-2 px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-700"
                    >
                      <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                      </svg>
                      Manual Job
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        )}
      </div>

      {/* History Chart */}
      <HistoryChart groupId={id!} />

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
                History ({historyData?.total_count ?? 0})
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
            <LayoutGroup>
              <div className="grid gap-6 lg:grid-cols-2">
                {/* Pending jobs with drag-and-drop */}
                <div>
                  <div className="mb-3 flex items-center justify-between">
                    <h3 className="text-sm font-medium text-zinc-300">
                      Pending ({pendingJobs.length})
                      {isAdmin && pendingJobs.length > 1 && !pendingSelectionMode && (
                        <span className="ml-2 text-xs text-zinc-500">(drag to reorder)</span>
                      )}
                    </h3>
                    {isAdmin && pendingJobs.length > 0 && (
                      <div className="flex items-center gap-2">
                        {pendingSelectionMode && (
                          <>
                            <button
                              onClick={() => setSelectedPendingIds(new Set(pendingJobs.map((j) => j.id)))}
                              className="text-xs text-zinc-400 hover:text-zinc-200"
                            >
                              Select all
                            </button>
                            <span className="text-zinc-600">|</span>
                            <button
                              onClick={() => setSelectedPendingIds(new Set())}
                              className="text-xs text-zinc-400 hover:text-zinc-200"
                            >
                              Deselect all
                            </button>
                            {selectedPendingIds.size > 0 && (
                              <span className="text-xs text-zinc-500">
                                ({selectedPendingIds.size} selected)
                              </span>
                            )}
                          </>
                        )}
                        <button
                          onClick={() => {
                            setPendingSelectionMode(!pendingSelectionMode);
                            setSelectedPendingIds(new Set());
                          }}
                          className={`rounded-xs px-2 py-1 text-xs ${
                            pendingSelectionMode
                              ? 'bg-zinc-700 text-zinc-200'
                              : 'text-zinc-400 hover:text-zinc-200'
                          }`}
                        >
                          {pendingSelectionMode ? 'Cancel' : 'Select'}
                        </button>
                      </div>
                    )}
                  </div>
                  {queueLoading ? (
                    <div className="text-zinc-500">Loading...</div>
                  ) : pendingJobs.length > 0 ? (
                    pendingSelectionMode ? (
                      <div className="space-y-2">
                        <AnimatePresence mode="popLayout">
                          {pendingJobs.map((job) => (
                            <motion.div
                              key={job.id}
                              layoutId={`job-${job.id}`}
                              initial={{ opacity: 0, x: -30, scale: 0.95 }}
                              animate={{ opacity: 1, x: 0, scale: 1 }}
                              exit={{ opacity: 0, x: 30, scale: 0.95 }}
                              transition={{ type: 'spring', stiffness: 500, damping: 30 }}
                              className={`flex items-start gap-3 ${
                                selectedPendingIds.has(job.id)
                                  ? 'rounded-xs ring-1 ring-blue-500/30'
                                  : ''
                              }`}
                            >
                              <input
                                type="checkbox"
                                checked={selectedPendingIds.has(job.id)}
                                onChange={() => setSelectedPendingIds(toggleSelection(selectedPendingIds, job.id))}
                                className="mt-4 size-4 shrink-0 rounded-xs border-zinc-600 bg-zinc-800 text-blue-500 focus:ring-blue-500 focus:ring-offset-zinc-900"
                              />
                              <div className="min-w-0 flex-1">
                                <JobCard job={job} template={getTemplateForJob(job)} />
                              </div>
                            </motion.div>
                          ))}
                        </AnimatePresence>
                      </div>
                    ) : (
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
                    )
                  ) : (
                    <div className="rounded-xs border border-dashed border-zinc-800 py-8 text-center text-zinc-500">
                      No pending jobs
                    </div>
                  )}
                </div>

                {/* Running jobs */}
                <div>
                  <div className="mb-3 flex items-center justify-between">
                    <h3 className="text-sm font-medium text-zinc-300">Running ({activeJobs.length})</h3>
                    {isAdmin && activeJobs.length > 0 && (
                      <div className="flex items-center gap-2">
                        {runningSelectionMode && (
                          <>
                            <button
                              onClick={() => setSelectedRunningIds(new Set(activeJobs.map((j) => j.id)))}
                              className="text-xs text-zinc-400 hover:text-zinc-200"
                            >
                              Select all
                            </button>
                            <span className="text-zinc-600">|</span>
                            <button
                              onClick={() => setSelectedRunningIds(new Set())}
                              className="text-xs text-zinc-400 hover:text-zinc-200"
                            >
                              Deselect all
                            </button>
                            {selectedRunningIds.size > 0 && (
                              <span className="text-xs text-zinc-500">
                                ({selectedRunningIds.size} selected)
                              </span>
                            )}
                          </>
                        )}
                        <button
                          onClick={() => {
                            setRunningSelectionMode(!runningSelectionMode);
                            setSelectedRunningIds(new Set());
                          }}
                          className={`rounded-xs px-2 py-1 text-xs ${
                            runningSelectionMode
                              ? 'bg-zinc-700 text-zinc-200'
                              : 'text-zinc-400 hover:text-zinc-200'
                          }`}
                        >
                          {runningSelectionMode ? 'Cancel' : 'Select'}
                        </button>
                      </div>
                    )}
                  </div>
                  {activeJobs.length > 0 ? (
                    <div className="space-y-2">
                      <AnimatePresence mode="popLayout">
                        {activeJobs.map((job) => (
                          <motion.div
                            key={job.id}
                            layoutId={`job-${job.id}`}
                            initial={{ opacity: 0, x: 30, scale: 0.95 }}
                            animate={{ opacity: 1, x: 0, scale: 1 }}
                            exit={{ opacity: 0, y: 20, scale: 0.9 }}
                            transition={{ type: 'spring', stiffness: 500, damping: 30 }}
                            className={`flex items-start gap-3 ${
                              runningSelectionMode && selectedRunningIds.has(job.id)
                                ? 'rounded-xs ring-1 ring-blue-500/30'
                                : ''
                            }`}
                          >
                            {runningSelectionMode && (
                              <input
                                type="checkbox"
                                checked={selectedRunningIds.has(job.id)}
                                onChange={() => setSelectedRunningIds(toggleSelection(selectedRunningIds, job.id))}
                                className="mt-4 size-4 shrink-0 rounded-xs border-zinc-600 bg-zinc-800 text-blue-500 focus:ring-blue-500 focus:ring-offset-zinc-900"
                              />
                            )}
                            <div className="min-w-0 flex-1">
                              <JobCard job={job} template={getTemplateForJob(job)} />
                            </div>
                          </motion.div>
                        ))}
                      </AnimatePresence>
                    </div>
                  ) : (
                    <div className="rounded-xs border border-dashed border-zinc-800 py-8 text-center text-zinc-500">
                      No running jobs
                    </div>
                  )}
                </div>
              </div>
            </LayoutGroup>
          ) : activeTab === 'history' ? (
            <div className="space-y-4">
              {/* View mode toggle and filters */}
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => setHistoryViewMode('linear')}
                    className={`px-2 py-1 text-xs rounded-xs ${
                      historyViewMode === 'linear'
                        ? 'bg-zinc-700 text-zinc-200'
                        : 'text-zinc-500 hover:text-zinc-300'
                    }`}
                  >
                    Linear
                  </button>
                  <button
                    onClick={() => setHistoryViewMode('grouped')}
                    className={`px-2 py-1 text-xs rounded-xs ${
                      historyViewMode === 'grouped'
                        ? 'bg-zinc-700 text-zinc-200'
                        : 'text-zinc-500 hover:text-zinc-300'
                    }`}
                  >
                    Grouped
                  </button>
                </div>

                {/* Filter controls */}
                <div className="space-y-2 rounded-xs border border-zinc-800 bg-zinc-900/50 p-3">
                  {/* Status filter */}
                  <div className="flex items-center gap-3">
                    <span className="w-16 shrink-0 text-xs text-zinc-500">Status</span>
                    <div className="flex flex-wrap gap-1">
                      {(['completed', 'failed', 'cancelled'] as const).map((status) => (
                        <button
                          key={status}
                          onClick={() => toggleHistoryStatusFilter(status)}
                          className={`rounded-xs px-2 py-0.5 text-xs capitalize transition-colors ${
                            historyFilters.statuses.includes(status)
                              ? status === 'completed'
                                ? 'bg-green-500/30 text-green-300 ring-1 ring-green-500/50'
                                : status === 'failed'
                                ? 'bg-red-500/30 text-red-300 ring-1 ring-red-500/50'
                                : 'bg-zinc-500/30 text-zinc-300 ring-1 ring-zinc-500/50'
                              : 'bg-zinc-800 text-zinc-400 hover:bg-zinc-700 hover:text-zinc-300'
                          }`}
                        >
                          {status}
                        </button>
                      ))}
                    </div>
                  </div>

                  {/* Label filters */}
                  {availableLabels.length > 0 && (
                    <div className="space-y-1.5">
                      {availableLabels.map(({ key, values }) => (
                        <div key={key} className="flex items-center gap-3">
                          <span className="w-16 shrink-0 text-xs text-zinc-500">{key}</span>
                          <div className="flex flex-wrap gap-1">
                            {values.map((value) => (
                              <button
                                key={value}
                                onClick={() => toggleHistoryLabelFilter(key, value)}
                                className={`rounded-xs px-2 py-0.5 text-xs transition-colors ${
                                  historyFilters.labels[key] === value
                                    ? 'bg-blue-500/30 text-blue-300 ring-1 ring-blue-500/50'
                                    : 'bg-zinc-800 text-zinc-400 hover:bg-zinc-700 hover:text-zinc-300'
                                }`}
                              >
                                {value}
                              </button>
                            ))}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* Clear filters */}
                  {hasActiveHistoryFilters && (
                    <div className="flex items-center justify-between pt-1">
                      <span className="text-xs text-zinc-500">
                        Showing {historyData?.total_count ?? 0} results
                      </span>
                      <button
                        onClick={clearHistoryFilters}
                        className="text-xs text-zinc-500 hover:text-zinc-300"
                      >
                        Clear filters
                      </button>
                    </div>
                  )}
                </div>
              </div>

              {historyLoading && historyJobs.length === 0 ? (
                <div className="text-zinc-500">Loading...</div>
              ) : historyJobs.length > 0 ? (
                <>
                  {historyViewMode === 'grouped' ? (
                    <div className="space-y-6">
                      {groupedHistory.map(({ template, jobs }) => {
                        const templateId = template?.id || jobs[0].template_id;
                        const isExpanded = expandedGroups.has(templateId);
                        const visibleJobs = isExpanded ? jobs : [jobs[0]];
                        const hiddenCount = jobs.length - 1;

                        return (
                          <div key={templateId}>
                            <div className="mb-3 flex items-center justify-between">
                              <h3 className="text-sm font-medium text-zinc-300">
                                {template?.name || templateId} ({jobs.length})
                              </h3>
                              {hiddenCount > 0 && (
                                <button
                                  onClick={() => toggleGroupExpanded(templateId)}
                                  className="flex items-center gap-1 text-xs text-zinc-500 hover:text-zinc-300"
                                >
                                  {isExpanded ? (
                                    <>
                                      <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
                                      </svg>
                                      Collapse
                                    </>
                                  ) : (
                                    <>
                                      <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                                      </svg>
                                      Show {hiddenCount} more
                                    </>
                                  )}
                                </button>
                              )}
                            </div>
                            <div className="space-y-2">
                              {visibleJobs.map((job) => (
                                <JobCard key={job.id} job={job} template={template} />
                              ))}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <div className="space-y-2">
                      {historyJobs.map((job) => (
                        <JobCard key={job.id} job={job} template={getTemplateForJob(job)} />
                      ))}
                    </div>
                  )}
                  {hasMoreHistory && (
                    <div className="mt-4 flex justify-center">
                      <button
                        onClick={loadMoreHistory}
                        disabled={isLoadingMore}
                        className="rounded-sm bg-zinc-800 px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-700 disabled:opacity-50"
                      >
                        {isLoadingMore ? 'Loading...' : 'Load More'}
                      </button>
                    </div>
                  )}
                </>
              ) : (
                <div className="rounded-sm border border-dashed border-zinc-800 py-8 text-center text-zinc-500">
                  No job history
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-4">
              {/* Description */}
              <p className="text-sm text-zinc-500">
                Predefined job templates provided via configuration. Select templates to add them to the queue.
              </p>

              {/* Label filters */}
              {(availableLabels.length > 0 || unlabeledCount > 0) && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium text-zinc-400">Filter by labels</span>
                    {(Object.keys(labelFilters).length > 0 || showUnlabeled) && (
                      <button
                        onClick={clearLabelFilters}
                        className="text-xs text-zinc-500 hover:text-zinc-300"
                      >
                        Clear
                      </button>
                    )}
                  </div>
                  <div className="space-y-1.5">
                    {availableLabels.map(({ key, values }) => (
                      <div key={key} className="flex items-center gap-2">
                        <span className="w-20 shrink-0 text-xs text-zinc-500">{key}</span>
                        <div className="flex flex-wrap gap-1">
                          {values.map((value) => (
                            <button
                              key={value}
                              onClick={() => toggleLabelFilter(key, value)}
                              className={`rounded-xs px-2 py-0.5 text-xs transition-colors ${
                                labelFilters[key] === value
                                  ? 'bg-blue-500/30 text-blue-300 ring-1 ring-blue-500/50'
                                  : 'bg-zinc-800 text-zinc-400 hover:bg-zinc-700 hover:text-zinc-300'
                              }`}
                            >
                              {value}
                            </button>
                          ))}
                        </div>
                      </div>
                    ))}
                    {unlabeledCount > 0 && (
                      <div className="flex items-center gap-2">
                        <span className="w-20 shrink-0 text-xs text-zinc-500">other</span>
                        <button
                          onClick={toggleUnlabeled}
                          className={`rounded-xs px-2 py-0.5 text-xs transition-colors ${
                            showUnlabeled
                              ? 'bg-blue-500/30 text-blue-300 ring-1 ring-blue-500/50'
                              : 'bg-zinc-800 text-zinc-400 hover:bg-zinc-700 hover:text-zinc-300'
                          }`}
                        >
                          unlabeled ({unlabeledCount})
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* Auto-requeue filter */}
              {autoRequeueTemplateIds.size > 0 && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium text-zinc-400">Filter by status</span>
                    {(showAutoRequeue || showNoAutoRequeue) && (
                      <button
                        onClick={() => updateTemplateFilters({ status: null, labels: labelFilters })}
                        className="text-xs text-zinc-500 hover:text-zinc-300"
                      >
                        Clear
                      </button>
                    )}
                  </div>
                  <div className="flex flex-wrap gap-1">
                    <button
                      onClick={toggleAutoRequeue}
                      className={`rounded-xs px-2 py-0.5 text-xs transition-colors ${
                        showAutoRequeue
                          ? 'bg-purple-500/30 text-purple-300 ring-1 ring-purple-500/50'
                          : 'bg-zinc-800 text-zinc-400 hover:bg-zinc-700 hover:text-zinc-300'
                      }`}
                    >
                      <span className="flex items-center gap-1">
                        <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                        </svg>
                        auto-requeue ({autoRequeueTemplateIds.size})
                      </span>
                    </button>
                    <button
                      onClick={toggleNoAutoRequeue}
                      className={`rounded-xs px-2 py-0.5 text-xs transition-colors ${
                        showNoAutoRequeue
                          ? 'bg-zinc-500/30 text-zinc-300 ring-1 ring-zinc-500/50'
                          : 'bg-zinc-800 text-zinc-400 hover:bg-zinc-700 hover:text-zinc-300'
                      }`}
                    >
                      no auto-requeue ({templates.length - autoRequeueTemplateIds.size})
                    </button>
                  </div>
                </div>
              )}

              {/* Template count with filter indicator */}
              {(Object.keys(labelFilters).length > 0 || showUnlabeled || showAutoRequeue || showNoAutoRequeue) && (
                <div className="text-xs text-zinc-500">
                  Showing {filteredTemplates.length} of {templates.length} templates
                </div>
              )}

              {/* Selection header for templates */}
              {isAdmin && filteredTemplates.filter((t) => t.in_config).length > 0 && (
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    {templateSelectionMode && (
                      <>
                        <button
                          onClick={() => setSelectedTemplateIds(new Set(filteredTemplates.filter((t) => t.in_config).map((t) => t.id)))}
                          className="text-xs text-zinc-400 hover:text-zinc-200"
                        >
                          Select all
                        </button>
                        <span className="text-zinc-600">|</span>
                        <button
                          onClick={() => setSelectedTemplateIds(new Set())}
                          className="text-xs text-zinc-400 hover:text-zinc-200"
                        >
                          Deselect all
                        </button>
                        {selectedTemplateIds.size > 0 && (
                          <span className="text-xs text-zinc-500">
                            ({selectedTemplateIds.size} selected)
                          </span>
                        )}
                      </>
                    )}
                  </div>
                  <button
                    onClick={() => {
                      setTemplateSelectionMode(!templateSelectionMode);
                      setSelectedTemplateIds(new Set());
                    }}
                    className={`rounded-sm px-2 py-1 text-xs ${
                      templateSelectionMode
                        ? 'bg-zinc-700 text-zinc-200'
                        : 'text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    {templateSelectionMode ? 'Cancel' : 'Select'}
                  </button>
                </div>
              )}

              {filteredTemplates.length > 0 ? (
                <div className="grid gap-3">
                  {filteredTemplates.map((template) => (
                    <div
                      key={template.id}
                      className={`rounded-sm border bg-zinc-900 p-4 ${
                        templateSelectionMode && selectedTemplateIds.has(template.id)
                          ? 'border-blue-500/50 ring-1 ring-blue-500/20'
                          : 'border-zinc-800'
                      }`}
                    >
                      <div className="flex items-start justify-between gap-3">
                        {/* Checkbox for selection mode */}
                        {templateSelectionMode && template.in_config && (
                          <input
                            type="checkbox"
                            checked={selectedTemplateIds.has(template.id)}
                            onChange={() => setSelectedTemplateIds(toggleSelection(selectedTemplateIds, template.id))}
                            className="mt-1 size-4 shrink-0 rounded-sm border-zinc-600 bg-zinc-800 text-blue-500 focus:ring-blue-500 focus:ring-offset-zinc-900"
                          />
                        )}
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <h4 className="text-sm font-medium text-zinc-200">
                              {template.name}
                            </h4>
                            {!template.in_config && (
                              <span className="inline-flex items-center gap-1 rounded-sm bg-amber-500/20 px-1.5 py-0.5 text-xs text-amber-400">
                                <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                                    d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                                </svg>
                                Not in config
                              </span>
                            )}
                            {template.source_type === 'file' && (
                              <span
                                className="inline-flex items-center gap-1 rounded-sm bg-blue-500/20 px-1.5 py-0.5 text-xs text-blue-300"
                                title={`Loaded from local file: ${template.source_path}`}
                              >
                                <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                                    d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                                </svg>
                                {template.source_path.split('/').pop()}
                              </span>
                            )}
                            {template.source_type === 'url' && (
                              <span
                                className="inline-flex items-center gap-1 rounded-sm bg-purple-500/20 px-1.5 py-0.5 text-xs text-purple-300"
                                title={`Loaded from URL: ${template.source_path}`}
                              >
                                <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                                    d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
                                </svg>
                                URL
                              </span>
                            )}
                          </div>
                          {/* Template labels */}
                          {template.labels && Object.keys(template.labels).length > 0 && (
                            <div className="mt-1.5">
                              <LabelsDisplay labels={template.labels} maxDisplay={0} />
                            </div>
                          )}
                          <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-zinc-500">
                            <a
                              href={`https://github.com/${template.owner}/${template.repo}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="flex items-center gap-1.5 hover:text-zinc-300"
                            >
                              <svg className="size-3.5 shrink-0" fill="currentColor" viewBox="0 0 24 24">
                                <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/>
                              </svg>
                              <span>{template.owner}/{template.repo}</span>
                            </a>
                            <a
                              href={`https://github.com/${template.owner}/${template.repo}/blob/${template.ref}/.github/workflows/${template.workflow_id}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="flex items-center gap-1.5 hover:text-zinc-300"
                            >
                              <svg className="size-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
                              </svg>
                              <span>{template.workflow_id}</span>
                              <span className="text-zinc-600">@</span>
                              <span>{template.ref}</span>
                            </a>
                          </div>
                        </div>
                        <div className="flex shrink-0 flex-col items-end gap-2">
                          {autoRequeueTemplateIds.has(template.id) && (
                            <span className="inline-flex items-center gap-1 rounded-sm bg-purple-500/20 px-1.5 py-0.5 text-xs text-purple-400">
                              <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                              </svg>
                              Auto-requeue enabled
                            </span>
                          )}
                          {isAdmin && template.in_config && (
                            <button
                              onClick={() => {
                                setPreselectedTemplateId(template.id);
                                setShowAddDialog(true);
                              }}
                              className="rounded-sm bg-blue-600 px-2.5 py-1.5 text-xs font-medium text-white hover:bg-blue-700"
                            >
                              Add to Queue
                            </button>
                          )}
                        </div>
                      </div>
                      {/* Workflow Inputs */}
                      {template.default_inputs && Object.keys(template.default_inputs).length > 0 && (
                        <div className="mt-3 border-t border-zinc-800 pt-3">
                          <h5 className="mb-2 text-xs font-medium text-zinc-400">Workflow Inputs</h5>
                          <div className="space-y-2">
                            {Object.entries(template.default_inputs).map(([key, value]) => (
                              <div key={key} className="text-xs">
                                <span className="font-medium text-zinc-400">{key}:</span>
                                {value.length > 80 ? (
                                  <pre className="mt-1 max-h-32 overflow-auto rounded-sm bg-zinc-800 p-2 font-mono text-zinc-300">
                                    {value.startsWith('{') || value.startsWith('[')
                                      ? (() => {
                                          try {
                                            return JSON.stringify(JSON.parse(value), null, 2);
                                          } catch {
                                            return value;
                                          }
                                        })()
                                      : value}
                                  </pre>
                                ) : (
                                  <span className="ml-1.5 font-mono text-zinc-300">{value}</span>
                                )}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              ) : templates.length > 0 ? (
                <div className="rounded-sm border border-dashed border-zinc-800 py-8 text-center text-zinc-500">
                  No templates match the selected filters
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
                    {busyRunners.map((runner) => {
                      const job = getJobForRunner(runner.id);
                      const template = job ? getTemplateForJob(job) : undefined;
                      return (
                        <RunnerCard key={runner.id} runner={runner} job={job} template={template} />
                      );
                    })}
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
        templates={templates.filter((t) => t.in_config)}
        isOpen={showAddDialog}
        onClose={() => {
          setShowAddDialog(false);
          setPreselectedTemplateId(undefined);
        }}
        preselectedTemplateId={preselectedTemplateId}
        autoRequeueTemplateIds={autoRequeueTemplateIds}
      />

      {/* Add Manual Job Dialog */}
      <AddManualJobDialog
        groupId={id!}
        templates={templates.filter((t) => t.in_config)}
        isOpen={showManualJobDialog}
        onClose={() => setShowManualJobDialog(false)}
      />

      {/* Bulk Action Bars */}
      {selectedTemplateIds.size > 0 && (
        <div className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-3 rounded-xs border border-zinc-700 bg-zinc-900 px-4 py-2 shadow-lg">
          <span className="text-sm text-zinc-300">{selectedTemplateIds.size} selected</span>
          <div className="h-4 w-px bg-zinc-700" />
          <button
            onClick={handleBulkAddToQueue}
            className="rounded-xs bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700"
          >
            Add to Queue
          </button>
          <button
            onClick={handleBulkAddToQueueWithAutoRequeue}
            className="rounded-xs bg-purple-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-purple-700"
          >
            Add with Auto-requeue
          </button>
          <button
            onClick={() => {
              setSelectedTemplateIds(new Set());
              setTemplateSelectionMode(false);
            }}
            className="text-xs text-zinc-400 hover:text-zinc-200"
          >
            Clear
          </button>
        </div>
      )}

      {selectedRunningIds.size > 0 && (
        <div className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-3 rounded-xs border border-zinc-700 bg-zinc-900 px-4 py-2 shadow-lg">
          <span className="text-sm text-zinc-300">{selectedRunningIds.size} selected</span>
          <div className="h-4 w-px bg-zinc-700" />
          <button
            onClick={handleBulkCancel}
            className="rounded-xs bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700"
          >
            Cancel
          </button>
          <button
            onClick={handleBulkEnableAutoRequeue}
            className="rounded-xs bg-purple-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-purple-700"
          >
            Enable Auto-requeue
          </button>
          <button
            onClick={handleBulkDisableAutoRequeue}
            className="rounded-xs bg-zinc-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-zinc-500"
          >
            Disable Auto-requeue
          </button>
          <button
            onClick={() => {
              setSelectedRunningIds(new Set());
              setRunningSelectionMode(false);
            }}
            className="text-xs text-zinc-400 hover:text-zinc-200"
          >
            Clear
          </button>
        </div>
      )}

      {selectedPendingIds.size > 0 && (
        <div className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-3 rounded-xs border border-zinc-700 bg-zinc-900 px-4 py-2 shadow-lg">
          <span className="text-sm text-zinc-300">{selectedPendingIds.size} selected</span>
          <div className="h-4 w-px bg-zinc-700" />
          <button
            onClick={handleBulkPause}
            className="rounded-xs bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-700"
          >
            Pause
          </button>
          <button
            onClick={handleBulkUnpause}
            className="rounded-xs bg-green-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-green-700"
          >
            Resume
          </button>
          <button
            onClick={handleBulkRemove}
            className="rounded-xs bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700"
          >
            Remove
          </button>
          <button
            onClick={() => {
              setSelectedPendingIds(new Set());
              setPendingSelectionMode(false);
            }}
            className="text-xs text-zinc-400 hover:text-zinc-200"
          >
            Clear
          </button>
        </div>
      )}

      {/* Bulk Action Confirmation Dialog */}
      {bulkActionConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60" onClick={() => setBulkActionConfirm(null)} />
          <div className="relative w-full max-w-lg mx-4 flex max-h-[80vh] flex-col rounded-xs border border-zinc-800 bg-zinc-900 shadow-xl">
            <div className="shrink-0 border-b border-zinc-800 px-4 py-3">
              <h2 className="text-lg font-semibold text-zinc-100">{bulkActionConfirm.title}</h2>
            </div>
            <div className="flex-1 overflow-y-auto p-4 space-y-4">
              <p className="text-sm text-zinc-300">{bulkActionConfirm.message}</p>
              {bulkActionConfirm.warning && (
                <div className="rounded-xs bg-amber-500/10 border border-amber-500/20 p-3">
                  <div className="flex items-start gap-2">
                    <svg className="size-5 shrink-0 text-amber-400 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                    </svg>
                    <p className="text-sm text-amber-400">{bulkActionConfirm.warning}</p>
                  </div>
                </div>
              )}
              {bulkActionConfirm.items && bulkActionConfirm.items.length > 0 && (
                <div className="rounded-xs border border-zinc-700 bg-zinc-800/50">
                  <ul className="divide-y divide-zinc-700/50">
                    {bulkActionConfirm.items.map((item, index) => {
                      const willBeSkipped = bulkActionConfirm.showDuplicateCheckbox && item.warning && !confirmDuplicates;
                      return (
                        <li
                          key={index}
                          className={`flex items-center justify-between px-3 py-2 text-sm ${willBeSkipped ? 'opacity-50' : ''}`}
                        >
                          <span className={`${item.warning ? 'text-amber-300' : 'text-zinc-300'} ${willBeSkipped ? 'line-through' : ''}`}>
                            {item.name}
                          </span>
                          {item.warning && (
                            <span className="flex items-center gap-1 text-xs text-amber-400">
                              <svg className="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                              </svg>
                              {willBeSkipped ? 'will be skipped' : 'has auto-requeue'}
                            </span>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                </div>
              )}
              {bulkActionConfirm.showDuplicateCheckbox && (
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={confirmDuplicates}
                    onChange={(e) => setConfirmDuplicates(e.target.checked)}
                    className="size-4 rounded-xs border-amber-600 bg-zinc-800 text-amber-500 focus:ring-amber-500 focus:ring-offset-zinc-900"
                  />
                  <span className="text-sm text-zinc-300">Include templates with existing auto-requeue jobs</span>
                </label>
              )}
            </div>
            <div className="shrink-0 flex justify-end gap-2 border-t border-zinc-800 px-4 py-3">
              <button
                onClick={() => {
                  setBulkActionConfirm(null);
                  setConfirmDuplicates(false);
                }}
                className="rounded-xs px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-800"
              >
                Cancel
              </button>
              <button
                onClick={() => bulkActionConfirm.onConfirm(confirmDuplicates)}
                className={`rounded-xs px-4 py-2 text-sm font-medium text-white ${bulkActionConfirm.buttonClass}`}
              >
                {bulkActionConfirm.buttonLabel}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Progress Indicator */}
      {bulkProgress && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="rounded-xs border border-zinc-700 bg-zinc-900 p-6 shadow-lg">
            <div className="flex items-center gap-3">
              <svg className="size-5 animate-spin text-blue-500" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
              </svg>
              <span className="text-sm text-zinc-300">
                {bulkProgress.action}: {bulkProgress.current} of {bulkProgress.total}
              </span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
