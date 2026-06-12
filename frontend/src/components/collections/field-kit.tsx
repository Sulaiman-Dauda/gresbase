'use client'

import * as React from 'react'
import Link from 'next/link'
import { format } from 'date-fns'
import { Check, Loader2, Paperclip, RefreshCw, Search, Upload, X } from 'lucide-react'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import type { Collection, RecordData, SchemaField } from '@/lib/types'

export type RecordEditorMode = 'create' | 'edit' | 'view'

interface RecordFieldInputProps {
  collection: Collection
  collections: Collection[]
  field: SchemaField
  value: any
  mode: RecordEditorMode
  recordId?: string
  draftSessionId?: string
  readOnly?: boolean
  onChange: (value: any) => void
  onPersistField?: (fieldName: string, value: any) => Promise<void>
  onRemoveStagedFile?: (path: string) => Promise<void> | void
}

export function RecordFieldInput({
  collection,
  collections,
  field,
  value,
  mode,
  recordId,
  draftSessionId,
  readOnly = false,
  onChange,
  onPersistField,
  onRemoveStagedFile,
}: RecordFieldInputProps) {
  const values = Array.isArray(field.options?.values) ? field.options.values : []

  if (field.type === 'bool') {
    return (
      <label className="flex items-center gap-3 rounded-lg border border-border px-3 py-3 text-sm">
        <input type="checkbox" checked={Boolean(value)} disabled={readOnly} onChange={(e) => onChange(e.target.checked)} />
        <span className="font-medium">{field.name}</span>
      </label>
    )
  }

  if (field.type === 'select' && values.length) {
    return (
      <FieldShell field={field}>
        <select
          value={value ?? ''}
          disabled={readOnly}
          onChange={(e) => onChange(e.target.value)}
          className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
        >
          <option value="">Select…</option>
          {values.map((option) => (
            <option key={String(option)} value={String(option)}>
              {String(option)}
            </option>
          ))}
        </select>
      </FieldShell>
    )
  }

  if (field.type === 'editor' || field.type === 'json' || field.type === 'geo_point') {
    return (
      <FieldShell field={field}>
        <textarea
          value={value ?? ''}
          disabled={readOnly}
          onChange={(e) => onChange(e.target.value)}
          rows={field.type === 'editor' ? 6 : 4}
          className="w-full rounded-lg border border-input bg-background px-3 py-2 text-sm"
          placeholder={field.type === 'json' ? '{"key":"value"}' : field.type === 'geo_point' ? '{"lat":0,"lng":0}' : field.name}
        />
      </FieldShell>
    )
  }

  if (field.type === 'relation') {
    return (
      <RelationFieldInput
        collections={collections}
        field={field}
        value={value}
        readOnly={readOnly}
        onChange={onChange}
      />
    )
  }

  if (field.type === 'file') {
    return (
      <FileFieldInput
        collection={collection}
        field={field}
        value={value}
        mode={mode}
        recordId={recordId}
        draftSessionId={draftSessionId}
        readOnly={readOnly}
        onChange={onChange}
        onPersistField={onPersistField}
        onRemoveStagedFile={onRemoveStagedFile}
      />
    )
  }

  return (
    <FieldShell field={field}>
      <Input
        type={field.type === 'number' ? 'number' : field.type === 'password' ? 'password' : field.type === 'email' ? 'email' : field.type === 'url' ? 'url' : 'text'}
        value={value ?? ''}
        disabled={readOnly || field.type === 'autodate'}
        onChange={(e) => onChange(e.target.value)}
        placeholder={field.name}
      />
    </FieldShell>
  )
}

