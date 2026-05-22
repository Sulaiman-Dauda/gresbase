'use client'

import { create } from 'zustand'
import { api } from './api'

interface AppState {
  admin: any | null
  isAuthenticated: boolean
  sidebarCollapsed: boolean

  login: (email: string, password: string) => Promise<void>
  logout: () => void
  checkAuth: () => Promise<void>
  setSidebarCollapsed: (c: boolean) => void
}

export const useStore = create<AppState>((set) => ({
  admin: null,
  isAuthenticated: false,
  sidebarCollapsed: false,

  login: async (email, password) => {
    const res = await api.login(email, password)
    api.setToken(res.token)
    localStorage.setItem('gresbase_refresh', res.refreshToken)
    set({ admin: res.admin, isAuthenticated: true })
  },

  logout: () => {
    api.setToken(null)
    localStorage.removeItem('gresbase_token')
    localStorage.removeItem('gresbase_refresh')
    set({ admin: null, isAuthenticated: false })
  },

  checkAuth: async () => {
    const token = localStorage.getItem('gresbase_token')
    if (!token) return
    api.setToken(token)
    try {
      const admin = await api.getMe()
      set({ admin, isAuthenticated: true })
    } catch {
      // Try refresh
      const refresh = localStorage.getItem('gresbase_refresh')
      if (refresh) {
        try {
          const { token: newToken, refreshToken } = await api.refresh(refresh)
          api.setToken(newToken)
          localStorage.setItem('gresbase_token', newToken)
          localStorage.setItem('gresbase_refresh', refreshToken)
          const admin = await api.getMe()
          set({ admin, isAuthenticated: true })
        } catch {
          localStorage.removeItem('gresbase_token')
          localStorage.removeItem('gresbase_refresh')
        }
      }
    }
  },

  setSidebarCollapsed: (c) => set({ sidebarCollapsed: c }),
}))
