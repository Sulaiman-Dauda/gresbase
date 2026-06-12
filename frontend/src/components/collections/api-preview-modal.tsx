'use client'

import * as React from 'react'
import { Code2, Copy, ExternalLink, Filter, Globe, Lock, X } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import type { Collection, SchemaField } from '@/lib/types'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface OperationParam {
  name: string
  type: string
  description: string
}

interface OperationSnippet {
  key: string
  label: string
  code: string
}

interface OperationExtra {
  title: string
  method: string
  path: string
  description: string
  code?: string
}

interface StatusChip {
  tone: 'success' | 'warning'
  label: string
}

interface ApiOperation {
  key: string
  title: string
  method: string
  path: string
  description: string
  /** Which collection access rule governs this operation (tri-state). */
  rule?: { label: string; value: string | null }
  ruleNote?: string
  status?: StatusChip
  snippets: OperationSnippet[]
  paramsTitle?: string
  params?: OperationParam[]
  requestBody?: string
  responseBody?: string
  extras?: OperationExtra[]
}

interface OperationGroup {
  label: string
  operations: ApiOperation[]
}

// ---------------------------------------------------------------------------
// Method + rule presentation
// ---------------------------------------------------------------------------

const METHOD_BADGE: Record<string, string> = {
  GET: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
  POST: 'border-sky-500/40 bg-sky-500/10 text-sky-600 dark:text-sky-400',
  PUT: 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  PATCH: 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  DELETE: 'border-red-500/40 bg-red-500/10 text-red-600 dark:text-red-400',
  SSE: 'border-purple-500/40 bg-purple-500/10 text-purple-600 dark:text-purple-400',
}

const METHOD_TEXT: Record<string, string> = {
  GET: 'text-emerald-600 dark:text-emerald-400',
  POST: 'text-sky-600 dark:text-sky-400',
  PUT: 'text-amber-600 dark:text-amber-400',
  PATCH: 'text-amber-600 dark:text-amber-400',
  DELETE: 'text-red-600 dark:text-red-400',
  SSE: 'text-purple-600 dark:text-purple-400',
}

function MethodBadge({ method, className }: { method: string; className?: string }) {
  return (
    <Badge variant="outline" className={cn('shrink-0 rounded-md px-1.5 font-mono text-[10px] tracking-wide', METHOD_BADGE[method] || '', className)}>
      {method}
    </Badge>
  )
}

function ruleState(value: string | null | undefined): 'locked' | 'public' | 'filtered' {
  if (value === null || value === undefined) return 'locked'
  if (value.trim() === '') return 'public'
  return 'filtered'
}