function RelationFieldInput({
  collections,
  field,
  value,
  readOnly,
  onChange,
}: {
  collections: Collection[]
  field: SchemaField
  value: any
  readOnly: boolean
  onChange: (value: any) => void
}) {
  const [loading, setLoading] = React.useState(false)
  const [options, setOptions] = React.useState<RecordData[]>([])
  const [query, setQuery] = React.useState('')

  const relationCollection = React.useMemo(() => {
    const collectionId = String(field.options?.collection_id || '')
    if (!collectionId) return null
    return collections.find((collection) => collection.id === collectionId) || null
  }, [collections, field.options])

  const maxSelect = Number(field.options?.max_select || 1)
  const normalizedValue = React.useMemo(() => normalizeRelationValue(value, maxSelect), [maxSelect, value])

  const loadOptions = React.useCallback(async () => {
    if (!relationCollection) return
    setLoading(true)
    try {
      const response = await api.getRecords(relationCollection.name, {
        page: '1',
        perPage: '30',
        sort: '-updated_at',
      })
      setOptions(Array.isArray(response?.items) ? response.items : [])
    } catch {
      setOptions([])
    } finally {
      setLoading(false)
    }
  }, [relationCollection])

  React.useEffect(() => {
    loadOptions()
  }, [loadOptions])

  const displayField = React.useMemo(() => guessDisplayField(relationCollection), [relationCollection])
  const filtered = React.useMemo(() => {
    const search = query.trim().toLowerCase()
    if (!search) return options
    return options.filter((record) => {
      const label = String(record[displayField] || record.id || '').toLowerCase()
      return label.includes(search) || String(record.id || '').toLowerCase().includes(search)
    })
  }, [displayField, options, query])

  if (!relationCollection) {
    return (
      <FieldShell field={field}>
        <div className="rounded-lg border border-border bg-accent/10 px-3 py-3 text-sm text-muted-foreground">
          Relation target is not configured yet.
        </div>
      </FieldShell>
    )
  }

  if (readOnly) {
    return (
      <FieldShell field={field} hint={`Related to ${relationCollection.name}`}>
        <div className="flex flex-wrap gap-2">
          {(maxSelect > 1 ? normalizedValue : normalizedValue.slice(0, 1)).map((id) => (
            <span key={id} className="rounded-md border border-border bg-accent/10 px-2 py-1 text-xs font-mono text-foreground">
              {findRecordLabel(options, id, displayField)}
            </span>
          ))}
          {!normalizedValue.length ? <span className="text-sm text-muted-foreground">No relation selected</span> : null}
        </div>
      </FieldShell>
    )
  }

  return (
    <FieldShell field={field} hint={`Related to ${relationCollection.name}`}>
      <div className="space-y-3 rounded-lg border border-border bg-accent/10 p-3">
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={query} onChange={(e) => setQuery(e.target.value)} className="pl-9" placeholder={`Search ${relationCollection.name}...`} />
          </div>
          <Button type="button" variant="outline" size="sm" onClick={loadOptions}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
          </Button>
        </div>

        {maxSelect === 1 ? (
          <select
            value={normalizedValue[0] || ''}
            onChange={(e) => onChange(e.target.value)}
            className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
          >
            <option value="">Select a record…</option>
            {filtered.map((record) => (
              <option key={record.id} value={record.id}>
                {findRecordLabel(filtered, record.id, displayField)}
              </option>
            ))}
          </select>
        ) : (
          <div className="space-y-2">
            <div className="max-h-48 space-y-2 overflow-y-auto rounded-lg border border-border bg-background p-2">
              {filtered.map((record) => {
                const checked = normalizedValue.includes(record.id)
                return (
                  <label key={record.id} className="flex items-center gap-3 rounded-md px-2 py-2 text-sm hover:bg-accent/20">
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(e) => {
                        if (e.target.checked) {
                          onChange([...normalizedValue, record.id])
                        } else {
                          onChange(normalizedValue.filter((id) => id !== record.id))
                        }
                      }}
                    />
                    <span>{findRecordLabel(filtered, record.id, displayField)}</span>
                  </label>
                )
              })}
              {!filtered.length ? <div className="px-2 py-2 text-sm text-muted-foreground">No related records loaded.</div> : null}
            </div>
            {normalizedValue.length ? (
              <div className="flex flex-wrap gap-2">
                {normalizedValue.map((id) => (
                  <span key={id} className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-1 text-xs font-mono text-foreground">
                    {findRecordLabel(options, id, displayField)}
                    <button type="button" onClick={() => onChange(normalizedValue.filter((item) => item !== id))}>
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
            ) : null}
          </div>
        )}
      </div>
    </FieldShell>
  )
}

function FileFieldInput({
  collection,
  field,
  value,
  mode,
  recordId,
  draftSessionId,
  readOnly,
  onChange,
  onPersistField,
  onRemoveStagedFile,
}: {
  collection: Collection
  field: SchemaField
  value: any
  mode: RecordEditorMode
  recordId?: string
  draftSessionId?: string
  readOnly: boolean
  onChange: (value: any) => void
  onPersistField?: (fieldName: string, value: any) => Promise<void>
  onRemoveStagedFile?: (path: string) => Promise<void> | void
}) {
  const [uploading, setUploading] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)

  const files = React.useMemo(() => normalizeFileValue(value), [value])
  const maxSelect = Number(field.options?.max_select || 1)
  const stagingMode = !recordId && mode === 'create' && !!draftSessionId
  const canUpload = (!!recordId || stagingMode) && !readOnly

  const handleUpload = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const selected = Array.from(event.target.files || [])
    event.target.value = ''
    if (!selected.length) return

    setUploading(true)
    setError(null)
    try {
      let nextFiles = files.slice()
      for (const file of selected) {
        const uploaded = await api.uploadFile(stagingMode ? '_tmp' : collection.name, stagingMode ? draftSessionId! : recordId!, file)
        const storedValue = stagingMode ? String(uploaded?.path || '') : basename(uploaded?.path || uploaded?.name || file.name)
        if (maxSelect > 1) {
          nextFiles = [...nextFiles, storedValue].slice(-maxSelect)
        } else {
          nextFiles = [storedValue]
        }
      }
      const nextValue = maxSelect > 1 ? nextFiles : nextFiles[0] || ''
      onChange(nextValue)
      if (!stagingMode && onPersistField) {
        await onPersistField(field.name, nextValue)
      }
    } catch (err: any) {
      setError(err.message || 'Upload failed')
    } finally {
      setUploading(false)
    }
  }

  const handleRemove = async (filePath: string) => {
    const nextFiles = files.filter((item) => item !== filePath)
    const nextValue = maxSelect > 1 ? nextFiles : nextFiles[0] || ''
    onChange(nextValue)
    if (isTempStoredPath(filePath) && onRemoveStagedFile) {
      await onRemoveStagedFile(filePath)
    }
    if (!stagingMode && onPersistField) {
      await onPersistField(field.name, nextValue)
    }
  }

  return (
    <FieldShell field={field} hint={maxSelect > 1 ? `Up to ${maxSelect} files` : undefined}>
      <div className="space-y-3 rounded-lg border border-border bg-accent/10 p-3">
        {files.length ? (
          <div className="flex flex-wrap gap-2">
            {files.map((filename) => {
              const fileHref = buildFileHref(recordId ? collection.name : '_tmp', recordId || draftSessionId || '', filename)
              return (
                <span key={filename} className="inline-flex items-center gap-2 rounded-full border border-border bg-background px-2 py-1 text-xs text-foreground">
                  <Link href={fileHref} target="_blank" className="inline-flex items-center gap-2 hover:text-primary">
                    <Paperclip className="h-3 w-3" />
                    {displayFileName(filename)}
                  </Link>
                  {!readOnly ? (
                    <button type="button" onClick={() => void handleRemove(filename)} className="text-muted-foreground hover:text-foreground">
                      <X className="h-3 w-3" />
                    </button>
                  ) : null}
                </span>
              )
            })}
          </div>
        ) : (
          <div className="text-sm text-muted-foreground">No files attached.</div>
        )}

        {canUpload ? (
          <label className="inline-flex cursor-pointer items-center gap-2 rounded-md border border-dashed border-border bg-background px-3 py-2 text-sm text-muted-foreground hover:border-foreground/30 hover:text-foreground">
            {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
            {stagingMode ? 'Stage file' : 'Upload file'}
            <input type="file" className="hidden" onChange={handleUpload} multiple={maxSelect > 1} />
          </label>
        ) : !readOnly ? (
          <div className="text-sm text-muted-foreground">
            Save the record first, then upload files directly into this field.
          </div>
        ) : null}

  {stagingMode ? <div className="text-xs text-muted-foreground">Staged uploads will be promoted automatically after the record is created.</div> : null}

        {error ? <div className="text-sm text-red-500">{error}</div> : null}
      </div>
    </FieldShell>
  )
}

function FieldShell({ field, hint, children }: { field: SchemaField; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <label className="text-sm font-medium">{field.name}</label>
        {field.required ? <span className="text-[10px] uppercase tracking-wide text-muted-foreground">required</span> : null}
        {hint ? <span className="text-[10px] text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </div>
  )
}

export function renderRecordFieldValue(
  value: any,
  field: SchemaField,
  record?: RecordData,
  collections?: Collection[],
  collection?: Collection
) {
  if (field.type === 'bool' && value != null && value !== '') {
    return value ? (
      <Check className="h-4 w-4 text-emerald-500" role="img" aria-label="Yes" />
    ) : (
      <X className="h-4 w-4 text-muted-foreground/50" role="img" aria-label="No" />
    )
  }

  if (value == null || value === '') return <span className="text-muted-foreground">—</span>

  switch (field.type) {
    case 'select': {
      const selected = normalizeSelectValues(value)
      if (!selected.length) return <span className="text-muted-foreground">—</span>
      const shown = selected.slice(0, 2)
      return (
        <span className="flex flex-wrap items-center gap-1" title={selected.join(', ')}>
          {shown.map((item) => (
            <Badge key={item} variant="secondary" className="font-normal">
              {truncate(item, 20)}
            </Badge>
          ))}
          {selected.length > 2 ? (
            <Badge variant="outline" className="font-normal">
              +{selected.length - 2}
            </Badge>
          ) : null}
        </span>
      )
    }
    case 'relation': {
      const ids = normalizeRelationValue(value, Number(field.options?.max_select || 1))
      if (!ids.length) return <span className="text-muted-foreground">—</span>
      const expandMap = (record?.expand || (record as any)?.['@expand']) as Record<string, any> | undefined
      const labels = relationLabelsFromExpanded(expandMap?.[field.name], ids, field, collections || [])
      if (ids.length > 1) {
        return (
          <span
            className="inline-flex items-center rounded-md border border-border bg-accent/20 px-1.5 py-0.5 text-xs text-foreground"
            title={(labels.length ? labels : ids).join(', ')}
          >
            {ids.length} linked
          </span>
        )
      }
      const label = labels[0] || truncate(ids[0], 12)
      return (
        <span
          className="inline-flex max-w-[180px] items-center truncate rounded-md border border-border bg-accent/20 px-1.5 py-0.5 text-xs text-foreground"
          title={labels[0] || ids[0]}
        >
          {truncate(label, 28)}
        </span>
      )
    }
    case 'date':
    case 'autodate': {
      const date = new Date(String(value))
      if (Number.isNaN(date.getTime())) return <span>{truncate(String(value), 40)}</span>
      return (
        <span className="whitespace-nowrap text-xs text-muted-foreground" title={date.toISOString()}>
          {format(date, 'MMM d, yyyy HH:mm')}
        </span>
      )
    }
    case 'json':
    case 'geo_point':
    case 'vector': {
      const text = (typeof value === 'string' ? value : JSON.stringify(value)).replace(/\s+/g, ' ').trim()
      return (
        <code className="font-mono text-xs text-muted-foreground" title={truncate(text, 200)}>
          {truncate(text, 40)}
        </code>
      )
    }
    case 'file': {
      const files = normalizeFileValue(value)
      if (!files.length) return <span className="text-muted-foreground">—</span>
      if (collection && record?.id) {
        return <RecordFileCell collection={collection} recordId={record.id} files={files} />
      }
      return <span>{files.map(displayFileName).join(', ')}</span>
    }
    case 'email':
    case 'url': {
      const text = String(value)
      return (
        <span
          className="text-muted-foreground underline decoration-muted-foreground/40 underline-offset-2"
          title={text}
        >
          {truncate(text, 40)}
        </span>
      )
    }
    case 'editor': {
      const text = String(value).replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim()
      if (!text) return <span className="text-muted-foreground">—</span>
      return (
        <span className="block max-w-[280px] truncate" title={truncate(text, 300)}>
          {text}
        </span>
      )
    }
  }

  if (typeof value === 'object') {
    return <code className="font-mono text-xs text-muted-foreground">{truncate(JSON.stringify(value), 40)}</code>
  }

  const text = String(value)
  return (
    <span className="block max-w-[280px] truncate" title={text.length > 48 ? text : undefined}>
      {text}
    </span>
  )
}

function normalizeSelectValues(value: any): string[] {
  if (value == null || value === '') return []
  if (Array.isArray(value)) return value.map(String)
  if (typeof value === 'string' && value.trim().startsWith('[')) {
    try {
      const parsed = JSON.parse(value)
      return Array.isArray(parsed) ? parsed.map(String) : [value]
    } catch {
      return [value]
    }
  }
  return [String(value)]
}

const IMAGE_FILE_PATTERN = /\.(png|jpe?g|webp|gif)$/i

function RecordFileCell({ collection, recordId, files }: { collection: Collection; recordId: string; files: string[] }) {
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {files.map((file) => {
        const filename = displayFileName(file)
        if (IMAGE_FILE_PATTERN.test(filename) && !isTempStoredPath(file)) {
          return <RecordImageThumb key={file} collection={collection.name} recordId={recordId} filename={filename} />
        }
        return (
          <span key={file} className="inline-flex max-w-[160px] items-center gap-1 text-xs text-muted-foreground" title={filename}>
            <Paperclip className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{filename}</span>
          </span>
        )
      })}
    </span>
  )
}

function RecordImageThumb({ collection, recordId, filename }: { collection: string; recordId: string; filename: string }) {
  const [failed, setFailed] = React.useState(false)
  if (failed) return <span>{filename}</span>
  const src = `${api.getFileURL(collection, recordId, filename)}?thumb=64x64`
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={src}
      alt={filename}
      title={filename}
      loading="lazy"
      className="h-8 w-8 rounded-md border border-border object-cover"
      onError={() => setFailed(true)}
    />
  )
}

