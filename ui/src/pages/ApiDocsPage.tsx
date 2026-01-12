import { useEffect, useRef } from 'react';
import { ApiReferenceReact } from '@scalar/api-reference-react';
import '@scalar/api-reference-react/style.css';
import { getConfig } from '../config';
import { useUIStore } from '../stores/uiStore';
import type { ReferenceProps } from '@scalar/api-reference';

export function ApiDocsPage() {
  const { sidebarCollapsed, setSidebarCollapsed } = useUIStore();
  const previousState = useRef<boolean | null>(null);
  const specUrl = `${getConfig().apiUrl}/openapi.json`;

  // Auto-collapse sidebar on mount, restore on unmount
  useEffect(() => {
    previousState.current = sidebarCollapsed;
    if (!sidebarCollapsed) {
      setSidebarCollapsed(true);
    }

    return () => {
      if (previousState.current === false) {
        setSidebarCollapsed(false);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally run only on mount/unmount
  }, []);

  const config: ReferenceProps['configuration'] = {
    spec: { url: specUrl },
    theme: 'purple',
    darkMode: true,
    layout: 'modern',
    hideDarkModeToggle: true,
    baseServerURL: getConfig().apiUrl,
  };

  return (
    <div className="fixed top-14 left-0 right-0 bottom-0 overflow-auto bg-zinc-950">
      <ApiReferenceReact configuration={config} />
    </div>
  );
}
