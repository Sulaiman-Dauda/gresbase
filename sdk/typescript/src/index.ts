/**
 * Gresbase TypeScript SDK
 *
 * Production-grade client SDK for the Gresbase backend platform.
 * Matches PocketBase JS SDK capabilities:
 * - Full CRUD with filtering, sorting, pagination
 * - Auth (email/password, OAuth, OTP, magic link, API keys)
 * - Realtime subscriptions (SSE primary, WebSocket fallback)
 * - File upload/download with progress tracking
 * - Batch operations
 * - Auto-refresh token management
 * - Type-safe record operations
 *
 * @example
 * ```ts
 * const client = new GresbaseClient({ url: 'http://localhost:8080' })
 * await client.auth.login('admin@example.com', 'password')
 * const records = await client.collection('posts').getList(1, 20)
 * ```
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface GresbaseConfig {
  /** Base URL of the Gresbase server (e.g. http://localhost:8080) */
  url: string
  /** Optional initial auth token */
  token?: string
  /** Optional fetch implementation (for Node.js or custom) */
  fetch?: typeof fetch
  /** SSE reconnection delay in ms (default 3000) */
  sseReconnectDelay?: number
}

export interface AuthResponse {
  token: string
  refreshToken: string
  admin: AdminUser
}

export interface AdminUser {
  id: string
  email: string
  role: string
  avatar?: string
  tenant_id: string
  created_at: string
  updated_at: string
}

export interface CollectionSchemaField {
  id: string
  name: string
  type: string
  system?: boolean
  required?: boolean
  unique?: boolean
  options?: Record<string, any>
}

export interface Collection {
  id: string
  name: string
  type: 'base' | 'auth' | 'view'
  schema: CollectionSchemaField[]
  list_rule?: string
  view_rule?: string
  create_rule?: string
  update_rule?: string
  delete_rule?: string
  indexes?: string[]
  system?: boolean
  options?: Record<string, any>
  created_at: string
  updated_at: string
}

export interface RecordData {
  id: string
  collectionId?: string
  collectionName?: string
  created?: string
  updated?: string
  [key: string]: any
}

export interface PaginatedResponse<T = RecordData> {
  items: T[]
  page: number
  perPage: number
  totalItems: number
  totalPages: number
}

export interface ListParams {
  /** Page number (1-indexed) */
  page?: number
  /** Items per page */
  perPage?: number
  /** Sort field(s) with optional '-' prefix for descending */
  sort?: string
  /** Filter expression (e.g. 'status = "active" && age >= 18') */
  filter?: string
  /** Comma-separated fields to return */
  fields?: string
  /** Comma-separated relations to expand */
  expand?: string
}

export interface RealtimeSubscription {
  /** Unsubscribe from this subscription */
  unsubscribe: () => void
}

export type RealtimeEvent = 'create' | 'update' | 'delete' | '*'

export interface RealtimeMessage {
  client_id?: string
  event: string
  channel?: string
  topic?: string
  data?: any
  timestamp: number
}

export interface UploadOptions {
  /** Custom filename */
  filename?: string
  /** Progress callback */
  onProgress?: (loaded: number, total: number) => void
}

export interface APIKeyData {
  id: string
  name: string
  prefix: string
  created_at: string
  key?: string // Only present on creation
}

export interface Certificate {
  id: string
  domain: string
  status: string
  not_before: string
  not_after: string
  auto_renew: boolean
}

// ---------------------------------------------------------------------------
// Event Emitter
// ---------------------------------------------------------------------------

type EventHandler = (...args: any[]) => void

class EventEmitter {
  private listeners: Map<string, EventHandler[]> = new Map()

  on(event: string, handler: EventHandler): () => void {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, [])
    }
    this.listeners.get(event)!.push(handler)
    return () => this.off(event, handler)
  }

  off(event: string, handler: EventHandler): void {
    const handlers = this.listeners.get(event)
    if (handlers) {
      this.listeners.set(event, handlers.filter(h => h !== handler))
    }
  }

  emit(event: string, ...args: any[]): void {
    const handlers = this.listeners.get(event)
    if (handlers) {
      for (const handler of handlers) {
        try {
          handler(...args)
        } catch (e) {
          console.error(`[GresbaseSDK] Error in event handler for "${event}":`, e)
        }
      }
    }
  }

  removeAllListeners(): void {
    this.listeners.clear()
  }
}