export function buildDraftFromRecord(collection: Collection, record?: RecordData | null) {
  const draft: Record<string, any> = {}
  for (const field of collection.schema.filter((item) => !item.system)) {
    draft[field.name] = normalizeDraftValue(field, record?.[field.name])
  }
  return draft
}

export function buildRecordPayload(schema: SchemaField[], draft: Record<string, any>, partial: boolean) {
  const payload: Record<string, any> = {}

  for (const field of schema.filter((item) => !item.system)) {
    const rawValue = draft[field.name]

    if (field.type === 'autodate') continue
    if (field.type === 'password' && partial && !rawValue) continue
    if ((rawValue === '' || rawValue == null) && !field.required) continue

    switch (field.type) {
      case 'bool':
        payload[field.name] = Boolean(rawValue)
        break
      case 'number':
        payload[field.name] = rawValue === '' ? null : Number(rawValue)
        break
      case 'json':
      case 'geo_point':
        payload[field.name] = rawValue === '' ? null : JSON.parse(rawValue)
        break
      case 'relation': {
        const maxSelect = Number(field.options?.max_select || 1)
        const relationValue = normalizeRelationValue(rawValue, maxSelect)
        payload[field.name] = maxSelect > 1 ? relationValue : relationValue[0] || ''
        break
      }
      case 'file': {
        const files = normalizeFileValue(rawValue)
        payload[field.name] = Number(field.options?.max_select || 1) > 1 ? files : files[0] || ''
        break
      }
      default:
        payload[field.name] = rawValue
        break
    }
  }

  return payload
}

