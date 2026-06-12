import { describe, it, expect, vi, afterEach } from 'vitest'
import { GresbaseClient, bufferToBase64Url, base64UrlToBuffer } from '../src/index'

/** Creates a vi.fn fetch mock that resolves with the given JSON body/status. */
function createFetchMock(body?: any, status: number = 200) {
  return vi.fn(async (_url: any, _init?: any) => ({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  })) as unknown as typeof fetch & ReturnType<typeof vi.fn>
}

describe('GresbaseClient', () => {
  it('should construct with config object', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    // Client should have auth, realtime, files services
    expect(pb.auth).toBeDefined()
    expect(pb.realtime).toBeDefined()
    expect(pb.files).toBeDefined()
  })

  it('should construct with custom base URL', () => {
    const pb = new GresbaseClient({ url: 'https://api.example.com' })
    expect(pb.auth).toBeDefined()
  })

  it('should not be authenticated initially', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(pb.auth.token).toBeNull()
  })

  it('should create collection service with collection()', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const posts = pb.collection('posts')
    expect(posts).toBeDefined()
    expect(typeof posts.getList).toBe('function')
    expect(typeof posts.getOne).toBe('function')
    expect(typeof posts.create).toBe('function')
    expect(typeof posts.update).toBe('function')
    expect(typeof posts.delete).toBe('function')
  })

  it('should have getFullList on collection', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const items = pb.collection('items')
    expect(typeof items.getFullList).toBe('function')
  })

  it('should have health method', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.health).toBe('function')
  })

  it('should have settings methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.getSettings).toBe('function')
    expect(typeof pb.updateSettings).toBe('function')
  })

  it('should have backup methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.getBackups).toBe('function')
    expect(typeof pb.createBackup).toBe('function')
  })

  it('should have batch method', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.batch).toBe('function')
  })

  it('should have auth admin methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.auth.getMe).toBe('function')
    expect(typeof pb.auth.verifyOTP).toBe('function')
    expect(typeof pb.auth.verifyMagicLink).toBe('function')
  })

  it('should have certificate methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.getCertificates).toBe('function')
    expect(typeof pb.issueCertificate).toBe('function')
  })
})

describe('RecordService', () => {
  it('should have filter helpers', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const posts = pb.collection('posts')
    expect(typeof posts.getFirstListItem).toBe('function')
  })

  it('should accept ListParams', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const posts = pb.collection('posts')
    // These are just type checks — they should exist
    expect(typeof posts.getList).toBe('function')
  })
})

describe('AuthService', () => {
  it('should have auth methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.auth.login).toBe('function')
    expect(typeof pb.auth.refresh).toBe('function')
    expect(typeof pb.auth.logout).toBe('function')
    expect(typeof pb.auth.isAuthenticated).toBe('boolean')
    expect(typeof pb.auth.getToken).toBe('function')
  })
})

describe('FileService', () => {
  it('should have file methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    expect(typeof pb.files.getURL).toBe('function')
    expect(typeof pb.files.upload).toBe('function')
  })

  it('should build plain file URLs without a query string', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const url = pb.files.getURL('posts', 'rec1', 'photo.png')
    expect(url).toBe('http://localhost:8080/api/v1/files/posts/rec1/photo.png')
  })

  it('should append thumb, format and quality to file URLs', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const url = pb.files.getURL('posts', 'rec1', 'photo.png', {
      thumb: '300x200',
      format: 'jpeg',
      quality: 80,
    })
    expect(url).toBe('http://localhost:8080/api/v1/files/posts/rec1/photo.png?thumb=300x200&format=jpeg&quality=80')
  })

  it('should combine token with thumb options in file URLs', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const url = pb.files.getURL('posts', 'rec1', 'photo.png', { token: 'abc 123', thumb: '100x100' })
    const parsed = new URL(url)
    expect(parsed.pathname).toBe('/api/v1/files/posts/rec1/photo.png')
    expect(parsed.searchParams.get('token')).toBe('abc 123')
    expect(parsed.searchParams.get('thumb')).toBe('100x100')
  })
})

