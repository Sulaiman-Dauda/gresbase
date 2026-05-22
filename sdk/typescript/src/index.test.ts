import { describe, it, expect } from 'vitest'
import { GresbaseClient } from '../src/index'

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
})