export function describeFieldSettings(field: SchemaField, collections: Collection[]) {
  const settings: string[] = []

  switch (field.type) {
    case 'text':
      if (field.options?.min != null) settings.push(`min ${field.options.min}`)
      if (field.options?.max != null) settings.push(`max ${field.options.max}`)
      if (field.options?.pattern) settings.push('pattern')
      break
    case 'number':
      if (field.options?.min != null) settings.push(`min ${field.options.min}`)
      if (field.options?.max != null) settings.push(`max ${field.options.max}`)
      break
    case 'select': {
      const count = Array.isArray(field.options?.values) ? field.options.values.length : 0
      if (count) settings.push(`${count} option${count !== 1 ? 's' : ''}`)
      break
    }
    case 'relation': {
      const target = collections.find((collection) => collection.id === String(field.options?.collection_id || ''))
      if (target) settings.push(`→ ${target.name}`)
      const maxSelect = Number(field.options?.max_select || 1)
      settings.push(maxSelect > 1 ? `multi (${maxSelect})` : 'single')
      break
    }
    case 'file': {
      const maxSelect = Number(field.options?.max_select || 1)
      settings.push(maxSelect > 1 ? `multi (${maxSelect})` : 'single')
      if (field.options?.max_size) settings.push(formatBytes(Number(field.options.max_size)))
      if (Array.isArray(field.options?.mime_types) && field.options.mime_types.length) settings.push(`${field.options.mime_types.length} mime types`)
      break
    }
    case 'editor':
      settings.push('rich text')
      break
    case 'autodate':
      settings.push('system managed')
      break
  }

  return settings
}

