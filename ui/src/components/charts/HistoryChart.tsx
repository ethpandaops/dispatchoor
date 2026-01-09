import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { BarChart, Bar, LineChart, Line, XAxis, YAxis, Tooltip } from 'recharts';
import { api } from '../../api/client';
import type { HistoryStatsTimeRange } from '../../types';

interface HistoryChartProps {
  groupId: string;
}

type ChartType = 'bar' | 'line';

const TIME_RANGES: { value: HistoryStatsTimeRange; label: string }[] = [
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
  { value: 'auto', label: 'Auto' },
];

const COLORS = {
  completed: '#22c55e', // green-500
  failed: '#ef4444', // red-500
  cancelled: '#71717a', // zinc-500
};

const CHART_HEIGHT = 192; // h-48 = 12rem = 192px

function formatTimestamp(timestamp: string, range: HistoryStatsTimeRange): string {
  const date = new Date(timestamp);

  switch (range) {
    case '1h':
    case '6h':
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
    case '24h':
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
    case '7d':
      return date.toLocaleDateString([], { weekday: 'short', hour: '2-digit', hour12: false });
    case '30d':
      return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
    case 'auto':
    default:
      // For auto, show date + time
      return date.toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', hour12: false });
  }
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
    <div className="rounded-xs border border-zinc-700 bg-zinc-900 px-3 py-2 shadow-lg">
      <p className="mb-1 text-xs text-zinc-400">{label}</p>
      {payload.map((entry) => (
        <p key={entry.name} className="text-sm" style={{ color: entry.color }}>
          {entry.name}: {entry.value}
        </p>
      ))}
    </div>
  );
}

export function HistoryChart({ groupId }: HistoryChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(0);
  const [timeRange, setTimeRange] = useState<HistoryStatsTimeRange>('auto');
  const [chartType, setChartType] = useState<ChartType>('bar');

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

  const { data, isLoading, error } = useQuery({
    queryKey: ['historyStats', groupId, timeRange],
    queryFn: () => api.getHistoryStats(groupId, timeRange),
    refetchInterval: 60000, // Refresh every minute
  });

  // Transform data for the chart
  const chartData = data?.buckets.map((bucket) => ({
    timestamp: formatTimestamp(bucket.timestamp, timeRange),
    completed: bucket.completed,
    failed: bucket.failed,
    cancelled: bucket.cancelled,
  })) ?? [];

  const hasData = data && data.totals.completed + data.totals.failed + data.totals.cancelled > 0;

  return (
    <div className="rounded-sm border border-zinc-800 bg-zinc-900 p-4">
      {/* Controls */}
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <span className="text-xs text-zinc-500">Time range:</span>
          <div className="flex gap-1">
            {TIME_RANGES.map((range) => (
              <button
                key={range.value}
                onClick={() => setTimeRange(range.value)}
                className={`rounded-xs px-2 py-1 text-xs transition-colors ${
                  timeRange === range.value
                    ? 'bg-zinc-700 text-zinc-200'
                    : 'text-zinc-500 hover:text-zinc-300'
                }`}
              >
                {range.label}
              </button>
            ))}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-xs text-zinc-500">Chart:</span>
          <div className="flex gap-1">
            <button
              onClick={() => setChartType('bar')}
              className={`rounded-xs px-2 py-1 text-xs transition-colors ${
                chartType === 'bar'
                  ? 'bg-zinc-700 text-zinc-200'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              Bar
            </button>
            <button
              onClick={() => setChartType('line')}
              className={`rounded-xs px-2 py-1 text-xs transition-colors ${
                chartType === 'line'
                  ? 'bg-zinc-700 text-zinc-200'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              Line
            </button>
          </div>
        </div>
      </div>

      {/* Chart */}
      <div ref={containerRef} className="h-48">
        {isLoading ? (
          <div className="flex h-full items-center justify-center">
            <div className="text-sm text-zinc-500">Loading...</div>
          </div>
        ) : error ? (
          <div className="flex h-full items-center justify-center">
            <div className="text-sm text-red-400">Failed to load history stats</div>
          </div>
        ) : !hasData ? (
          <div className="flex h-full items-center justify-center">
            <div className="text-sm text-zinc-500">No historical data available</div>
          </div>
        ) : containerWidth > 0 ? (
          chartType === 'bar' ? (
            <BarChart
              width={containerWidth}
              height={CHART_HEIGHT}
              data={chartData}
              margin={{ top: 5, right: 5, left: -20, bottom: 5 }}
            >
              <XAxis
                dataKey="timestamp"
                tick={{ fontSize: 10, fill: '#71717a' }}
                tickLine={false}
                axisLine={{ stroke: '#3f3f46' }}
                interval="preserveStartEnd"
              />
              <YAxis
                tick={{ fontSize: 10, fill: '#71717a' }}
                tickLine={false}
                axisLine={false}
                allowDecimals={false}
              />
              <Tooltip content={<CustomTooltip />} />
              <Bar dataKey="completed" stackId="a" fill={COLORS.completed} name="Completed" />
              <Bar dataKey="failed" stackId="a" fill={COLORS.failed} name="Failed" />
              <Bar dataKey="cancelled" stackId="a" fill={COLORS.cancelled} name="Cancelled" />
            </BarChart>
          ) : (
            <LineChart
              width={containerWidth}
              height={CHART_HEIGHT}
              data={chartData}
              margin={{ top: 5, right: 5, left: -20, bottom: 5 }}
            >
              <XAxis
                dataKey="timestamp"
                tick={{ fontSize: 10, fill: '#71717a' }}
                tickLine={false}
                axisLine={{ stroke: '#3f3f46' }}
                interval="preserveStartEnd"
              />
              <YAxis
                tick={{ fontSize: 10, fill: '#71717a' }}
                tickLine={false}
                axisLine={false}
                allowDecimals={false}
              />
              <Tooltip content={<CustomTooltip />} />
              <Line
                type="monotone"
                dataKey="completed"
                stroke={COLORS.completed}
                strokeWidth={2}
                dot={false}
                name="Completed"
              />
              <Line
                type="monotone"
                dataKey="failed"
                stroke={COLORS.failed}
                strokeWidth={2}
                dot={false}
                name="Failed"
              />
              <Line
                type="monotone"
                dataKey="cancelled"
                stroke={COLORS.cancelled}
                strokeWidth={2}
                dot={false}
                name="Cancelled"
              />
            </LineChart>
          )
        ) : null}
      </div>

      {/* Legend with totals */}
      {hasData && (
        <div className="mt-3 flex flex-wrap items-center justify-center gap-4 border-t border-zinc-800 pt-3">
          <div className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-full" style={{ backgroundColor: COLORS.completed }} />
            <span className="text-xs text-zinc-400">
              Completed ({data.totals.completed})
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-full" style={{ backgroundColor: COLORS.failed }} />
            <span className="text-xs text-zinc-400">
              Failed ({data.totals.failed})
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-full" style={{ backgroundColor: COLORS.cancelled }} />
            <span className="text-xs text-zinc-400">
              Cancelled ({data.totals.cancelled})
            </span>
          </div>
        </div>
      )}
    </div>
  );
}