function RuleBadge({ rule }: { rule: { label: string; value: string | null } }) {
  const state = ruleState(rule.value)
  if (state === 'locked') {
    return (
      <Badge variant="outline" className="gap-1 border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400">
        <Lock className="h-3 w-3" />
        {rule.label}: Locked (superusers only)
      </Badge>
    )
  }
  if (state === 'public') {
    return (
      <Badge variant="outline" className="gap-1 border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
        <Globe className="h-3 w-3" />
        {rule.label}: Public
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="gap-1 border-sky-500/40 bg-sky-500/10 text-sky-600 dark:text-sky-400">
      <Filter className="h-3 w-3" />
      {rule.label}: Filtered
    </Badge>
  )
}

const RULE_DOT: Record<string, string> = {
  locked: 'bg-amber-500',
  public: 'bg-emerald-500',
  filtered: 'bg-sky-500',
}

// ---------------------------------------------------------------------------
// Lightweight syntax tinting (no external highlighter dependencies)
// ---------------------------------------------------------------------------

const CODE_KEYWORDS = new Set([
  'const', 'let', 'var', 'await', 'async', 'new', 'import', 'from', 'function',
  'return', 'if', 'else', 'true', 'false', 'null', 'undefined', 'curl', 'export',
])

function tintTokens(line: string): React.ReactNode {
  const segments = line.split(/('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*")/g)
  return segments.map((segment, index) => {
    if (!segment) return null
    if (segment.startsWith("'") || segment.startsWith('"')) {
      return (
        <span key={index} className="text-emerald-600 dark:text-emerald-300">
          {segment}
        </span>
      )
    }
    const words = segment.split(
      /\b(const|let|var|await|async|new|import|from|function|return|if|else|true|false|null|undefined|curl|export)\b/g
    )
    return (
      <React.Fragment key={index}>
        {words.map((word, wordIndex) =>
          CODE_KEYWORDS.has(word) ? (
            <span key={wordIndex} className="text-purple-600 dark:text-purple-400">
              {word}
            </span>
          ) : (
            <span key={wordIndex}>{word}</span>
          )
        )}
      </React.Fragment>
    )
  })
}

function tintLine(line: string): React.ReactNode {
  const trimmed = line.trimStart()
  if (trimmed.startsWith('//') || trimmed.startsWith('#')) {
    return <span className="italic text-muted-foreground/80">{line}</span>
  }
  const endpoint = line.match(/^(GET|POST|PUT|PATCH|DELETE|SSE)(\s+)(.*)$/)
  if (endpoint) {
    return (
      <>
        <span className={cn('font-semibold', METHOD_TEXT[endpoint[1]])}>{endpoint[1]}</span>
        <span>{endpoint[2]}</span>
        <span className="text-sky-600 dark:text-sky-300">{endpoint[3]}</span>
      </>
    )
  }
  return tintTokens(line)
}

function copyText(text: string) {
  navigator.clipboard
    .writeText(text)
    .then(() => toast.success('Copied to clipboard'))
    .catch(() => toast.error('Copy failed'))
}

function CodeBlock({ code, label, className }: { code: string; label?: string; className?: string }) {
  return (
    <div className={cn('overflow-hidden rounded-xl border border-border bg-accent/10', className)}>
      {label ? (
        <div className="flex items-center justify-between border-b border-border px-4 py-2">
          <span className="text-xs font-medium text-foreground">{label}</span>
          <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => copyText(code)} aria-label={`Copy ${label}`}>
            <Copy className="mr-1.5 h-3 w-3" />
            Copy
          </Button>
        </div>
      ) : null}
      <div className="relative">
        {!label ? (
          <Button
            variant="ghost"
            size="sm"
            className="absolute right-2 top-2 z-10 h-7 px-2 text-xs"
            onClick={() => copyText(code)}
            aria-label="Copy code"
          >
            <Copy className="mr-1.5 h-3 w-3" />
            Copy
          </Button>
        ) : null}
        <pre className="overflow-x-auto p-4 font-mono text-xs leading-relaxed text-foreground/90">
          <code>
            {code.split('\n').map((line, index) => (
              <div key={index} className="min-h-[1em] whitespace-pre">
                {tintLine(line)}
              </div>
            ))}
          </code>
        </pre>
      </div>
    </div>
  )
}

function SnippetTabs({ snippets }: { snippets: OperationSnippet[] }) {
  if (!snippets.length) return null
  return (
    <Tabs key={snippets.map((snippet) => snippet.key).join('-')} defaultValue={snippets[0].key}>
      <TabsList aria-label="Code snippet language">
        {snippets.map((snippet) => (
          <TabsTrigger key={snippet.key} value={snippet.key}>
            {snippet.label}
          </TabsTrigger>
        ))}
      </TabsList>
      {snippets.map((snippet) => (
        <TabsContent key={snippet.key} value={snippet.key}>
          <CodeBlock code={snippet.code} />
        </TabsContent>
      ))}
    </Tabs>
  )
}

function ParamsTable({ title, params }: { title: string; params: OperationParam[] }) {
  return (
    <div className="overflow-hidden rounded-xl border border-border">
      <div className="border-b border-border bg-accent/10 px-4 py-2.5 text-sm font-medium text-foreground">{title}</div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[480px] text-left text-xs">
          <thead className="bg-accent/10 text-[10px] uppercase tracking-wider text-muted-foreground">
            <tr>
              <th className="px-4 py-2 font-medium">Param</th>
              <th className="w-24 px-4 py-2 font-medium">Type</th>
              <th className="px-4 py-2 font-medium">Description</th>
            </tr>
          </thead>
          <tbody>
            {params.map((param) => (
              <tr key={param.name} className="border-t border-border align-top">
                <td className="px-4 py-2.5 font-mono text-foreground">{param.name}</td>
                <td className="px-4 py-2.5 text-muted-foreground">{param.type}</td>
                <td className="px-4 py-2.5 text-muted-foreground">{param.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Panel
// ---------------------------------------------------------------------------

interface ApiPreviewPanelProps {
  collection: Collection
  collections?: Collection[]
  className?: string
  onClose?: () => void
}

export function ApiPreviewPanel({ collection, collections = [], className, onClose }: ApiPreviewPanelProps) {
  const [origin, setOrigin] = React.useState('http://localhost:8080')
  React.useEffect(() => {
    setOrigin(window.location.origin)
  }, [])

  const groups = React.useMemo(() => buildOperationGroups(collection, collections, origin), [collection, collections, origin])
  const flat = React.useMemo(() => groups.flatMap((group) => group.operations), [groups])

  const [activeKey, setActiveKey] = React.useState(flat[0]?.key || '')
  React.useEffect(() => {
    setActiveKey('list')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collection.id])

  const active = flat.find((operation) => operation.key === activeKey) || flat[0]

  const handleSidebarKeyDown = (event: React.KeyboardEvent<HTMLElement>) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button[data-operation]'))
    if (!buttons.length) return
    const currentIndex = buttons.findIndex((button) => button === document.activeElement)
    const delta = event.key === 'ArrowDown' ? 1 : -1
    const nextIndex = (currentIndex + delta + buttons.length) % buttons.length
    event.preventDefault()
    buttons[nextIndex]?.focus()
    buttons[nextIndex]?.click()
  }

  if (!active) return null

  return (
    <div className={cn('flex overflow-hidden rounded-xl border border-border bg-background', className)}>
      <aside className="flex w-full max-w-[250px] shrink-0 flex-col border-r border-border bg-accent/10">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3 text-sm font-medium text-foreground">
          <Code2 className="h-4 w-4 shrink-0" />
          <span className="truncate">API · {collection.name}</span>
        </div>
        <nav className="min-h-0 flex-1 overflow-y-auto p-2" aria-label="API operations" onKeyDown={handleSidebarKeyDown}>
          {groups.map((group) => (
            <div key={group.label}>
              <div className="px-2 pb-1 pt-3 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                {group.label}
              </div>
              {group.operations.map((operation) => {
                const isActive = operation.key === active.key
                return (
                  <button
                    key={operation.key}
                    type="button"
                    data-operation={operation.key}
                    onClick={() => setActiveKey(operation.key)}
                    aria-current={isActive ? 'true' : undefined}
                    className={cn(
                      'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                      isActive
                        ? 'bg-background text-foreground shadow-sm'
                        : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                    )}
                  >
                    <MethodBadge method={operation.method} className="w-14 justify-center" />
                    <span className="min-w-0 flex-1 truncate">{operation.title}</span>
                    {operation.rule ? (
                      <span
                        className={cn('h-1.5 w-1.5 shrink-0 rounded-full', RULE_DOT[ruleState(operation.rule.value)])}
                        title={`${operation.rule.label}: ${ruleStateLabel(operation.rule.value)}`}
                      />
                    ) : null}
                  </button>
                )
              })}
            </div>
          ))}
        </nav>
        <div className="border-t border-border px-4 py-3">
          <a
            href="/api/v1/openapi.json"
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            <ExternalLink className="h-3 w-3" />
            Full OpenAPI spec
          </a>
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-start justify-between gap-3 border-b border-border px-6 py-4">
          <div className="min-w-0 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold">{active.title}</h2>
              {active.rule ? <RuleBadge rule={active.rule} /> : null}
              {active.status ? (
                <Badge
                  variant="outline"
                  className={cn(
                    'gap-1',
                    active.status.tone === 'success'
                      ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                      : 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
                  )}
                >
                  {active.status.label}
                </Badge>
              ) : null}
            </div>
            <p className="text-sm text-muted-foreground">{active.description}</p>
          </div>
          {onClose ? (
            <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close API preview">
              <X className="h-4 w-4" />
            </Button>
          ) : null}
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
          <div className="space-y-5">
            <div className="flex items-center gap-2 rounded-lg border border-border bg-accent/10 px-3 py-2">
              <MethodBadge method={active.method} />
              <code className="min-w-0 flex-1 truncate font-mono text-xs text-foreground" title={active.path}>
                {active.path}
              </code>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 shrink-0 px-2 text-xs"
                onClick={() => copyText(`${origin}${active.path}`)}
                aria-label="Copy endpoint URL"
              >
                <Copy className="mr-1.5 h-3 w-3" />
                Copy URL
              </Button>
            </div>

            {active.rule && ruleState(active.rule.value) === 'filtered' ? (
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>Active rule:</span>
                <code className="rounded-md border border-border bg-accent/20 px-2 py-1 font-mono text-xs text-foreground">
                  {active.rule.value}
                </code>
              </div>
            ) : null}
            {active.ruleNote ? <p className="text-xs text-muted-foreground">{active.ruleNote}</p> : null}

            <SnippetTabs snippets={active.snippets} />

            {active.params?.length ? <ParamsTable title={active.paramsTitle || 'Query parameters'} params={active.params} /> : null}

            {active.requestBody ? <CodeBlock label="Request body" code={active.requestBody} /> : null}
            {active.responseBody ? <CodeBlock label="Sample response" code={active.responseBody} /> : null}

            {active.extras?.map((extra) => (
              <div key={extra.title} className="space-y-3 rounded-xl border border-border p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <MethodBadge method={extra.method} />
                  <code className="font-mono text-xs text-foreground">{extra.path}</code>
                </div>
                <p className="text-sm text-muted-foreground">{extra.description}</p>
                {extra.code ? <CodeBlock code={extra.code} /> : null}
              </div>
            ))}
          </div>
        </div>
      </main>
    </div>
  )
}

function ruleStateLabel(value: string | null): string {
  const state = ruleState(value)
  if (state === 'locked') return 'Locked (superusers only)'
  if (state === 'public') return 'Public'
  return 'Filtered'
}

// ---------------------------------------------------------------------------
// Modal wrapper
// ---------------------------------------------------------------------------

interface ApiPreviewModalProps {
  open: boolean
  collection: Collection
  collections?: Collection[]
  onClose: () => void
}

export function ApiPreviewModal({ open, collection, collections = [], onClose }: ApiPreviewModalProps) {
  React.useEffect(() => {
    if (!open) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose, open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div className="absolute inset-0" onClick={onClose} />
      <div className="relative z-10 w-full max-w-6xl">
        <ApiPreviewPanel collection={collection} collections={collections} onClose={onClose} className="h-[88vh] shadow-2xl" />
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Operation builders
// ---------------------------------------------------------------------------

interface SnippetContext {
  name: string
  origin: string
  schema: SchemaField[]
  filterExample: string
  expandExample: string | null
  backRelationExample: string | null
  numberField: string | null
  groupField: string | null
  groupValue: string
  payload: Record<string, any>
}

function buildSnippetContext(collection: Collection, collections: Collection[], origin: string): SnippetContext {
  const schema = collection.schema || []
  const fields = schema.filter((field) => !field.system)

  const relationField = fields.find((field) => field.type === 'relation')
  const numberField = fields.find((field) => field.type === 'number')?.name || null
  const selectField = fields.find((field) => field.type === 'select' && Array.isArray(field.options?.values) && field.options.values.length)
  const boolField = fields.find((field) => field.type === 'bool')
  const textField = fields.find((field) => ['text', 'email', 'url'].includes(field.type))
  const groupField = selectField?.name || boolField?.name || textField?.name || null
  const groupValue = selectField ? String(selectField.options?.values?.[0] ?? 'option') : boolField ? 'true' : 'example'

  let filterExample = 'created_at >= "2026-01-01"'
  if (selectField) filterExample = `${selectField.name} = "${String(selectField.options?.values?.[0] ?? 'option')}"`
  else if (boolField) filterExample = `${boolField.name} = true`
  else if (numberField) filterExample = `${numberField} >= 10`
  else if (textField) filterExample = `${textField.name} ~ "demo"`

  // Back-relation expand: another collection pointing at this one via a relation field.
  let backRelationExample: string | null = null
  for (const other of collections) {
    if (other.id === collection.id) continue
    const pointer = (other.schema || []).find(
      (field) => field.type === 'relation' && String(field.options?.collection_id || '') === collection.id
    )
    if (pointer) {
      backRelationExample = `${other.name}_via_${pointer.name}`
      break
    }
  }

  return {
    name: collection.name,
    origin,
    schema,
    filterExample,
    expandExample: relationField?.name || null,
    backRelationExample,
    numberField,
    groupField,
    groupValue,
    payload: examplePayload(schema),
  }
}

function examplePayload(schema: SchemaField[]): Record<string, any> {
  const payload: Record<string, any> = {}
  const fields = schema.filter((field) => !field.system && field.type !== 'autodate').slice(0, 3)
  for (const field of fields) {
    payload[field.name] = sampleValueForField(field)
  }
  return payload
}

function sampleValueForField(field: SchemaField): any {
  switch (field.type) {
    case 'number':
      return typeof field.options?.min === 'number' ? field.options.min : 42
    case 'bool':
      return true
    case 'select': {
      const option = Array.isArray(field.options?.values) ? field.options.values[0] : 'option'
      return option || 'option'
    }
    case 'json':
      return { key: 'value' }
    case 'geo_point':
      return { lat: 40.7128, lng: -74.006 }
    case 'relation': {
      const maxSelect = Number(field.options?.max_select || 1)
      return maxSelect > 1 ? ['RELATED_RECORD_ID'] : 'RELATED_RECORD_ID'
    }
    case 'file': {
      const maxSelect = Number(field.options?.max_select || 1)
      return maxSelect > 1 ? ['photo.jpg'] : 'photo.jpg'
    }
    case 'password':
      return 'secret1234'
    case 'email':
      return 'user@example.com'
    case 'url':
      return 'https://example.com'
    case 'date':
      return '2026-01-01T00:00:00Z'
    case 'vector':
      return [0.12, -0.45, 0.9]
    default:
      return `${field.name} value`
  }
}

function indentJSON(value: any, indent: string): string {
  return JSON.stringify(value, null, 2).replace(/\n/g, `\n${indent}`)
}

function sdkPrelude(origin: string): string {
  return `import GresbaseClient from 'gresbase-sdk'\n\nconst client = new GresbaseClient({ url: '${origin}' })`
}

const EXPAND_DOC = (ctx: SnippetContext) =>
  `Comma-separated relations to expand inline${ctx.expandExample ? `, e.g. expand=${ctx.expandExample}` : ''}. Back-relations use otherCollection_via_relationField${ctx.backRelationExample ? `, e.g. expand=${ctx.backRelationExample}` : ' (e.g. comments_via_post)'}.`

const FILTER_DOC = (ctx: SnippetContext) =>
  `Filter expression, e.g. ${ctx.filterExample}. Operators: = != > >= < <= ~ !~ combined with && and ||. Use @request.auth.* to reference the caller.`

function listOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  const responseRecord = { id: 'RECORD_ID', ...ctx.payload, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }
  return {
    key: 'list',
    title: 'List records',
    method: 'GET',
    path: `/api/v1/records/${ctx.name}?page=1&perPage=30`,
    description: `Fetch a paginated list of ${ctx.name} records. Supports filtering, sorting, relation expansion and field selection.`,
    rule: { label: 'List rule', value: collection.list_rule },
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `// List ${ctx.name} — paginated
const params = new URLSearchParams({
  page: '1',
  perPage: '30',
  sort: '-created_at',
  filter: '${ctx.filterExample}',
})

const res = await fetch('${ctx.origin}/api/v1/records/${ctx.name}?' + params, {
  // Authorization is optional when the list rule is public
  headers: { Authorization: 'Bearer YOUR_TOKEN' },
})
const { items, page, perPage, totalItems, totalPages } = await res.json()`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

const result = await client.collection('${ctx.name}').getList(1, 30, {
  filter: '${ctx.filterExample}',
  sort: '-created_at',${ctx.expandExample ? `\n  expand: '${ctx.expandExample}',` : ''}
})

console.log(result.items, result.totalItems)`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `curl '${ctx.origin}/api/v1/records/${ctx.name}?page=1&perPage=30&sort=-created_at' \\
  -H 'Authorization: Bearer YOUR_TOKEN'`,
      },
    ],
    params: [
      { name: 'page', type: 'number', description: 'Page number, 1-indexed. Defaults to 1.' },
      { name: 'perPage', type: 'number', description: 'Records per page. Defaults to 30.' },
      { name: 'sort', type: 'string', description: 'Comma-separated sort fields. Prefix with - for descending, e.g. sort=-created_at,title.' },
      { name: 'filter', type: 'string', description: FILTER_DOC(ctx) },
      { name: 'expand', type: 'string', description: EXPAND_DOC(ctx) },
      { name: 'fields', type: 'string', description: 'Comma-separated list of fields to return, e.g. fields=id,title.' },
    ],
    responseBody: JSON.stringify({ items: [responseRecord], page: 1, perPage: 30, totalItems: 1, totalPages: 1 }, null, 2),
  }
}

function viewOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  const responseRecord = { id: 'RECORD_ID', ...ctx.payload, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }
  const expandSuffix = ctx.expandExample ? `?expand=${ctx.expandExample}` : ''
  return {
    key: 'view',
    title: 'View record',
    method: 'GET',
    path: `/api/v1/records/${ctx.name}/{recordId}`,
    description: `Fetch a single ${ctx.name} record by its id.`,
    rule: { label: 'View rule', value: collection.view_rule },
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `const res = await fetch('${ctx.origin}/api/v1/records/${ctx.name}/RECORD_ID${expandSuffix}', {
  headers: { Authorization: 'Bearer YOUR_TOKEN' },
})
const record = await res.json()`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

const record = await client.collection('${ctx.name}').getOne('RECORD_ID'${ctx.expandExample ? `, { expand: '${ctx.expandExample}' }` : ''})`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `curl '${ctx.origin}/api/v1/records/${ctx.name}/RECORD_ID${expandSuffix}' \\
  -H 'Authorization: Bearer YOUR_TOKEN'`,
      },
    ],
    params: [
      { name: 'expand', type: 'string', description: EXPAND_DOC(ctx) },
      { name: 'fields', type: 'string', description: 'Comma-separated list of fields to return.' },
    ],
    responseBody: JSON.stringify(responseRecord, null, 2),
  }
}

function createOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  const body = JSON.stringify(ctx.payload, null, 2)
  return {
    key: 'create',
    title: 'Create record',
    method: 'POST',
    path: `/api/v1/records/${ctx.name}`,
    description: `Create a new ${ctx.name} record. The example body uses this collection's actual schema fields.`,
    rule: { label: 'Create rule', value: collection.create_rule },
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `const res = await fetch('${ctx.origin}/api/v1/records/${ctx.name}', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    Authorization: 'Bearer YOUR_TOKEN',
  },
  body: JSON.stringify(${indentJSON(ctx.payload, '  ')}),
})
const record = await res.json()`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

const record = await client.collection('${ctx.name}').create(${JSON.stringify(ctx.payload, null, 2)})`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `curl -X POST '${ctx.origin}/api/v1/records/${ctx.name}' \\
  -H 'Content-Type: application/json' \\
  -H 'Authorization: Bearer YOUR_TOKEN' \\
  -d '${JSON.stringify(ctx.payload)}'`,
      },
    ],
    requestBody: body,
    responseBody: JSON.stringify({ id: 'NEW_RECORD_ID', ...ctx.payload, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }, null, 2),
  }
}

function updateOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  const partial = Object.fromEntries(Object.entries(ctx.payload).slice(0, 2))
  return {
    key: 'update',
    title: 'Update record',
    method: 'PATCH',
    path: `/api/v1/records/${ctx.name}/{recordId}`,
    description: `Update an existing ${ctx.name} record. Send only the fields you want to change (PUT is also accepted).`,
    rule: { label: 'Update rule', value: collection.update_rule },
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `const res = await fetch('${ctx.origin}/api/v1/records/${ctx.name}/RECORD_ID', {
  method: 'PATCH',
  headers: {
    'Content-Type': 'application/json',
    Authorization: 'Bearer YOUR_TOKEN',
  },
  body: JSON.stringify(${indentJSON(partial, '  ')}),
})
const record = await res.json()`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

const record = await client.collection('${ctx.name}').update('RECORD_ID', ${JSON.stringify(partial, null, 2)})`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `curl -X PATCH '${ctx.origin}/api/v1/records/${ctx.name}/RECORD_ID' \\
  -H 'Content-Type: application/json' \\
  -H 'Authorization: Bearer YOUR_TOKEN' \\
  -d '${JSON.stringify(partial)}'`,
      },
    ],
    requestBody: JSON.stringify(partial, null, 2),
    responseBody: JSON.stringify({ id: 'RECORD_ID', ...ctx.payload, ...partial, updated_at: '2026-01-01T00:00:00Z' }, null, 2),
  }
}

function deleteOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  return {
    key: 'delete',
    title: 'Delete record',
    method: 'DELETE',
    path: `/api/v1/records/${ctx.name}/{recordId}`,
    description: `Permanently delete a ${ctx.name} record. Relations with cascade delete enabled remove dependent records too.`,
    rule: { label: 'Delete rule', value: collection.delete_rule },
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `await fetch('${ctx.origin}/api/v1/records/${ctx.name}/RECORD_ID', {
  method: 'DELETE',
  headers: { Authorization: 'Bearer YOUR_TOKEN' },
})`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

await client.collection('${ctx.name}').delete('RECORD_ID')`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `curl -X DELETE '${ctx.origin}/api/v1/records/${ctx.name}/RECORD_ID' \\
  -H 'Authorization: Bearer YOUR_TOKEN'`,
      },
    ],
    responseBody: JSON.stringify({ deleted: 'RECORD_ID' }, null, 2),
  }
}

function realtimeOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  return {
    key: 'realtime',
    title: 'Realtime',
    method: 'SSE',
    path: '/api/v1/sse',
    description: `Subscribe to live create/update/delete events for ${ctx.name} over Server-Sent Events (WebSocket fallback at /api/v1/realtime).`,
    rule: { label: 'List rule', value: collection.list_rule },
    ruleNote: 'Record events are only delivered to subscribers the list rule allows to see the record.',
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `// Connect over SSE, then register subscriptions
const source = new EventSource('${ctx.origin}/api/v1/sse')

source.onmessage = (event) => {
  const msg = JSON.parse(event.data)

  if (msg.event === 'connection:established') {
    // Register interest in ${ctx.name} record events
    fetch('${ctx.origin}/api/v1/realtime', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        type: 'subscribe',
        clientId: msg.client_id,
        subscriptions: ['${ctx.name}/*'],
      }),
    })
  }

  if (msg.event && msg.event.startsWith('record:')) {
    // record:create | record:update | record:delete
    console.log(msg.event, msg.data)
  }
}`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

client.realtime.connect()

const unsubscribe = client.realtime.subscribe('${ctx.name}', ({ action, record }) => {
  console.log(action, record) // create | update | delete
}, { filter: '${ctx.filterExample}' })

// later
unsubscribe()`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `# Stream events (SSE)
curl -N '${ctx.origin}/api/v1/sse'`,
      },
    ],
    paramsTitle: 'Subscription options',
    params: [
      { name: 'filter', type: 'string', description: `Only receive events for matching records, e.g. ${ctx.filterExample}.` },
      { name: 'fields', type: 'string', description: 'Limit the fields included in event payloads.' },
      { name: 'expand', type: 'string', description: EXPAND_DOC(ctx) },
    ],
    extras: [
      {
        title: 'Broadcast',
        method: 'POST',
        path: '/api/v1/realtime/broadcast',
        description: 'Push a custom event to every subscriber of a channel. Requires an Authorization header.',
        code: `curl -X POST '${ctx.origin}/api/v1/realtime/broadcast' \\
  -H 'Authorization: Bearer YOUR_TOKEN' \\
  -H 'Content-Type: application/json' \\
  -d '{"channel":"room1","event":"my-event","data":{"hello":"world"}}'`,
      },
    ],
  }
}

function aggregateOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  const aggregate = ctx.numberField ? `count,sum:${ctx.numberField},avg:${ctx.numberField}` : 'count'
  const groupSuffix = ctx.groupField ? `&groupBy=${ctx.groupField}` : ''
  const sampleItem: Record<string, any> = {}
  if (ctx.groupField) sampleItem[ctx.groupField] = ctx.groupValue
  sampleItem.count = 10
  if (ctx.numberField) {
    sampleItem[`sum_${ctx.numberField}`] = 125.5
    sampleItem[`avg_${ctx.numberField}`] = 12.55
  }

  return {
    key: 'aggregate',
    title: 'Aggregate',
    method: 'GET',
    path: `/api/v1/records/${ctx.name}/aggregate?aggregate=${aggregate}${groupSuffix}`,
    description: `Run server-side aggregations (count, sum, avg, min, max) over ${ctx.name}, optionally grouped and filtered.`,
    rule: { label: 'List rule', value: collection.list_rule },
    ruleNote: "Aggregate queries are enforced by the collection's list rule.",
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `const params = new URLSearchParams({
  aggregate: '${aggregate}',${ctx.groupField ? `\n  groupBy: '${ctx.groupField}',` : ''}
  filter: '${ctx.filterExample}',
  limit: '100',
})

const res = await fetch('${ctx.origin}/api/v1/records/${ctx.name}/aggregate?' + params, {
  headers: { Authorization: 'Bearer YOUR_TOKEN' },
})
const { items } = await res.json()`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

const { items } = await client.collection('${ctx.name}').aggregate({
  aggregate: '${aggregate}',${ctx.groupField ? `\n  groupBy: '${ctx.groupField}',` : ''}
  filter: '${ctx.filterExample}',
})`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `curl '${ctx.origin}/api/v1/records/${ctx.name}/aggregate?aggregate=${aggregate}${groupSuffix}' \\
  -H 'Authorization: Bearer YOUR_TOKEN'`,
      },
    ],
    params: [
      {
        name: 'aggregate',
        type: 'string',
        description: 'Required. Comma-separated aggregate functions: count, sum:field, avg:field, min:field, max:field.',
      },
      { name: 'groupBy', type: 'string', description: 'Optional comma-separated fields to group results by.' },
      { name: 'filter', type: 'string', description: 'Optional filter — same syntax as the list endpoint.' },
      { name: 'sort', type: 'string', description: 'Optional sort over the grouped output.' },
      { name: 'limit', type: 'number', description: 'Max grouped rows to return. Default 100, max 1000.' },
    ],
    responseBody: JSON.stringify({ items: [sampleItem] }, null, 2),
  }
}

function filesOperation(collection: Collection, ctx: SnippetContext): ApiOperation {
  const fileField = ctx.schema.find((field) => !field.system && field.type === 'file')
  return {
    key: 'files',
    title: 'File thumbs',
    method: 'GET',
    path: `/api/v1/files/${ctx.name}/{recordId}/{filename}?thumb=400x300f`,
    description: fileField
      ? `Download files and generate on-the-fly thumbnails for file fields (first file field: ${fileField.name}).`
      : `Download files and generate on-the-fly thumbnails. This collection has no file fields yet — add one in the Schema tab.`,
    ruleNote: 'Files in fields marked protected require an Authorization header or a short-lived token from POST /api/v1/files/token.',
    snippets: [
      {
        key: 'js',
        label: 'JavaScript',
        code: `// 400x300 fitted JPEG thumbnail
const src = '${ctx.origin}/api/v1/files/${ctx.name}/RECORD_ID/photo.jpg'
  + '?thumb=400x300f&format=jpeg&quality=80'

document.querySelector('img').src = src`,
      },
      {
        key: 'sdk',
        label: 'SDK',
        code: `${sdkPrelude(ctx.origin)}

const url = client.files.getURL('${ctx.name}', 'RECORD_ID', 'photo.jpg')
const thumb = url + '?thumb=400x300f&format=jpeg&quality=80'`,
      },
      {
        key: 'curl',
        label: 'curl',
        code: `curl -o thumb.jpg \\
  '${ctx.origin}/api/v1/files/${ctx.name}/RECORD_ID/photo.jpg?thumb=400x300f&format=jpeg&quality=80'`,
      },
    ],
    params: [
      {
        name: 'thumb',
        type: 'string',
        description: 'Thumbnail size and mode: WxH center crop (400x300), WxHt top crop (400x300t), WxHf fit without cropping (400x300f).',
      },
      { name: 'format', type: 'string', description: 'Output format: jpeg or png.' },
      { name: 'quality', type: 'number', description: 'JPEG quality from 1 to 100.' },
      { name: 'token', type: 'string', description: 'Short-lived file token for protected files (from POST /api/v1/files/token).' },
    ],
  }
}

function authOperations(collection: Collection, ctx: SnippetContext): ApiOperation[] {
  const base = `/api/v1/collections/${ctx.name}/auth`
  const authResponse = JSON.stringify(
    {
      token: 'RECORD_ACCESS_TOKEN',
      refreshToken: 'RECORD_REFRESH_TOKEN',
      record: { id: 'RECORD_ID', email: 'user@example.com' },
    },
    null,
    2
  )
  const anonymousEnabled = Boolean(collection.options?.allowAnonymous)

  return [
    {
      key: 'auth-password',
      title: 'Auth with password',
      method: 'POST',
      path: `${base}/auth-with-password`,
      description: `Authenticate a ${ctx.name} record with its identity (email or username) and password. Rate limited per IP.`,
      snippets: [
        {
          key: 'js',
          label: 'JavaScript',
          code: `const res = await fetch('${ctx.origin}${base}/auth-with-password', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    identity: 'user@example.com', // email or username
    password: 'YOUR_PASSWORD',
  }),
})
const { token, refreshToken, record } = await res.json()`,
        },
        {
          key: 'sdk',
          label: 'SDK',
          code: `${sdkPrelude(ctx.origin)}

