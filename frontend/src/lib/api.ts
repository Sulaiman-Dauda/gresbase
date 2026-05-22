const API_BASE = '/api/v1'

class ApiClient {
  private token: string | null = null

  setToken(token: string | null) {
    this.token = token
    if (token) localStorage.setItem('gresbase_token', token)
    else localStorage.removeItem('gresbase_token')
  }

  getToken(): string | null {
    if (this.token) return this.token
    this.token = localStorage.getItem('gresbase_token')
    return this.token
  }

  async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string> || {}),
    }
    if (!(options.body instanceof FormData)) {
      headers['Content-Type'] = 'application/json'
    }
    const token = this.getToken()
    if (token) headers['Authorization'] = `Bearer ${token}`

    const res = await fetch(`${API_BASE}${path}`, { ...options, headers, credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: `HTTP ${res.status}` }))
      throw new Error(err.message || err.error?.message || `Request failed (${res.status})`)
    }
    return res.json()
  }

  // Auth
  login = (email: string, password: string) =>
    this.request<{ token: string; refreshToken: string; admin: any }>('/auth/login', {
      method: 'POST', body: JSON.stringify({ email, password }),
    })

  refresh = (refreshToken: string) =>
    this.request<{ token: string; refreshToken: string }>('/auth/refresh', {
      method: 'POST', body: JSON.stringify({ refreshToken }),
    })

  // Admin
  getMe = () => this.request<any>('/admin/me')
  getAdmins = () => this.request<any[]>('/admin/users')
  createAdmin = (body: { email: string; password: string; role: string }) =>
    this.request<any>('/admin/users', { method: 'POST', body: JSON.stringify(body) })
  deleteAdmin = (id: string) => this.request<any>(`/admin/users/${id}`, { method: 'DELETE' })

  // Collections
  getCollections = () => this.request<any[]>('/collections')
  createCollection = (body: any) =>
    this.request<any>('/collections', { method: 'POST', body: JSON.stringify(body) })
  updateCollection = (id: string, body: any) =>
    this.request<any>(`/collections/${id}`, { method: 'PUT', body: JSON.stringify(body) })
  deleteCollection = (id: string) =>
    this.request<any>(`/collections/${id}`, { method: 'DELETE' })

  // Records
  getRecords = (collection: string, params?: Record<string, string>) => {
    const q = params ? '?' + new URLSearchParams(params).toString() : ''
    return this.request<{ items: any[]; page: number; perPage: number; totalItems: number; totalPages: number }>(
      `/records/${collection}${q}`
    )
  }
  createRecord = (collection: string, data: any) =>
    this.request<any>(`/records/${collection}`, { method: 'POST', body: JSON.stringify(data) })
  updateRecord = (collection: string, id: string, data: any) =>
    this.request<any>(`/records/${collection}/${id}`, { method: 'PUT', body: JSON.stringify(data) })
  deleteRecord = (collection: string, id: string) =>
    this.request<any>(`/records/${collection}/${id}`, { method: 'DELETE' })

  // Files
  getFileURL = (collection: string, recordId: string, filename: string) =>
    `${API_BASE}/files/${collection}/${recordId}/${filename}`

  // Certificates
  getCertificates = () => this.request<any[]>('/certificates')
  issueCertificate = (domain: string) =>
    this.request<any>('/certificates/issue', { method: 'POST', body: JSON.stringify({ domain }) })
  revokeCertificate = (id: string) =>
    this.request<any>(`/certificates/${id}`, { method: 'DELETE' })

  // API Keys
  getApiKeys = () => this.request<any[]>('/api-keys')
  createApiKey = (name: string) =>
    this.request<{ key: string; apiKey: any }>('/api-keys', { method: 'POST', body: JSON.stringify({ name }) })
  deleteApiKey = (id: string) => this.request<any>(`/api-keys/${id}`, { method: 'DELETE' })

  // Logs
  getLogs = (params?: Record<string, string>) => {
    const q = params ? '?' + new URLSearchParams(params).toString() : ''
    return this.request<{ items: any[]; page: number; totalItems: number }>(`/logs${q}`)
  }

  // Settings
  getSettings = () => this.request<any>('/settings')

  // Health
  health = () => this.request<any>('/health')
}

export const api = new ApiClient()
