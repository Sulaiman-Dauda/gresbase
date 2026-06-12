const API_BASE = '/api/v1'

// readCookie returns the value of a non-HttpOnly cookie, or null. Used to read
// the gb_csrf double-submit token the server sets on login/refresh.
function readCookie(name: string): string | null {
  if (typeof document === 'undefined') return null
  const match = document.cookie.match(new RegExp('(?:^|; )' + name.replace(/([.$?*|{}()[\]\\/+^])/g, '\\$1') + '=([^;]*)'))
  return match ? decodeURIComponent(match[1]) : null
}

const MUTATING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

class ApiClient {
  // token is kept only in memory so SDK-style Authorization-header auth still
  // works within a session; it is no longer persisted to localStorage. Browser
  // auth is carried by the HttpOnly gb_access cookie set by the server.
  private token: string | null = null

  setToken(token: string | null) {
    this.token = token
  }

  getToken(): string | null {
    return this.token
  }

  async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const normalizedPath = path.startsWith(API_BASE)
      ? path.slice(API_BASE.length) || '/'
      : path

    const headers: Record<string, string> = {
      ...((options.headers as Record<string, string>) || {}),
    }
    if (!(options.body instanceof FormData)) {
      headers['Content-Type'] = 'application/json'
    }
    const token = this.getToken()
    if (token) headers['Authorization'] = `Bearer ${token}`

    // Double-submit CSRF: for cookie-authenticated mutating requests, echo the
    // gb_csrf cookie back in the X-CSRF-Token header.
    const method = (options.method || 'GET').toUpperCase()
    if (MUTATING_METHODS.has(method)) {
      const csrf = readCookie('gb_csrf')
      if (csrf) headers['X-CSRF-Token'] = csrf
    }

    const res = await fetch(`${API_BASE}${normalizedPath}`, { ...options, headers, credentials: 'include' })
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

  // Refresh uses the HttpOnly gb_refresh cookie; an explicit token may still be
  // passed for SDK-style callers that hold the refresh token in memory.
  refresh = (refreshToken?: string) =>
    this.request<{ token: string; refreshToken: string }>('/auth/refresh', {
      method: 'POST', body: JSON.stringify(refreshToken ? { refreshToken } : {}),
    })

  logout = () => this.request<{ message: string }>('/auth/logout', { method: 'POST' })

  // Admin
  getMe = () => this.request<any>('/admin/me')
  getAdmins = () => this.request<any[]>('/admin/users')
  createAdmin = (body: { email: string; password: string; role: string }) =>
    this.request<any>('/admin/users', { method: 'POST', body: JSON.stringify(body) })
  deleteAdmin = (id: string) => this.request<any>(`/admin/users/${id}`, { method: 'DELETE' })

  // Collections
  getCollections = () => this.request<any[]>('/collections')
  getCollection = (id: string) => this.request<any>(`/collections/${id}`)
  exportCollections = () => this.request<{ collections: any[] }>('/collections/export')
  importCollections = (collections: any[], deleteMissing = false) =>
    this.request<any>('/collections/import', {
      method: 'POST',
      body: JSON.stringify({ collections, deleteMissing }),
    })
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

  // Transactional collection-scoped batch (creates/updates/deletes in one transaction)
  batchRecords = (
    collection: string,
    body: { creates?: Record<string, any>[]; updates?: Record<string, Record<string, any>>; deletes?: string[] }
  ) =>
    this.request<{ created?: any[]; updated?: number; deleted?: number }>(`/batch/${encodeURIComponent(collection)}`, {
      method: 'POST',
      body: JSON.stringify(body),
    })

  // Files
  getFileURL = (collection: string, recordId: string, filename: string) =>
    `${API_BASE}/files/${encodeURIComponent(collection)}/${encodeURIComponent(recordId)}/${encodeURIComponent(filename)}`
  uploadFile = (collection: string, recordId: string, file: File) => {
    const formData = new FormData()
    formData.append('file', file)
    return this.request<any>(`/files/upload?collection=${encodeURIComponent(collection)}&record=${encodeURIComponent(recordId)}`, {
      method: 'POST',
      body: formData,
    })
  }
  promoteFiles = (collection: string, recordId: string, files: string[]) =>
    this.request<{ files: any[]; filenames: string[] }>('/files/promote', {
      method: 'POST',
      body: JSON.stringify({ collection, recordId, files }),
    })
  deleteFile = (collection: string, recordId: string, filename: string) =>
    this.request<any>(`/files/${encodeURIComponent(collection)}/${encodeURIComponent(recordId)}/${encodeURIComponent(filename)}`, { method: 'DELETE' })