// identity is the record email or username — tokens are stored on the client
const { token, refreshToken, record } = await client
  .collection('${ctx.name}')
  .authWithPassword('user@example.com', 'YOUR_PASSWORD')`,
        },
        {
          key: 'curl',
          label: 'curl',
          code: `curl -X POST '${ctx.origin}${base}/auth-with-password' \\
  -H 'Content-Type: application/json' \\
  -d '{"identity":"user@example.com","password":"YOUR_PASSWORD"}'`,
        },
      ],
      paramsTitle: 'Body parameters',
      params: [
        { name: 'identity', type: 'string', description: 'The record email or username.' },
        { name: 'password', type: 'string', description: 'The record password.' },
      ],
      responseBody: authResponse,
    },
    {
      key: 'auth-refresh',
      title: 'Auth refresh',
      method: 'POST',
      path: `${base}/auth-refresh`,
      description: 'Exchange a valid refresh token for a fresh access/refresh token pair.',
      snippets: [
        {
          key: 'js',
          label: 'JavaScript',
          code: `const res = await fetch('${ctx.origin}${base}/auth-refresh', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ refreshToken: 'YOUR_REFRESH_TOKEN' }),
})
const { token, refreshToken } = await res.json()`,
        },
        {
          key: 'sdk',
          label: 'SDK',
          code: `${sdkPrelude(ctx.origin)}

