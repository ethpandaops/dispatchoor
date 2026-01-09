import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { BarChart, Bar, XAxis, YAxis, Tooltip } from 'recharts';
import { api } from '../../api/client';

interface MiniHistoryChartProps {
  groupId: string;
}

const COLORS = {
  completed: '#22c55e', // green-500
  failed: '#ef4444', // red-500
  cancelled: '#71717a', // zinc-500
};

function formatTimestamp(timestamp: string): string {
  const date = new Date(timestamp);
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
}

interface TooltipPayload {
  name: string;
  value: number;
  color: string;
}

interface CustomTooltipProps {
  active?: boolean;
  payload?: TooltipPayload[];
  label?: string;
}

function CustomTooltip({ active, payload, label }: CustomTooltipProps) {
  if (!active || !payload) return null;

  return (
    <div className="rounded-xs border border-zinc-700 bg-zinc-900 px-2 py-1 shadow-lg">
      <p className="mb-0.5 text-xs text-zinc-400">{label}</p>
      {payload.map((entry) => (
        <p key={entry.name} className="text-xs" style={{ color: entry.color }}>
          {entry.name}: {entry.value}
        </p>
      ))}
    </div>
  );
}

const CHART_HEIGHT = 80; // h-20 = 5rem = 80px

export function MiniHistoryChart({ groupId }: MiniHistoryChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(0);

  const { data, isLoading } = useQuery({
    queryKey: ['historyStats', groupId, '24h'],
    queryFn: () => api.getHistoryStats(groupId, '24h'),
    refetchInterval: 60000,
  });

  // Track container width with ResizeObserver.
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const updateWidth = () => {
      const { width } = container.getBoundingClientRect();
      if (width > 0) {
        setContainerWidth(width);
      }
    };

    // Check immediately.
    updateWidth();

    // Observe for size changes.
    const observer = new ResizeObserver(updateWidth);
    observer.observe(container);

    return () => observer.disconnect();
  }, []);

  const chartData = data?.buckets.map((bucket) => ({
    timestamp: formatTimestamp(bucket.timestamp),
    completed: bucket.completed,
    failed: bucket.failed,
    cancelled: bucket.cancelled,
  })) ?? [];

  const hasData = data && data.totals.completed + data.totals.failed + data.totals.cancelled > 0;

  return (
    <div ref={containerRef} className="h-20">
      {isLoading ? (
        <div className="flex h-full items-center justify-center">
          <div className="text-xs text-zinc-600">Loading...</div>
        </div>
      ) : !hasData ? (
        <div className="flex h-full items-center justify-center">
          <div className="text-xs text-zinc-600">No history data</div>
        </div>
      ) : containerWidth > 0 ? (
        <BarChart
          width={containerWidth}
          height={CHART_HEIGHT}
          data={chartData}
          margin={{ top: 5, right: 5, left: -30, bottom: 0 }}
        >
          <XAxis
            dataKey="timestamp"
            tick={{ fontSize: 8, fill: '#52525b' }}
            tickLine={false}
            axisLine={false}
            interval="preserveStartEnd"
          />
          <YAxis
            tick={{ fontSize: 8, fill: '#52525b' }}
            tickLine={false}
            axisLine={false}
            allowDecimals={false}
            width={30}
          />
          <Tooltip content={<CustomTooltip />} />
          <Bar dataKey="completed" stackId="a" fill={COLORS.completed} />
          <Bar dataKey="failed" stackId="a" fill={COLORS.failed} />
          <Bar dataKey="cancelled" stackId="a" fill={COLORS.cancelled} />
        </BarChart>
      ) : null}
    </div>
  );
}
