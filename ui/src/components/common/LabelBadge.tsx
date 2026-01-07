// Color palettes for keys and values (hash-based assignment)
const keyColors = [
  'bg-blue-500/20 text-blue-300',
  'bg-purple-500/20 text-purple-300',
  'bg-cyan-500/20 text-cyan-300',
  'bg-pink-500/20 text-pink-300',
  'bg-indigo-500/20 text-indigo-300',
  'bg-teal-500/20 text-teal-300',
];

const valueColors = [
  'bg-emerald-500/20 text-emerald-300',
  'bg-amber-500/20 text-amber-300',
  'bg-rose-500/20 text-rose-300',
  'bg-lime-500/20 text-lime-300',
  'bg-orange-500/20 text-orange-300',
  'bg-sky-500/20 text-sky-300',
];

function getColorForString(str: string, palette: string[]): string {
  const hash = str.split('').reduce((acc, char) => acc + char.charCodeAt(0), 0);
  return palette[hash % palette.length];
}

interface LabelBadgeProps {
  labelKey: string;
  value: string;
  onClick?: () => void;
  isActive?: boolean;
}

export function LabelBadge({ labelKey, value, onClick, isActive }: LabelBadgeProps) {
  const keyColor = getColorForString(labelKey, keyColors);
  const valueColor = getColorForString(value, valueColors);

  const baseClasses = 'inline-flex rounded-xs overflow-hidden text-xs';
  const interactiveClasses = onClick ? 'cursor-pointer hover:opacity-80' : '';
  const activeClasses = isActive ? 'ring-2 ring-white/30' : '';

  const badge = (
    <span className={`${baseClasses} ${interactiveClasses} ${activeClasses}`} onClick={onClick}>
      <span className={`px-1.5 py-0.5 ${keyColor}`}>{labelKey}</span>
      <span className={`px-1.5 py-0.5 ${valueColor}`}>{value}</span>
    </span>
  );

  return badge;
}

interface LabelsDisplayProps {
  labels?: Record<string, string>;
  maxDisplay?: number;
}

export function LabelsDisplay({ labels, maxDisplay = 3 }: LabelsDisplayProps) {
  if (!labels || Object.keys(labels).length === 0) {
    return null;
  }

  const entries = Object.entries(labels);
  const displayEntries = maxDisplay > 0 ? entries.slice(0, maxDisplay) : entries;
  const remainingCount = entries.length - displayEntries.length;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {displayEntries.map(([key, value]) => (
        <LabelBadge key={key} labelKey={key} value={value} />
      ))}
      {remainingCount > 0 && (
        <span className="rounded-xs bg-zinc-700/50 px-1.5 py-0.5 text-xs text-zinc-400">
          +{remainingCount}
        </span>
      )}
    </div>
  );
}
