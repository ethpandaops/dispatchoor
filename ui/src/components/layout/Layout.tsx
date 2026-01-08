import { Outlet, useLocation } from 'react-router-dom';
import { Header } from './Header';
import { Sidebar } from './Sidebar';
import { useUIStore } from '../../stores/uiStore';

// Routes that should take up full viewport without padding
const fullBleedRoutes = ['/api-docs'];

export function Layout() {
  const { sidebarCollapsed } = useUIStore();
  const location = useLocation();
  const isFullBleed = fullBleedRoutes.includes(location.pathname);

  return (
    <div className="min-h-dvh bg-zinc-950">
      <Header />
      <Sidebar />
      <main
        className={`transition-all duration-200 ${
          sidebarCollapsed ? 'ml-0' : 'ml-64'
        }`}
      >
        {isFullBleed ? (
          <Outlet />
        ) : (
          <div className="p-6">
            <Outlet />
          </div>
        )}
      </main>
    </div>
  );
}
