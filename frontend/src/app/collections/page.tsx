'use client'

import { Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import {
  ArrowLeft,
  Code2,
  Database,
  Download,
  Eye,
  Filter,
  Loader2,
  Plus,
  RefreshCw,
  Search,
  Shield,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'
import { AppLayout } from '@/components/layout/app-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import type { Collection, PaginatedResponse, RecordData, SchemaField } from '@/lib/types'
import { ApiPreviewModal, ApiPreviewPanel } from '@/components/collections/api-preview-modal'
import { AuthTools } from '@/components/collections/auth-tools'
import { type RecordEditorMode } from '@/components/collections/field-kit'
import { RecordWorkbenchModal } from '@/components/collections/record-modal'
import { RecordsTable } from '@/components/collections/records-table'
import { SchemaEditor } from '@/components/collections/schema-editor'
import { CollectionsTransferTools } from '@/components/collections/transfer-tools'

const PER_PAGE = 25
const LAST_ACTIVE_COLLECTION_KEY = 'gresbase_last_active_collection'

type WorkbenchTab = 'data' | 'schema' | 'api' | 'auth'

export default function CollectionsPage() {
  return (
    <Suspense
      fallback={
        <div className="flex items-center justify-center min-h-[60vh]">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      }
    >
      <CollectionsPageContent />
    </Suspense>
  )
}

function CollectionsPageContent() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const selectedCollectionId = searchParams.get('collection')
  const homeMode = searchParams.get('home') === '1'

  const [collections, setCollections] = useState<Collection[]>([])
  const [collectionsLoading, setCollectionsLoading] = useState(true)
  const [recordsLoading, setRecordsLoading] = useState(false)
  const [recordsResult, setRecordsResult] = useState<PaginatedResponse<RecordData> | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<WorkbenchTab>('data')

  const [newCollectionName, setNewCollectionName] = useState('')
  const [newCollectionType, setNewCollectionType] = useState<Collection['type']>('base')
  const [creatingCollection, setCreatingCollection] = useState(false)

  const [filterInput, setFilterInput] = useState('')
  const [appliedFilter, setAppliedFilter] = useState('')
  const [sortInput, setSortInput] = useState('-created_at')
  const [page, setPage] = useState(1)

  const [recordModalMode, setRecordModalMode] = useState<RecordEditorMode | null>(null)
  const [recordModalRecord, setRecordModalRecord] = useState<RecordData | null>(null)
  const [apiPreviewOpen, setApiPreviewOpen] = useState(false)
  const [vectorSupported, setVectorSupported] = useState(false)
  const [downloadingSdk, setDownloadingSdk] = useState(false)

  const selectedCollection = useMemo(
    () => collections.find((collection) => collection.id === selectedCollectionId) || null,
    [collections, selectedCollectionId]
  )

  const vectorFields = useMemo(() => {
    if (!selectedCollection) return []
    return selectedCollection.schema.filter((field) => field.type === 'vector')
  }, [selectedCollection])

  const loadCollections = useCallback(async () => {
    setCollectionsLoading(true)
    setError(null)
    try {
      const data = await api.getCollections()
      setCollections(Array.isArray(data) ? data : [])
    } catch (err: any) {
      setError(err.message || 'Failed to load collections')
    } finally {
      setCollectionsLoading(false)
    }
  }, [])

  const loadRecords = useCallback(async () => {
    if (!selectedCollection) return

    setRecordsLoading(true)
    setError(null)
    try {
      const params: Record<string, string> = {
        page: String(page),
        perPage: String(PER_PAGE),
      }
      if (appliedFilter.trim()) params.filter = appliedFilter.trim()
      if (sortInput.trim()) params.sort = sortInput.trim()
      const relationFields = selectedCollection.schema
        .filter((field) => !field.system && field.type === 'relation')
        .slice(0, 4)
        .map((field) => field.name)
      if (relationFields.length) {
        params.expand = relationFields.join(',')
      }

      const result = await api.getRecords(selectedCollection.name, params)
      setRecordsResult(result)
    } catch (err: any) {
      setError(err.message || 'Failed to load records')
      setRecordsResult(null)
    } finally {
      setRecordsLoading(false)
    }
  }, [appliedFilter, page, selectedCollection, sortInput])

  useEffect(() => {
    loadCollections()
  }, [loadCollections])

  useEffect(() => {
    let active = true
    api
      .features()
      .then((features) => {
        if (active) setVectorSupported(Boolean(features?.vector))
      })
      .catch(() => {
        /* features endpoint optional */
      })
    return () => {
      active = false
    }
  }, [])

  const handleDownloadSdk = async () => {
    setDownloadingSdk(true)
    try {
      await api.downloadTypes()
      toast.success('Typed SDK downloaded')
    } catch (err: any) {
      toast.error(err.message || 'Failed to download SDK')
    } finally {
      setDownloadingSdk(false)
    }
  }

  useEffect(() => {
    if (collectionsLoading) return
    if (selectedCollectionId || homeMode) return
    try {
      const saved = window.localStorage.getItem(LAST_ACTIVE_COLLECTION_KEY)
      if (saved && collections.some((collection) => collection.id === saved)) {
        router.replace(`/collections?collection=${saved}`)
      }
    } catch {
      // ignore localStorage errors
    }
  }, [collections, collectionsLoading, homeMode, router, selectedCollectionId])

  useEffect(() => {
    setActiveTab('data')
    setPage(1)
    setAppliedFilter('')
    setFilterInput('')
    setSortInput('-created_at')
    setRecordModalMode(null)
    setRecordModalRecord(null)
    setApiPreviewOpen(false)
  }, [selectedCollectionId])

  useEffect(() => {
    if (!selectedCollection) {
      setRecordsResult(null)
      return
    }
    if (activeTab !== 'data') return
    loadRecords()
  }, [activeTab, loadRecords, selectedCollection])

  const openCollection = (collectionId: string) => {
    try {
      window.localStorage.setItem(LAST_ACTIVE_COLLECTION_KEY, collectionId)
    } catch {
      // ignore
    }
    router.push(`/collections?collection=${collectionId}`)
  }

  const handleCreateCollection = async () => {
    if (!newCollectionName.trim()) return

    setCreatingCollection(true)
    setError(null)
    try {
      const created = await api.createCollection({
        name: newCollectionName.trim(),
        type: newCollectionType,
        schema:
          newCollectionType === 'auth'
            ? [
                { name: 'email', type: 'email', required: true, unique: true },
                { name: 'password', type: 'password', required: true },
                { name: 'name', type: 'text' },
              ]
            : [
                { name: 'title', type: 'text', required: true },
                { name: 'status', type: 'select', options: { values: ['draft', 'published'] } },
                { name: 'content', type: 'editor' },
              ],
      })

      setNewCollectionName('')
      setNewCollectionType('base')
      toast.success(`Collection ${created.name} created`, {
        description: 'Access rules are locked to superusers by default. Open the Schema tab to configure them.',
      })
      await loadCollections()
      window.dispatchEvent(new Event('gresbase:collections:changed'))
      if (created?.id) openCollection(created.id)
    } catch (err: any) {
      setError(err.message || 'Failed to create collection')
      toast.error(err.message || 'Failed to create collection')
    } finally {
      setCreatingCollection(false)
    }
  }

  const handleDeleteCollection = async () => {
    if (!selectedCollection) return
    if (!confirm(`Delete collection "${selectedCollection.name}" and all its records? This cannot be undone.`)) {
      return
    }

    try {
      await api.deleteCollection(selectedCollection.id)
      toast.success(`Collection ${selectedCollection.name} deleted`)
      await loadCollections()
      window.dispatchEvent(new Event('gresbase:collections:changed'))
      try {
        window.localStorage.removeItem(LAST_ACTIVE_COLLECTION_KEY)
      } catch {
        // ignore
      }
      router.push('/collections?home=1')
    } catch (err: any) {
      setError(err.message || 'Failed to delete collection')
      toast.error(err.message || 'Failed to delete collection')
    }
  }

  const openCreateRecord = () => {
    if (!selectedCollection) return
    setRecordModalRecord(null)
    setRecordModalMode(selectedCollection.type === 'view' ? 'view' : 'create')
  }

  const openExistingRecord = (record: RecordData) => {
    if (!selectedCollection) return
    setRecordModalRecord(record)
    setRecordModalMode(selectedCollection.type === 'view' ? 'view' : 'edit')
  }

  const recordItems = recordsResult?.items ?? []
  const openRecordIndex = recordModalRecord?.id
    ? recordItems.findIndex((item) => item.id === recordModalRecord.id)
    : -1

  const navigateRecord = (direction: 'prev' | 'next') => {
    if (openRecordIndex === -1) return
    const nextIndex = direction === 'prev' ? openRecordIndex - 1 : openRecordIndex + 1
    const target = recordItems[nextIndex]
    if (target) openExistingRecord(target)
  }

  const duplicateRecord = (record: RecordData) => {
    if (!selectedCollection || selectedCollection.type === 'view') return
    // Prefill a new record from the source, minus identity and file values
    // (staged uploads cannot be copied client-side).
    const copy = { ...record } as RecordData
    delete (copy as Record<string, unknown>).id
    delete (copy as Record<string, unknown>).created_at
    delete (copy as Record<string, unknown>).updated_at
    for (const field of selectedCollection.schema) {
      if (field.type === 'file') delete (copy as Record<string, unknown>)[field.name]
    }
    setRecordModalRecord(copy)
    setRecordModalMode('create')
  }

  const visibleTabs: Array<{ key: WorkbenchTab; label: string }> = [
    { key: 'data', label: 'Data' },
    { key: 'schema', label: 'Schema' },
    ...(selectedCollection?.type === 'auth' ? ([{ key: 'auth', label: 'Auth' }] as Array<{ key: WorkbenchTab; label: string }>) : []),
    { key: 'api', label: 'API' },
  ]

  if (collectionsLoading && collections.length === 0) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <AppLayout>
      <div className="space-y-6 animate-fade-in">
        {error ? (
          <Card className="border-red-500/50 bg-red-500/5">
            <CardContent className="py-3">
              <p className="text-sm text-red-500">{error}</p>
            </CardContent>
          </Card>
        ) : null}

        {!selectedCollection ? (
          <CollectionsHome
            collections={collections}
            creatingCollection={creatingCollection}
            newCollectionName={newCollectionName}
            newCollectionType={newCollectionType}
            downloadingSdk={downloadingSdk}
            onDownloadSdk={handleDownloadSdk}
            onCollectionNameChange={setNewCollectionName}
            onCollectionTypeChange={(value) => setNewCollectionType(value as Collection['type'])}
            onCreateCollection={handleCreateCollection}
            onOpenCollection={openCollection}
            onImported={async () => {
              await loadCollections()
              window.dispatchEvent(new Event('gresbase:collections:changed'))
            }}
          />
        ) : (
          <div className="space-y-6">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
              <div className="space-y-3">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <button
                    onClick={() => router.push('/collections?home=1')}
                    className="inline-flex items-center gap-1 rounded-md px-2 py-1 hover:bg-accent/30 hover:text-foreground"
                  >
                    <ArrowLeft className="h-4 w-4" />
                    Collections
                  </button>
                  <span>/</span>
                  <span className="text-foreground">{selectedCollection.name}</span>
                </div>
                <div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h1 className="text-2xl font-bold tracking-tight">{selectedCollection.name}</h1>
                    <Badge variant="outline">{selectedCollection.type}</Badge>
                    {selectedCollection.system ? <Badge variant="secondary">system</Badge> : null}
                  </div>
                  <p className="mt-1 text-sm text-muted-foreground">
                    PocketBase-shaped workbench on PostgreSQL — {selectedCollection.schema.length} field
                    {selectedCollection.schema.length !== 1 ? 's' : ''}, {countRules(selectedCollection)} access rule
                    {countRules(selectedCollection) !== 1 ? 's' : ''}.
                  </p>
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-2">
                <Button variant="outline" size="sm" onClick={handleDownloadSdk} disabled={downloadingSdk}>
                  {downloadingSdk ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
                  Download SDK (.ts)
                </Button>
                <Button variant="outline" size="sm" onClick={() => setApiPreviewOpen(true)}>
                  <Code2 className="mr-2 h-4 w-4" />
                  API preview
                </Button>
                <Button variant="outline" size="sm" onClick={loadRecords} disabled={recordsLoading || activeTab !== 'data'}>
                  <RefreshCw className={`mr-2 h-4 w-4 ${recordsLoading ? 'animate-spin' : ''}`} />
                  Refresh
                </Button>
                {selectedCollection.type !== 'view' ? (
                  <Button size="sm" onClick={openCreateRecord}>
                    <Plus className="mr-2 h-4 w-4" />
                    New record
                  </Button>
                ) : null}
                {!selectedCollection.system ? (
                  <Button variant="destructive" size="sm" onClick={handleDeleteCollection}>
                    <Trash2 className="mr-2 h-4 w-4" />
                    Delete collection
                  </Button>
                ) : null}
              </div>
            </div>

            <div className="flex flex-wrap gap-2 border-b border-border pb-2">
              {visibleTabs.map((tab) => (
                <button
                  key={tab.key}
                  onClick={() => setActiveTab(tab.key)}
                  className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
                    activeTab === tab.key
                      ? 'bg-accent text-foreground'
                      : 'text-muted-foreground hover:bg-accent/30 hover:text-foreground'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>

            {activeTab === 'data' ? (
              <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_320px]">
                <Card>
                  <CardHeader>
                    <CardTitle className="text-base">Record explorer</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_220px_auto]">
                      <div className="relative">
                        <Filter className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                          value={filterInput}
                          onChange={(e) => setFilterInput(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              setPage(1)
                              setAppliedFilter(filterInput)
                            }
                          }}
                          className="pl-9"
                          placeholder='Raw filter expression, e.g. status = "published"'
                        />
                      </div>
                      <div className="relative">
                        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                          value={sortInput}
                          onChange={(e) => setSortInput(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              setPage(1)
                              void loadRecords()
                            }
                          }}
                          className="pl-9"
                          placeholder="Sort, e.g. -created_at"
                        />
                      </div>
                      <Button
                        variant="outline"
                        onClick={() => {
                          setPage(1)
                          setAppliedFilter(filterInput)
                        }}
                      >
                        Apply
                      </Button>
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                      <span>Fields:</span>
                      {selectedCollection.schema
                        .filter((field) => !field.system)
                        .slice(0, 8)
                        .map((field) => (
                          <button
                            key={field.id || field.name}
                            type="button"
                            onClick={() => setFilterInput((current) => (current ? `${current} && ${field.name} ` : `${field.name} `))}
                            className="rounded border border-border bg-accent/20 px-1.5 py-0.5 font-mono text-[11px] transition-colors hover:bg-accent/50 hover:text-foreground"
                            title={`Insert ${field.name} into the filter`}
                          >
                            {field.name}
                          </button>
                        ))}
                      <span className="ml-1">
                        Operators: <code className="font-mono">= != &gt; &gt;= &lt; &lt;= ~ !~ && ||</code> — press Enter to apply
                      </span>
                    </div>

                    {appliedFilter ? (
                      <div className="rounded-lg border border-border bg-accent/20 px-3 py-2 text-xs text-muted-foreground">
                        Active filter: <code className="font-mono text-foreground">{appliedFilter}</code>
                      </div>
                    ) : null}

                    <RecordsTable
                      collection={selectedCollection}
                      collections={collections}
                      records={recordsResult?.items ?? []}
                      loading={recordsLoading}
                      sort={sortInput}
                      hasActiveFilter={Boolean(appliedFilter)}
                      onSortChange={(nextSort) => {
                        setSortInput(nextSort)
                        setPage(1)
                      }}
                      onRowClick={openExistingRecord}
                      onEdit={openExistingRecord}
                      onDuplicate={duplicateRecord}
                      onCreate={openCreateRecord}
                      onClearFilter={() => {
                        setFilterInput('')
                        setAppliedFilter('')
                        setPage(1)
                      }}
                      onRefresh={loadRecords}
                    />

                    <div className="flex items-center justify-between text-sm text-muted-foreground">
                      <span>
                        {recordsResult?.totalItems || 0} total · page {recordsResult?.page || page} of {recordsResult?.totalPages || 1}
                      </span>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={!recordsResult || recordsResult.page <= 1}
                          onClick={() => setPage((current) => Math.max(1, current - 1))}
                        >
                          Previous
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={!recordsResult || recordsResult.page >= recordsResult.totalPages}
                          onClick={() => setPage((current) => current + 1)}
                        >
                          Next
                        </Button>
                      </div>
                    </div>
                  </CardContent>
                </Card>

                <div className="space-y-6">
                  <Card>
                    <CardHeader>
                      <CardTitle className="text-base">Workbench focus</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4 text-sm text-muted-foreground">
                      <p>
                        Records are edited in a real modal now, with local drafts and field-specific inputs instead of a generic control panel side sheet.
                      </p>
                      <div className="rounded-lg border border-border bg-accent/10 p-4">
                        <div className="font-medium text-foreground">Quick facts</div>
                        <div className="mt-2 space-y-1 text-xs">
                          <div>Type: {selectedCollection.type}</div>
                          <div>Fields: {selectedCollection.schema.length}</div>
                          <div>Rules: {countRules(selectedCollection)}</div>
                          <div>Auth collection: {selectedCollection.type === 'auth' ? 'yes' : 'no'}</div>
                        </div>
                      </div>
                      <Button onClick={openCreateRecord} disabled={selectedCollection.type === 'view'}>
                        <Sparkles className="mr-2 h-4 w-4" />
                        {selectedCollection.type === 'view' ? 'View-only collection' : 'Create record'}
                      </Button>
                    </CardContent>
                  </Card>

                  {vectorSupported && vectorFields.length ? (
                    <SemanticSearchCard collection={selectedCollection} vectorFields={vectorFields} />
                  ) : null}

                  <Card>
                    <CardHeader>
                      <CardTitle className="text-base">Filter syntax</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2 text-xs text-muted-foreground">
                      <p><code className="font-mono text-foreground">status = &quot;published&quot;</code></p>
                      <p><code className="font-mono text-foreground">owner = @request.auth.id</code></p>
                      <p><code className="font-mono text-foreground">created_at &gt; &quot;2024-01-01&quot;</code></p>
                    </CardContent>
                  </Card>
                </div>
              </div>
            ) : null}

            {activeTab === 'schema' ? (
              <SchemaEditor
                collection={selectedCollection}
                collections={collections}
                onSaved={(updated) => {
                  setApiPreviewOpen(false)
                  setRecordModalMode(null)
                  setRecordModalRecord(null)
                  setCollections((current) => current.map((item) => (item.id === updated.id ? updated : item)))
                  window.dispatchEvent(new Event('gresbase:collections:changed'))
                }}
              />
            ) : null}

            {activeTab === 'auth' && selectedCollection.type === 'auth' ? <AuthTools collection={selectedCollection} /> : null}

            {activeTab === 'api' ? (
              <ApiPreviewPanel
                collection={selectedCollection}
                collections={collections}
                className="h-[calc(100vh-280px)] min-h-[560px]"
              />
            ) : null}
          </div>
        )}
      </div>

      {selectedCollection ? (
        <>
          <RecordWorkbenchModal
            open={Boolean(recordModalMode)}
            mode={recordModalMode || 'view'}
            collection={selectedCollection}
            collections={collections}
            record={recordModalRecord}
            onClose={() => {
              setRecordModalMode(null)
              setRecordModalRecord(null)
            }}
            onSaved={(savedRecord) => {
              if (savedRecord?.id) {
                setRecordModalRecord(savedRecord)
              }
              void loadRecords()
            }}
            onDeleted={() => {
              setRecordModalMode(null)
              setRecordModalRecord(null)
              void loadRecords()
            }}
            onDuplicate={duplicateRecord}
            onNavigate={navigateRecord}
            hasPrev={openRecordIndex > 0}
            hasNext={openRecordIndex !== -1 && openRecordIndex < recordItems.length - 1}
          />

          <ApiPreviewModal
            open={apiPreviewOpen}
            collection={selectedCollection}
            collections={collections}
            onClose={() => setApiPreviewOpen(false)}
          />
        </>
      ) : null}
    </AppLayout>
  )
}

