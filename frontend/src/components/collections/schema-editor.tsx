'use client'

import * as React from 'react'
import { ChevronDown, Download, FlaskConical, GripVertical, Loader2, Lock, LockOpen, Plus, Save, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import type { Collection, FieldType, SchemaField } from '@/lib/types'

const FIELD_TYPES: Array<{ value: FieldType; label: string }> = [
  { value: 'text', label: 'Text' },
  { value: 'number', label: 'Number' },
  { value: 'bool', label: 'Boolean' },
  { value: 'email', label: 'Email' },
  { value: 'url', label: 'URL' },
  { value: 'date', label: 'Date' },
  { value: 'select', label: 'Select' },
  { value: 'json', label: 'JSON' },
  { value: 'file', label: 'File' },
  { value: 'relation', label: 'Relation' },
  { value: 'password', label: 'Password' },
  { value: 'editor', label: 'Editor' },
  { value: 'geo_point', label: 'Geo point' },
  { value: 'autodate', label: 'Autodate' },
  { value: 'vector', label: 'Vector (embeddings)' },
]

interface RulePreset {
  key: string
  label: string
  description: string
  value: string | null
  kind: string
}

interface SchemaEditorProps {
  collection: Collection
  collections: Collection[]
  onSaved: (collection: Collection) => void
}

export function SchemaEditor({ collection, collections, onSaved }: SchemaEditorProps) {
  const [draft, setDraft] = React.useState<Collection>(() => cloneCollection(collection))
  const [saving, setSaving] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [newFieldName, setNewFieldName] = React.useState('')
  const [newFieldType, setNewFieldType] = React.useState<FieldType>('text')
  const [vectorSupported, setVectorSupported] = React.useState(true)
  const [rulePresets, setRulePresets] = React.useState<RulePreset[]>([])
  const [expandedFields, setExpandedFields] = React.useState<Set<string>>(new Set())
  const [dragFieldId, setDragFieldId] = React.useState<string | null>(null)

  React.useEffect(() => {
    setDraft(cloneCollection(collection))
    setError(null)
  }, [collection])

  React.useEffect(() => {
    let active = true
    api
      .features()
      .then((features) => {
        if (active) setVectorSupported(Boolean(features?.vector))
      })
      .catch(() => {
        /* leave default; backend returns a clear error on save */
      })
    api
      .rulePresets()
      .then((presets) => {
        if (active) setRulePresets(Array.isArray(presets) ? presets : [])
      })
      .catch(() => {
        /* presets are optional */
      })
    return () => {
      active = false
    }
  }, [])

  const updateField = (fieldId: string, updater: (field: SchemaField) => SchemaField) => {
    setDraft((current) => ({
      ...current,
      schema: current.schema.map((field) => (field.id === fieldId ? updater(field) : field)),
    }))
  }

  const removeField = (fieldId: string) => {
    setDraft((current) => ({
      ...current,
      schema: current.schema.filter((field) => field.id !== fieldId),
    }))
  }

  const toggleFieldExpanded = (fieldId: string) => {
    setExpandedFields((current) => {
      const next = new Set(current)
      if (next.has(fieldId)) next.delete(fieldId)
      else next.add(fieldId)
      return next
    })
  }

  const reorderField = (sourceId: string, targetId: string) => {
    if (sourceId === targetId) return
    setDraft((current) => {
      const fields = [...current.schema]
      const from = fields.findIndex((f) => f.id === sourceId)
      const to = fields.findIndex((f) => f.id === targetId)
      if (from === -1 || to === -1) return current
      const [moved] = fields.splice(from, 1)
      fields.splice(to, 0, moved)
      return { ...current, schema: fields }
    })
  }

  const addField = () => {
    if (!newFieldName.trim()) return
    const nextField: SchemaField = {
      id: typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}`,
      name: newFieldName.trim(),
      type: newFieldType,
      system: false,
      required: false,
      unique: false,
      options: defaultOptionsForType(newFieldType),
    }
    setDraft((current) => ({ ...current, schema: [...current.schema, nextField] }))
    setExpandedFields((current) => new Set(current).add(nextField.id))
    setNewFieldName('')
    setNewFieldType('text')
  }

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const payload = normalizeCollectionDraft(draft)
      const updated = await api.updateCollection(collection.id, payload)
      toast.success('Collection schema saved')
      onSaved(updated)
    } catch (err: any) {
      setError(err.message || 'Failed to save schema')
      toast.error(err.message || 'Failed to save schema')
    } finally {
      setSaving(false)
    }
  }

  const exportCurrent = () => {
    const payload = normalizeCollectionDraft(draft)
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `${draft.name || 'collection'}.json`
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-6">
      {error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/5 px-4 py-3 text-sm text-red-500">{error}</div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
          Save schema
        </Button>
        <Button variant="outline" onClick={exportCurrent}>
          <Download className="mr-2 h-4 w-4" />
          Export current collection
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">General</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <label className="text-sm font-medium">Collection name</label>
            <Input value={draft.name} onChange={(e) => setDraft((current) => ({ ...current, name: e.target.value }))} />
          </div>
          <div className="space-y-2">
            <label className="text-sm font-medium">Type</label>
            <select
              value={draft.type}
              onChange={(e) => setDraft((current) => ({ ...current, type: e.target.value as Collection['type'] }))}
              className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
            >
              <option value="base">Base</option>
              <option value="auth">Auth</option>
              <option value="view">View</option>
            </select>
          </div>
          {draft.type === 'auth' ? (
            <div className="space-y-2 md:col-span-2">
              <label className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
                <input
                  type="checkbox"
                  checked={Boolean(draft.options?.allowAnonymous)}
                  onChange={(e) =>
                    setDraft((current) => ({
                      ...current,
                      options: { ...(current.options || {}), allowAnonymous: e.target.checked },
                    }))
                  }
                />
                Allow anonymous sign-in
              </label>
              <p className="text-xs text-muted-foreground">
                Enables <code className="rounded bg-muted px-1">POST /api/v1/collections/{draft.name || '{collection}'}/auth/auth-with-anonymous</code> —
                guest sessions without credentials that receive record-scoped tokens.
              </p>
              <label className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
                <input
                  type="checkbox"
                  checked={Boolean(draft.options?.allowPasskeys)}
                  onChange={(e) =>
                    setDraft((current) => ({
                      ...current,
                      options: { ...(current.options || {}), allowPasskeys: e.target.checked },
                    }))
                  }
                />
                Allow passkeys (WebAuthn)
              </label>
              <p className="text-xs text-muted-foreground">
                Enables <code className="rounded bg-muted px-1">POST /api/v1/collections/{draft.name || '{collection}'}/auth/passkey/*</code> —
                phishing-resistant sign-in with platform authenticators (Touch ID, Windows Hello, security keys). Off by default.
              </p>
            </div>
          ) : null}
          {draft.type === 'view' ? (
            <div className="space-y-2 md:col-span-2">
              <label className="text-sm font-medium">View query</label>
              <textarea
                value={draft.view_query || ''}
                onChange={(e) => setDraft((current) => ({ ...current, view_query: e.target.value }))}
                rows={6}
                className="w-full rounded-lg border border-input bg-background px-3 py-2 text-sm"
                placeholder="SELECT id, title, created_at FROM posts"
              />
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Fields</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px_auto]">
            <Input value={newFieldName} onChange={(e) => setNewFieldName(e.target.value)} placeholder="new_field_name" />
            <select
              value={newFieldType}
              onChange={(e) => setNewFieldType(e.target.value as FieldType)}
              className="h-9 rounded-lg border border-input bg-background px-3 text-sm"
            >
              {FIELD_TYPES.map((type) => (
                <option key={type.value} value={type.value}>
                  {type.label}
                </option>
              ))}
            </select>
            <Button variant="outline" onClick={addField}>
              <Plus className="mr-2 h-4 w-4" />
              Add field
            </Button>
          </div>

          {draft.schema.map((field) => {
            const expanded = expandedFields.has(field.id)
            const typeLabel = FIELD_TYPES.find((t) => t.value === field.type)?.label || field.type
            return (
              <div
                key={field.id}
                className={`rounded-xl border border-border transition-opacity ${dragFieldId === field.id ? 'opacity-40' : ''}`}
                onDragOver={(e) => {
                  if (dragFieldId && dragFieldId !== field.id) e.preventDefault()
                }}
                onDrop={(e) => {
                  e.preventDefault()
                  if (dragFieldId) reorderField(dragFieldId, field.id)
                  setDragFieldId(null)
                }}
              >
                <div
                  className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left"
                  onClick={() => toggleFieldExpanded(field.id)}
                >
                  <span
                    draggable
                    onClick={(e) => e.stopPropagation()}
                    onDragStart={(e) => {
                      e.dataTransfer.effectAllowed = 'move'
                      setDragFieldId(field.id)
                    }}
                    onDragEnd={() => setDragFieldId(null)}
                    className="cursor-grab text-muted-foreground/60 hover:text-muted-foreground active:cursor-grabbing"
                    title="Drag to reorder"
                  >
                    <GripVertical className="h-4 w-4" />
                  </span>
                  <span className="font-mono text-sm text-foreground">{field.name || <em className="text-muted-foreground">unnamed</em>}</span>
                  <Badge variant="secondary" className="font-normal">{typeLabel}</Badge>
                  {field.required ? <Badge className="font-normal">required</Badge> : null}
                  {field.unique ? <Badge className="font-normal">unique</Badge> : null}
                  {field.system ? <Badge variant="secondary" className="font-normal opacity-70">system</Badge> : null}
                  <span className="ml-auto flex items-center gap-1">
                    {!field.system ? (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        onClick={(e) => {
                          e.stopPropagation()
                          removeField(field.id)
                        }}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    ) : null}
                    <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${expanded ? 'rotate-180' : ''}`} />
                  </span>
                </div>

                {expanded ? (
                  <div className="space-y-4 border-t border-border p-4">
                    <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_160px_auto_auto]">
                      <Input
                        value={field.name}
                        onChange={(e) => updateField(field.id, (current) => ({ ...current, name: e.target.value }))}
                        disabled={field.system}
                        placeholder="field_name"
                      />
                      <select
                        value={field.type}
                        onChange={(e) => updateField(field.id, (current) => ({ ...current, type: e.target.value as FieldType, options: { ...defaultOptionsForType(e.target.value as FieldType), ...current.options } }))}
                        disabled={field.system}
                        className="h-9 rounded-lg border border-input bg-background px-3 text-sm"
                      >
                        {FIELD_TYPES.map((type) => (
                          <option key={type.value} value={type.value}>
                            {type.label}
                          </option>
                        ))}
                      </select>
                      <label className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
                        <input type="checkbox" checked={field.required} onChange={(e) => updateField(field.id, (current) => ({ ...current, required: e.target.checked }))} disabled={field.system} />
                        Required
                      </label>
                      <label className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
                        <input type="checkbox" checked={field.unique} onChange={(e) => updateField(field.id, (current) => ({ ...current, unique: e.target.checked }))} disabled={field.system || field.type === 'bool' || field.type === 'json' || field.type === 'geo_point' || field.type === 'file'} />
                        Unique
                      </label>
                    </div>

                    <FieldSettingsEditor
                      field={field}
                      collections={collections}
                      vectorSupported={vectorSupported}
                      onChange={(nextOptions) => updateField(field.id, (current) => ({ ...current, options: nextOptions }))}
                    />
                  </div>
                ) : null}
              </div>
            )
          })}
        </CardContent>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Access rules</CardTitle>
            <p className="text-xs text-muted-foreground">
              Locked rules are restricted to superusers. Unlock a rule and leave it empty to make the
              operation public, or write a filter like <code className="rounded bg-muted px-1">owner = @request.auth.id</code>.
            </p>
          </CardHeader>
          <CardContent className="space-y-4">
            {RULE_FIELDS.map(([key, label]) => (
              <RuleEditor
                key={key}
                label={label}
                value={(draft as any)[key] as string | null}
                presets={rulePresets}
                onChange={(next) => setDraft((current) => ({ ...current, [key]: next }))}
              />
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Indexes</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <textarea
              value={(draft.indexes || []).join('\n')}
              onChange={(e) => setDraft((current) => ({ ...current, indexes: e.target.value.split(/\n+/).map((item) => item.trim()).filter(Boolean) }))}
              rows={10}
              className="w-full rounded-lg border border-input bg-background px-3 py-2 font-mono text-sm"
              placeholder='CREATE INDEX idx_posts_status ON posts(status)'
            />
            <p className="text-xs text-muted-foreground">
              One index expression per line. Keep them concise and valid for PostgreSQL.
            </p>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function FieldSettingsEditor({
  field,
  collections,
  vectorSupported,
  onChange,
}: {
  field: SchemaField
  collections: Collection[]
  vectorSupported: boolean
  onChange: (options: Record<string, any>) => void
}) {
  const options = field.options || {}
  const update = (patch: Record<string, any>) => onChange({ ...options, ...patch })

  return (
    <div className="grid gap-3 md:grid-cols-2">
      {showsDefault(field.type) ? (
        <FieldOption label="Default value">
          {field.type === 'bool' ? (
            <select
              value={options.default === true ? 'true' : options.default === false ? 'false' : ''}
              onChange={(e) => update({ default: e.target.value })}
              className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
            >
              <option value="">No default</option>
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          ) : (
            <Input value={stringOrEmpty(options.default)} onChange={(e) => update({ default: e.target.value })} />
          )}
        </FieldOption>
      ) : null}

      {(field.type === 'text' || field.type === 'password') ? (
        <>
          <FieldOption label="Min length">
            <Input type="number" value={stringOrEmpty(options.min)} onChange={(e) => update({ min: numericOrNil(e.target.value) })} />
          </FieldOption>
          <FieldOption label="Max length">
            <Input type="number" value={stringOrEmpty(options.max)} onChange={(e) => update({ max: numericOrNil(e.target.value) })} />
          </FieldOption>
          {field.type === 'text' ? (
            <FieldOption label="Pattern">
              <Input value={stringOrEmpty(options.pattern)} onChange={(e) => update({ pattern: e.target.value })} placeholder="^[a-z0-9_-]+$" />
            </FieldOption>
          ) : null}
        </>
      ) : null}

      {field.type === 'number' ? (
        <>
          <FieldOption label="Min value">
            <Input type="number" value={stringOrEmpty(options.min)} onChange={(e) => update({ min: numericOrNil(e.target.value) })} />
          </FieldOption>
          <FieldOption label="Max value">
            <Input type="number" value={stringOrEmpty(options.max)} onChange={(e) => update({ max: numericOrNil(e.target.value) })} />
          </FieldOption>
        </>
      ) : null}

      {field.type === 'select' ? (
        <FieldOption label="Options" className="md:col-span-2">
          <textarea
            value={Array.isArray(options.values) ? options.values.join(', ') : ''}
            onChange={(e) => update({ values: splitComma(e.target.value) })}
            rows={3}
            className="w-full rounded-lg border border-input bg-background px-3 py-2 text-sm"
            placeholder="draft, published, archived"
          />
        </FieldOption>
      ) : null}

      {field.type === 'relation' ? (
        <>
          <FieldOption label="Related collection">
            <select
              value={stringOrEmpty(options.collection_id)}
              onChange={(e) => update({ collection_id: e.target.value, collection_name: collections.find((item) => item.id === e.target.value)?.name || '' })}
              className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
            >
              <option value="">Select…</option>
              {collections.map((collection) => (
                <option key={collection.id} value={collection.id}>
                  {collection.name}
                </option>
              ))}
            </select>
          </FieldOption>
          <FieldOption label="Max select">
            <Input type="number" min="1" value={stringOrEmpty(options.max_select || 1)} onChange={(e) => update({ max_select: numericOrNil(e.target.value) || 1 })} />
          </FieldOption>
          <FieldOption label="Display fields" className="md:col-span-2">
            <Input value={Array.isArray(options.display_fields) ? options.display_fields.join(', ') : ''} onChange={(e) => update({ display_fields: splitComma(e.target.value) })} placeholder="name, email" />
          </FieldOption>
          <FieldOption label="Cascade delete">
            <label className="flex h-9 items-center gap-2 rounded-lg border border-border px-3 text-sm">
              <input type="checkbox" checked={Boolean(options.cascade_delete)} onChange={(e) => update({ cascade_delete: e.target.checked })} />
              Enable
            </label>
          </FieldOption>
        </>
      ) : null}

      {field.type === 'file' ? (
        <>
          <FieldOption label="Max files">
            <Input type="number" min="1" value={stringOrEmpty(options.max_select || 1)} onChange={(e) => update({ max_select: numericOrNil(e.target.value) || 1 })} />
          </FieldOption>
          <FieldOption label="Max size (bytes)">
            <Input type="number" min="0" value={stringOrEmpty(options.max_size)} onChange={(e) => update({ max_size: numericOrNil(e.target.value) })} />
          </FieldOption>
          <FieldOption label="Allowed mime types" className="md:col-span-2">
            <Input value={Array.isArray(options.mime_types) ? options.mime_types.join(', ') : ''} onChange={(e) => update({ mime_types: splitComma(e.target.value) })} placeholder="image/png, image/jpeg" />
          </FieldOption>
          <FieldOption label="Thumb presets" className="md:col-span-2">
            <Input value={Array.isArray(options.thumbs) ? options.thumbs.join(', ') : ''} onChange={(e) => update({ thumbs: splitComma(e.target.value) })} placeholder="100x100, 300x300" />
          </FieldOption>
          <FieldOption label="Protected downloads">
            <label className="flex h-9 items-center gap-2 rounded-lg border border-border px-3 text-sm">
              <input type="checkbox" checked={Boolean(options.protected)} onChange={(e) => update({ protected: e.target.checked })} />
              Require auth
            </label>
          </FieldOption>
        </>
      ) : null}

      {field.type === 'json' ? (
        <FieldOption label="Max JSON size">
          <Input type="number" min="0" value={stringOrEmpty(options.max_size)} onChange={(e) => update({ max_size: numericOrNil(e.target.value) })} />
        </FieldOption>
      ) : null}

      {field.type === 'vector' ? (
        <>
          <FieldOption label="Dimensions">
            <Input type="number" min="1" value={stringOrEmpty(options.dimensions)} onChange={(e) => update({ dimensions: numericOrNil(e.target.value) })} placeholder="1536" />
          </FieldOption>
          <FieldOption label="Distance">
            <select
              value={options.distance || 'cosine'}
              onChange={(e) => update({ distance: e.target.value })}
              className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
            >
              <option value="cosine">Cosine</option>
              <option value="l2">L2 (Euclidean)</option>
              <option value="inner">Inner product</option>
            </select>
          </FieldOption>
          <FieldOption label="Index">
            <select
              value={options.index || 'hnsw'}
              onChange={(e) => update({ index: e.target.value })}
              className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
            >
              <option value="hnsw">HNSW</option>
              <option value="ivfflat">IVFFlat</option>
              <option value="none">None</option>
            </select>
          </FieldOption>
          {!vectorSupported ? (
            <p className="md:col-span-2 text-xs text-amber-600 dark:text-amber-400">
              pgvector not detected on this PostgreSQL — vector fields need the &lsquo;vector&rsquo; extension (use external Postgres or install pgvector).
            </p>
          ) : null}
        </>
      ) : null}
    </div>
  )
}

const RULE_FIELDS: Array<[string, string]> = [
  ['list_rule', 'List rule'],
  ['view_rule', 'View rule'],
  ['create_rule', 'Create rule'],
  ['update_rule', 'Update rule'],
  ['delete_rule', 'Delete rule'],
]

function RuleEditor({
  label,
  value,
  presets,
  onChange,
}: {
  label: string
  value: string | null
  presets: RulePreset[]
  onChange: (next: string | null) => void
}) {
  const locked = value === null || value === undefined
  const [testing, setTesting] = React.useState(false)

  const applyPreset = (key: string) => {
    const preset = presets.find((item) => item.key === key)
    if (!preset) return
    onChange(preset.value)
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <label className="text-sm font-medium">{label}</label>
        <div className="flex items-center gap-2">
          {presets.length ? (
            <select
              value=""
              onChange={(e) => {
                if (e.target.value) applyPreset(e.target.value)
              }}
              className="h-7 rounded-md border border-input bg-background px-2 text-xs"
              title="Apply a rule preset"
            >
              <option value="">Preset…</option>
              {presets.map((preset) => (
                <option key={preset.key} value={preset.key} title={preset.description}>
                  {preset.label}
                </option>
              ))}
            </select>
          ) : null}
          <button
            type="button"
            onClick={() => setTesting((current) => !current)}
            className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition-colors ${
              testing ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:bg-muted'
            }`}
            title="Simulate this rule against a test request"
          >
            <FlaskConical className="h-3 w-3" />
            Test
          </button>
          <button
            type="button"
            onClick={() => onChange(locked ? '' : null)}
            className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition-colors ${
              locked
                ? 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
                : 'border-border text-muted-foreground hover:bg-muted'
            }`}
            title={locked ? 'Unlock to define a custom rule' : 'Lock to superusers only'}
          >
            {locked ? <Lock className="h-3 w-3" /> : <LockOpen className="h-3 w-3" />}
            {locked ? 'Superusers only' : 'Lock'}
          </button>
        </div>
      </div>
      {locked ? (
        <div className="rounded-lg border border-dashed border-border bg-muted/40 px-3 py-2.5 text-sm text-muted-foreground">
          Locked — only superusers can perform this operation.
        </div>
      ) : (
        <>
          <textarea
            value={value ?? ''}
            onChange={(e) => onChange(e.target.value)}
            rows={2}
            className="w-full rounded-lg border border-input bg-background px-3 py-2 font-mono text-sm"
            placeholder="owner = @request.auth.id"
          />
          {value === '' ? (
            <p className="text-xs text-amber-600 dark:text-amber-400">Empty rule — anyone can perform this operation.</p>
          ) : null}
        </>
      )}
      {testing ? <RuleSimulator rule={value} /> : null}
    </div>
  )
}

function RuleSimulator({ rule }: { rule: string | null }) {
  const [role, setRole] = React.useState('')
  const [isAdmin, setIsAdmin] = React.useState(false)
  const [verified, setVerified] = React.useState(false)
  const [recordJson, setRecordJson] = React.useState('')
  const [running, setRunning] = React.useState(false)
  const [result, setResult] = React.useState<{
    valid?: boolean
    allowed?: boolean
    resolvedFilter?: string
    locked?: boolean
    error?: string
    note?: string
  } | null>(null)
  const [localError, setLocalError] = React.useState<string | null>(null)

  const run = async () => {
    setLocalError(null)
    setResult(null)
    let record: Record<string, any> | undefined
    if (recordJson.trim()) {
      try {
        record = JSON.parse(recordJson)
      } catch {
        setLocalError('Record JSON is not valid JSON.')
        return
      }
    }
    setRunning(true)
    try {
      const res = await api.ruleSimulate({
        rule,
        auth: { isAdmin, role, verified, id: 'test-user', isRecordAuth: !isAdmin },
        record,
      })
      setResult(res)
    } catch (err: any) {
      setLocalError(err.message || 'Simulation failed')
    } finally {
      setRunning(false)
    }
  }

  const parseFailed = result && result.valid === false

  return (
    <div className="space-y-3 rounded-lg border border-border bg-muted/30 p-3">
      <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto_auto]">
        <Input value={role} onChange={(e) => setRole(e.target.value)} placeholder="role (e.g. editor)" className="h-8 text-sm" />
        <label className="flex h-8 items-center gap-2 rounded-lg border border-border px-3 text-xs">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
          Is admin
        </label>
        <label className="flex h-8 items-center gap-2 rounded-lg border border-border px-3 text-xs">
          <input type="checkbox" checked={verified} onChange={(e) => setVerified(e.target.checked)} />
          Verified
        </label>
      </div>
      <textarea
        value={recordJson}
        onChange={(e) => setRecordJson(e.target.value)}
        rows={2}
        className="w-full rounded-lg border border-input bg-background px-3 py-2 font-mono text-xs"
        placeholder='Optional record JSON, e.g. {"owner":"test-user"}'
      />
      <div className="flex items-center gap-2">
        <Button size="sm" variant="outline" onClick={run} disabled={running}>
          {running ? <Loader2 className="mr-2 h-3 w-3 animate-spin" /> : <FlaskConical className="mr-2 h-3 w-3" />}
          Run
        </Button>
        {result && !parseFailed ? (
          result.allowed ? (
            <Badge variant="success">✓ Allowed</Badge>
          ) : (
            <Badge variant="destructive">✗ Denied</Badge>
          )
        ) : null}
        {result?.locked ? <Badge variant="secondary">locked</Badge> : null}
      </div>
      {localError ? <p className="text-xs text-red-500">{localError}</p> : null}
      {parseFailed ? (
        <p className="text-xs text-red-500">Invalid rule: {result?.error || 'parse error'}</p>
      ) : null}
      {result && !parseFailed && result.note ? (
        <p className="text-xs text-muted-foreground">{result.note}</p>
      ) : null}
      {result && !parseFailed && result.resolvedFilter ? (
        <pre className="overflow-x-auto rounded-lg border border-border bg-background px-3 py-2 font-mono text-xs">
          {result.resolvedFilter}
        </pre>
      ) : null}
    </div>
  )
}

function FieldOption({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <div className={className ? className : ''}>
      <div className="space-y-2">
        <label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</label>
        {children}
      </div>
    </div>
  )
}

function cloneCollection(collection: Collection): Collection {
  return JSON.parse(JSON.stringify(collection))
}

function defaultOptionsForType(type: FieldType): Record<string, any> {
  switch (type) {
    case 'select':
      return { values: [] }
    case 'relation':
      return { max_select: 1, display_fields: [] }
    case 'file':
      return { max_select: 1, mime_types: [], thumbs: [] }
    case 'vector':
      return { distance: 'cosine', index: 'hnsw' }
    default:
      return {}
  }
}

function normalizeCollectionDraft(collection: Collection): Collection {
  const clone = cloneCollection(collection)
  clone.schema = clone.schema.map((field) => ({
    ...field,
    name: field.name.trim(),
    options: normalizeOptions(field),
  }))
  clone.indexes = (clone.indexes || []).map((item) => item.trim()).filter(Boolean)
  // Rules are tri-state: null = locked (superusers only), '' = public, expression = filtered.
  clone.list_rule = normalizeRule(clone.list_rule)
  clone.view_rule = normalizeRule(clone.view_rule)
  clone.create_rule = normalizeRule(clone.create_rule)
  clone.update_rule = normalizeRule(clone.update_rule)
  clone.delete_rule = normalizeRule(clone.delete_rule)
  clone.view_query = clone.view_query || ''
  return clone
}

function normalizeRule(rule: string | null | undefined): string | null {
  if (rule === null || rule === undefined) return null
  return rule.trim() === '' ? '' : rule.trim()
}

function normalizeOptions(field: SchemaField) {
  const options = { ...(field.options || {}) }

  if (Array.isArray(options.values)) options.values = options.values.map(String).map((item: string) => item.trim()).filter(Boolean)
  if (Array.isArray(options.display_fields)) options.display_fields = options.display_fields.map(String).map((item: string) => item.trim()).filter(Boolean)
  if (Array.isArray(options.mime_types)) options.mime_types = options.mime_types.map(String).map((item: string) => item.trim()).filter(Boolean)
  if (Array.isArray(options.thumbs)) options.thumbs = options.thumbs.map(String).map((item: string) => item.trim()).filter(Boolean)

  for (const key of ['min', 'max', 'max_select', 'max_size']) {
    if (options[key] === '' || options[key] == null || Number.isNaN(options[key])) delete options[key]
  }
  if (!options.pattern) delete options.pattern
  if (!options.collection_id) delete options.collection_id
  if (!options.collection_name) delete options.collection_name

  if (options.default === '' || options.default == null) {
    delete options.default
  } else {
    switch (field.type) {
      case 'number':
        options.default = Number(options.default)
        if (Number.isNaN(options.default)) delete options.default
        break
      case 'bool':
        if (typeof options.default === 'string') {
          options.default = options.default === 'true'
        } else {
          options.default = Boolean(options.default)
        }
        break
      default:
        options.default = String(options.default)
        break
    }
  }

  return options
}

function numericOrNil(value: string) {
  if (value.trim() === '') return undefined
  const parsed = Number(value)
  return Number.isNaN(parsed) ? undefined : parsed
}

function splitComma(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function stringOrEmpty(value: any) {
  return value == null ? '' : String(value)
}

function showsDefault(type: FieldType) {
  return !['file', 'relation', 'autodate', 'vector'].includes(type)
}
