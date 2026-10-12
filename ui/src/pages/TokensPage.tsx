import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import type { APIToken, CreateAPITokenResponse } from '../types';

const EXPIRY_OPTIONS = [
  { days: 7, label: '7 days' },
  { days: 30, label: '30 days' },
  { days: 90, label: '90 days' },
  { days: 365, label: '1 year' },
];

function formatDate(value: string | null): string {
  return value ? new Date(value).toLocaleString() : 'Never';
}

function isExpired(token: APIToken): boolean {
  return token.expires_at !== null && new Date(token.expires_at) < new Date();
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <button
      onClick={handleCopy}
      className="shrink-0 rounded-sm bg-zinc-700 px-3 py-1.5 text-sm font-medium text-zinc-100 hover:bg-zinc-600"
    >
      {copied ? 'Copied!' : 'Copy'}
    </button>
  );
}

function NewTokenBanner({ created, onDismiss }: { created: CreateAPITokenResponse; onDismiss: () => void }) {
  const example = `curl -H "Authorization: Bearer ${created.token}" ${api.getApiUrl()}/groups`;

  return (
    <div className="space-y-3 rounded-sm border border-green-500/30 bg-green-500/10 p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="font-medium text-green-300">Token "{created.api_token.name}" created</p>
          <p className="text-sm text-zinc-400">Copy it now. You won't be able to see it again.</p>
        </div>
        <button onClick={onDismiss} className="text-sm text-zinc-400 hover:text-zinc-200">
          Done
        </button>
      </div>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-sm bg-zinc-950 px-3 py-2 font-mono text-sm text-zinc-100">
          {created.token}
        </code>
        <CopyButton value={created.token} />
      </div>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-sm bg-zinc-950 px-3 py-2 font-mono text-xs text-zinc-400">
          {example}
        </code>
        <CopyButton value={example} />
      </div>
    </div>
  );
}

export function TokensPage() {
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [expiresInDays, setExpiresInDays] = useState(90);
  const [created, setCreated] = useState<CreateAPITokenResponse | null>(null);

  const { data: tokens, isLoading, error } = useQuery({
    queryKey: ['api-tokens'],
    queryFn: () => api.getAPITokens(),
  });

  const createMutation = useMutation({
    mutationFn: () => api.createAPIToken(name.trim(), expiresInDays),
    onSuccess: (result) => {
      setCreated(result);
      setName('');
      queryClient.invalidateQueries({ queryKey: ['api-tokens'] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteAPIToken(id),
    onSuccess: (_, id) => {
      if (created?.api_token.id === id) setCreated(null);
      queryClient.invalidateQueries({ queryKey: ['api-tokens'] });
    },
  });

  const handleRevoke = (token: APIToken) => {
    if (window.confirm(`Revoke token "${token.name}"? Anything using it will stop working.`)) {
      deleteMutation.mutate(token.id);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-zinc-100">API Tokens</h1>
        <p className="mt-1 text-sm text-zinc-400">
          Personal tokens for scripts, CLIs and agents. They act as you, with your current role. Send them as{' '}
          <code className="font-mono text-zinc-300">Authorization: Bearer &lt;token&gt;</code>.
        </p>
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) createMutation.mutate();
        }}
        className="flex flex-col gap-2 sm:flex-row"
      >
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Token name, e.g. claude @ laptop"
          maxLength={100}
          className="min-w-0 flex-1 rounded-sm border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-zinc-500 focus:outline-hidden"
        />
        <select
          value={expiresInDays}
          onChange={(e) => setExpiresInDays(Number(e.target.value))}
          className="rounded-sm border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 focus:border-zinc-500 focus:outline-hidden"
        >
          {EXPIRY_OPTIONS.map((opt) => (
            <option key={opt.days} value={opt.days}>
              Expires in {opt.label}
            </option>
          ))}
        </select>
        <button
          type="submit"
          disabled={!name.trim() || createMutation.isPending}
          className="rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {createMutation.isPending ? 'Creating...' : 'Create token'}
        </button>
      </form>

      {createMutation.error && (
        <div className="rounded-sm border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
          Failed to create token: {createMutation.error.message}
        </div>
      )}

      {created && <NewTokenBanner created={created} onDismiss={() => setCreated(null)} />}

      {error && (
        <div className="rounded-sm border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
          Failed to load tokens: {error.message}
        </div>
      )}

      <div className="overflow-x-auto rounded-sm border border-zinc-800 bg-zinc-900">
        <table className="w-full min-w-[640px]">
          <thead>
            <tr className="border-b border-zinc-800 text-left text-xs uppercase text-zinc-500">
              <th className="px-4 py-3 font-medium">Name</th>
              <th className="px-4 py-3 font-medium">Token</th>
              <th className="px-4 py-3 font-medium">Created</th>
              <th className="px-4 py-3 font-medium">Last used</th>
              <th className="px-4 py-3 font-medium">Expires</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-sm text-zinc-500">
                  Loading...
                </td>
              </tr>
            )}
            {tokens?.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-sm text-zinc-500">
                  No API tokens yet.
                </td>
              </tr>
            )}
            {tokens?.map((token) => (
              <tr key={token.id} className="border-b border-zinc-800 last:border-b-0">
                <td className="px-4 py-3 font-medium text-zinc-100">{token.name}</td>
                <td className="px-4 py-3 font-mono text-sm text-zinc-400">{token.token_prefix}…</td>
                <td className="px-4 py-3 text-sm text-zinc-400">{formatDate(token.created_at)}</td>
                <td className="px-4 py-3 text-sm text-zinc-400">{formatDate(token.last_used_at)}</td>
                <td className="px-4 py-3 text-sm">
                  {isExpired(token) ? (
                    <span className="rounded-xs bg-red-500/20 px-1.5 py-0.5 text-xs text-red-400">expired</span>
                  ) : (
                    <span className="text-zinc-400">{formatDate(token.expires_at)}</span>
                  )}
                </td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => handleRevoke(token)}
                    disabled={deleteMutation.isPending}
                    className="text-sm text-red-400 hover:text-red-300 disabled:opacity-50"
                  >
                    Revoke
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
