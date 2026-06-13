/**
 * Gresbase TypeScript SDK
 *
 * Production-grade client SDK for the Gresbase backend platform.
 * Capabilities:
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
  /** Access rules: null = locked (superusers only), '' = public, expression = filtered. */
  list_rule?: string | null
  view_rule?: string | null
  create_rule?: string | null
  update_rule?: string | null
  delete_rule?: string | null
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

export interface RecordAuthResponse<T = RecordData> {
  token: string
  refreshToken: string
  record: T
}

export interface AggregateOptions {
  /** Comma-separated aggregations: `count`, `sum:field`, `avg:field`, `min:field`, `max:field` */
  aggregate: string
  /** Comma-separated field names to group by */
  groupBy?: string
  /** Filter expression (same syntax as getList filter) */
  filter?: string
  /** Sort field(s) with optional '-' prefix for descending */
  sort?: string
  /** Max result rows (default 100, max 1000) */
  limit?: number
}

export interface AggregateResponse {
  /** Each item contains the groupBy fields plus `count`, `sum_<field>`, `avg_<field>`, `min_<field>`, `max_<field>` keys. */
  items: Record<string, any>[]
}

export interface VectorSearchOptions {
  /** Name of the `vector` field to search against. */
  field: string
  /** The query embedding. Must match the field's configured dimensions. */
  vector: number[]
  /** Max results to return (default 20, max 200). */
  limit?: number
  /**
   * Distance metric override. Defaults to the field's configured metric.
   * `cosine` (angle), `l2` (Euclidean), or `inner` (negative inner product).
   */
  distance?: 'cosine' | 'l2' | 'inner'
}

export interface VectorSearchResponse<T = RecordData> {
  /** Matching records, nearest first. Each carries a numeric `_distance`. */
  items: (T & { _distance: number })[]
  totalItems: number
}

export interface ChannelMessage {
  event: string
  data: any
  client_id?: string
}

export interface PresenceMember {
  client_id: string
  state: any
}

export interface PresenceInfo {
  clients: number
  members: PresenceMember[]
}

export interface FileURLOptions {
  /** Access token for protected files */
  token?: string
  /** Thumbnail size (e.g. '100x100') */
  thumb?: string
  /** Convert the image to this format */
  format?: 'jpeg' | 'png'
  /** Quality from 1 to 100 (lossy formats) */
  quality?: number
}

export interface PasskeyDescriptor {
  id: string
  name: string
  created: string
  lastUsedAt?: string | null
}

export interface APIKeyData {
  id: string
  name: string
  prefix: string
  permissions?: string[]
  last_used_at?: string
  expires_at?: string
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
// WebAuthn (passkey) helpers
// ---------------------------------------------------------------------------

/**
 * Encode an ArrayBuffer/Uint8Array as base64url (no padding) — the encoding
 * WebAuthn servers exchange binary credential fields in.
 */
export function bufferToBase64Url(buffer: ArrayBuffer | Uint8Array): string {
  const bytes = buffer instanceof Uint8Array ? buffer : new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

/** Decode a base64url (padded or unpadded) string into an ArrayBuffer. */
export function base64UrlToBuffer(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (value.length % 4)) % 4)
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes.buffer
}

/**
 * Convert the base64url-encoded binary fields of server-provided
 * PublicKeyCredential{Creation,Request}Options into ArrayBuffers, as required
 * by navigator.credentials.create/get.
 */
function decodePublicKeyOptions(publicKey: any): any {
  const out = { ...publicKey }
  if (typeof out.challenge === 'string') {
    out.challenge = base64UrlToBuffer(out.challenge)
  }
  if (out.user && typeof out.user.id === 'string') {
    out.user = { ...out.user, id: base64UrlToBuffer(out.user.id) }
  }
  for (const key of ['excludeCredentials', 'allowCredentials'] as const) {
    if (Array.isArray(out[key])) {
      out[key] = out[key].map((cred: any) =>
        typeof cred?.id === 'string' ? { ...cred, id: base64UrlToBuffer(cred.id) } : cred
      )
    }
  }
  return out
}

