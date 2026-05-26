export interface AppConfig {
  apiUrl: string;
  // Base URL for Benchmarkoor (e.g. https://benchmarkoor.core.ethpandaops.io).
  // When set, jobs with a GitHub run_id display a link to the corresponding
  // benchmark run, filtered via the github.run_id metadata label.
  benchmarkoorUrl?: string;
}

const defaultConfig: AppConfig = {
  apiUrl: '/api/v1',
};

export function buildBenchmarkoorRunUrl(runId: number | string): string | undefined {
  const base = config.benchmarkoorUrl?.replace(/\/+$/, '');
  if (!base) return undefined;
  return `${base}/runs?labels=${encodeURIComponent('github.run_id')}:${encodeURIComponent(String(runId))}`;
}

let config: AppConfig = defaultConfig;
let configLoaded = false;
let configPromise: Promise<AppConfig> | null = null;

export async function loadConfig(): Promise<AppConfig> {
  if (configLoaded) {
    return config;
  }

  if (configPromise) {
    return configPromise;
  }

  configPromise = (async () => {
    try {
      const response = await fetch('/config.json');
      if (response.ok) {
        const loaded = await response.json();
        config = { ...defaultConfig, ...loaded };
      }
    } catch {
      console.warn('Failed to load config.json, using defaults');
    }
    configLoaded = true;
    return config;
  })();

  return configPromise;
}

export function getConfig(): AppConfig {
  return config;
}
