/**
 * Gresbase JavaScript/TypeScript SDK
 *
 * A proper isomorphic SDK for browsers and Node.js, matching PocketBase's JS SDK
 * capabilities. Supports:
 *   - Admin and Record authentication
 *   - CRUD operations with real-time subscriptions
 *   - File upload/download
 *   - Realtime (SSE + WebSocket fallback)
 *   - Auto-refresh tokens
 *   - TypeScript generics for typed records
 *
 * @example
 * ```ts
 * import { Gresbase } from 'gresbase-sdk'
 *
 * const pb = new Gresbase('http://localhost:8080')
 *
 * // Admin auth
 * await pb.admins.authWithPassword('admin@example.com', 'password')
 *
 * // Record (end-user) auth
 * await pb.collection('users').authWithPassword('user@example.com', 'password')
 *
 * // CRUD
 * const records = await pb.collection('posts').getList(1, 30, { filter: 'published = true' })
 * const record = await pb.collection('posts').create({ title: 'Hello' })
 * ```
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface AuthModel {
  id: string
  email?: string
  username?: string
  verified?: boolean
  created?: string
  updated?: string
  [key: string]: any
}

export interface AdminAuthModel {
  id: string
  email: string
  role: string
  avatar?: string
  created_at?: string
  updated_at?: string
}

export interface RecordAuthResponse<T = any> {
  token: string
  refresh_token?: string
  record: T
}

export interface AdminAuthResponse {
  token: string
  refreshToken: string
  admin: AdminAuthModel
}

export interface ListResult<T = any> {
  page: number
  perPage: number
  totalItems: number
  totalPages: number
  items: T[]
}

export interface RealtimeMessage {
  client_id?: string
  event: string
  channel?: string
  topic?: string
  data?: any
  timestamp?: number
}

export interface RealtimeSubscription {
  unsubscribe: () => void
}

export type UnsubscribeFunc = () => void

// ---------------------------------------------------------------------------
// Auth Store (auto-refresh, persistence)
// ---------------------------------------------------------------------------

class AuthStore {
  private _token: string = ''
  private _refreshToken: string = ''
  private _model: AuthModel | AdminAuthModel | null = null
  private _isAdmin: boolean = false
  private _refreshTimer: any = null
  private _baseUrl: string

  constructor(baseUrl: string) {
    this._baseUrl = baseUrl
    this._loadFromStorage()
  }

  get token(): string { return this._token }
  get refreshToken(): string { return this._refreshToken }
  get model(): AuthModel | AdminAuthModel | null { return this._model }
  get isValid(): boolean { return !!this._token }
  get isAdmin(): boolean { return this._isAdmin }

  save(token: string, refreshToken: string, model: AuthModel | AdminAuthModel | null, isAdmin = false): void {
    this._token = token
    this._refreshToken = refreshToken
    this._model = model
    this._isAdmin = isAdmin
    this._persist()
    this._scheduleRefresh()
  }

  clear(): void {
    this._token = ''
    this._refreshToken = ''
    this._model = null
    this._isAdmin = false
    if (this._refreshTimer) clearTimeout(this._refreshTimer)
    this._persist()
  }

  async refresh(): Promise<boolean> {
    if (!this._refreshToken) return false
    try {
      const resp = await fetch(`${this._baseUrl}/api/v1/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken: this._refreshToken }),
      })
      if (!resp.ok) {
        this.clear()
        return false
      }
      const data = await resp.json()
      this._token = data.token
      this._refreshToken = data.refreshToken
      this._persist()
      this._scheduleRefresh()
      return true
    } catch {
      this.clear()
      return false
    }
  }

  private _scheduleRefresh(): void {
    if (this._refreshTimer) clearTimeout(this._refreshTimer)
    // Refresh 1 minute before expiry (assuming 15 min token)
    this._refreshTimer = setTimeout(() => this.refresh(), 14 * 60 * 1000)
  }

  private _persist(): void {
    if (typeof localStorage !== 'undefined') {
      if (this._token) {
        localStorage.setItem('gresbase_auth', JSON.stringify({
          token: this._token,
          refreshToken: this._refreshToken,
          model: this._model,
          isAdmin: this._isAdmin,
        }))
      } else {
        localStorage.removeItem('gresbase_auth')
      }
    }
  }

  private _loadFromStorage(): void {
    if (typeof localStorage !== 'undefined') {
      const stored = localStorage.getItem('gresbase_auth')
      if (stored) {
        try {
          const data = JSON.parse(stored)
          this._token = data.token || ''
          this._refreshToken = data.refreshToken || ''
          this._model = data.model || null
          this._isAdmin = data.isAdmin || false
          if (this._token) this._scheduleRefresh()
        } catch { /* ignore */ }
      }
    }
  }
}