// Uses the refresh token stored by a previous auth call
const { token, refreshToken } = await client.collection('${ctx.name}').authRefresh()`,
        },
        {
          key: 'curl',
          label: 'curl',
          code: `curl -X POST '${ctx.origin}${base}/auth-refresh' \\
  -H 'Content-Type: application/json' \\
  -d '{"refreshToken":"YOUR_REFRESH_TOKEN"}'`,
        },
      ],
      paramsTitle: 'Body parameters',
      params: [{ name: 'refreshToken', type: 'string', description: 'The refresh token issued at authentication.' }],
      responseBody: JSON.stringify({ token: 'RECORD_ACCESS_TOKEN', refreshToken: 'RECORD_REFRESH_TOKEN' }, null, 2),
    },
    {
      key: 'auth-anonymous',
      title: 'Anonymous auth',
      method: 'POST',
      path: `${base}/auth-with-anonymous`,
      description: 'Create a guest session without credentials — no request body required. Returns the same shape as auth-with-password.',
      status: anonymousEnabled
        ? { tone: 'success', label: 'Enabled' }
        : { tone: 'warning', label: 'Disabled — enable "Allow anonymous sign-in" in the Schema tab' },
      snippets: [
        {
          key: 'js',
          label: 'JavaScript',
          code: `// No body required — works only when "Allow anonymous sign-in" is enabled
