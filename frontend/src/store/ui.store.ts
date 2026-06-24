/**
 * ui.store.ts — État de l'interface utilisateur (Zustand)
 * Sidebar, notifications, thème.
 */
import { create } from 'zustand'

export interface Notification {
  id:        string
  type:      'info' | 'success' | 'warning' | 'error'
  title:     string
  message?:  string
  createdAt: number
}

interface UIState {
  sidebarOpen:   boolean
  theme:         'light' | 'dark' | 'system'
  notifications: Notification[]

  toggleSidebar:      () => void
  setSidebarOpen:     (open: boolean) => void
  setTheme:           (theme: UIState['theme']) => void
  addNotification:    (n: Omit<Notification, 'id' | 'createdAt'>) => void
  removeNotification: (id: string) => void
  clearNotifications: () => void
}

let _notifId = 0

export const useUIStore = create<UIState>((set) => ({
  sidebarOpen:   true,
  theme:         'system',
  notifications: [],

  toggleSidebar:  () => set(s => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setTheme:       (theme) => set({ theme }),

  addNotification: (n) => set(s => ({
    notifications: [
      {
        ...n,
        id:        String(++_notifId),
        createdAt: Date.now(),
      },
      ...s.notifications,
    ].slice(0, 20),  /* max 20 notifications en mémoire */
  })),

  removeNotification: (id) => set(s => ({
    notifications: s.notifications.filter(n => n.id !== id),
  })),

  clearNotifications: () => set({ notifications: [] }),
}))