// ---------------------------------------------------------------------------
// HTTP Client
// ---------------------------------------------------------------------------

class HttpClient {
  constructor(
    private baseUrl: string,
    private authStore: AuthStore,
  ) {}

  private async _send<T>(method: string, path: string, body?: any, options: { headers?: Record<string, string>; params?: Record<string, string> } = {}): Promise<T> {
    const url = new URL(`${this.baseUrl}${path}`)
    if (options.params) {
      Object.entries(options.params).forEach(([k, v]) => url.searchParams.set(k, v))
    }

    const headers: Record<string, string> = {
      ...options.headers,
    }

    if (!(body instanceof FormData)) {
      headers['Content-Type'] = 'application/json'
    }

    if (this.authStore.token) {
      headers['Authorization'] = `Bearer ${this.authStore.token}`
    }

    let resp = await fetch(url.toString(), {
      method,
      headers,
      body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined,
      credentials: 'include',
    })

    // Auto-refresh on 401
    if (resp.status === 401 && this.authStore.refreshToken) {
      const refreshed = await this.authStore.refresh()
      if (refreshed) {
        headers['Authorization'] = `Bearer ${this.authStore.token}`
        resp = await fetch(url.toString(), {
          method,
          headers,
          body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined,
          credentials: 'include',
        })
      }
    }

    if (!resp.ok) {
      const errBody = await resp.json().catch(() => ({ message: `HTTP ${resp.status}` }))
      throw new ClientError(resp.status, errBody)
    }

    return resp.json()
  }

  get<T>(path: string, params?: Record<string, string>): Promise<T> {
    return this._send<T>('GET', path, undefined, { params })
  }

  post<T>(path: string, body?: any): Promise<T> {
    return this._send<T>('POST', path, body)
  }

  put<T>(path: string, body?: any): Promise<T> {
    return this._send<T>('PUT', path, body)
  }

  patch<T>(path: string, body?: any): Promise<T> {
    return this._send<T>('PATCH', path, body)
  }

  delete<T>(path: string): Promise<T> {
    return this._send<T>('DELETE', path)
  }
}

// ---------------------------------------------------------------------------
// Error
// ---------------------------------------------------------------------------

export class ClientError extends Error {
  status: number
  data: any

  constructor(status: number, data: any) {
    super(data?.message || `Request failed with status ${status}`)
    this.name = 'ClientError'
    this.status = status
    this.data = data
  }
}

// ---------------------------------------------------------------------------
// Record Service (CRUD + Auth for a single collection)
// ---------------------------------------------------------------------------

export class RecordService<T = any> {
  private http: HttpClient
  private collectionName: string

  constructor(http: HttpClient, collectionName: string) {
    this.http = http
    this.collectionName = collectionName
  }

  // CRUD

  /**
   * List records with filtering, sorting, pagination, expansion, and field picking.
   */
  async getList(page = 1, perPage = 30, options: {
    filter?: string
    sort?: string
    expand?: string
    fields?: string
    skipTotal?: boolean
  } = {}): Promise<ListResult<T>> {
    const params: Record<string, string> = {
      page: String(page),
      perPage: String(perPage),
    }
    if (options.filter) params.filter = options.filter
    if (options.sort) params.sort = options.sort
    if (options.expand) params.expand = options.expand
    if (options.fields) params.fields = options.fields
    if (options.skipTotal) params.skipTotal = '1'

    return this.http.get<ListResult<T>>(`/api/v1/records/${this.collectionName}`, params)
  }