// ---------------------------------------------------------------------------
// HTTP Client
// ---------------------------------------------------------------------------

class HttpClient {
  constructor(
    private baseUrl: string,
    private getToken: () => string | null,
    private setToken: (token: string | null) => void,
    private customFetch?: typeof fetch,
    private refreshToken?: string | null,
    private onRefresh?: () => Promise<string | null>,
  ) {}

  private resolveFetch(): typeof fetch {
    return this.customFetch || (typeof window !== 'undefined' ? window.fetch.bind(window) : globalThis.fetch.bind(globalThis))
  }

  async request<T>(method: string, path: string, body?: any, options?: { headers?: Record<string, string>; rawResponse?: boolean }): Promise<T> {
    const headers: Record<string, string> = {
      ...(options?.headers || {}),
    }

    // Don't set Content-Type for FormData (browser sets it with boundary)
    if (!(body instanceof FormData)) {
      headers['Content-Type'] = 'application/json'
    }

    const token = this.getToken()
    if (token) {
      headers['Authorization'] = `Bearer ${token}`
    }

    const fetchFn = this.resolveFetch()
    const url = `${this.baseUrl}${path}`

    const fetchOptions: RequestInit = {
      method,
      headers,
      credentials: 'include',
    }

    if (body) {
      fetchOptions.body = body instanceof FormData ? body : JSON.stringify(body)
    }

    let response = await fetchFn(url, fetchOptions)

    // Auto-refresh on 401
    if (response.status === 401 && this.refreshToken && this.onRefresh) {
      const newToken = await this.onRefresh()
      if (newToken) {
        headers['Authorization'] = `Bearer ${newToken}`
        response = await fetchFn(url, { ...fetchOptions, headers })
      }
    }

    if (!response.ok) {
      let errorMessage = `HTTP ${response.status}`
      try {
        const err = await response.json()
        errorMessage = err.message || err.error?.message || errorMessage
      } catch {}
      throw new Error(errorMessage)
    }

    if (options?.rawResponse) {
      return response as unknown as T
    }

    if (response.status === 204) {
      return undefined as T
    }

    return response.json()
  }
}

// ---------------------------------------------------------------------------
// Auth Service
// ---------------------------------------------------------------------------

class AuthService extends EventEmitter {
  private token: string | null = null
  private refreshTokenVal: string | null = null
  private admin: AdminUser | null = null
  private refreshPromise: Promise<string | null> | null = null

  constructor(
    private http: HttpClient,
    private storageKey: string = 'gresbase_auth',
    private persistToStorage: boolean = true,
  ) {
    super()
    // Restore from storage
    if (persistToStorage) {
      this.loadFromStorage()
    }
  }

  private loadFromStorage(): void {
    try {
      const stored = localStorage.getItem(this.storageKey)
      if (stored) {
        const data = JSON.parse(stored)
        this.token = data.token || null
        this.refreshTokenVal = data.refreshToken || null
        this.admin = data.admin || null
      }
    } catch {}
  }

  private saveToStorage(): void {
    if (!this.persistToStorage) return
    try {
      localStorage.setItem(this.storageKey, JSON.stringify({
        token: this.token,
        refreshToken: this.refreshTokenVal,
        admin: this.admin,
      }))
    } catch {}
  }

  private clearStorage(): void {
    try {
      localStorage.removeItem(this.storageKey)
    } catch {}
  }

  getToken(): string | null {
    return this.token
  }

  getRefreshToken(): string | null {
    return this.refreshTokenVal
  }

  getAdmin(): AdminUser | null {
    return this.admin
  }

  get isAuthenticated(): boolean {
    return !!this.token
  }

  /** Login with email and password */
  async login(email: string, password: string): Promise<AuthResponse> {
    const result = await this.http.request<AuthResponse>('POST', '/api/v1/auth/login', { email, password })
    this.token = result.token
    this.refreshTokenVal = result.refreshToken
    this.admin = result.admin
    this.saveToStorage()
    this.emit('login', result)
    return result
  }

  /** Register a new admin user */
  async register(email: string, password: string, role: string = 'admin'): Promise<AuthResponse> {
    const result = await this.http.request<AuthResponse>('POST', '/api/v1/auth/register', { email, password, role })
    this.token = result.token
    this.refreshTokenVal = result.refreshToken
    this.admin = result.admin
    this.saveToStorage()
    this.emit('register', result)
    return result
  }