  // API Keys
  getApiKeys = () => this.request<any[]>('/api-keys')
  createApiKey = (name: string, permissions: string[] = []) =>
    this.request<{ key: string; apiKey: any }>('/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name, permissions }),
    })
  deleteApiKey = (id: string) => this.request<any>(`/api-keys/${id}`, { method: 'DELETE' })

  // Logs
  getLogs = (params?: Record<string, string>) => {
    const q = params ? '?' + new URLSearchParams(params).toString() : ''
    return this.request<{ items: any[]; page: number; totalItems: number }>(`/logs${q}`)
  }

  // Settings
  getSettings = () => this.request<any>('/settings')
  updateSettings = (body: any) => this.request<any>('/settings', { method: 'PUT', body: JSON.stringify(body) })
  getEmailTemplates = () =>
    this.request<Array<{
      id: string
      name: string
      description: string
      placeholders: string[]
      defaultSubject: string
      defaultBody: string
      customSubject?: string
      customBody?: string
    }>>('/settings/email-templates')

  // Record auth
  getRecordAuthMethods = (collection: string) =>
    this.request<any>(`/collections/${collection}/auth-methods`)
  recordAuthWithPassword = (collection: string, identity: string, password: string) =>
    this.request<any>(`/collections/${collection}/auth/auth-with-password`, {
      method: 'POST',
      body: JSON.stringify({ identity, password }),
    })
  recordAuthOTPRequest = (collection: string, email: string) =>
    this.request<any>(`/collections/${collection}/auth/auth-otp-request`, {
      method: 'POST',
      body: JSON.stringify({ email }),
    })
  recordAuthOTPVerify = (collection: string, otpId: string, code: string) =>
    this.request<any>(`/collections/${collection}/auth/auth-with-otp`, {
      method: 'POST',
      body: JSON.stringify({ otpId, code }),
    })
  recordAuthWithOAuth2 = (
    collection: string,
    provider: string,
    code: string,
    state: string,
    redirectURL: string,
    codeVerifier?: string
  ) =>
    this.request<any>(`/collections/${collection}/auth/auth-with-oauth2`, {
      method: 'POST',
      body: JSON.stringify({ provider, code, state, redirectURL, codeVerifier }),
    })
  impersonateRecord = (collection: string, recordId: string) =>
    this.request<{ token: string; refreshToken: string; record: any }>(
      `/collections/${encodeURIComponent(collection)}/auth/impersonate/${encodeURIComponent(recordId)}`,
      { method: 'POST' }
    )
  requestRecordVerification = (collection: string, email: string) =>
    this.request<any>(`/collections/${collection}/auth/request-verification`, {
      method: 'POST',
      body: JSON.stringify({ email }),
    })
  requestRecordPasswordReset = (collection: string, email: string) =>
    this.request<any>(`/collections/${collection}/auth/request-password-reset`, {
      method: 'POST',
      body: JSON.stringify({ email }),
    })

  // Health & metrics
  health = () => this.request<any>('/health')
  metrics = () => this.request<any>('/metrics')

  // Feature flags
  features = () =>
    this.request<{
      vector: boolean
      realtime: boolean
      oauth: boolean
      acme: boolean
      embeddedDB: boolean
      typedSDK: boolean
      ruleSimulator: boolean
    }>('/features')

  // Vector search
  vectorSearch = (collection: string, field: string, vector: number[], limit?: number, distance?: string) =>
    this.request<{ items: any[]; totalItems: number }>(`/records/${collection}/search-vector`, {
      method: 'POST',
      body: JSON.stringify({ field, vector, limit, distance }),
    })

  // Rule presets & simulation
  rulePresets = () =>
    this.request<Array<{ key: string; label: string; description: string; value: string | null; kind: string }>>(
      '/collections/meta/rule-presets'
    )
  ruleSimulate = (body: {
    rule: string | null
    auth?: {
      isAdmin?: boolean
      isRecordAuth?: boolean
      id?: string
      role?: string
      email?: string
      tenant?: string
      collection?: string
      verified?: boolean
    }
    record?: Record<string, any>
    body?: Record<string, any>
    query?: Record<string, any>
  }) =>
    this.request<{
      valid?: boolean
      allowed?: boolean
      resolvedFilter?: string
      locked?: boolean
      error?: string
      note?: string
    }>('/collections/meta/rule-simulate', { method: 'POST', body: JSON.stringify(body) })

  // Typed SDK download
  downloadTypes = async () => {
    const token = this.getToken()
    const headers: Record<string, string> = {}
    if (token) headers['Authorization'] = `Bearer ${token}`
    const res = await fetch(`${API_BASE}/types.ts`, { headers, credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: `HTTP ${res.status}` }))
      throw new Error(err.message || err.error?.message || `Request failed (${res.status})`)
    }
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = 'gresbase.ts'
    anchor.click()
    URL.revokeObjectURL(url)
  }
}

export const api = new ApiClient()