describe('CollectionService.aggregate', () => {
  it('should build the aggregate URL with all query params', async () => {
    const fetchMock = createFetchMock({ items: [] })
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    await pb.collection('orders').aggregate({
      aggregate: 'count,sum:total,avg:total',
      groupBy: 'status,region',
      filter: 'total > 10',
      sort: '-count',
      limit: 50,
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = (fetchMock as any).mock.calls[0]
    expect(init.method).toBe('GET')
    const parsed = new URL(url)
    expect(parsed.pathname).toBe('/api/v1/records/orders/aggregate')
    expect(parsed.searchParams.get('aggregate')).toBe('count,sum:total,avg:total')
    expect(parsed.searchParams.get('groupBy')).toBe('status,region')
    expect(parsed.searchParams.get('filter')).toBe('total > 10')
    expect(parsed.searchParams.get('sort')).toBe('-count')
    expect(parsed.searchParams.get('limit')).toBe('50')
  })

  it('should omit optional aggregate params when not provided', async () => {
    const fetchMock = createFetchMock({ items: [] })
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    await pb.collection('orders').aggregate({ aggregate: 'count' })

    const [url] = (fetchMock as any).mock.calls[0]
    const parsed = new URL(url)
    expect(parsed.searchParams.get('aggregate')).toBe('count')
    expect(parsed.searchParams.has('groupBy')).toBe(false)
    expect(parsed.searchParams.has('filter')).toBe(false)
    expect(parsed.searchParams.has('sort')).toBe(false)
    expect(parsed.searchParams.has('limit')).toBe(false)
  })

  it('should return the items payload', async () => {
    const items = [{ status: 'paid', count: 3, sum_total: 42 }]
    const fetchMock = createFetchMock({ items })
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    const result = await pb.collection('orders').aggregate({ aggregate: 'count,sum:total', groupBy: 'status' })
    expect(result.items).toEqual(items)
  })
})

describe('CollectionService record auth', () => {
  it('should expose all record auth methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const users = pb.collection('users')
    expect(typeof users.authWithPassword).toBe('function')
    expect(typeof users.authWithAnonymous).toBe('function')
    expect(typeof users.authRefresh).toBe('function')
    expect(typeof users.requestPasswordReset).toBe('function')
    expect(typeof users.confirmPasswordReset).toBe('function')
    expect(typeof users.requestVerification).toBe('function')
    expect(typeof users.confirmVerification).toBe('function')
  })

  it('authWithAnonymous should hit the endpoint and store tokens on the client', async () => {
    const record = { id: 'anon1', anonymous: true }
    const fetchMock = createFetchMock({ token: 'tok-abc', refreshToken: 'ref-xyz', record })
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    const loginEvents: any[] = []
    pb.auth.on('login', (res) => loginEvents.push(res))

    const result = await pb.collection('users').authWithAnonymous()

    const [url, init] = (fetchMock as any).mock.calls[0]
    expect(url).toBe('http://localhost:8080/api/v1/collections/users/auth/auth-with-anonymous')
    expect(init.method).toBe('POST')

    expect(result.token).toBe('tok-abc')
    // Shared token store: subsequent requests are authenticated
    expect(pb.auth.getToken()).toBe('tok-abc')
    expect(pb.auth.getRefreshToken()).toBe('ref-xyz')
    expect(pb.auth.getRecord()).toEqual(record)
    expect(pb.auth.isAuthenticated).toBe(true)
    expect(loginEvents).toHaveLength(1)

    // Next request carries the stored token
    await pb.collection('posts').getList()
    const [, nextInit] = (fetchMock as any).mock.calls[1]
    expect(nextInit.headers['Authorization']).toBe('Bearer tok-abc')
  })

  it('authWithPassword should post identity/password and store tokens', async () => {
    const fetchMock = createFetchMock({ token: 't1', refreshToken: 'r1', record: { id: 'u1' } })
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    await pb.collection('users').authWithPassword('a@b.com', 'secret')

    const [url, init] = (fetchMock as any).mock.calls[0]
    expect(url).toBe('http://localhost:8080/api/v1/collections/users/auth/auth-with-password')
    expect(JSON.parse(init.body)).toEqual({ identity: 'a@b.com', password: 'secret' })
    expect(pb.auth.getToken()).toBe('t1')
    expect(pb.auth.getRecord()).toEqual({ id: 'u1' })
  })

  it('authRefresh should reject without a stored refresh token', async () => {
    const fetchMock = createFetchMock({})
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })
    await expect(pb.collection('users').authRefresh()).rejects.toThrow(/refresh token/i)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('password reset and verification methods should hit the auth routes', async () => {
    const fetchMock = createFetchMock(undefined, 204)
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })
    const users = pb.collection('users')

    await users.requestPasswordReset('a@b.com')
    await users.confirmPasswordReset('reset-tok', 'newpass')
    await users.requestVerification('a@b.com')
    await users.confirmVerification('verify-tok')

    const calls = (fetchMock as any).mock.calls
    expect(calls[0][0]).toBe('http://localhost:8080/api/v1/collections/users/auth/request-password-reset')
    expect(JSON.parse(calls[0][1].body)).toEqual({ email: 'a@b.com' })
    expect(calls[1][0]).toBe('http://localhost:8080/api/v1/collections/users/auth/confirm-password-reset')
    expect(JSON.parse(calls[1][1].body)).toEqual({ token: 'reset-tok', password: 'newpass' })
    expect(calls[2][0]).toBe('http://localhost:8080/api/v1/collections/users/auth/request-verification')
    expect(calls[3][0]).toBe('http://localhost:8080/api/v1/collections/users/auth/confirm-verification')
    expect(JSON.parse(calls[3][1].body)).toEqual({ token: 'verify-tok' })
  })
})