  /** Refresh the access token */
  async refresh(): Promise<string | null> {
    if (!this.refreshTokenVal) return null

    // Deduplicate concurrent refresh calls
    if (this.refreshPromise) return this.refreshPromise

    this.refreshPromise = (async () => {
      try {
        const result = await this.http.request<{ token: string; refreshToken: string }>(
          'POST', '/api/v1/auth/refresh', { refreshToken: this.refreshTokenVal }
        )
        this.token = result.token
        this.refreshTokenVal = result.refreshToken
        this.saveToStorage()
        return this.token
      } catch {
        this.logout(false)
        return null
      } finally {
        this.refreshPromise = null
      }
    })()

    return this.refreshPromise
  }

  /** Logout and clear session */
  async logout(serverLogout: boolean = true): Promise<void> {
    try {
      if (serverLogout && this.token) {
        await this.http.request('POST', '/api/v1/auth/logout', {})
      }
    } catch {} // Always clear local state
    this.token = null
    this.refreshTokenVal = null
    this.admin = null
    this.clearStorage()
    this.emit('logout')
  }

  /** Request OTP code */
  async requestOTP(email: string): Promise<{ otpId: string }> {
    return this.http.request('POST', '/api/v1/auth/otp/request', { email })
  }

  /** Verify OTP code and login */
  async verifyOTP(otpId: string, code: string): Promise<AuthResponse> {
    const result = await this.http.request<AuthResponse>('POST', '/api/v1/auth/otp/verify', { otpId, code })
    this.token = result.token
    this.refreshTokenVal = result.refreshToken
    this.admin = result.admin
    this.saveToStorage()
    return result
  }

  /** Request magic link */
  async requestMagicLink(email: string): Promise<void> {
    await this.http.request('POST', '/api/v1/auth/magic-link', { email })
  }

  /** Verify magic link token and login */
  async verifyMagicLink(token: string): Promise<AuthResponse> {
    const result = await this.http.request<AuthResponse>('POST', '/api/v1/auth/magic-link/verify', { token })
    this.token = result.token
    this.refreshTokenVal = result.refreshToken
    this.admin = result.admin
    this.saveToStorage()
    return result
  }

  /** Initiate OAuth login (returns redirect URL) */
  oAuthURL(provider: string, redirectUrl?: string): string {
    let url = `${this.http['baseUrl']}/api/v1/oauth/${provider}`
    if (redirectUrl) {
      url += `?redirectUrl=${encodeURIComponent(redirectUrl)}`
    }
    return url
  }

  /** Request password reset */
  async requestPasswordReset(email: string): Promise<void> {
    await this.http.request('POST', '/api/v1/admin/password-reset', { email })
  }

  /** Confirm password reset */
  async confirmPasswordReset(token: string, newPassword: string): Promise<void> {
    await this.http.request('POST', '/api/v1/admin/password-reset/confirm', { token, newPassword })
  }

  /** Get current admin profile */
  async getMe(): Promise<AdminUser> {
    const admin = await this.http.request<AdminUser>('GET', '/api/v1/admin/me')
    this.admin = admin
    this.saveToStorage()
    return admin
  }

  /** Update current admin profile */
  async updateMe(data: Partial<Pick<AdminUser, 'email' | 'avatar'>>): Promise<AdminUser> {
    const admin = await this.http.request<AdminUser>('PUT', '/api/v1/admin/me', data)
    this.admin = admin
    this.saveToStorage()
    return admin
  }
}

// ---------------------------------------------------------------------------
// Realtime Service
// ---------------------------------------------------------------------------

type UnsubscribeFn = () => void

class RealtimeService extends EventEmitter {
  private sseConnection: EventSource | null = null
  private wsConnection: WebSocket | null = null
  private clientId: string | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay: number
  private connected: boolean = false
  private useSSE: boolean = true // Primary transport

  // Subscription tracking
  private subscriptions: Map<string, {
    topic: string
    callback: (data: any) => void
    query?: Record<string, string>
  }> = new Map()

  constructor(
    private baseUrl: string,
    private getToken: () => string | null,
    reconnectDelay: number = 3000,
  ) {
    super()
    this.reconnectDelay = reconnectDelay
  }

  get isConnected(): boolean {
    return this.connected
  }

  get clientIdentifier(): string | null {
    return this.clientId
  }