function SemanticSearchCard({
  collection,
  vectorFields,
}: {
  collection: Collection
  vectorFields: SchemaField[]
}) {
  const [field, setField] = useState(vectorFields[0]?.name || '')
  const [vectorText, setVectorText] = useState('')
  const [limit, setLimit] = useState(10)
  const [running, setRunning] = useState(false)
  const [results, setResults] = useState<RecordData[] | null>(null)

  useEffect(() => {
    setField(vectorFields[0]?.name || '')
    setResults(null)
    setVectorText('')
  }, [collection.id, vectorFields])

  const textFieldName = useMemo(() => {
    const candidate = collection.schema.find(
      (f) => !f.system && (f.type === 'text' || f.type === 'editor' || f.type === 'email')
    )
    return candidate?.name || null
  }, [collection])

  const run = async () => {
    const trimmed = vectorText.trim()
    if (!trimmed) {
      toast.error('Enter a query vector')
      return
    }
    let vector: number[]
    try {
      const raw = trimmed.startsWith('[') ? JSON.parse(trimmed) : trimmed.split(',')
      vector = (Array.isArray(raw) ? raw : []).map((value: any) => {
        const num = Number(typeof value === 'string' ? value.trim() : value)
        if (Number.isNaN(num)) throw new Error('not a number')
        return num
      })
    } catch {
      toast.error('Query vector must be comma-separated numbers or a JSON array')
      return
    }
    if (!vector.length) {
      toast.error('Query vector is empty')
      return
    }
    setRunning(true)
    try {
      const res = await api.vectorSearch(collection.name, field, vector, limit)
      setResults(Array.isArray(res?.items) ? res.items : [])
    } catch (err: any) {
      toast.error(err.message || 'Vector search failed')
    } finally {
      setRunning(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Semantic search</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="space-y-2">
          <label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Vector field</label>
          <select
            value={field}
            onChange={(e) => setField(e.target.value)}
            className="h-9 w-full rounded-lg border border-input bg-background px-3 text-sm"
          >
            {vectorFields.map((f) => (
              <option key={f.id || f.name} value={f.name}>
                {f.name}
              </option>
            ))}
          </select>
        </div>
        <div className="space-y-2">
          <label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Query vector</label>
          <textarea
            value={vectorText}
            onChange={(e) => setVectorText(e.target.value)}
            rows={3}
            className="w-full rounded-lg border border-input bg-background px-3 py-2 font-mono text-xs"
            placeholder="0.12, -0.45, 0.9, … or [0.12, -0.45, 0.9]"
          />
        </div>
        <div className="grid grid-cols-[1fr_auto] items-end gap-2">
          <div className="space-y-2">
            <label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Limit</label>
            <Input
              type="number"
              min={1}
              value={limit}
              onChange={(e) => setLimit(Math.max(1, Number(e.target.value) || 1))}
              className="h-9"
            />
          </div>
          <Button size="sm" onClick={run} disabled={running}>
            {running ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Search className="mr-2 h-4 w-4" />}
            Run
          </Button>
        </div>

        {results ? (
          results.length ? (
            <div className="space-y-2">
              {results.map((record) => (
                <div key={record.id} className="rounded-lg border border-border bg-accent/10 px-3 py-2 text-xs">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-mono text-muted-foreground">{truncate(record.id)}</span>
                    <span className="font-mono text-foreground">{formatDistance(record._distance)}</span>
                  </div>
                  {textFieldName && record[textFieldName] ? (
                    <div className="mt-1 truncate text-foreground">{String(record[textFieldName])}</div>
                  ) : null}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">No matches found.</p>
          )
        ) : null}
      </CardContent>
    </Card>
  )
}

function CollectionsHome({
  collections,
  creatingCollection,
  newCollectionName,
  newCollectionType,
  downloadingSdk,
  onDownloadSdk,
  onCollectionNameChange,
  onCollectionTypeChange,
  onCreateCollection,
  onOpenCollection,
  onImported,
}: {
  collections: Collection[]
  creatingCollection: boolean
  newCollectionName: string
  newCollectionType: Collection['type']
  downloadingSdk: boolean
  onDownloadSdk: () => void
  onCollectionNameChange: (value: string) => void
  onCollectionTypeChange: (value: string) => void
  onCreateCollection: () => void
  onOpenCollection: (collectionId: string) => void
  onImported: () => void | Promise<void>
}) {
  const [lastActive, setLastActive] = useState<string | null>(null)

  useEffect(() => {
    try {
      setLastActive(window.localStorage.getItem(LAST_ACTIVE_COLLECTION_KEY))
    } catch {
      setLastActive(null)
    }
  }, [])

  const lastCollection = collections.find((collection) => collection.id === lastActive) || null

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Collections</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Gresbase should feel like PocketBase on PostgreSQL: collections first, records immediately underneath, everything else in support.
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={onDownloadSdk} disabled={downloadingSdk}>
          {downloadingSdk ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
          Download SDK (.ts)
        </Button>
      </div>

      {lastCollection ? (
        <Card>
          <CardContent className="flex flex-col gap-3 py-5 md:flex-row md:items-center md:justify-between">
            <div>
              <div className="text-sm font-medium">Resume last active collection</div>
              <div className="mt-1 text-sm text-muted-foreground">{lastCollection.name} · {lastCollection.type}</div>
            </div>
            <Button onClick={() => onOpenCollection(lastCollection.id)}>
              <Sparkles className="mr-2 h-4 w-4" />
              Open {lastCollection.name}
            </Button>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Create collection</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px_auto]">
          <Input
            placeholder="Collection name (posts, customers, invoices)"
            value={newCollectionName}
            onChange={(e) => onCollectionNameChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onCreateCollection()
            }}
          />
          <select
            value={newCollectionType}
            onChange={(e) => onCollectionTypeChange(e.target.value)}
            className="h-9 rounded-lg border border-input bg-background px-3 text-sm"
          >
            <option value="base">Base</option>
            <option value="auth">Auth</option>
            <option value="view">View</option>
          </select>
          <Button onClick={onCreateCollection} disabled={creatingCollection || !newCollectionName.trim()}>
            {creatingCollection ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
            Create
          </Button>
        </CardContent>
      </Card>

      <CollectionsTransferTools onImported={() => { void onImported() }} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {collections.map((collection) => {
          const Icon = collection.type === 'auth' ? Shield : collection.type === 'view' ? Eye : Database
          return (
            <button key={collection.id} onClick={() => onOpenCollection(collection.id)} className="text-left">
              <Card className="h-full transition-all hover:border-primary/40 hover:shadow-sm">
                <CardContent className="flex h-full flex-col gap-4 pt-6">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
                      <Icon className="h-5 w-5 text-primary" />
                    </div>
                    <Badge variant="outline">{collection.type}</Badge>
                  </div>
                  <div>
                    <div className="font-medium">{collection.name}</div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {collection.schema.length} fields · {countRules(collection)} rule{countRules(collection) !== 1 ? 's' : ''}
                    </p>
                  </div>
                </CardContent>
              </Card>
            </button>
          )
        })}
      </div>

      {collections.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <Database className="mb-4 h-12 w-12 text-muted-foreground opacity-50" />
            <h3 className="text-lg font-medium">No collections yet</h3>
            <p className="mt-1 max-w-sm text-sm text-muted-foreground">
              Start with a real collection, not a dashboard page. The product gets tighter the faster users land in data.
            </p>
          </CardContent>
        </Card>
      ) : null}
    </div>
  )
}

function countRules(collection: Collection) {
  return [collection.list_rule, collection.view_rule, collection.create_rule, collection.update_rule, collection.delete_rule].filter(
    (value) => !!value?.trim()
  ).length
}

function truncate(value: string, limit = 16) {
  return value.length > limit ? `${value.slice(0, limit)}…` : value
}

function formatDistance(value: unknown) {
  const num = Number(value)
  return Number.isFinite(num) ? num.toFixed(4) : '—'
}

function formatDate(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