function normalizeDraftValue(field: SchemaField, value: any) {
  if (field.type === 'bool') return Boolean(value)
  if (field.type === 'relation') return Number(field.options?.max_select || 1) > 1 ? normalizeRelationValue(value, Number(field.options?.max_select || 1)) : value ?? ''
  if (field.type === 'file') return Number(field.options?.max_select || 1) > 1 ? normalizeFileValue(value) : value ?? ''
  if (value == null) return ''
  if (field.type === 'json' || field.type === 'geo_point') return typeof value === 'string' ? value : JSON.stringify(value, null, 2)
  return String(value)
}

function normalizeRelationValue(value: any, maxSelect: number) {
  if (value == null || value === '') return []
  if (Array.isArray(value)) return value.map(String)
  if (typeof value === 'string' && value.trim().startsWith('[')) {
    try {
      const parsed = JSON.parse(value)
      return Array.isArray(parsed) ? parsed.map(String) : []
    } catch {
      return maxSelect > 1 ? [] : [value]
    }
  }
  return [String(value)]
}

export function extractDraftFileValues(schema: SchemaField[], draft: Record<string, any>) {
  const result: Record<string, string[]> = {}
  for (const field of schema) {
    if (field.type !== 'file') continue
    const files = normalizeFileValue(draft[field.name])
    if (files.length) {
      result[field.name] = files
    }
  }
  return result
}