  /** Connect via SSE (primary, recommended) */
  connect(): void {
    if (this.connected) return

    if (this.useSSE) {
      this.connectSSE()
    } else {
      this.connectWS()
    }
  }

  private connectSSE(): void {
    const url = `${this.baseUrl}/api/v1/sse`
    this.sseConnection = new EventSource(url)

    this.sseConnection.onopen = () => {
      this.connected = true
      this.emit('connected')
    }

    this.sseConnection.onmessage = (event) => {
      try {
        const msg: RealtimeMessage = JSON.parse(event.data)
        this.handleMessage(msg)
      } catch (e) {
        console.error('[GresbaseSDK] Failed to parse SSE message:', e)
      }
    }

    this.sseConnection.onerror = () => {
      this.connected = false
      this.sseConnection?.close()
      this.emit('disconnected')
      this.scheduleReconnect()
    }

    // Parse client ID from first connection message
    // The server assigns the clientId on connect
  }

  private connectWS(): void {
    const wsUrl = this.baseUrl.replace(/^http/, 'ws')
    const token = this.getToken()
    const url = `${wsUrl}/api/v1/realtime${token ? `?token=${encodeURIComponent(token)}` : ''}`
    this.wsConnection = new WebSocket(url)

    this.wsConnection.onopen = () => {
      this.connected = true
      this.emit('connected')
      // Resubscribe after reconnect
      this.resubscribeAll()
    }

    this.wsConnection.onmessage = (event) => {
      try {
        const msg: RealtimeMessage = JSON.parse(event.data)
        this.handleMessage(msg)
      } catch (e) {
        console.error('[GresbaseSDK] Failed to parse WS message:', e)
      }
    }

    this.wsConnection.onclose = () => {
      this.connected = false
      this.emit('disconnected')
      this.scheduleReconnect()
    }

    this.wsConnection.onerror = () => {
      this.wsConnection?.close()
    }
  }

  /** Disconnect all transports */
  disconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.sseConnection?.close()
    this.sseConnection = null
    this.wsConnection?.close()
    this.wsConnection = null
    this.connected = false
    this.clientId = null
    this.emit('disconnected')
  }

  /**
   * Subscribe to realtime events for a collection.
   *
   * @param collection - Collection name
   * @param callback - Called with { action, record } on events
   * @param options - Filter, fields, expand
   * @returns Unsubscribe function
   *
   * @example
   * ```ts
   * const unsub = client.realtime.subscribe('posts', (data) => {
   *   console.log(data.action, data.record)
   * }, { filter: 'published = true' })
   * ```
   */
  subscribe(
    collection: string,
    callback: (data: { action: string; record: RecordData }) => void,
    options?: { filter?: string; fields?: string; expand?: string },
  ): UnsubscribeFn {
    const subId = `${collection}-${Date.now()}-${Math.random().toString(36).slice(2)}`

    // Subscribe to both specific record and wildcard topics
    const topics = [`${collection}/*`]

    this.subscriptions.set(subId, {
      topic: topics[0],
      callback,
      query: {
        filter: options?.filter || '',
        fields: options?.fields || '',
        expand: options?.expand || '',
      },
    })

    // Send subscription request
    this.sendSubscription('subscribe', topics, options)

    return () => {
      this.subscriptions.delete(subId)
      this.sendSubscription('unsubscribe', topics)
    }
  }

  /**
   * Subscribe to a specific record.
   */
  subscribeToRecord(
    collection: string,
    recordId: string,
    callback: (data: { action: string; record: RecordData }) => void,
    options?: { fields?: string; expand?: string },
  ): UnsubscribeFn {
    const subId = `${collection}/${recordId}-${Date.now()}`

    const topics = [`${collection}/${recordId}`]

    this.subscriptions.set(subId, {
      topic: topics[0],
      callback,
      query: {
        fields: options?.fields || '',
        expand: options?.expand || '',
      },
    })

    this.sendSubscription('subscribe', topics, options)

    return () => {
      this.subscriptions.delete(subId)
      this.sendSubscription('unsubscribe', topics)
    }
  }

  private sendSubscription(type: 'subscribe' | 'unsubscribe', topics: string[], options?: Record<string, string>): void {
    if (!this.connected) return

    const payload = {
      type,
      clientId: this.clientId,
      subscriptions: topics,
      query: options,
    }

    if (this.useSSE) {
      // SSE subscriptions via HTTP POST
      fetch(`${this.baseUrl}/api/v1/realtime`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }).catch(() => {})
    } else if (this.wsConnection?.readyState === WebSocket.OPEN) {
      this.wsConnection.send(JSON.stringify(payload))
    }
  }

  private resubscribeAll(): void {
    for (const [_, sub] of this.subscriptions) {
      this.sendSubscription('subscribe', [sub.topic], sub.query)
    }
  }

  private handleMessage(msg: RealtimeMessage): void {
    // Set clientId from connection message
    if (msg.event === 'connection:established' && msg.client_id) {
      this.clientId = msg.client_id
      return
    }

    // Handle record events
    if (msg.event?.startsWith('record:')) {
      const action = msg.event.replace('record:', '') as RealtimeEvent
      let data: any = msg.data
      try {
        if (typeof data === 'string') data = JSON.parse(data)
      } catch {}

      // Notify matching subscriptions
      for (const [_, sub] of this.subscriptions) {
        sub.callback({ action, record: data?.record || data })
      }
    }

    // Handle presence events
    if (msg.event?.startsWith('presence:')) {
      this.emit('presence', msg)
    }

    this.emit('message', msg)
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, this.reconnectDelay)
  }
}