describe('RealtimeService broadcast and presence', () => {
  it('broadcast should POST the channel/event/data payload with auth header', async () => {
    const fetchMock = createFetchMock(undefined, 204)
    const pb = new GresbaseClient({ url: 'http://localhost:8080', token: 'tok-123', fetch: fetchMock })

    await pb.realtime.broadcast('room:1', 'chat', { text: 'hi' })

    const [url, init] = (fetchMock as any).mock.calls[0]
    expect(url).toBe('http://localhost:8080/api/v1/realtime/broadcast')
    expect(init.method).toBe('POST')
    expect(init.headers['Authorization']).toBe('Bearer tok-123')
    expect(JSON.parse(init.body)).toEqual({ channel: 'room:1', event: 'chat', data: { text: 'hi' } })
  })

  it('broadcast should work without data', async () => {
    const fetchMock = createFetchMock(undefined, 204)
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    await pb.realtime.broadcast('room:1', 'ping')

    const [, init] = (fetchMock as any).mock.calls[0]
    expect(JSON.parse(init.body)).toEqual({ channel: 'room:1', event: 'ping' })
  })

  it('presence should return clients and members from the snapshot', async () => {
    const members = [{ client_id: 'c1', state: { name: 'Ada' } }]
    const fetchMock = createFetchMock({ topic: 'room:1', clients: 1, members })
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    const info = await pb.realtime.presence('room:1')

    const [url, init] = (fetchMock as any).mock.calls[0]
    expect(url).toBe('http://localhost:8080/api/v1/realtime')
    expect(JSON.parse(init.body)).toMatchObject({ type: 'presence', channel: 'room:1' })
    expect(info.clients).toBe(1)
    expect(info.members).toEqual(members)
  })

  it('presence should unwrap a realtime message envelope', async () => {
    const fetchMock = createFetchMock({
      event: 'presence',
      topic: 'room:1',
      data: { topic: 'room:1', clients: 2, members: [] },
    })
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })

    const info = await pb.realtime.presence('room:1')
    expect(info.clients).toBe(2)
    expect(info.members).toEqual([])
  })

  it('should expose subscribeToChannel returning an unsubscribe function', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const unsub = pb.realtime.subscribeToChannel('room:1', () => {}, { presence: { name: 'Ada' } })
    expect(typeof unsub).toBe('function')
    unsub()
  })
})

