import { create } from 'zustand'

interface UIStore {
  // Sidebar state
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
  toggleSidebar: () => void

  // Confirmation dialog state
  confirmDialog: {
    open: boolean
    title: string
    description: string
    action: (() => void) | null
    variant: 'destructive' | 'default'
  }
  openConfirmDialog: (opts: {
    title: string
    description: string
    action: () => void
    variant?: 'destructive' | 'default'
  }) => void
  closeConfirmDialog: () => void
}

export const useUIStore = create<UIStore>()((set) => ({
  sidebarOpen: false,
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  toggleSidebar: () => set((state) => ({ sidebarOpen: !state.sidebarOpen })),

  confirmDialog: {
    open: false,
    title: '',
    description: '',
    action: null,
    variant: 'default',
  },
  openConfirmDialog: (opts) =>
    set({
      confirmDialog: {
        open: true,
        title: opts.title,
        description: opts.description,
        action: opts.action,
        variant: opts.variant ?? 'default',
      },
    }),
  closeConfirmDialog: () =>
    set({
      confirmDialog: {
        open: false,
        title: '',
        description: '',
        action: null,
        variant: 'default',
      },
    }),
}))
