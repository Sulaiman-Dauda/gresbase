export interface AdminUser {
  id: string
  tenant_id: string
  email: string
  avatar: string
  role: string
  last_login_at: string
  created_at: string
  updated_at: string
}

export interface Collection {
  id: string
  tenant_id: string
  name: string
  type: 'base' | 'auth' | 'view'
  schema: SchemaField[]
  /** Access rules: null = locked (superusers only), '' = public, expression = filtered. */
  list_rule: string | null
  view_rule: string | null
  create_rule: string | null
  update_rule: string | null
  delete_rule: string | null
  view_query?: string
  indexes: string[]
  system: boolean
  options: Record<string, any>
  created_at: string
  updated_at: string
}

export interface SchemaField {
  id: string
  name: string
  type: FieldType
  system: boolean
  required: boolean
  unique: boolean
  options: Record<string, any>
}

export type FieldType =
  | 'text'
  | 'number'
  | 'bool'
  | 'email'
  | 'url'
  | 'date'
  | 'select'
  | 'json'
  | 'file'
  | 'relation'
  | 'password'
  | 'editor'
  | 'geo_point'
  | 'autodate'
  | 'vector'

export interface Certificate {
  id: string
  domain: string
  issuer: string
  not_before: string
  not_after: string
  auto_renew: boolean
  challenge_type: string
  status: 'active' | 'expired' | 'revoked' | 'replaced'
  created_at: string
}

export interface APIKey {
  id: string
  admin_id: string
  name: string
  prefix: string
  permissions?: string[]
  last_used_at?: string
  expires_at?: string
  created_at: string
}

export interface RecordData {
  id: string
  expand?: Record<string, any>
  [key: string]: any
}

export interface PaginatedResponse<T> {
  items: T[]
  page: number
  perPage: number
  totalItems: number
  totalPages: number
}