  /** Get all records (auto-paginated). */
  async getFullList(options: {
    filter?: string
    sort?: string
    expand?: string
    fields?: string
    batch?: number
  } = {}): Promise<T[]> {
    const batch = options.batch || 200
    const items: T[] = []
    let page = 1

    while (true) {
      const result = await this.getList(page, batch, options)
      items.push(...result.items)
      if (result.page >= result.totalPages) break
      page++
    }

    return items
  }

  /** Get first matching record. */
  async getFirstListItem(filter: string, options: { sort?: string; expand?: string; fields?: string } = {}): Promise<T> {
    const result = await this.getList(1, 1, { ...options, filter })
    if (result.items.length === 0) throw new ClientError(404, { message: 'Record not found' })
    return result.items[0]
  }

  /** Get a single record by ID. */
  async getOne(id: string, options: { expand?: string; fields?: string } = {}): Promise<T> {
    const params: Record<string, string> = {}
    if (options.expand) params.expand = options.expand
    if (options.fields) params.fields = options.fields
    return this.http.get<T>(`/api/v1/records/${this.collectionName}/${id}`, params)
  }

  /** Create a new record. */
  async create(body: Partial<T> | FormData): Promise<T> {
    return this.http.post<T>(`/api/v1/records/${this.collectionName}`, body)
  }

  /** Update an existing record. */
  async update(id: string, body: Partial<T> | FormData): Promise<T> {
    return this.http.put<T>(`/api/v1/records/${this.collectionName}/${id}`, body)
  }

  /** Delete a record. */
  async delete(id: string): Promise<boolean> {
    await this.http.delete(`/api/v1/records/${this.collectionName}/${id}`)
    return true
  }

  // Record Auth

  /**
   * Authenticate with email/password.
   * Returns the auth response with token and record data.
   */
  async authWithPassword(identity: string, password: string): Promise<RecordAuthResponse<T>> {
    const result = await this.http.post<RecordAuthResponse<T>>(
      `/api/v1/collections/${this.collectionName}/auth/auth-with-password`,
      { identity, password }
    )
    return result
  }

  /** Request an OTP for authentication. */
  async requestOTP(email: string): Promise<{ otpId: string }> {
    return this.http.post<{ otpId: string }>(
      `/api/v1/collections/${this.collectionName}/auth/auth-otp-request`,
      { email }
    )
  }

  /** Verify OTP and get auth tokens. */
  async authWithOTP(otpId: string, code: string): Promise<RecordAuthResponse<T>> {
    const result = await this.http.post<RecordAuthResponse<T>>(
      `/api/v1/collections/${this.collectionName}/auth/auth-with-otp`,
      { otpId, code }
    )
    return result
  }

  /** Inspect auth methods and OAuth2 providers for this collection. */
  async getAuthMethods(): Promise<any> {
    return this.http.get<any>(`/api/v1/collections/${this.collectionName}/auth-methods`)
  }

  /** Complete PocketBase-style OAuth2 record authentication. */
  async authWithOAuth2(provider: string, code: string, state: string, redirectURL: string, codeVerifier?: string): Promise<RecordAuthResponse<T>> {
    return this.http.post<RecordAuthResponse<T>>(
      `/api/v1/collections/${this.collectionName}/auth/auth-with-oauth2`,
      { provider, code, state, redirectURL, codeVerifier }
    )
  }

  /** Get OAuth2 redirect URL. */
  oAuth2URL(provider: string): string {
    return `${this.http['baseUrl']}/api/v1/collections/${this.collectionName}/auth/oauth2/${provider}`
  }

  /** Request password reset. */
  async requestPasswordReset(email: string): Promise<void> {
    await this.http.post(`/api/v1/collections/${this.collectionName}/auth/request-password-reset`, { email })
  }

  /** Confirm password reset. */
  async confirmPasswordReset(token: string, password: string): Promise<void> {
    await this.http.post(`/api/v1/collections/${this.collectionName}/auth/confirm-password-reset`, { token, password })
  }

