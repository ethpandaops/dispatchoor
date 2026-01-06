import { Link } from 'react-router-dom';
import { useGroups, useSystemStatus } from '../hooks/useGroups';
import type { GroupWithStats } from '../types';

function GroupCard({ group }: { group: GroupWithStats }) {
  const totalJobs = group.queued_jobs + group.running_jobs;

  return (
    <Link
      to={`/groups/${group.id}`}
      className="block rounded-sm border border-zinc-800 bg-zinc-900 p-6 transition-colors hover:border-zinc-700 hover:bg-zinc-800/50"
    >
      <div className="flex items-start justify-between">
        <div>
          <h3 className="text-lg font-semibold text-zinc-100">{group.name}</h3>
          {group.description && (
            <p className="mt-1 text-sm text-zinc-400">{group.description}</p>
          )}
        </div>
        {!group.enabled && (
          <span className="rounded-sm bg-zinc-800 px-2 py-0.5 text-xs font-medium text-zinc-500">
            Disabled
          </span>
        )}
      </div>

      <div className="mt-4 grid grid-cols-3 gap-4">
        <div>
          <p className="text-2xl font-bold text-zinc-100">{totalJobs}</p>
          <p className="text-xs text-zinc-500">Jobs in queue</p>
        </div>
        <div>
          <p className="text-2xl font-bold text-green-400">{group.idle_runners}</p>
          <p className="text-xs text-zinc-500">Idle runners</p>
        </div>
        <div>
          <p className="text-2xl font-bold text-amber-400">{group.busy_runners}</p>
          <p className="text-xs text-zinc-500">Busy runners</p>
        </div>
      </div>

      <div className="mt-4 flex flex-wrap gap-1">
        {group.runner_labels.map((label) => (
          <span
            key={label}
            className="rounded-sm bg-zinc-800 px-2 py-0.5 text-xs text-zinc-400"
          >
            {label}
          </span>
        ))}
      </div>

      <div className="mt-4 text-sm text-zinc-500">
        {group.template_count} job template{group.template_count !== 1 ? 's' : ''}
      </div>
    </Link>
  );
}

function StatusSummary() {
  const { data: status, isLoading } = useSystemStatus();

  if (isLoading || !status) {
    return null;
  }

  return (
    <div className="flex items-center gap-3 rounded-sm border border-zinc-800 bg-zinc-900 px-4 py-2">
      <span
        className={`size-2 rounded-full ${
          status.status === 'ok' ? 'bg-green-500' : 'bg-red-500'
        }`}
      />
      <span className="text-sm font-medium text-zinc-200">
        System {status.status}
      </span>
      <span className="text-xs text-zinc-500">v{status.version}</span>
    </div>
  );
}

export function DashboardPage() {
  const { data: groups, isLoading, error } = useGroups();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-zinc-100">Dashboard</h1>
        <StatusSummary />
      </div>

      {error && (
        <div className="rounded-sm border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
          Failed to load groups: {error.message}
        </div>
      )}

      {isLoading ? (
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {[1, 2, 3].map((i) => (
            <div
              key={i}
              className="h-48 animate-pulse rounded-sm border border-zinc-800 bg-zinc-900"
            />
          ))}
        </div>
      ) : groups && groups.length > 0 ? (
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {groups.map((group) => (
            <GroupCard key={group.id} group={group} />
          ))}
        </div>
      ) : (
        <div className="rounded-sm border border-dashed border-zinc-800 py-12 text-center">
          <p className="text-zinc-500">
            No groups configured yet. Add groups to your configuration file to get started.
          </p>
        </div>
      )}
    </div>
  );
}