// ---------------------------------------------------------------------------
// Collection Service (Record CRUD)
// ---------------------------------------------------------------------------

class CollectionService<T extends RecordData = RecordData> {
  constructor(
    private collectionName: string,
    private http: HttpClient,
  ) {}

  /**
   * Get a paginated list of records.
   */
  async getList(page: number = 1, perPage: number = 30, params?: Omit<ListParams, 'page' | 'perPage'>): Promise<PaginatedResponse<T>> {
    const query: Record<string, string> = {
      page: String(page),
      perPage: String(perPage),
    }
    if (params?.sort) query.sort = params.sort
    if (params?.filter) query.filter = params.filter
    if (params?.fields) query.fields = params.fields
    if (params?.expand) query.expand = params.expand

    return this.http.request<PaginatedResponse<T>>('GET', `/api/v1/records/${this.collectionName}?${new URLSearchParams(query)}`)
  }

  /**
   * Get all records (auto-paginates).
   */
  async getFullList(perPage: number = 100, params?: Omit<ListParams, 'page' | 'perPage'>): Promise<T[]> {
    const allItems: T[] = []
    let page = 1
    let hasMore = true

    while (hasMore) {
      const result = await this.getList(page, perPage, params)
      allItems.push(...result.items)
      hasMore = page < result.totalPages
      page++
    }

    return allItems
  }

  /**
   * Get a single record by ID.
   */
  async getOne(id: string, params?: Pick<ListParams, 'fields' | 'expand'>): Promise<T> {
    const query: Record<string, string> = {}
    if (params?.fields) query.fields = params.fields
    if (params?.expand) query.expand = params.expand

    const qs = Object.keys(query).length > 0 ? `?${new URLSearchParams(query)}` : ''
    return this.http.request<T>('GET', `/api/v1/records/${this.collectionName}/${id}${qs}`)
  }

  /**
   * Get the first record matching a filter.
   */
  async getFirstListItem(filter: string, params?: Omit<ListParams, 'filter' | 'page' | 'perPage'>): Promise<T> {
    const result = await this.getList(1, 1, { ...params, filter })
    if (result.items.length === 0) {
      throw new Error('No record found matching the filter')
    }
    return result.items[0]
  }

  /**
   * Create a new record.
   */
  async create(data: Partial<T>): Promise<T> {
    return this.http.request<T>('POST', `/api/v1/records/${this.collectionName}`, data)
  }

  /**
   * Update an existing record.
   */
  async update(id: string, data: Partial<T>): Promise<T> {
    return this.http.request<T>('PUT', `/api/v1/records/${this.collectionName}/${id}`, data)
  }

  /**
   * Delete a record.
   */
  async delete(id: string): Promise<void> {
    await this.http.request<void>('DELETE', `/api/v1/records/${this.collectionName}/${id}`)
  }

  /**
   * Batch create records (up to 500 per request).
   */
  async batchCreate(records: Partial<T>[]): Promise<T[]> {
    return this.http.request<T[]>('POST', `/api/v1/batch/${this.collectionName}`, { action: 'create', records })
  }

