import type { JobStatus, RunnerStatus } from '../../types';

interface StatusBadgeProps {
  status: JobStatus | RunnerStatus | 'idle';
  size?: 'sm' | 'md';
}

const jobStatusConfig: Record<JobStatus, { bg: string; text: string; dot: string }> = {
  pending: {
    bg: 'bg-amber-500/10',
    text: 'text-amber-400',
    dot: 'bg-amber-400',
  },
  triggered: {
    bg: 'bg-blue-500/10',
    text: 'text-blue-400',
    dot: 'bg-blue-400',
  },
  running: {
    bg: 'bg-green-500/10',
    text: 'text-green-400',
    dot: 'bg-green-400 animate-pulse',
  },
  completed: {
    bg: 'bg-emerald-500/10',
    text: 'text-emerald-400',
    dot: 'bg-emerald-400',
  },
  failed: {
    bg: 'bg-red-500/10',
    text: 'text-red-400',
    dot: 'bg-red-400',
  },
  cancelled: {
    bg: 'bg-zinc-500/10',
    text: 'text-zinc-400',
    dot: 'bg-zinc-400',
  },
};

const runnerStatusConfig: Record<RunnerStatus | 'idle', { bg: string; text: string; dot: string }> = {
  online: {
    bg: 'bg-green-500/10',
    text: 'text-green-400',
    dot: 'bg-green-500',
  },
  offline: {
    bg: 'bg-zinc-500/10',
    text: 'text-zinc-400',
    dot: 'bg-zinc-500',
  },
  idle: {
    bg: 'bg-green-500/10',
    text: 'text-green-400',
    dot: 'bg-green-500',
  },
};

export function StatusBadge({ status, size = 'sm' }: StatusBadgeProps) {
  const config =
    status in jobStatusConfig
      ? jobStatusConfig[status as JobStatus]
      : runnerStatusConfig[status as RunnerStatus | 'idle'];

  const sizeClasses = size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-sm';

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-sm font-medium ${config.bg} ${config.text} ${sizeClasses}`}
    >
      <span className={`size-1.5 rounded-full ${config.dot}`} />
      {status}
    </span>
  );
}
