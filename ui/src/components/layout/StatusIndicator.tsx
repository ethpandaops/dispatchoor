import { useState, useRef, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { useWebSocket } from '../../hooks/useWebSocket';
import type { ComponentStatus, SystemStatus } from '../../types';

function getStatusColor(status: ComponentStatus): string {
  switch (status) {
    case 'healthy':
      return 'bg-green-500';
    case 'degraded':
      return 'bg-amber-500';
    case 'unhealthy':
      return 'bg-red-500';
    default:
      return 'bg-zinc-600';
  }
}

function getStatusTextColor(status: ComponentStatus): string {
  switch (status) {
    case 'healthy':
      return 'text-green-400';
    case 'degraded':
      return 'text-amber-400';
    case 'unhealthy':
      return 'text-red-400';
    default:
      return 'text-zinc-500';
  }
}

function getOverallStatus(wsConnected: boolean, systemStatus?: SystemStatus): ComponentStatus {
  if (!wsConnected) return 'unhealthy';
  if (!systemStatus) return 'degraded';
  return systemStatus.status;
}

function getStatusLabel(status: ComponentStatus, wsConnected: boolean): string {
  if (!wsConnected) return 'Offline';
  switch (status) {
    case 'healthy':
      return 'Live';
    case 'degraded':
      return 'Degraded';
    case 'unhealthy':
      return 'Unhealthy';
    default:
      return 'Unknown';
  }
}

export function StatusIndicator() {
  const [showDropdown, setShowDropdown] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const { isConnected } = useWebSocket();

  const { data: systemStatus } = useQuery({
    queryKey: ['systemStatus'],
    queryFn: () => api.getStatus(),
    refetchInterval: 30000,
    staleTime: 10000,
  });

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setShowDropdown(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const overallStatus = getOverallStatus(isConnected, systemStatus);
  const statusLabel = getStatusLabel(overallStatus, isConnected);

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setShowDropdown(!showDropdown)}
        className="flex items-center gap-1.5 rounded-sm px-2 py-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-300"
      >
        <div className={`size-2 rounded-full ${getStatusColor(overallStatus)}`} />
        <span className="text-xs">{statusLabel}</span>
        <svg className="size-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {showDropdown && (
        <div className="absolute right-0 mt-1 w-72 rounded-sm border border-zinc-800 bg-zinc-900 shadow-lg">
          {/* WebSocket Status */}
          <div className="border-b border-zinc-800 px-3 py-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-zinc-400">WebSocket</span>
              <div className="flex items-center gap-1.5">
                <div className={`size-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
                <span className={`text-xs ${isConnected ? 'text-green-400' : 'text-red-400'}`}>
                  {isConnected ? 'Connected' : 'Disconnected'}
                </span>
              </div>
            </div>
          </div>

          {/* Database Status */}
          <div className="border-b border-zinc-800 px-3 py-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-zinc-400">Database</span>
              {systemStatus?.database ? (
                <div className="flex items-center gap-1.5">
                  <div className={`size-2 rounded-full ${getStatusColor(systemStatus.database.status)}`} />
                  <span className={`text-xs ${getStatusTextColor(systemStatus.database.status)}`}>
                    {systemStatus.database.status === 'healthy'
                      ? systemStatus.database.latency || 'OK'
                      : systemStatus.database.error || 'Error'}
                  </span>
                </div>
              ) : (
                <span className="text-xs text-zinc-500">—</span>
              )}
            </div>
          </div>

          {/* GitHub API Status */}
          <div className="border-b border-zinc-800 px-3 py-2">
            <div className="mb-1">
              <span className="text-xs font-medium text-zinc-400">GitHub API</span>
            </div>
            {systemStatus?.github ? (
              <div className="space-y-1.5">
                {/* Runners Client */}
                {systemStatus.github.runners && (
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-zinc-500">Runners</span>
                    <div className="flex items-center gap-1.5">
                      <div className={`size-2 rounded-full ${getStatusColor(systemStatus.github.runners.status)}`} />
                      <span className={`text-xs ${getStatusTextColor(systemStatus.github.runners.status)}`}>
                        {systemStatus.github.runners.connected
                          ? `${systemStatus.github.runners.rate_limit_remaining} remaining`
                          : 'Not configured'}
                      </span>
                    </div>
                  </div>
                )}
                {systemStatus.github.runners?.connected && systemStatus.github.runners.reset_in && (
                  <div className="text-right text-xs text-zinc-500">
                    Resets {systemStatus.github.runners.reset_in}
                  </div>
                )}
                {/* Dispatch Client */}
                {systemStatus.github.dispatch && (
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-zinc-500">Dispatch</span>
                    <div className="flex items-center gap-1.5">
                      <div className={`size-2 rounded-full ${getStatusColor(systemStatus.github.dispatch.status)}`} />
                      <span className={`text-xs ${getStatusTextColor(systemStatus.github.dispatch.status)}`}>
                        {systemStatus.github.dispatch.connected
                          ? `${systemStatus.github.dispatch.rate_limit_remaining} remaining`
                          : 'Not configured'}
                      </span>
                    </div>
                  </div>
                )}
                {systemStatus.github.dispatch?.connected && systemStatus.github.dispatch.reset_in && (
                  <div className="text-right text-xs text-zinc-500">
                    Resets {systemStatus.github.dispatch.reset_in}
                  </div>
                )}
                {!systemStatus.github.runners && !systemStatus.github.dispatch && (
                  <span className="text-xs text-zinc-500">No clients configured</span>
                )}
              </div>
            ) : (
              <span className="text-xs text-zinc-500">—</span>
            )}
          </div>

          {/* Queue Stats */}
          <div className="border-b border-zinc-800 px-3 py-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-zinc-400">Queue</span>
            </div>
            {systemStatus?.queue ? (
              <div className="mt-1 flex gap-3 text-xs">
                <span className="text-zinc-400">
                  <span className="text-amber-400">{systemStatus.queue.pending_jobs}</span> pending
                </span>
                <span className="text-zinc-400">
                  <span className="text-blue-400">{systemStatus.queue.triggered_jobs}</span> triggered
                </span>
                <span className="text-zinc-400">
                  <span className="text-green-400">{systemStatus.queue.running_jobs}</span> running
                </span>
              </div>
            ) : (
              <span className="text-xs text-zinc-500">—</span>
            )}
          </div>

          {/* Version Info */}
          <div className="px-3 py-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-zinc-400">Version</span>
              {systemStatus?.version ? (
                <span className="text-xs text-zinc-300">{systemStatus.version.version}</span>
              ) : (
                <span className="text-xs text-zinc-500">—</span>
              )}
            </div>
            {systemStatus?.version && (
              <div className="mt-1 text-xs text-zinc-500">
                {systemStatus.version.git_commit.substring(0, 7)} · {systemStatus.version.build_date}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
