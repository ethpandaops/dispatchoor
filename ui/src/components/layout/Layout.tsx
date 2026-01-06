import { Outlet } from 'react-router-dom';
import { Header } from './Header';
import { Sidebar } from './Sidebar';
import { useUIStore } from '../../stores/uiStore';

export function Layout() {
  const { sidebarCollapsed } = useUIStore();

  return (
    <div className="min-h-dvh bg-zinc-950">
      <Header />
      <Sidebar />
      <main
        className={`transition-all duration-200 ${
          sidebarCollapsed ? 'ml-0' : 'ml-64'
        }`}
      >
        <div className="p-6">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