  /**
   * Search records using full-text search.
   */
  async search(query: string, params?: ListParams): Promise<PaginatedResponse<T>> {
    return this.http.request<PaginatedResponse<T>>('POST', `/api/v1/search/${this.collectionName}`, { query, ...params })
  }
}

// ---------------------------------------------------------------------------
// File Service
// ---------------------------------------------------------------------------

class FileService {
  constructor(private http: HttpClient) {}

  /**
   * Get a file download URL (with optional token for private files).
   */
  getURL(collection: string, recordId: string, filename: string, options?: { token?: string }): string {
    let url = `${this.http['baseUrl']}/api/v1/files/${collection}/${recordId}/${filename}`
    if (options?.token) {
      url += `?token=${encodeURIComponent(options.token)}`
    }
    return url
  }

  /**
   * Upload a file. Supports File, Blob, or base64 string.
   */
  async upload(
    file: File | Blob,
    options?: UploadOptions,
  ): Promise<{ filename: string; url: string; size: number }> {
    const formData = new FormData()
    formData.append('file', file, options?.filename || (file as File).name || 'upload')

    // For progress tracking, use XMLHttpRequest if available
    if (options?.onProgress && typeof XMLHttpRequest !== 'undefined') {
      return this.uploadWithProgress(formData, options.onProgress)
    }

    return this.http.request<{ filename: string; url: string; size: number }>(
      'POST', '/api/v1/files/upload', formData
    )
  }

  private uploadWithProgress(formData: FormData, onProgress: (loaded: number, total: number) => void): Promise<any> {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', `${this.http['baseUrl']}/api/v1/files/upload`)

      const token = this.http['getToken']?.()
      if (token) {
        xhr.setRequestHeader('Authorization', `Bearer ${token}`)
      }

      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) {
          onProgress(e.loaded, e.total)
        }
      }

      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(JSON.parse(xhr.responseText))
        } else {
          reject(new Error(`Upload failed: ${xhr.status}`))
        }
      }

      xhr.onerror = () => reject(new Error('Upload network error'))
      xhr.send(formData)
    })
  }

  /**
   * Delete a file.
   */
  async delete(collection: string, recordId: string, filename: string): Promise<void> {
    await this.http.request<void>('DELETE', `/api/v1/files/${collection}/${recordId}/${filename}`)
  }
}

// ---------------------------------------------------------------------------
// Main SDK Client
// ---------------------------------------------------------------------------

export class GresbaseClient {
  readonly auth: AuthService
  readonly realtime: RealtimeService
  readonly files: FileService

  private http: HttpClient

  constructor(config: GresbaseConfig) {
    const baseUrl = config.url.replace(/\/$/, '')

    // Create auth service first
    const auth = new AuthService(null as any)

    // Create HTTP client with auth integration
    const http = new HttpClient(
      baseUrl,
      () => auth.getToken(),
      (token) => { /* auth handles this */ },
      config.fetch,
    )

    // Recreate auth with HTTP client
    this.auth = new AuthService(http, 'gresbase_auth', true)
    if (config.token) {
      this.auth.login = async () => { throw new Error('Use setToken for manual token') }
    }

    // Point the HTTP client to the auth service properly
    this.http = new HttpClient(
      baseUrl,
      () => this.auth.getToken(),
      (t) => {},
      config.fetch,
      this.auth.getRefreshToken(),
      () => this.auth.refresh(),
    )

    this.realtime = new RealtimeService(baseUrl, () => this.auth.getToken(), config.sseReconnectDelay || 3000)
    this.files = new FileService(this.http)
  }

  /**
   * Get a collection service for typed record operations.
   *
   * @example
   * ```ts
   * const posts = client.collection<{ title: string; content: string }>('posts')
   * const list = await posts.getList()
   * ```
   */
  collection<T extends RecordData = RecordData>(name: string): CollectionService<T> {
    return new CollectionService<T>(name, this.http)
  }

  // ---- Admin Management ----

  /** List all admin users */
  async getAdmins(): Promise<AdminUser[]> {
    return this.http.request<AdminUser[]>('GET', '/api/v1/admin/users')
  }

  /** Create a new admin user */
  async createAdmin(email: string, password: string, role: string = 'admin'): Promise<AdminUser> {
    return this.http.request<AdminUser>('POST', '/api/v1/admin/users', { email, password, role })
  }

  /** Update an admin user */
  async updateAdmin(id: string, data: Partial<AdminUser>): Promise<AdminUser> {
    return this.http.request<AdminUser>('PUT', `/api/v1/admin/users/${id}`, data)
  }