/** Throws a clear error outside browser/secure contexts (e.g. Node.js, SSR). */
function requireWebAuthn(): { create: Function; get: Function } {
  const nav: any = typeof navigator !== 'undefined' ? navigator : undefined
  if (!nav || !nav.credentials || typeof nav.credentials.create !== 'function' || typeof nav.credentials.get !== 'function') {
    throw new Error(
      '[GresbaseSDK] Passkeys require a browser with WebAuthn support (navigator.credentials is unavailable in this environment)'
    )
  }
  return nav.credentials
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
    private customFetch?: typeof fetch,
    private getRefreshToken?: () => string | null,
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
    if (response.status === 401 && this.getRefreshToken?.() && this.onRefresh) {
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
  private record: RecordData | null = null
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
        this.record = data.record || null
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
        record: this.record,
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

  /** The authenticated collection record (set by record auth, e.g. collection('users').authWithPassword). */
  getRecord(): RecordData | null {
    return this.record
  }

  get isAuthenticated(): boolean {
    return !!this.token
  }

  /** Manually set the auth tokens (e.g. from a server-issued token). */
  setToken(token: string | null, refreshToken?: string | null): void {
    this.token = token
    if (refreshToken !== undefined) {
      this.refreshTokenVal = refreshToken
    }
    this.saveToStorage()
  }

  /**
   * Store a record-auth result (token + refresh token + auth record) in the
   * shared token store. Used by CollectionService record auth methods so that
   * record auth and admin auth share the exact same plumbing.
   * Fires the same 'login' event admin login does.
   */
  setRecordAuth<T extends RecordData>(result: RecordAuthResponse<T>): void {
    this.token = result.token
    this.refreshTokenVal = result.refreshToken
    this.record = result.record
    this.saveToStorage()
    this.emit('login', result)
  }

  /** Login with email and password */
  async login(email: string, password: string): Promise<AuthResponse> {
    const result = await this.http.request<AuthResponse>('POST', '/api/v1/auth/login', { email, password })
    this.token = result.token
    this.refreshTokenVal = result.refreshToken
    this.admin = result.admin
    this.record = null
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
    this.record = null
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
    this.record = null
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
    this.record = null
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
    this.record = null
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
    options?: { presence?: Record<string, any> }
    /** Channel subscriptions receive raw {event, data, client_id} messages */
    isChannel?: boolean
  }> = new Map()

  constructor(
    private baseUrl: string,
    private getToken: () => string | null,
    private http: HttpClient,
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

  /**
   * Subscribe to an arbitrary realtime channel (broadcast/presence messaging).
   *
   * The callback receives every message published on the channel, including
   * `presence:join` / `presence:leave` events (data: `{client_id, state}`).
   * Pass `options.presence` to announce your own presence state on join.
   *
   * @example
   * ```ts
   * const unsub = client.realtime.subscribeToChannel('room:1', (msg) => {
   *   console.log(msg.event, msg.data)
   * }, { presence: { name: 'Ada' } })
   * ```
   */
  subscribeToChannel(
    channel: string,
    callback: (msg: ChannelMessage) => void,
    options?: { presence?: Record<string, any> },
  ): UnsubscribeFn {
    const subId = `channel:${channel}-${Date.now()}-${Math.random().toString(36).slice(2)}`

    this.subscriptions.set(subId, {
      topic: channel,
      callback,
      options,
      isChannel: true,
    })

    this.sendSubscription('subscribe', [channel], undefined, options)

    return () => {
      this.subscriptions.delete(subId)
      this.sendSubscription('unsubscribe', [channel])
    }
  }

  /**
   * Publish a message to a channel via the REST broadcast endpoint.
   * Requires an auth token.
   */
  async broadcast(channel: string, event: string, data?: any): Promise<void> {
    await this.http.request<void>('POST', '/api/v1/realtime/broadcast', { channel, event, data })
  }

  /**
   * Fetch a presence snapshot for a channel: connected client count and the
   * presence state of each member.
   */
  async presence(channel: string): Promise<PresenceInfo> {
    const payload: Record<string, any> = { type: 'presence', channel }
    if (this.clientId) payload.clientId = this.clientId

    const res = await this.http.request<any>('POST', '/api/v1/realtime', payload)

    // The response may be the presence object itself, or a realtime message
    // envelope with the presence object in `data`.
    let info: any = res
    if (info && typeof info === 'object' && info.data !== undefined && info.clients === undefined) {
      info = typeof info.data === 'string' ? JSON.parse(info.data) : info.data
    }

    return {
      clients: info?.clients ?? 0,
      members: info?.members ?? [],
    }
  }

  private sendSubscription(
    type: 'subscribe' | 'unsubscribe',
    topics: string[],
    query?: Record<string, string>,
    options?: { presence?: Record<string, any> },
  ): void {
    if (!this.connected) return

    const payload: Record<string, any> = {
      type,
      clientId: this.clientId,
      subscriptions: topics,
      query,
    }
    if (options) {
      payload.options = options
    }

    if (this.useSSE) {
      // SSE subscriptions via HTTP POST (authenticated, fire-and-forget)
      this.http.request('POST', '/api/v1/realtime', payload).catch(() => {})
    } else if (this.wsConnection?.readyState === WebSocket.OPEN) {
      this.wsConnection.send(JSON.stringify(payload))
    }
  }

  private resubscribeAll(): void {
    for (const [_, sub] of this.subscriptions) {
      this.sendSubscription('subscribe', [sub.topic], sub.query, sub.options)
    }
  }

  private handleMessage(msg: RealtimeMessage): void {
    // Set clientId from connection message
    if (msg.event === 'connection:established' && msg.client_id) {
      this.clientId = msg.client_id
      // Flush any subscriptions registered before the server assigned an ID
      this.resubscribeAll()
      return
    }

    let data: any = msg.data
    try {
      if (typeof data === 'string') data = JSON.parse(data)
    } catch {}

    // Handle record events
    if (msg.event?.startsWith('record:')) {
      const action = msg.event.replace('record:', '') as RealtimeEvent

      // Notify matching subscriptions
      for (const [_, sub] of this.subscriptions) {
        if (sub.isChannel) continue
        sub.callback({ action, record: data?.record || data })
      }
    } else {
      // Route channel messages (broadcasts, presence:join/leave, custom events)
      const target = msg.channel || msg.topic
      if (target) {
        for (const [_, sub] of this.subscriptions) {
          if (!sub.isChannel || target !== sub.topic) continue
          sub.callback({ event: msg.event, data, client_id: msg.client_id })
        }
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
    private auth: AuthService,
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
   * Batch create records in one transaction (up to 500 per request).
   */
  async batchCreate(records: Partial<T>[]): Promise<T[]> {
    const result = await this.http.request<{ created?: T[] }>(
      'POST',
      `/api/v1/batch/${this.collectionName}`,
      { creates: records }
    )
    return result.created ?? []
  }

  /**
   * Batch update records in one transaction. Keys are record ids, values are patches.
   */
  async batchUpdate(updates: Record<string, Partial<T>>): Promise<number> {
    const result = await this.http.request<{ updated?: number }>(
      'POST',
      `/api/v1/batch/${this.collectionName}`,
      { updates }
    )
    return result.updated ?? 0
  }

  /**
   * Batch delete records in one transaction.
   */
  async batchDelete(ids: string[]): Promise<number> {
    const result = await this.http.request<{ deleted?: number }>(
      'POST',
      `/api/v1/batch/${this.collectionName}`,
      { deletes: ids }
    )
    return result.deleted ?? 0
  }

  /**
   * Search records using full-text search.
   */
  async search(query: string, params?: ListParams): Promise<PaginatedResponse<T>> {
    return this.http.request<PaginatedResponse<T>>('POST', `/api/v1/search/${this.collectionName}`, { query, ...params })
  }

  /**
   * Run aggregations over the collection.
   *
   * @example
   * ```ts
   * const { items } = await client.collection('orders').aggregate({
   *   aggregate: 'count,sum:total,avg:total',
   *   groupBy: 'status',
   *   filter: 'created >= "2026-01-01"',
   * })
   * // items: [{ status: 'paid', count: 12, sum_total: 423.5, avg_total: 35.29 }, ...]
   * ```
   */
  async aggregate(options: AggregateOptions): Promise<AggregateResponse> {
    const query: Record<string, string> = {
      aggregate: options.aggregate,
    }
    if (options.groupBy) query.groupBy = options.groupBy
    if (options.filter) query.filter = options.filter
    if (options.sort) query.sort = options.sort
    if (options.limit !== undefined) query.limit = String(options.limit)

    return this.http.request<AggregateResponse>('GET', `/api/v1/records/${this.collectionName}/aggregate?${new URLSearchParams(query)}`)
  }

  /**
   * Similarity search over a `vector` field (pgvector). Returns the records
   * nearest to the query embedding, each annotated with a numeric `_distance`
   * (smaller = closer). The collection list rule is enforced, so results never
   * include rows the caller may not read.
   *
   * @example
   * const { items } = await pb.collection('docs').searchVector({
   *   field: 'embedding',
   *   vector: await embed('how do I reset my password?'),
   *   limit: 5,
   * })
   */
  async searchVector(options: VectorSearchOptions): Promise<VectorSearchResponse<T>> {
    const body: Record<string, unknown> = {
      field: options.field,
      vector: options.vector,
    }
    if (options.limit !== undefined) body.limit = options.limit
    if (options.distance) body.distance = options.distance

    return this.http.request<VectorSearchResponse<T>>('POST', `/api/v1/records/${this.collectionName}/search-vector`, body)
  }

  // ---- Record Auth (for auth collections) ----

  private get authBasePath(): string {
    return `/api/v1/collections/${this.collectionName}/auth`
  }

  /**
   * Authenticate a collection record with identity (email/username) and password.
   * On success the token, refresh token and record are stored on the client,
   * so subsequent requests and realtime connections are authenticated.
   */
  async authWithPassword(identity: string, password: string): Promise<RecordAuthResponse<T>> {
    const result = await this.http.request<RecordAuthResponse<T>>(
      'POST', `${this.authBasePath}/auth-with-password`, { identity, password }
    )
    this.auth.setRecordAuth(result)
    return result
  }

  /**
   * Authenticate anonymously (creates an anonymous user record).
   * Only works when the collection allows anonymous auth.
   */
  async authWithAnonymous(): Promise<RecordAuthResponse<T>> {
    const result = await this.http.request<RecordAuthResponse<T>>(
      'POST', `${this.authBasePath}/auth-with-anonymous`
    )
    this.auth.setRecordAuth(result)
    return result
  }

  /**
   * Refresh the record auth session using the stored refresh token.
   */
  async authRefresh(): Promise<RecordAuthResponse<T>> {
    const refreshToken = this.auth.getRefreshToken()
    if (!refreshToken) {
      throw new Error('No refresh token available — authenticate first')
    }
    const result = await this.http.request<RecordAuthResponse<T>>(
      'POST', `${this.authBasePath}/auth-refresh`, { refreshToken }
    )
    this.auth.setRecordAuth(result)
    return result
  }

  /** Request a password reset email for a record. */
  async requestPasswordReset(email: string): Promise<void> {
    await this.http.request<void>('POST', `${this.authBasePath}/request-password-reset`, { email })
  }

  /** Confirm a password reset with the emailed token. */
  async confirmPasswordReset(token: string, newPassword: string): Promise<void> {
    await this.http.request<void>('POST', `${this.authBasePath}/confirm-password-reset`, { token, password: newPassword })
  }

  /** Request an email verification message for a record. */
  async requestVerification(email: string): Promise<void> {
    await this.http.request<void>('POST', `${this.authBasePath}/request-verification`, { email })
  }

  /** Confirm email verification with the emailed token. */
  async confirmVerification(token: string): Promise<void> {
    await this.http.request<void>('POST', `${this.authBasePath}/confirm-verification`, { token })
  }

  // ---- Passkeys (WebAuthn) ----
  // The collection must opt in with the `allowPasskeys` option. Browser-only:
  // these methods wrap navigator.credentials.create/get.

  /**
   * Register a passkey for the currently authenticated record (a record auth
   * token for this collection must be stored on the client). Resolves with
   * the stored passkey descriptor.
   *
   * @param name - Optional user-facing label (defaults server-side).
   */
  async registerPasskey(name?: string): Promise<PasskeyDescriptor> {
    const credentials = requireWebAuthn()

    const begin = await this.http.request<{ sessionId: string; options: { publicKey: any } }>(
      'POST', `${this.authBasePath}/passkey/register-begin`, {}
    )

    const credential: any = await credentials.create({
      publicKey: decodePublicKeyOptions(begin.options.publicKey),
    })
    if (!credential) {
      throw new Error('[GresbaseSDK] Passkey registration was cancelled')
    }

    const response = credential.response
    const payload: Record<string, any> = {
      sessionId: begin.sessionId,
      credential: {
        id: credential.id,
        rawId: bufferToBase64Url(credential.rawId),
        type: credential.type,
        response: {
          attestationObject: bufferToBase64Url(response.attestationObject),
          clientDataJSON: bufferToBase64Url(response.clientDataJSON),
          transports: typeof response.getTransports === 'function' ? response.getTransports() : undefined,
        },
        clientExtensionResults: typeof credential.getClientExtensionResults === 'function'
          ? credential.getClientExtensionResults()
          : {},
      },
    }
    if (name) payload.name = name

    return this.http.request<PasskeyDescriptor>('POST', `${this.authBasePath}/passkey/register-finish`, payload)
  }

  /**
   * Authenticate with a passkey. Without an email this performs a
   * discoverable (usernameless) login; with an email the server scopes
   * allowCredentials to that account's passkeys. On success the token,
   * refresh token and record are stored on the client, exactly like
   * authWithPassword.
   */
  async authWithPasskey(email?: string): Promise<RecordAuthResponse<T>> {
    const credentials = requireWebAuthn()

    const begin = await this.http.request<{ sessionId: string; options: { publicKey: any } }>(
      'POST', `${this.authBasePath}/passkey/login-begin`, email ? { email } : {}
    )

    const assertion: any = await credentials.get({
      publicKey: decodePublicKeyOptions(begin.options.publicKey),
    })
    if (!assertion) {
      throw new Error('[GresbaseSDK] Passkey login was cancelled')
    }

    const response = assertion.response
    const result = await this.http.request<RecordAuthResponse<T>>(
      'POST', `${this.authBasePath}/passkey/login-finish`, {
        sessionId: begin.sessionId,
        credential: {
          id: assertion.id,
          rawId: bufferToBase64Url(assertion.rawId),
          type: assertion.type,
          response: {
            authenticatorData: bufferToBase64Url(response.authenticatorData),
            clientDataJSON: bufferToBase64Url(response.clientDataJSON),
            signature: bufferToBase64Url(response.signature),
            userHandle: response.userHandle ? bufferToBase64Url(response.userHandle) : undefined,
          },
          clientExtensionResults: typeof assertion.getClientExtensionResults === 'function'
            ? assertion.getClientExtensionResults()
            : {},
        },
      }
    )
    this.auth.setRecordAuth(result)
    return result
  }

  /** List the authenticated record's own passkeys (descriptors only). */
  async listPasskeys(): Promise<PasskeyDescriptor[]> {
    const result = await this.http.request<{ items: PasskeyDescriptor[] }>(
      'GET', `${this.authBasePath}/passkeys`
    )
    return result.items ?? []
  }

  /** Delete one of the authenticated record's own passkeys. */
  async deletePasskey(id: string): Promise<void> {
    await this.http.request<void>('DELETE', `${this.authBasePath}/passkeys/${id}`)
  }
}

// ---------------------------------------------------------------------------
// File Service
// ---------------------------------------------------------------------------

class FileService {
  constructor(private http: HttpClient) {}

  /**
   * Get a file download URL.
   *
   * Options:
   * - `token` — access token for protected files
   * - `thumb` — thumbnail size (e.g. '100x100')
   * - `format` — convert the image ('jpeg' | 'png')
   * - `quality` — quality 1-100 for lossy formats
   *
   * @example
   * ```ts
   * client.files.getURL('posts', recordId, 'cover.png', { thumb: '300x200', format: 'jpeg', quality: 80 })
   * ```
   */
  getURL(collection: string, recordId: string, filename: string, options?: FileURLOptions): string {
    let url = `${this.http['baseUrl']}/api/v1/files/${collection}/${recordId}/${filename}`
    const query = new URLSearchParams()
    if (options?.token) query.set('token', options.token)
    if (options?.thumb) query.set('thumb', options.thumb)
    if (options?.format) query.set('format', options.format)
    if (options?.quality !== undefined) query.set('quality', String(options.quality))
    const qs = query.toString()
    if (qs) {
      url += `?${qs}`
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

    // HTTP client resolves auth lazily so it can be created first
    this.http = new HttpClient(
      baseUrl,
      () => this.auth.getToken(),
      config.fetch,
      () => this.auth.getRefreshToken(),
      () => this.auth.refresh(),
    )

    this.auth = new AuthService(this.http, 'gresbase_auth', true)
    if (config.token) {
      this.auth.setToken(config.token)
    }

    this.realtime = new RealtimeService(baseUrl, () => this.auth.getToken(), this.http, config.sseReconnectDelay || 3000)
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
    return new CollectionService<T>(name, this.http, this.auth)
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
  async createApiKey(name: string, permissions: string[] = []): Promise<APIKeyData & { key: string }> {
    return this.http.request<APIKeyData & { key: string }>('POST', '/api/v1/api-keys', { name, permissions })
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

  /** Execute a batch of requests sequentially in one transaction. */
  async batch(requests: { method: string; path: string; body?: any }[]): Promise<any[]> {
    // The server expects `url` per request; `path` is kept as the friendlier
    // public parameter name.
    return this.http.request('POST', '/api/v1/batch', {
      requests: requests.map(({ method, path, body }) => ({ method, url: path, body })),
    })
  }
}

// Default export for ESM
export default GresbaseClient
