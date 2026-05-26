import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface UIState {
  // Desktop preference: whether the pinned sidebar is collapsed (persisted).
  sidebarCollapsed: boolean;
  // Mobile-only: whether the overlay nav drawer is open (transient, not persisted).
  mobileNavOpen: boolean;
  darkMode: boolean | null; // null = system preference
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleMobileNav: () => void;
  setMobileNavOpen: (open: boolean) => void;
  setDarkMode: (mode: boolean | null) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      mobileNavOpen: false,
      darkMode: null,
      toggleSidebar: () =>
        set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      toggleMobileNav: () =>
        set((state) => ({ mobileNavOpen: !state.mobileNavOpen })),
      setMobileNavOpen: (open) => set({ mobileNavOpen: open }),
      setDarkMode: (mode) => set({ darkMode: mode }),
    }),
    {
      name: 'dispatchoor-ui',
      // Only persist durable preferences; the mobile drawer is transient.
      partialize: (state) => ({
        sidebarCollapsed: state.sidebarCollapsed,
        darkMode: state.darkMode,
      }),
    }
  )
);
