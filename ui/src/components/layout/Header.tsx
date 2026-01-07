import { useState, useRef, useEffect } from 'react';
import { useUIStore } from '../../stores/uiStore';
import { useAuthStore } from '../../stores/authStore';
import { StatusIndicator } from './StatusIndicator';

export function Header() {
  const { sidebarCollapsed, toggleSidebar } = useUIStore();
  const { user, logout } = useAuthStore();
  const [showUserMenu, setShowUserMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setShowUserMenu(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const handleLogout = async () => {
    await logout();
    setShowUserMenu(false);
  };

  return (
    <header className="sticky top-0 z-40 flex h-14 items-center gap-4 border-b border-zinc-800 bg-zinc-950 px-4">
      <button
        onClick={toggleSidebar}
        className="inline-flex size-9 items-center justify-center rounded-sm text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
        aria-label={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
      >
        <svg className="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M4 6h16M4 12h16M4 18h16"
          />
        </svg>
      </button>

      <div className="flex flex-1 items-center gap-2">
        <h1 className="text-lg font-semibold text-zinc-100">Dispatchoor</h1>
        <span className="rounded-sm bg-blue-500/20 px-2 py-0.5 text-xs font-medium text-blue-400">
          beta
        </span>
      </div>

      <div className="flex items-center gap-3">
        {/* System status indicator */}
        <StatusIndicator />

        {/* GitHub link */}
        <a
          href="https://github.com/ethpandaops/dispatchoor"
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex size-9 items-center justify-center rounded-sm text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
        >
          <svg className="size-5" fill="currentColor" viewBox="0 0 24 24">
            <path
              fillRule="evenodd"
              clipRule="evenodd"
              d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
            />
          </svg>
        </a>

        {/* User menu */}
        {user && (
          <div className="relative" ref={menuRef}>
            <button
              onClick={() => setShowUserMenu(!showUserMenu)}
              className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-zinc-300 hover:bg-zinc-800"
            >
              <div className="flex size-7 items-center justify-center rounded-full bg-zinc-700 text-sm font-medium uppercase">
                {user.username.charAt(0)}
              </div>
              <span className="text-sm">{user.username}</span>
              {user.role === 'admin' && (
                <span className="rounded-xs bg-amber-500/20 px-1.5 py-0.5 text-xs text-amber-400">
                  admin
                </span>
              )}
              <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M19 9l-7 7-7-7"
                />
              </svg>
            </button>

            {showUserMenu && (
              <div className="absolute right-0 mt-1 w-48 rounded-sm border border-zinc-800 bg-zinc-900 py-1 shadow-lg">
                <div className="px-3 py-2 border-b border-zinc-800">
                  <p className="text-sm font-medium text-zinc-200">{user.username}</p>
                  <p className="text-xs text-zinc-500">{user.auth_provider} auth</p>
                </div>
                <button
                  onClick={handleLogout}
                  className="w-full px-3 py-2 text-left text-sm text-zinc-300 hover:bg-zinc-800"
                >
                  Sign out
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </header>
  );
}