  /** Request email verification. */
  async requestVerification(email: string): Promise<void> {
    await this.http.post(`/api/v1/collections/${this.collectionName}/auth/request-verification`, { email })
  }

  /** Confirm email verification. */
  async confirmVerification(token: string): Promise<void> {
    await this.http.post(`/api/v1/collections/${this.collectionName}/auth/confirm-verification`, { token })
  }

  /** Impersonate a record (requires admin auth). */
  async impersonate(recordId: string): Promise<RecordAuthResponse<T>> {
    return this.http.post<RecordAuthResponse<T>>(
      `/api/v1/collections/${this.collectionName}/auth/impersonate/${recordId}`
    )
  }
}

// ---------------------------------------------------------------------------
// Admin Service
// ---------------------------------------------------------------------------

export class AdminService {
  constructor(private http: HttpClient) {}

  async authWithPassword(email: string, password: string): Promise<AdminAuthResponse> {
    return this.http.post<AdminAuthResponse>('/api/v1/auth/login', { email, password })
  }

  async refresh(): Promise<{ token: string; refreshToken: string }> {
    return this.http.post('/api/v1/auth/refresh')
  }

  async getMe(): Promise<AdminAuthModel> {
    return this.http.get('/api/v1/admin/me')
  }

  async listUsers(): Promise<AdminAuthModel[]> {
    return this.http.get('/api/v1/admin/users')
  }

  async createUser(body: { email: string; password: string; role: string }): Promise<AdminAuthModel> {
    return this.http.post('/api/v1/admin/users', body)
  }

  async deleteUser(id: string): Promise<void> {
    await this.http.delete(`/api/v1/admin/users/${id}`)
  }
}

// ---------------------------------------------------------------------------
// File Service
// ---------------------------------------------------------------------------

export class FileService {
  private baseUrl: string

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl
  }

  /** Get a file download URL. */
  getURL(collection: string, recordId: string, filename: string, options?: { thumb?: string }): string {
    const params = options?.thumb ? `?thumb=${options.thumb}` : ''
    return `${this.baseUrl}/api/v1/files/${collection}/${recordId}/${filename}${params}`
  }

  /** Create a FormData object for file upload with record data. */
  createUploadForm(file: File | Blob, fileName: string, recordData?: Record<string, any>): FormData {
    const form = new FormData()
    form.append('file', file, fileName)
    if (recordData) {
      for (const [key, value] of Object.entries(recordData)) {
        if (value !== undefined && value !== null) {
          form.append(key, typeof value === 'string' ? value : JSON.stringify(value))
        }
      }
    }
    return form
  }
}

// ---------------------------------------------------------------------------
// Realtime Service
// ---------------------------------------------------------------------------