describe('base64url helpers', () => {
  it('encodes bytes as unpadded base64url', () => {
    // 0xfb 0xff 0xfe encodes to '+//+' in plain base64 — must become '-__-'
    expect(bufferToBase64Url(new Uint8Array([0xfb, 0xff, 0xfe]))).toBe('-__-')
  })

  it('accepts both Uint8Array and ArrayBuffer', () => {
    const bytes = new Uint8Array([1, 2, 3, 4])
    expect(bufferToBase64Url(bytes)).toBe(bufferToBase64Url(bytes.buffer))
  })

  it('never emits padding characters', () => {
    for (const len of [1, 2, 3, 4, 5]) {
      const encoded = bufferToBase64Url(new Uint8Array(len).fill(7))
      expect(encoded).not.toContain('=')
      expect(encoded).not.toContain('+')
      expect(encoded).not.toContain('/')
    }
  })

  it('decodes unpadded base64url back to the original bytes', () => {
    expect(Array.from(new Uint8Array(base64UrlToBuffer('-__-')))).toEqual([0xfb, 0xff, 0xfe])
  })

  it('round-trips arbitrary byte lengths', () => {
    for (const len of [0, 1, 2, 3, 31, 32, 33]) {
      const original = new Uint8Array(len)
      for (let i = 0; i < len; i++) original[i] = (i * 37 + 11) % 256
      const decoded = new Uint8Array(base64UrlToBuffer(bufferToBase64Url(original)))
      expect(Array.from(decoded)).toEqual(Array.from(original))
    }
  })

  it('tolerates padded input when decoding', () => {
    expect(Array.from(new Uint8Array(base64UrlToBuffer('AQI=')))).toEqual([1, 2])
  })
})

