'use client'

import * as React from 'react'
import {
  AlertCircle,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Clipboard,
  Copy,
  CopyPlus,
  Loader2,
  MoreHorizontal,
  Save,
  Trash2,
  UserRound,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { AlertDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import type { Collection, RecordData } from '@/lib/types'
import { api } from '@/lib/api'
import {
  buildDraftFromRecord,
  buildRecordPayload,
  DropdownMenu,
  DropdownMenuItem,
  extractDraftFileValues,
  isTempStoredPath,
  type RecordEditorMode,
  RecordFieldInput,
} from '@/components/collections/field-kit'

interface RecordWorkbenchModalProps {
  open: boolean
  mode: RecordEditorMode
  collection: Collection
  collections: Collection[]
  record?: RecordData | null
  onClose: () => void
  onSaved?: (record: RecordData) => void
  onDeleted?: (recordId: string) => void
  /** Opens create mode prefilled from the given record (PB-style duplicate). */
  onDuplicate?: (record: RecordData) => void
  /** Navigate to the previous/next record within the currently loaded page. */
  onNavigate?: (direction: 'prev' | 'next') => void
  hasPrev?: boolean
  hasNext?: boolean
}

/**
 * PB-style record editor: a full-height panel sliding in from the right edge.
 * Keeps local draft autosave + recovery, staged file uploads, Cmd/Ctrl+S and Esc.
 */
export function RecordWorkbenchModal({
  open,
  mode,
  collection,
  collections,
  record,
  onClose,
  onSaved,
  onDeleted,
  onDuplicate,
  onNavigate,
  hasPrev = false,
  hasNext = false,
}: RecordWorkbenchModalProps) {
  const [currentMode, setCurrentMode] = React.useState<RecordEditorMode>(mode)
  const [currentRecord, setCurrentRecord] = React.useState<RecordData | null>(record || null)
  const [draft, setDraft] = React.useState<Record<string, any>>({})
  const [initialHash, setInitialHash] = React.useState('')
  const [restorableDraft, setRestorableDraft] = React.useState<Record<string, any> | null>(null)
  const [draftSessionId, setDraftSessionId] = React.useState<string>('')
  const [saving, setSaving] = React.useState(false)
  const [deleting, setDeleting] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [metaOpen, setMetaOpen] = React.useState(false)
  const [confirmDeleteOpen, setConfirmDeleteOpen] = React.useState(false)
  const [impersonating, setImpersonating] = React.useState(false)
  const [impersonationResult, setImpersonationResult] = React.useState<{ token: string; refreshToken: string } | null>(null)

  // Slide-in/out animation state (Tailwind transitions, no framer-motion).
  const [mounted, setMounted] = React.useState(false)
  const [shown, setShown] = React.useState(false)

  React.useEffect(() => {
    if (open) {
      setMounted(true)
      const timer = setTimeout(() => setShown(true), 20)
      return () => clearTimeout(timer)
    }
    setShown(false)
    const timer = setTimeout(() => setMounted(false), 220)
    return () => clearTimeout(timer)
  }, [open])

  const draftStorageKey = React.useMemo(() => {
    const recordKey = currentRecord?.id || record?.id || 'new'
    return `gresbase_record_draft:${collection.id}:${recordKey}`
  }, [collection.id, currentRecord?.id, record?.id])

  React.useEffect(() => {
    if (!open) return

    const nextDraft = buildDraftFromRecord(collection, record || null)
    const nextHash = serializeDraft(nextDraft)
    setCurrentMode(mode)
    setCurrentRecord(record || null)
    setDraft(nextDraft)
    setInitialHash(nextHash)
    setDraftSessionId(typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}`)
    setError(null)
    setConfirmDeleteOpen(false)
    setImpersonationResult(null)

    const savedDraft = loadDraft(draftStorageKey)
    if (savedDraft && serializeDraft(savedDraft) !== nextHash) {
      setRestorableDraft(savedDraft)
    } else {
      setRestorableDraft(null)
    }
  }, [collection, draftStorageKey, mode, open, record])

  const isDirty = React.useMemo(() => serializeDraft(draft) !== initialHash, [draft, initialHash])

  React.useEffect(() => {
    if (!open) return
    if (currentMode === 'view') return

    if (isDirty) {
      persistDraft(draftStorageKey, draft)
    } else {
      clearDraft(draftStorageKey)
    }
  }, [currentMode, draft, draftStorageKey, isDirty, open])

  React.useEffect(() => {
    if (!open) return

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !saving && !deleting) {
        event.preventDefault()
        if (impersonationResult) {
          setImpersonationResult(null)
          return
        }
        handleRequestClose()
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's' && currentMode !== 'view') {
        event.preventDefault()
        void handleSave()
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [currentMode, deleting, impersonationResult, open, saving])

  const handleRestoreDraft = () => {
    if (!restorableDraft) return
    setDraft(restorableDraft)
    setRestorableDraft(null)
  }

  const handleDiscardStoredDraft = () => {
    clearDraft(draftStorageKey)
    setRestorableDraft(null)
  }

  const cleanupTempDraftFiles = React.useCallback(async (sourceDraft: Record<string, any>) => {
    const stagedFiles = Object.values(extractDraftFileValues(collection.schema, sourceDraft)).flat().filter(isTempStoredPath)
    await Promise.all(
      stagedFiles.map(async (path) => {
        const parts = path.split('/')
        if (parts.length < 3) return
        await api.deleteFile(parts[0], parts[1], parts[parts.length - 1]).catch(() => undefined)
      })
    )
  }, [collection.schema])

  const handleRequestClose = () => {
    if (saving || deleting) return
    if (isDirty && !window.confirm('Discard unsaved changes?')) {
      return
    }
    if (!isDirty) {
      clearDraft(draftStorageKey)
    }
    if (currentMode === 'create') {
      void cleanupTempDraftFiles(draft)
    }
    onClose()
  }

  const handleNavigate = (direction: 'prev' | 'next') => {
    if (saving || deleting || !onNavigate) return
    if (isDirty && !window.confirm('Discard unsaved changes?')) return
    onNavigate(direction)
  }

  const handleDuplicate = () => {
    if (!currentRecord || !onDuplicate) return
    if (isDirty && !window.confirm('Discard unsaved changes and duplicate this record?')) return
    onDuplicate(currentRecord)
  }

  const handleCopyJson = async () => {
    if (!currentRecord) return
    try {
      await navigator.clipboard.writeText(JSON.stringify(currentRecord, null, 2))
      toast.success('Record JSON copied to clipboard')
    } catch {
      toast.error('Failed to copy to clipboard')
    }
  }

  const handleImpersonate = async () => {
    if (!currentRecord?.id || impersonating) return
    setImpersonating(true)
    try {
      const result = await api.impersonateRecord(collection.name, currentRecord.id)
      setImpersonationResult({ token: result.token, refreshToken: result.refreshToken })
    } catch (err: any) {
      toast.error(err.message || 'Impersonation not allowed')
    } finally {
      setImpersonating(false)
    }
  }

  const persistField = async (fieldName: string, value: any) => {
    if (!currentRecord?.id) return
    await api.updateRecord(collection.name, currentRecord.id, { [fieldName]: value })
    setCurrentRecord((prev) => (prev ? { ...prev, [fieldName]: value } : prev))
    setDraft((current) => {
      const nextDraft = { ...current, [fieldName]: value }
      setInitialHash(serializeDraft(nextDraft))
      clearDraft(draftStorageKey)
      return nextDraft
    })
    onSaved?.({ ...(currentRecord || {}), [fieldName]: value } as RecordData)
  }

  const handleSave = async () => {
    if (currentMode === 'view') return

    setSaving(true)
    setError(null)
    let createdRecord: RecordData | null = null
    try {
      if (currentMode === 'create') {
        const stagedFiles = extractDraftFileValues(collection.schema, draft)
        const createDraft = { ...draft }
        for (const field of collection.schema.filter((item) => item.type === 'file')) {
          if (stagedFiles[field.name]?.some(isTempStoredPath)) {
            delete createDraft[field.name]
          }
        }

        const payload = buildRecordPayload(collection.schema, createDraft, false)
        let created = await api.createRecord(collection.name, payload)
        createdRecord = created
        setCurrentRecord(created)
        setCurrentMode('edit')

        const promotePatch: Record<string, any> = {}
        for (const [fieldName, files] of Object.entries(stagedFiles)) {
          const staged = files.filter(isTempStoredPath)
          if (!staged.length) continue
          const promoted = await api.promoteFiles(collection.name, created.id, staged)
          const filenames = promoted.filenames || []
          const field = collection.schema.find((item) => item.name === fieldName)
          if (!field) continue
          promotePatch[fieldName] = Number(field.options?.max_select || 1) > 1 ? filenames : filenames[0] || ''
        }
        if (Object.keys(promotePatch).length) {
          created = await api.updateRecord(collection.name, created.id, promotePatch)
        }

        toast.success('Record created')
        setCurrentRecord(created)
        setCurrentMode('edit')
        setDraftSessionId(typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}`)
        const nextDraft = buildDraftFromRecord(collection, created)
        setDraft(nextDraft)
        setInitialHash(serializeDraft(nextDraft))
        clearDraft(draftStorageKey)
        onSaved?.(created)
        return
      }

      if (currentRecord?.id) {
        const payload = buildRecordPayload(collection.schema, draft, true)
        const updated = await api.updateRecord(collection.name, currentRecord.id, payload)
        toast.success('Record updated')
        setCurrentRecord(updated)
        const nextDraft = buildDraftFromRecord(collection, updated)
        setDraft(nextDraft)
        setInitialHash(serializeDraft(nextDraft))
        clearDraft(draftStorageKey)
        onSaved?.(updated)
      }
    } catch (err: any) {
      if (createdRecord) {
        setCurrentRecord(createdRecord)
        setCurrentMode('edit')
      }
      const message = err.message || 'Failed to save record'
      setError(message)
      toast.error(message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!currentRecord?.id) return

    setDeleting(true)
    setError(null)
    try {
      await api.deleteRecord(collection.name, currentRecord.id)
      clearDraft(draftStorageKey)
      toast.success('Record deleted')
      setConfirmDeleteOpen(false)
      onDeleted?.(currentRecord.id)
      onClose()
    } catch (err: any) {
      const message = err.message || 'Failed to delete record'
      setError(message)
      toast.error(message)
    } finally {
      setDeleting(false)
    }
  }

  if (!mounted) return null

  const showNavigation = Boolean(onNavigate) && currentMode !== 'create' && Boolean(currentRecord?.id)

  return (
    <div className="fixed inset-0 z-50">
      <div
        className={cn(
          'absolute inset-0 bg-background/80 backdrop-blur-sm transition-opacity duration-200',
          shown ? 'opacity-100' : 'opacity-0'
        )}
        onClick={handleRequestClose}
      />

      <div
        className={cn(
          'absolute inset-y-0 right-0 flex w-[min(640px,92vw)] flex-col border-l border-border bg-background shadow-2xl transition-transform duration-200 ease-out',
          shown ? 'translate-x-0' : 'translate-x-full'
        )}
        role="dialog"
        aria-modal="true"
      >
        <div className="flex items-start justify-between gap-3 border-b border-border px-5 py-4">
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold">
                {currentMode === 'create' ? 'New record' : currentMode === 'view' ? 'Record preview' : 'Edit record'}
              </h2>
              <Badge variant="outline">{collection.name}</Badge>
              {isDirty ? <Badge variant="secondary">unsaved</Badge> : null}
            </div>
            <p className="truncate font-mono text-xs text-muted-foreground">
              {currentRecord?.id || 'Not created yet'}
            </p>
          </div>

          <div className="flex shrink-0 items-center gap-1">
            {showNavigation ? (
              <>
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={!hasPrev || saving || deleting}
                  onClick={() => handleNavigate('prev')}
                  title="Previous record"
                >
                  <ChevronLeft className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={!hasNext || saving || deleting}
                  onClick={() => handleNavigate('next')}
                  title="Next record"
                >
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </>
            ) : null}

            {currentRecord?.id ? (
              <DropdownMenu
                width="w-48"
                trigger={
                  <Button variant="ghost" size="icon" title="Record actions">
                    <MoreHorizontal className="h-4 w-4" />
                  </Button>
                }
              >
                {(close) => (
                  <>
                    {onDuplicate && collection.type !== 'view' ? (
                      <DropdownMenuItem
                        icon={<CopyPlus className="h-4 w-4" />}
                        onSelect={() => {
                          close()
                          handleDuplicate()
                        }}
                      >
                        Duplicate
                      </DropdownMenuItem>
                    ) : null}
                    <DropdownMenuItem
                      icon={<Clipboard className="h-4 w-4" />}
                      onSelect={() => {
                        close()
                        void handleCopyJson()
                      }}
                    >
                      Copy raw JSON
                    </DropdownMenuItem>
                    {collection.type === 'auth' ? (
                      <DropdownMenuItem
                        icon={impersonating ? <Loader2 className="h-4 w-4 animate-spin" /> : <UserRound className="h-4 w-4" />}
                        disabled={impersonating}
                        onSelect={() => {
                          close()
                          void handleImpersonate()
                        }}
                      >
                        Impersonate
                      </DropdownMenuItem>
                    ) : null}
                  </>
                )}
              </DropdownMenu>
            ) : null}

            <Button variant="ghost" size="icon" onClick={handleRequestClose} title="Close (Esc)">
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {restorableDraft ? (
          <div className="flex flex-wrap items-center gap-3 border-b border-amber-500/20 bg-amber-500/5 px-5 py-3 text-sm">
            <AlertCircle className="h-4 w-4 text-amber-500" />
            <span className="text-muted-foreground">A newer local draft was found for this record.</span>
            <Button size="sm" variant="outline" onClick={handleRestoreDraft}>
              Restore draft
            </Button>
            <Button size="sm" variant="ghost" onClick={handleDiscardStoredDraft}>
              Discard draft
            </Button>
          </div>
        ) : null}

        {error ? (
          <div className="border-b border-red-500/20 bg-red-500/5 px-5 py-3 text-sm text-red-500">{error}</div>
        ) : null}

        <div className="flex-1 overflow-y-auto px-5 py-5">
          <div className="grid gap-5">
            {collection.schema
              .filter((field) => !field.system)
              .map((field) => (
                <RecordFieldInput
                  key={field.id || field.name}
                  collection={collection}
                  collections={collections}
                  field={field}
                  value={draft[field.name]}
                  mode={currentMode}
                  recordId={currentRecord?.id}
                  draftSessionId={draftSessionId}
                  readOnly={currentMode === 'view'}
                  onChange={(value) => setDraft((current) => ({ ...current, [field.name]: value }))}
                  onPersistField={persistField}
                  onRemoveStagedFile={async (path) => {
                    const parts = path.split('/')
                    if (parts.length < 3) return
                    await api.deleteFile(parts[0], parts[1], parts[parts.length - 1]).catch(() => undefined)
                  }}
                />
              ))}
          </div>

          <div className="mt-6 rounded-lg border border-border">
            <button
              type="button"
              onClick={() => setMetaOpen((current) => !current)}
              className="flex w-full items-center justify-between px-3 py-2 text-xs uppercase tracking-[0.14em] text-muted-foreground transition-colors hover:text-foreground"
            >
              Record metadata
              <ChevronDown className={cn('h-4 w-4 transition-transform', metaOpen && 'rotate-180')} />
            </button>
            {metaOpen ? (
              <div className="space-y-1.5 border-t border-border px-3 py-3 text-xs text-muted-foreground">
                <div>
                  <span className="font-medium text-foreground">ID:</span>{' '}
                  <span className="font-mono">{currentRecord?.id || 'Not created yet'}</span>
                </div>
                <div>
                  <span className="font-medium text-foreground">Created:</span> {formatDate(currentRecord?.created_at)}
                </div>
                <div>
                  <span className="font-medium text-foreground">Updated:</span> {formatDate(currentRecord?.updated_at)}
                </div>
              </div>
            ) : null}
          </div>

          <p className="mt-3 text-[11px] text-muted-foreground">
            ⌘/Ctrl+S saves · Esc closes · drafts autosave locally per collection + record
            {currentMode === 'create' ? ' · staged files are promoted after the record is created' : ''}
          </p>
        </div>

        <div className="flex items-center justify-between gap-2 border-t border-border px-5 py-4">
          <div>
            {currentMode === 'edit' && currentRecord?.id ? (
              <Button variant="destructive" onClick={() => setConfirmDeleteOpen(true)} disabled={deleting || saving}>
                {deleting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Trash2 className="mr-2 h-4 w-4" />}
                Delete
              </Button>
            ) : null}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={handleRequestClose} disabled={saving || deleting}>
              {currentMode === 'view' ? 'Close' : 'Cancel'}
            </Button>
            {currentMode !== 'view' ? (
              <Button onClick={handleSave} disabled={saving || deleting}>
                {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
                {currentMode === 'create' ? 'Create' : 'Save'}
              </Button>
            ) : null}
          </div>
        </div>
      </div>

      <AlertDialog
        open={confirmDeleteOpen}
        title="Delete record?"
        description={
          <>
            This will permanently delete{' '}
            <code className="font-mono text-xs text-foreground">{currentRecord?.id}</code>. This cannot be undone.
          </>
        }
        confirmLabel="Delete"
        destructive
        loading={deleting}
        onConfirm={handleDelete}
        onCancel={() => setConfirmDeleteOpen(false)}
      />

      {impersonationResult ? (
        <div className="fixed inset-0 z-[70] flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
          <div className="absolute inset-0" onClick={() => setImpersonationResult(null)} />
          <div className="relative z-10 w-full max-w-md space-y-4 rounded-2xl border border-border bg-background p-5 shadow-2xl">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h3 className="text-base font-semibold">Impersonation tokens</h3>
                <p className="mt-1 text-xs text-muted-foreground">
                  Auth tokens issued for <span className="font-mono">{currentRecord?.id}</span>. Treat them like passwords.
                </p>
              </div>
              <Button variant="ghost" size="icon" onClick={() => setImpersonationResult(null)}>
                <X className="h-4 w-4" />
              </Button>
            </div>
            <CopyableTokenRow label="Token" value={impersonationResult.token} />
            <CopyableTokenRow label="Refresh token" value={impersonationResult.refreshToken} />
          </div>
        </div>
      ) : null}
    </div>
  )
}

function CopyableTokenRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-1.5">
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="flex items-center gap-2">
        <code className="block min-w-0 flex-1 truncate rounded-md border border-border bg-accent/10 px-2 py-1.5 font-mono text-xs">
          {value || '—'}
        </code>
        <Button
          variant="outline"
          size="icon"
          className="h-8 w-8 shrink-0"
          title={`Copy ${label.toLowerCase()}`}
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(value)
              toast.success(`${label} copied to clipboard`)
            } catch {
              toast.error('Failed to copy to clipboard')
            }
          }}
        >
          <Copy className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  )
}

function serializeDraft(value: Record<string, any>) {
  return JSON.stringify(value)
}

function loadDraft(key: string) {
  try {
    const raw = window.localStorage.getItem(key)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

function persistDraft(key: string, value: Record<string, any>) {
  try {
    window.localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // ignore localStorage quota issues
  }
}

function clearDraft(key: string) {
  try {
    window.localStorage.removeItem(key)
  } catch {
    // ignore
  }
}

function formatDate(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
