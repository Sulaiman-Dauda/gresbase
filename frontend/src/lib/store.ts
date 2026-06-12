'use client'

import { create } from 'zustand'
import { api } from './api'

interface AppState {
  admin: any | null
  isAuthenticated: boolean
  sidebarCollapsed: boolean

  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
  checkAuth: () => Promise<void>
  setSidebarCollapsed: (c: boolean) => void
}

export const useStore = create<AppState>((set) => ({
  admin: null,
  isAuthenticated: false,
  sidebarCollapsed: false,

  login: async (email, password) => {
    // Auth tokens are returned as HttpOnly cookies (gb_access/gb_refresh) by the
    // server; we keep the access token in memory only (not localStorage) so the
    // Authorization header can also be sent within the session.
    const res = await api.login(email, password)
    api.setToken(res.token)
    set({ admin: res.admin, isAuthenticated: true })
  },

  logout: async () => {
    // Server clears the auth cookies; we drop in-memory state.
    try {
      await api.logout()
    } catch {
      // ignore network/logout errors — still clear local state
    }
    api.setToken(null)
    set({ admin: null, isAuthenticated: false })
  },

  checkAuth: async () => {
    // The gb_access HttpOnly cookie (if present) authenticates this call.
    try {
      const admin = await api.getMe()
      set({ admin, isAuthenticated: true })
    } catch {
      // Try a cookie-based refresh, then retry.
      try {
        await api.refresh()
        const admin = await api.getMe()
        set({ admin, isAuthenticated: true })
      } catch {
        set({ admin: null, isAuthenticated: false })
      }
    }
  },

  setSidebarCollapsed: (c) => set({ sidebarCollapsed: c }),
}))
