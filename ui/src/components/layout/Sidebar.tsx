import { NavLink } from 'react-router-dom';
import { useGroups } from '../../hooks/useGroups';
import { useUIStore } from '../../stores/uiStore';

export function Sidebar() {
  const { sidebarCollapsed } = useUIStore();
  const { data: groups, isLoading } = useGroups();

  if (sidebarCollapsed) {
    return null;
  }

  return (
    <aside className="fixed inset-y-0 left-0 z-30 w-64 border-r border-zinc-800 bg-zinc-900 pt-14">
      <nav className="flex h-full flex-col gap-2 p-4">
        <NavLink
          to="/"
          className={({ isActive }) =>
            `flex items-center gap-3 rounded-sm px-3 py-2 text-sm font-medium transition-colors ${
              isActive
                ? 'bg-blue-500/20 text-blue-400'
                : 'text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100'
            }`
          }
          end
        >
          <svg className="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"
            />
          </svg>
          Dashboard
        </NavLink>

        <div className="mt-4">
          <h3 className="px-3 text-xs font-semibold uppercase tracking-wider text-zinc-500">
            Groups
          </h3>
          <div className="mt-2 space-y-1">
            {isLoading ? (
              <div className="px-3 py-2 text-sm text-zinc-500">Loading...</div>
            ) : groups && groups.length > 0 ? (
              groups.map((group) => (
                <NavLink
                  key={group.id}
                  to={`/groups/${group.id}`}
                  className={({ isActive }) =>
                    `flex items-center justify-between rounded-sm px-3 py-2 text-sm transition-colors ${
                      isActive
                        ? 'bg-blue-500/20 text-blue-400'
                        : 'text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100'
                    }`
                  }
                >
                  <span className="truncate">{group.name}</span>
                  <div className="flex items-center gap-1.5">
                    {group.running_jobs > 0 && (
                      <span className="inline-flex items-center justify-center rounded-full bg-green-500/20 px-2 py-0.5 text-xs font-medium text-green-400">
                        {group.running_jobs}
                      </span>
                    )}
                    {group.queued_jobs > 0 && (
                      <span className="inline-flex items-center justify-center rounded-full bg-amber-500/20 px-2 py-0.5 text-xs font-medium text-amber-400">
                        {group.queued_jobs}
                      </span>
                    )}
                  </div>
                </NavLink>
              ))
            ) : (
              <div className="px-3 py-2 text-sm text-zinc-500">No groups configured</div>
            )}
          </div>
        </div>

        <div className="mt-4">
          <NavLink
            to="/runners"
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-sm px-3 py-2 text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-blue-500/20 text-blue-400'
                  : 'text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100'
              }`
            }
          >
            <svg className="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01"
              />
            </svg>
            All Runners
          </NavLink>
        </div>
      </nav>
    </aside>
  );
}
