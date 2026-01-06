import { useAllRunners } from '../hooks/useGroups';
import { StatusBadge } from '../components/common/StatusBadge';
import type { Runner } from '../types';

function RunnerRow({ runner }: { runner: Runner }) {
  const status = runner.busy ? 'running' : runner.status === 'online' ? 'idle' : 'offline';

  return (
    <tr className="border-b border-zinc-800 last:border-b-0">
      <td className="px-4 py-3">
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
          <span className="font-medium text-zinc-100">{runner.name}</span>
        </div>
      </td>
      <td className="px-4 py-3">
        <StatusBadge
          status={status === 'running' ? 'running' : status === 'idle' ? 'idle' : 'offline'}
        />
      </td>
      <td className="px-4 py-3 text-sm text-zinc-400">{runner.os || '-'}</td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap gap-1">
          {runner.labels.slice(0, 4).map((label) => (
            <span
              key={label}
              className="rounded-sm bg-zinc-800 px-1.5 py-0.5 text-xs text-zinc-400"
            >
              {label}
            </span>
          ))}
          {runner.labels.length > 4 && (
            <span className="text-xs text-zinc-500">+{runner.labels.length - 4}</span>
          )}
        </div>
      </td>
      <td className="px-4 py-3 text-sm text-zinc-400">
        {new Date(runner.last_seen_at).toLocaleString()}
      </td>
    </tr>
  );
}

export function RunnersPage() {
  const { data: runners, isLoading, error } = useAllRunners();

  const onlineCount = runners?.filter((r) => r.status === 'online').length || 0;
  const busyCount = runners?.filter((r) => r.busy).length || 0;
  const idleCount = onlineCount - busyCount;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-zinc-100">All Runners</h1>
        <div className="flex items-center gap-4 text-sm">
          <span className="text-zinc-400">
            <span className="font-semibold text-green-400">{idleCount}</span> idle
          </span>
          <span className="text-zinc-400">
            <span className="font-semibold text-amber-400">{busyCount}</span> busy
          </span>
          <span className="text-zinc-400">
            <span className="font-semibold text-zinc-200">{runners?.length || 0}</span> total
          </span>
        </div>
      </div>

      {error && (
        <div className="rounded-sm border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
          Failed to load runners: {error.message}
        </div>
      )}

      {isLoading ? (
        <div className="space-y-2">
          {[1, 2, 3, 4, 5].map((i) => (
            <div
              key={i}
              className="h-14 animate-pulse rounded-sm bg-zinc-900"
            />
          ))}
        </div>
      ) : runners && runners.length > 0 ? (
        <div className="overflow-hidden rounded-sm border border-zinc-800 bg-zinc-900">
          <table className="w-full">
            <thead className="border-b border-zinc-800 bg-zinc-900/50">
              <tr className="text-left text-sm font-medium text-zinc-400">
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">OS</th>
                <th className="px-4 py-3">Labels</th>
                <th className="px-4 py-3">Last Seen</th>
              </tr>
            </thead>
            <tbody>
              {runners.map((runner) => (
                <RunnerRow key={runner.id} runner={runner} />
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="rounded-sm border border-dashed border-zinc-800 py-12 text-center">
          <p className="text-zinc-500">
            No runners found. Make sure your GitHub token has access to the runners
            and the dispatcher is running.
          </p>
        </div>
      )}
    </div>
  );
}
