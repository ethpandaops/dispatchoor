export interface AppConfig {
  apiUrl: string;
}

const defaultConfig: AppConfig = {
  apiUrl: '/api/v1',
};

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