describe('CollectionService passkeys', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  /** fetch mock that returns a different JSON body per sequential call. */
  function createSequentialFetchMock(bodies: any[]) {
    let call = 0
    return vi.fn(async (_url: any, _init?: any) => {
      const body = bodies[Math.min(call++, bodies.length - 1)]
      return { ok: true, status: 200, json: async () => body }
    }) as unknown as typeof fetch & ReturnType<typeof vi.fn>
  }

  function stubNavigatorCredentials(impl: { create?: any; get?: any }) {
    vi.stubGlobal('navigator', {
      credentials: {
        create: impl.create ?? vi.fn(),
        get: impl.get ?? vi.fn(),
      },
    })
  }

  it('exposes the passkey methods', () => {
    const pb = new GresbaseClient({ url: 'http://localhost:8080' })
    const users = pb.collection('users')
    expect(typeof users.registerPasskey).toBe('function')
    expect(typeof users.authWithPasskey).toBe('function')
    expect(typeof users.listPasskeys).toBe('function')
    expect(typeof users.deletePasskey).toBe('function')
  })

  it('registerPasskey and authWithPasskey fail with a clear error outside the browser', async () => {
    vi.stubGlobal('navigator', undefined)
    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: createSequentialFetchMock([{}]) })
    await expect(pb.collection('users').registerPasskey()).rejects.toThrow(/browser|WebAuthn/i)
    await expect(pb.collection('users').authWithPasskey()).rejects.toThrow(/browser|WebAuthn/i)
  })

  it('registerPasskey munges options, calls navigator.credentials.create and posts the finish payload', async () => {
    const challenge = bufferToBase64Url(new Uint8Array([9, 8, 7]))
    const userId = bufferToBase64Url(new Uint8Array([1, 1, 2, 3]))
    const fetchMock = createSequentialFetchMock([
      {
        sessionId: 'sess-1',
        options: {
          publicKey: {
            challenge,
            rp: { id: 'localhost', name: 'Gresbase' },
            user: { id: userId, name: 'a@b.com', displayName: 'a@b.com' },
            excludeCredentials: [{ type: 'public-key', id: bufferToBase64Url(new Uint8Array([5, 5])) }],
          },
        },
      },
      { id: 'pk-1', name: 'My passkey', created: '2026-06-12T00:00:00Z' },
    ])

    const createMock = vi.fn(async (_options: any) => ({
      id: 'cred-id',
      type: 'public-key',
      rawId: new Uint8Array([0xde, 0xad]).buffer,
      response: {
        attestationObject: new Uint8Array([1, 2, 3]).buffer,
        clientDataJSON: new Uint8Array([4, 5, 6]).buffer,
        getTransports: () => ['internal', 'hybrid'],
      },
      getClientExtensionResults: () => ({}),
    }))
    stubNavigatorCredentials({ create: createMock })

    const pb = new GresbaseClient({ url: 'http://localhost:8080', token: 'rec-tok', fetch: fetchMock })
    const descriptor = await pb.collection('users').registerPasskey('My passkey')

    // Begin call: authenticated POST to register-begin.
    const [beginUrl, beginInit] = (fetchMock as any).mock.calls[0]
    expect(beginUrl).toBe('http://localhost:8080/api/v1/collections/users/auth/passkey/register-begin')
    expect(beginInit.method).toBe('POST')
    expect(beginInit.headers['Authorization']).toBe('Bearer rec-tok')

    // navigator.credentials.create received decoded ArrayBuffers.
    const createOptions = createMock.mock.calls[0][0].publicKey
    expect(Array.from(new Uint8Array(createOptions.challenge))).toEqual([9, 8, 7])
    expect(Array.from(new Uint8Array(createOptions.user.id))).toEqual([1, 1, 2, 3])
    expect(Array.from(new Uint8Array(createOptions.excludeCredentials[0].id))).toEqual([5, 5])

    // Finish call: serialized credential with base64url fields.
    const [finishUrl, finishInit] = (fetchMock as any).mock.calls[1]
    expect(finishUrl).toBe('http://localhost:8080/api/v1/collections/users/auth/passkey/register-finish')
    const finishBody = JSON.parse(finishInit.body)
    expect(finishBody.sessionId).toBe('sess-1')
    expect(finishBody.name).toBe('My passkey')
    expect(finishBody.credential.id).toBe('cred-id')
    expect(finishBody.credential.rawId).toBe(bufferToBase64Url(new Uint8Array([0xde, 0xad])))
    expect(finishBody.credential.response.attestationObject).toBe(bufferToBase64Url(new Uint8Array([1, 2, 3])))
    expect(finishBody.credential.response.clientDataJSON).toBe(bufferToBase64Url(new Uint8Array([4, 5, 6])))
    expect(finishBody.credential.response.transports).toEqual(['internal', 'hybrid'])

    expect(descriptor.id).toBe('pk-1')
  })

  it('authWithPasskey posts the email hint, wraps navigator.credentials.get and stores the auth result', async () => {
    const challenge = bufferToBase64Url(new Uint8Array([42]))
    const credId = bufferToBase64Url(new Uint8Array([7, 7]))
    const fetchMock = createSequentialFetchMock([
      {
        sessionId: 'sess-2',
        options: {
          publicKey: {
            challenge,
            rpId: 'localhost',
            allowCredentials: [{ type: 'public-key', id: credId }],
          },
        },
      },
      { token: 'pk-token', refreshToken: 'pk-refresh', record: { id: 'u1', email: 'a@b.com' } },
    ])

    const getMock = vi.fn(async (_options: any) => ({
      id: 'cred-id',
      type: 'public-key',
      rawId: new Uint8Array([7, 7]).buffer,
      response: {
        authenticatorData: new Uint8Array([1]).buffer,
        clientDataJSON: new Uint8Array([2]).buffer,
        signature: new Uint8Array([3]).buffer,
        userHandle: new Uint8Array([4]).buffer,
      },
      getClientExtensionResults: () => ({}),
    }))
    stubNavigatorCredentials({ get: getMock })

    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })
    const result = await pb.collection('users').authWithPasskey('a@b.com')

    const [beginUrl, beginInit] = (fetchMock as any).mock.calls[0]
    expect(beginUrl).toBe('http://localhost:8080/api/v1/collections/users/auth/passkey/login-begin')
    expect(JSON.parse(beginInit.body)).toEqual({ email: 'a@b.com' })

    // allowCredentials ids decoded to ArrayBuffers for navigator.credentials.get.
    const getOptions = getMock.mock.calls[0][0].publicKey
    expect(Array.from(new Uint8Array(getOptions.challenge))).toEqual([42])
    expect(Array.from(new Uint8Array(getOptions.allowCredentials[0].id))).toEqual([7, 7])

    const [finishUrl, finishInit] = (fetchMock as any).mock.calls[1]
    expect(finishUrl).toBe('http://localhost:8080/api/v1/collections/users/auth/passkey/login-finish')
    const finishBody = JSON.parse(finishInit.body)
    expect(finishBody.sessionId).toBe('sess-2')
    expect(finishBody.credential.response.signature).toBe(bufferToBase64Url(new Uint8Array([3])))
    expect(finishBody.credential.response.userHandle).toBe(bufferToBase64Url(new Uint8Array([4])))

    // Same shared token store as authWithPassword.
    expect(result.token).toBe('pk-token')
    expect(pb.auth.getToken()).toBe('pk-token')
    expect(pb.auth.getRefreshToken()).toBe('pk-refresh')
    expect(pb.auth.getRecord()).toEqual({ id: 'u1', email: 'a@b.com' })
  })

  it('authWithPasskey without an email sends an empty body (discoverable login)', async () => {
    const fetchMock = createSequentialFetchMock([
      { sessionId: 's', options: { publicKey: { challenge: bufferToBase64Url(new Uint8Array([1])) } } },
      { token: 't', refreshToken: 'r', record: { id: 'u' } },
    ])
    stubNavigatorCredentials({
      get: vi.fn(async () => ({
        id: 'c',
        type: 'public-key',
        rawId: new Uint8Array([1]).buffer,
        response: {
          authenticatorData: new Uint8Array([1]).buffer,
          clientDataJSON: new Uint8Array([1]).buffer,
          signature: new Uint8Array([1]).buffer,
          userHandle: null,
        },
        getClientExtensionResults: () => ({}),
      })),
    })

    const pb = new GresbaseClient({ url: 'http://localhost:8080', fetch: fetchMock })
    await pb.collection('users').authWithPasskey()

    const [, beginInit] = (fetchMock as any).mock.calls[0]
    expect(JSON.parse(beginInit.body)).toEqual({})

    const [, finishInit] = (fetchMock as any).mock.calls[1]
    const finishBody = JSON.parse(finishInit.body)
    expect(finishBody.credential.response).not.toHaveProperty('userHandle')
  })

  it('listPasskeys and deletePasskey hit the management endpoints', async () => {
    const items = [{ id: 'pk-1', name: 'Laptop', created: '2026-06-12T00:00:00Z', lastUsedAt: null }]
    const fetchMock = createSequentialFetchMock([{ items }, { message: 'Passkey deleted' }])
    const pb = new GresbaseClient({ url: 'http://localhost:8080', token: 'rec-tok', fetch: fetchMock })
    const users = pb.collection('users')

    const list = await users.listPasskeys()
    expect(list).toEqual(items)

    await users.deletePasskey('pk-1')

    const calls = (fetchMock as any).mock.calls
    expect(calls[0][0]).toBe('http://localhost:8080/api/v1/collections/users/auth/passkeys')
    expect(calls[0][1].method).toBe('GET')
    expect(calls[1][0]).toBe('http://localhost:8080/api/v1/collections/users/auth/passkeys/pk-1')
    expect(calls[1][1].method).toBe('DELETE')
  })
})
