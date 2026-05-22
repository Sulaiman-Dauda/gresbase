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
  list_rule: string
  view_rule: string
  create_rule: string
  update_rule: string
  delete_rule: string
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
  created_at: string
}

export interface RecordData {
  id: string
  [key: string]: any
}

export interface PaginatedResponse<T> {
  items: T[]
  page: number
  perPage: number
  totalItems: number
  totalPages: number
}