export class RealtimeService {
  private baseUrl: string
  private wsUrl: string
  private sseUrl: string
  private eventSource: EventSource | null = null
  private ws: WebSocket | null = null
  private listeners: Map<string, Set<(data: any) => void>> = new Map()
  private reconnectTimer: any = null
  private clientId: string = ''
  private useSSE: boolean = true

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl
    this.wsUrl = baseUrl.replace(/^http/, 'ws') + '/api/v1/realtime'
    this.sseUrl = baseUrl + '/api/v1/sse'
    this.clientId = 'client_' + Math.random().toString(36).substring(2, 10)
  }

  /** Subscribe to realtime events on a channel/collection. */
  subscribe(channel: string, callback: (data: RealtimeMessage) => void): RealtimeSubscription {
    const key = channel
    if (!this.listeners.has(key)) {
      this.listeners.set(key, new Set())
    }
    this.listeners.get(key)!.add(callback)

    // Start connection if not already started
    if (!this.eventSource && !this.ws) {
      this._connect()
    }

    // Send subscription
    this._sendSubscription('subscribe', channel)

    return {
      unsubscribe: () => {
        this.listeners.get(key)?.delete(callback)
        this._sendSubscription('unsubscribe', channel)
      },
    }
  }

  /** Disconnect and clean up. */
  disconnect(): void {
    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
    }
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
    }
    this.listeners.clear()
  }

  private _connect(): void {
    if (this.useSSE) {
      this._connectSSE()
    } else {
      this._connectWS()
    }
  }

  private _connectSSE(): void {
    this.eventSource = new EventSource(this.sseUrl)

    this.eventSource.onmessage = (event) => {
      try {
        const msg: RealtimeMessage = JSON.parse(event.data)
        this._dispatch(msg.channel || msg.topic || '', msg)
      } catch { /* ignore parse errors */ }
    }

    this.eventSource.onerror = () => {
      this.eventSource?.close()
      this.eventSource = null
      this.reconnectTimer = setTimeout(() => this._connect(), 3000)
    }
  }

  private _connectWS(): void {
    try {
      this.ws = new WebSocket(this.wsUrl)

      this.ws.onmessage = (event) => {
        try {
          const msg: RealtimeMessage = JSON.parse(event.data)
          this._dispatch(msg.channel || msg.topic || '', msg)
        } catch { /* ignore */ }
      }

      this.ws.onclose = () => {
        this.ws = null
        this.reconnectTimer = setTimeout(() => this._connect(), 3000)
      }

      this.ws.onerror = () => {
        this.ws?.close()
      }
    } catch {
      // WebSocket failed, fall back to SSE
      this.useSSE = true
      this._connectSSE()
    }
  }

  private _sendSubscription(type: string, channel: string): void {
    const msg = JSON.stringify({
      type,
      clientId: this.clientId,
      subscriptions: [channel],
      channel,
    })

    // Send via active transport
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(msg)
    }
    // SSE subscriptions via POST
    if (this.useSSE) {
      fetch(`${this.baseUrl}/api/v1/realtime`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: msg,
      }).catch(() => {})
    }
  }

  private _dispatch(channel: string, data: RealtimeMessage): void {
    this.listeners.get(channel)?.forEach(cb => cb(data))
    this.listeners.get('*')?.forEach(cb => cb(data))
  }
}

// ---------------------------------------------------------------------------
// Main Gresbase Client
// ---------------------------------------------------------------------------

export class Gresbase {
  private http: HttpClient
  private authStore: AuthStore
  private _baseUrl: string

  readonly admins: AdminService
  readonly files: FileService
  readonly realtime: RealtimeService

  constructor(baseUrl = 'http://localhost:8080') {
    // Normalize base URL
    this._baseUrl = baseUrl.replace(/\/+$/, '')
    this.authStore = new AuthStore(this._baseUrl)
    this.http = new HttpClient(this._baseUrl, this.authStore)

    this.admins = new AdminService(this.http)
    this.files = new FileService(this._baseUrl)
    this.realtime = new RealtimeService(this._baseUrl)
  }

  /** Get the base URL. */
  get baseUrl(): string { return this._baseUrl }

  /** Get the current auth token. */
  get authToken(): string { return this.authStore.token }

  /** Get the current auth model (admin or record). */
  get authModel(): AuthModel | AdminAuthModel | null { return this.authStore.model }

  /** Check if authenticated. */
  get authIsValid(): boolean { return this.authStore.isValid }

  /** Get a RecordService for the given collection. */
  collection<T = any>(name: string): RecordService<T> {
    return new RecordService<T>(this.http, name)
  }

  /** Clear auth state (logout). */
  async logout(): Promise<void> {
    try {
      await this.http.post('/api/v1/auth/logout')
    } catch { /* ignore */ }
    this.authStore.clear()
  }

  /** Health check. */
  async health(): Promise<any> {
    return this.http.get('/api/v1/health')
  }

  /** Get application settings. */
  async getSettings(): Promise<any> {
    return this.http.get('/api/v1/settings')
  }

  /** Run an arbitrary API request (for custom extensions). */
  async send<T = any>(path: string, options: { method?: string; body?: any; params?: Record<string, string>; headers?: Record<string, string> } = {}): Promise<T> {
    return this.http['_send']<T>(options.method || 'GET', path, options.body, {
      headers: options.headers,
      params: options.params,
    })
  }
}

export default Gresbase