const res = await fetch('${ctx.origin}${base}/auth-with-anonymous', {
  method: 'POST',
})
const { token, refreshToken, record } = await res.json()`,
        },
        {
          key: 'sdk',
          label: 'SDK',
          code: `${sdkPrelude(ctx.origin)}

const { token, record } = await client.collection('${ctx.name}').authWithAnonymous()`,
        },
        {
          key: 'curl',
          label: 'curl',
          code: `curl -X POST '${ctx.origin}${base}/auth-with-anonymous'`,
        },
      ],
      responseBody: authResponse,
    },
    {
      key: 'auth-password-reset',
      title: 'Password reset',
      method: 'POST',
      path: `${base}/request-password-reset`,
      description: 'Start a password reset flow. Always responds with success to avoid leaking which emails exist.',
      snippets: [
        {
          key: 'js',
          label: 'JavaScript',
          code: `await fetch('${ctx.origin}${base}/request-password-reset', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email: 'user@example.com' }),
})`,
        },
        {
          key: 'sdk',
          label: 'SDK',
          code: `${sdkPrelude(ctx.origin)}

await client.collection('${ctx.name}').requestPasswordReset('user@example.com')

// Then, with the token from the emailed link:
await client.collection('${ctx.name}').confirmPasswordReset('EMAILED_TOKEN', 'NEW_PASSWORD')`,
        },
        {
          key: 'curl',
          label: 'curl',
          code: `curl -X POST '${ctx.origin}${base}/request-password-reset' \\
  -H 'Content-Type: application/json' \\
  -d '{"email":"user@example.com"}'`,
        },
      ],
      paramsTitle: 'Body parameters',
      params: [{ name: 'email', type: 'string', description: 'The account email to send the reset link to.' }],
      responseBody: JSON.stringify({ message: 'If the email exists, a reset email has been sent' }, null, 2),
    },
    {
      key: 'auth-verification',
      title: 'Email verification',
      method: 'POST',
      path: `${base}/request-verification`,
      description: 'Send an email verification message to a record. Always responds with success to avoid leaking which emails exist.',
      snippets: [
        {
          key: 'js',
          label: 'JavaScript',
          code: `await fetch('${ctx.origin}${base}/request-verification', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email: 'user@example.com' }),
})`,
        },
        {
          key: 'sdk',
          label: 'SDK',
          code: `${sdkPrelude(ctx.origin)}