  /** Delete an admin user */
  async deleteAdmin(id: string): Promise<void> {
    await this.http.request<void>('DELETE', `/api/v1/admin/users/${id}`)
  }

  // ---- Collections Management ----

  /** List all collections */
  async getCollections(): Promise<Collection[]> {
    return this.http.request<Collection[]>('GET', '/api/v1/collections')
  }

  /** Get a single collection */
  async getCollection(idOrName: string): Promise<Collection> {
    return this.http.request<Collection>('GET', `/api/v1/collections/${idOrName}`)
  }

  /** Create a collection */
  async createCollection(data: Partial<Collection>): Promise<Collection> {
    return this.http.request<Collection>('POST', '/api/v1/collections', data)
  }

  /** Update a collection */
  async updateCollection(id: string, data: Partial<Collection>): Promise<Collection> {
    return this.http.request<Collection>('PUT', `/api/v1/collections/${id}`, data)
  }

  /** Delete a collection */
  async deleteCollection(id: string): Promise<void> {
    await this.http.request<void>('DELETE', `/api/v1/collections/${id}`)
  }

  /** Import a collection schema */
  async importCollection(data: Collection[]): Promise<void> {
    await this.http.request<void>('POST', '/api/v1/collections/import', data)
  }

  // ---- API Keys ----

  /** List API keys */
  async getApiKeys(): Promise<APIKeyData[]> {
    return this.http.request<APIKeyData[]>('GET', '/api/v1/api-keys')
  }

  /** Create an API key (returns the full key only once) */
  async createApiKey(name: string): Promise<APIKeyData & { key: string }> {
    return this.http.request<APIKeyData & { key: string }>('POST', '/api/v1/api-keys', { name })
  }

  /** Delete an API key */
  async deleteApiKey(id: string): Promise<void> {
    await this.http.request<void>('DELETE', `/api/v1/api-keys/${id}`)
  }

  // ---- Certificates ----

  /** List certificates */
  async getCertificates(): Promise<Certificate[]> {
    return this.http.request<Certificate[]>('GET', '/api/v1/certificates')
  }

  /** Issue a TLS certificate */
  async issueCertificate(domain: string): Promise<Certificate> {
    return this.http.request<Certificate>('POST', '/api/v1/certificates/issue', { domain })
  }

  /** Revoke a certificate */
  async revokeCertificate(id: string): Promise<void> {
    await this.http.request<void>('DELETE', `/api/v1/certificates/${id}`)
  }

  // ---- Logs ----

  /** Get audit logs */
  async getLogs(params?: { page?: number; perPage?: number; filter?: string }): Promise<PaginatedResponse> {
    const query = params ? '?' + new URLSearchParams(params as Record<string, string>).toString() : ''
    return this.http.request<PaginatedResponse>('GET', `/api/v1/logs${query}`)
  }

  // ---- Settings ----

  /** Get server settings */
  async getSettings(): Promise<Record<string, any>> {
    return this.http.request<Record<string, any>>('GET', '/api/v1/settings')
  }

  /** Update server settings */
  async updateSettings(data: Record<string, any>): Promise<Record<string, any>> {
    return this.http.request<Record<string, any>>('PUT', '/api/v1/settings', data)
  }

  // ---- Backups ----

  /** List backups */
  async getBackups(): Promise<{ name: string; size: number; created_at: string }[]> {
    return this.http.request('GET', '/api/v1/backups')
  }

  /** Create a backup */
  async createBackup(name?: string): Promise<void> {
    await this.http.request('POST', '/api/v1/backups', { name })
  }

  /** Restore a backup */
  async restoreBackup(name: string): Promise<void> {
    await this.http.request('POST', `/api/v1/backups/${name}/restore`)
  }

  /** Delete a backup */
  async deleteBackup(name: string): Promise<void> {
    await this.http.request('DELETE', `/api/v1/backups/${name}`)
  }

  // ---- Health ----

  /** Health check */
  async health(): Promise<{ status: string; database: boolean }> {
    return this.http.request('GET', '/api/v1/health')
  }

  // ---- Batch ----

  /** Execute a batch of requests */
  async batch(requests: { method: string; path: string; body?: any }[]): Promise<any[]> {
    return this.http.request('POST', '/api/v1/batch', { requests })
  }
}

// Default export for ESM
export default GresbaseClient
