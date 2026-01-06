import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface UIState {
  sidebarCollapsed: boolean;
  darkMode: boolean | null; // null = system preference
  toggleSidebar: () => void;
  setDarkMode: (mode: boolean | null) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      darkMode: null,
      toggleSidebar: () =>
        set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      setDarkMode: (mode) => set({ darkMode: mode }),
    }),
    {
      name: 'dispatchoor-ui',
    }
  )
);