await client.collection('${ctx.name}').requestVerification('user@example.com')

// Then, with the token from the emailed link:
await client.collection('${ctx.name}').confirmVerification('EMAILED_TOKEN')`,
        },
        {
          key: 'curl',
          label: 'curl',
          code: `curl -X POST '${ctx.origin}${base}/request-verification' \\
  -H 'Content-Type: application/json' \\
  -d '{"email":"user@example.com"}'`,
        },
      ],
      paramsTitle: 'Body parameters',
      params: [{ name: 'email', type: 'string', description: 'The account email to send the verification link to.' }],
      responseBody: JSON.stringify({ message: 'If the email exists, a verification has been sent' }, null, 2),
    },
    {
      key: 'auth-oauth2',
      title: 'OAuth2 redirect',
      method: 'GET',
      path: `${base}/oauth2/{provider}`,
      description: 'Redirect the user to the OAuth2 provider consent screen. The callback completes the flow and issues record tokens.',
      snippets: [
        {
          key: 'js',
          label: 'JavaScript',
          code: `// Send the user to the provider's consent screen
window.location.href = '${ctx.origin}${base}/oauth2/google'

// The provider redirects back to
// ${base}/oauth2/google/callback
// which completes the flow and issues record tokens`,
        },
        {
          key: 'curl',
          label: 'curl',
          code: `# Inspect the redirect target
curl -I '${ctx.origin}${base}/oauth2/google'`,
        },
      ],
      extras: [
        {
          title: 'Manual code exchange',
          method: 'POST',
          path: `${base}/auth-with-oauth2`,
          description: 'For custom flows: exchange the provider code + state yourself instead of using the redirect bridge.',
          code: JSON.stringify(
            {
              provider: 'google',
              code: 'AUTHORIZATION_CODE',
              state: 'STATE_VALUE',
              codeVerifier: 'CODE_VERIFIER',
              redirectURL: `${ctx.origin}/api/v1/oauth2-redirect`,
            },
            null,
            2
          ),
        },
      ],
      responseBody: authResponse,
    },
  ]
}

function buildOperationGroups(collection: Collection, collections: Collection[], origin: string): OperationGroup[] {
  const ctx = buildSnippetContext(collection, collections, origin)
  const groups: OperationGroup[] = []

  const records: ApiOperation[] = [listOperation(collection, ctx), viewOperation(collection, ctx)]
  if (collection.type !== 'view') {
    records.push(createOperation(collection, ctx), updateOperation(collection, ctx), deleteOperation(collection, ctx))
  }
  groups.push({ label: 'Records', operations: records })

  groups.push({ label: 'Realtime', operations: [realtimeOperation(collection, ctx)] })
  groups.push({ label: 'Analytics', operations: [aggregateOperation(collection, ctx)] })
  groups.push({ label: 'Files', operations: [filesOperation(collection, ctx)] })

  if (collection.type === 'auth') {
    groups.push({ label: 'Auth', operations: authOperations(collection, ctx) })
  }

  return groups
}