export function isTempStoredPath(value: string) {
  return typeof value === 'string' && value.startsWith('_tmp/')
}

function normalizeFileValue(value: any) {
  if (value == null || value === '') return []
  if (Array.isArray(value)) return value.map(String)
  if (typeof value === 'string' && value.trim().startsWith('[')) {
    try {
      const parsed = JSON.parse(value)
      return Array.isArray(parsed) ? parsed.map(String) : [value]
    } catch {
      return [value]
    }
  }
  return [String(value)]
}

function basename(path: string) {
  const parts = path.split('/')
  return parts[parts.length - 1] || path
}

function displayFileName(path: string) {
  const name = basename(path)
  return name || path
}

function buildFileHref(collection: string, recordId: string, pathOrFilename: string) {
  if (isTempStoredPath(pathOrFilename)) {
    const parts = pathOrFilename.split('/')
    if (parts.length >= 3) {
      return api.getFileURL(parts[0], parts[1], parts[parts.length - 1])
    }
  }
  const filename = basename(pathOrFilename)
  return api.getFileURL(collection, recordId, filename)
}

function guessDisplayField(collection: Collection | null) {
  if (!collection) return 'id'
  const preferred = ['title', 'name', 'label', 'email', 'username']
  for (const name of preferred) {
    if (collection.schema.some((field) => field.name === name)) return name
  }
  const candidate = collection.schema.find((field) => ['text', 'email', 'url', 'select'].includes(field.type))
  return candidate?.name || 'id'
}

function relationLabelsFromExpanded(expandedValue: any, ids: string[], field: SchemaField, collections: Collection[]) {
  if (!expandedValue) return []

  const displayFields = relationDisplayFields(field, collections)
  const records = Array.isArray(expandedValue) ? expandedValue : [expandedValue]
  const byId = new Map(records.map((item: any) => [String(item?.id || ''), item]))
  return ids.map((id) => renderExpandedRecordLabel(byId.get(id), displayFields)).filter(Boolean) as string[]
}

