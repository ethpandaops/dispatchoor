import { useState, useEffect, type FormEvent } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { useAuthStore } from '../stores/authStore';
import { api } from '../api/client';
import type { HealthAuthConfig } from '../types';

export function LoginPage() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [apiAvailable, setApiAvailable] = useState<boolean | null>(null);
  const [authConfig, setAuthConfig] = useState<HealthAuthConfig | null>(null);
  const { login, isAuthenticated, isLoading, error, clearError } = useAuthStore();
  const location = useLocation();

  const from = (location.state as { from?: { pathname: string } })?.from?.pathname || '/';

  // Check API health on mount and periodically
  useEffect(() => {
    let mounted = true;

    const checkHealth = async () => {
      try {
        const health = await api.getHealth();
        if (mounted) {
          setApiAvailable(true);
          setAuthConfig(health.config.auth);
        }
      } catch {
        if (mounted) {
          setApiAvailable(false);
          setAuthConfig(null);
        }
      }
    };

    checkHealth();

    // Poll more frequently when API is unavailable
    const interval = setInterval(checkHealth, apiAvailable === false ? 5000 : 30000);

    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, [apiAvailable]);

  if (isAuthenticated) {
    return <Navigate to={from} replace />;
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    clearError();
    try {
      await login(username, password);
    } catch {
      // Error is handled in the store
    }
  };

  const handleGitHubLogin = () => {
    window.location.href = api.getGitHubAuthUrl();
  };

  const showBasicAuth = authConfig?.basic ?? false;
  const showGitHubAuth = authConfig?.github ?? false;
  const noAuthConfigured = authConfig !== null && !showBasicAuth && !showGitHubAuth;

  return (
    <div className="min-h-dvh flex items-center justify-center bg-zinc-950 px-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <img
            src="/images/dispatchoor_logo_white.png"
            alt="Dispatchoor Logo"
            className="h-24 mx-auto mb-4"
          />
          <h1 className="text-2xl font-bold text-white">Dispatchoor</h1>
          <p className="text-zinc-400 mt-2">Sign in to manage your workflows</p>
        </div>

        <div className="bg-zinc-900 rounded-lg p-6 border border-zinc-800">
          {/* API unavailable warning */}
          {apiAvailable === false && (
            <div className="bg-red-500/10 border border-red-500/20 text-red-400 px-4 py-3 rounded-sm text-sm mb-4">
              <p className="font-medium">Unable to connect to API server</p>
              <p className="mt-1 text-red-400/80">Please check if the server is running.</p>
            </div>
          )}

          {/* Loading state while checking API */}
          {apiAvailable === null && (
            <div className="flex items-center justify-center py-8">
              <div className="flex items-center gap-2 text-zinc-400">
                <svg className="size-5 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                </svg>
                <span>Connecting to API...</span>
              </div>
            </div>
          )}

          {/* No auth methods configured */}
          {noAuthConfigured && (
            <div className="text-center py-4 text-zinc-400">
              <p>No authentication methods configured.</p>
              <p className="text-sm mt-1">Please contact your administrator.</p>
            </div>
          )}

          {/* Basic auth form */}
          {showBasicAuth && (
            <form onSubmit={handleSubmit} className="space-y-4">
              {error && (
                <div className="bg-red-500/10 border border-red-500/20 text-red-400 px-4 py-3 rounded-sm text-sm">
                  {error}
                </div>
              )}

              <div>
                <label htmlFor="username" className="block text-sm font-medium text-zinc-300 mb-1">
                  Username
                </label>
                <input
                  id="username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="w-full px-3 py-2 bg-zinc-800 border border-zinc-700 rounded-sm text-white placeholder-zinc-500 focus:outline-hidden focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  placeholder="Enter your username"
                  required
                  disabled={isLoading || apiAvailable === false}
                />
              </div>

              <div>
                <label htmlFor="password" className="block text-sm font-medium text-zinc-300 mb-1">
                  Password
                </label>
                <input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full px-3 py-2 bg-zinc-800 border border-zinc-700 rounded-sm text-white placeholder-zinc-500 focus:outline-hidden focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  placeholder="Enter your password"
                  required
                  disabled={isLoading || apiAvailable === false}
                />
              </div>

              <button
                type="submit"
                disabled={isLoading || apiAvailable === false}
                className="w-full py-2 px-4 bg-blue-600 hover:bg-blue-700 disabled:bg-blue-600/50 text-white font-medium rounded-sm transition-colors focus:outline-hidden focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 focus:ring-offset-zinc-900"
              >
                {isLoading ? 'Signing in...' : 'Sign in'}
              </button>
            </form>
          )}

          {/* Divider between basic auth and GitHub auth */}
          {showBasicAuth && showGitHubAuth && (
            <div className="mt-6">
              <div className="relative">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-zinc-700" />
                </div>
                <div className="relative flex justify-center text-sm">
                  <span className="px-2 bg-zinc-900 text-zinc-500">Or continue with</span>
                </div>
              </div>
            </div>
          )}

          {/* GitHub auth button */}
          {showGitHubAuth && (
            <button
              onClick={handleGitHubLogin}
              disabled={isLoading || apiAvailable === false}
              className={`w-full py-2 px-4 bg-zinc-800 hover:bg-zinc-700 disabled:bg-zinc-800/50 text-white font-medium rounded-sm transition-colors border border-zinc-700 flex items-center justify-center gap-2 ${showBasicAuth ? 'mt-4' : ''}`}
            >
              <svg className="size-5" fill="currentColor" viewBox="0 0 24 24">
                <path
                  fillRule="evenodd"
                  clipRule="evenodd"
                  d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"
                />
              </svg>
              Sign in with GitHub
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