function relationDisplayFields(field: SchemaField, collections: Collection[]) {
  if (Array.isArray(field.options?.display_fields) && field.options.display_fields.length) {
    return field.options.display_fields.map(String)
  }
  const relatedCollection = collections.find((collection) => collection.id === String(field.options?.collection_id || ''))
  const preferred = ['title', 'name', 'label', 'email', 'username']
  for (const key of preferred) {
    if (relatedCollection?.schema.some((candidate) => candidate.name === key)) {
      return [key]
    }
  }
  return ['id']
}

function renderExpandedRecordLabel(record: any, displayFields: string[]) {
  if (!record) return null
  if (record._label) return String(record._label)
  const parts = displayFields.map((field) => String(record?.[field] || '')).map((item) => item.trim()).filter(Boolean)
  if (parts.length) return parts.join(' · ')
  return String(record?.id || '')
}

function findRecordLabel(records: RecordData[], id: string, displayField: string) {
  const record = records.find((item) => item.id === id)
  if (!record) return id
  return String(record[displayField] || record.id)
}

function formatBytes(bytes: number) {
  if (!bytes || Number.isNaN(bytes)) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function truncate(value: string, limit = 16) {
  return value.length > limit ? `${value.slice(0, limit)}…` : value
}

// ---------------------------------------------------------------------------
// Lightweight dropdown menu (no Radix). Used by the records table row menus,
// the columns toggle, and the record drawer "⋯" actions menu.
// ---------------------------------------------------------------------------

type DropdownPosition = { top?: number; bottom?: number; left?: number; right?: number }

export function DropdownMenu({
  trigger,
  children,
  align = 'end',
  width = 'w-52',
}: {
  trigger: React.ReactNode
  children: React.ReactNode | ((close: () => void) => React.ReactNode)
  align?: 'start' | 'end'
  width?: string
}) {
  const [open, setOpen] = React.useState(false)
  const [position, setPosition] = React.useState<DropdownPosition | null>(null)
  const containerRef = React.useRef<HTMLDivElement>(null)
  const triggerRef = React.useRef<HTMLDivElement>(null)

  const close = React.useCallback(() => setOpen(false), [])

  const toggle = () => {
    if (open) {
      setOpen(false)
      return
    }
    const rect = triggerRef.current?.getBoundingClientRect()
    if (!rect) return
    const estimatedHeight = 280
    const openUp = rect.bottom + estimatedHeight > window.innerHeight && rect.top > estimatedHeight
    const horizontal: DropdownPosition =
      align === 'end' ? { right: Math.max(8, window.innerWidth - rect.right) } : { left: Math.max(8, rect.left) }
    setPosition(
      openUp
        ? { ...horizontal, bottom: window.innerHeight - rect.top + 4 }
        : { ...horizontal, top: rect.bottom + 4 }
    )
    setOpen(true)
  }

  React.useEffect(() => {
    if (!open) return

    const onPointerDown = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setOpen(false)
      }
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        setOpen(false)
      }
    }
    const onScrollOrResize = (event: Event) => {
      if (event.target instanceof Node && containerRef.current?.contains(event.target)) return
      setOpen(false)
    }

    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown, true)
    window.addEventListener('scroll', onScrollOrResize, true)
    window.addEventListener('resize', onScrollOrResize)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown, true)
      window.removeEventListener('scroll', onScrollOrResize, true)
      window.removeEventListener('resize', onScrollOrResize)
    }
  }, [open])

  return (
    <div ref={containerRef} className="relative inline-block" onClick={(event) => event.stopPropagation()}>
      <div ref={triggerRef} onClick={toggle}>
        {trigger}
      </div>
      {open && position ? (
        <div
          role="menu"
          className={cn(
            'fixed z-[60] max-h-80 overflow-y-auto rounded-lg border border-border bg-background p-1 shadow-xl',
            width
          )}
          style={position}
        >
          {typeof children === 'function' ? children(close) : children}
        </div>
      ) : null}
    </div>
  )
}

export function DropdownMenuItem({
  icon,
  destructive = false,
  disabled = false,
  onSelect,
  children,
}: {
  icon?: React.ReactNode
  destructive?: boolean
  disabled?: boolean
  onSelect: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      onClick={onSelect}
      className={cn(
        'flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors',
        destructive ? 'text-red-500 hover:bg-red-500/10' : 'text-foreground hover:bg-accent/50',
        disabled && 'pointer-events-none opacity-50'
      )}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{children}</span>
    </button>
  )
}
