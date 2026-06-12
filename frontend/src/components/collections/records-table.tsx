'use client'

import * as React from 'react'
import { ChevronDown, ChevronUp, CopyPlus, Database, Eye, FilterX, MoreHorizontal, Pencil, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { AlertDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { DropdownMenu, DropdownMenuItem, renderRecordFieldValue } from '@/components/collections/field-kit'
import type { Collection, RecordData, SchemaField } from '@/lib/types'

const COLUMN_PREFS_PREFIX = 'gresbase_cols:'
const DEFAULT_HIDDEN_COLUMNS = ['created_at']
const SKELETON_WIDTHS = ['w-24', 'w-16', 'w-32', 'w-20', 'w-28']

interface RecordsTableProps {
  collection: Collection
  collections: Collection[]
  records: RecordData[]
  loading: boolean
  /** Current sort expression: 'field' (asc) or '-field' (desc). */
  sort: string
  hasActiveFilter: boolean
  onSortChange: (sort: string) => void
  onRowClick: (record: RecordData) => void
  onEdit: (record: RecordData) => void
  onDuplicate: (record: RecordData) => void
  onCreate: () => void
  onClearFilter: () => void
  onRefresh: () => void | Promise<void>
}

interface ColumnDef {
  key: string
  label: string
  field: SchemaField
}

function syntheticDateField(name: string): SchemaField {
  return { id: name, name, type: 'autodate', system: true, required: false, unique: false, options: {} }
}

function loadHiddenColumns(collectionId: string): string[] {
  if (typeof window === 'undefined') return DEFAULT_HIDDEN_COLUMNS
  try {
    const raw = window.localStorage.getItem(`${COLUMN_PREFS_PREFIX}${collectionId}`)
    if (!raw) return DEFAULT_HIDDEN_COLUMNS
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.map(String) : DEFAULT_HIDDEN_COLUMNS
  } catch {
    return DEFAULT_HIDDEN_COLUMNS
  }
}

export function RecordsTable({
  collection,
  collections,
  records,
  loading,
  sort,
  hasActiveFilter,
  onSortChange,
  onRowClick,
  onEdit,
  onDuplicate,
  onCreate,
  onClearFilter,
  onRefresh,
}: RecordsTableProps) {
  const selectable = collection.type !== 'view'

  const [selected, setSelected] = React.useState<Set<string>>(new Set())
  const [hiddenColumns, setHiddenColumns] = React.useState<string[]>(() => loadHiddenColumns(collection.id))
  const [deleteTarget, setDeleteTarget] = React.useState<RecordData | null>(null)
  const [singleDeleting, setSingleDeleting] = React.useState(false)
  const [bulkConfirmOpen, setBulkConfirmOpen] = React.useState(false)
  const [bulkDeleting, setBulkDeleting] = React.useState(false)
  const selectAllRef = React.useRef<HTMLInputElement>(null)

  const columns = React.useMemo<ColumnDef[]>(() => {
    const fieldColumns = collection.schema
      .filter((field) => !field.system && field.type !== 'password')
      .map((field) => ({ key: field.name, label: field.name, field }))
    return [
      ...fieldColumns,
      { key: 'created_at', label: 'created', field: syntheticDateField('created_at') },
      { key: 'updated_at', label: 'updated', field: syntheticDateField('updated_at') },
    ]
  }, [collection])

  const visibleColumns = React.useMemo(
    () => columns.filter((column) => !hiddenColumns.includes(column.key)),
    [columns, hiddenColumns]
  )

  // Reset per-collection state when switching collections.
  React.useEffect(() => {
    setSelected(new Set())
    setHiddenColumns(loadHiddenColumns(collection.id))
    setDeleteTarget(null)
    setBulkConfirmOpen(false)
  }, [collection.id])

  // Prune selection to records still present on the current page.
  React.useEffect(() => {
    setSelected((current) => {
      if (!current.size) return current
      const ids = new Set(records.map((record) => record.id))
      const next = new Set(Array.from(current).filter((id) => ids.has(id)))
      return next.size === current.size ? current : next
    })
  }, [records])

  const allSelected = records.length > 0 && records.every((record) => selected.has(record.id))
  const someSelected = records.some((record) => selected.has(record.id))

  React.useEffect(() => {
    if (selectAllRef.current) {
      selectAllRef.current.indeterminate = someSelected && !allSelected
    }
  }, [allSelected, someSelected])

  const toggleColumn = (key: string) => {
    setHiddenColumns((current) => {
      const next = current.includes(key) ? current.filter((item) => item !== key) : [...current, key]
      try {
        window.localStorage.setItem(`${COLUMN_PREFS_PREFIX}${collection.id}`, JSON.stringify(next))
      } catch {
        // ignore localStorage errors
      }
      return next
    })
  }

  const toggleSelected = (id: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleSelectAll = () => {
    setSelected(allSelected ? new Set() : new Set(records.map((record) => record.id)))
  }

  const sortField = sort.startsWith('-') ? sort.slice(1) : sort
  const sortDir: 'asc' | 'desc' = sort.startsWith('-') ? 'desc' : 'asc'

  const handleSort = (key: string) => {
    if (sortField === key) {
      onSortChange(sortDir === 'asc' ? `-${key}` : key)
    } else {
      onSortChange(key)
    }
  }

  const handleSingleDelete = async () => {
    if (!deleteTarget) return
    setSingleDeleting(true)
    try {
      await api.deleteRecord(collection.name, deleteTarget.id)
      toast.success('Record deleted')
      setSelected((current) => {
        const next = new Set(current)
        next.delete(deleteTarget.id)
        return next
      })
      setDeleteTarget(null)
      await onRefresh()
    } catch (err: any) {
      toast.error(err.message || 'Failed to delete record')
    } finally {
      setSingleDeleting(false)
    }
  }

  const handleBulkDelete = async () => {
    const ids = Array.from(selected)
    if (!ids.length) return
    setBulkDeleting(true)
    try {
      await api.batchRecords(collection.name, { deletes: ids })
      toast.success(`Deleted ${ids.length} record${ids.length !== 1 ? 's' : ''}`)
      setSelected(new Set())
      setBulkConfirmOpen(false)
      await onRefresh()
    } catch (err: any) {
      toast.error(err.message || 'Bulk delete failed')
    } finally {
      setBulkDeleting(false)
    }
  }

  const showEmptyState = !loading && records.length === 0

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-end">
        <DropdownMenu
          trigger={
            <Button variant="outline" size="sm">
              <Eye className="mr-2 h-4 w-4" />
              Columns
            </Button>
          }
        >
          <div className="px-2.5 py-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
            Visible columns
          </div>
          <label className="flex w-full cursor-not-allowed items-center gap-2 rounded-md px-2.5 py-1.5 text-sm text-muted-foreground">
            <input type="checkbox" checked disabled className="h-3.5 w-3.5" />
            id
          </label>
          {columns.map((column) => (
            <label
              key={column.key}
              className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2.5 py-1.5 text-sm hover:bg-accent/50"
            >
              <input
                type="checkbox"
                checked={!hiddenColumns.includes(column.key)}
                onChange={() => toggleColumn(column.key)}
                className="h-3.5 w-3.5"
              />
              <span className="truncate">{column.label}</span>
            </label>
          ))}
        </DropdownMenu>
      </div>

      {selected.size > 0 ? (
        <div className="sticky top-2 z-20 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-primary/30 bg-background/95 px-4 py-2 text-sm shadow-lg backdrop-blur">
          <span className="font-medium">
            {selected.size} selected
          </span>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
              Clear selection
            </Button>
            <Button variant="destructive" size="sm" onClick={() => setBulkConfirmOpen(true)}>
              <Trash2 className="mr-2 h-4 w-4" />
              Delete
            </Button>
          </div>
        </div>
      ) : null}

      {showEmptyState ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-border py-16 text-center">
          {hasActiveFilter ? (
            <>
              <FilterX className="mb-4 h-10 w-10 text-muted-foreground opacity-50" />
              <h3 className="text-base font-medium">No records match your filter</h3>
              <p className="mt-1 max-w-sm text-sm text-muted-foreground">
                Try adjusting the expression, or clear it to see everything again.
              </p>
              <Button variant="outline" size="sm" className="mt-4" onClick={onClearFilter}>
                Clear filter
              </Button>
            </>
          ) : (
            <>
              <Database className="mb-4 h-10 w-10 text-muted-foreground opacity-50" />
              <h3 className="text-base font-medium">No records yet</h3>
              <p className="mt-1 max-w-sm text-sm text-muted-foreground">
                {collection.type === 'view'
                  ? 'This view has no rows to show right now.'
                  : 'This collection is empty. Create the first record to see it here.'}
              </p>
              {collection.type !== 'view' ? (
                <Button size="sm" className="mt-4" onClick={onCreate}>
                  <Plus className="mr-2 h-4 w-4" />
                  Create your first record
                </Button>
              ) : null}
            </>
          )}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full min-w-[720px] text-sm">
            <thead className="bg-accent/20 text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                {selectable ? (
                  <th className="w-10 px-3 py-3">
                    <input
                      ref={selectAllRef}
                      type="checkbox"
                      checked={allSelected}
                      onChange={toggleSelectAll}
                      disabled={loading || records.length === 0}
                      className="h-4 w-4"
                      aria-label="Select all records on this page"
                    />
                  </th>
                ) : null}
                <SortableHeader label="id" columnKey="id" sortField={sortField} sortDir={sortDir} onSort={handleSort} />
                {visibleColumns.map((column) => (
                  <SortableHeader
                    key={column.key}
                    label={column.label}
                    columnKey={column.key}
                    sortField={sortField}
                    sortDir={sortDir}
                    onSort={handleSort}
                  />
                ))}
                <th className="w-10 px-2 py-3" aria-label="Row actions" />
              </tr>
            </thead>
            <tbody>
              {loading
                ? Array.from({ length: 8 }).map((_, rowIndex) => (
                    <tr key={rowIndex} className="border-t border-border">
                      {selectable ? (
                        <td className="px-3 py-3">
                          <Skeleton className="h-4 w-4" />
                        </td>
                      ) : null}
                      <td className="px-4 py-3">
                        <Skeleton className="h-4 w-20" />
                      </td>
                      {visibleColumns.map((column, columnIndex) => (
                        <td key={column.key} className="px-4 py-3">
                          <Skeleton className={cn('h-4', SKELETON_WIDTHS[(rowIndex + columnIndex) % SKELETON_WIDTHS.length])} />
                        </td>
                      ))}
                      <td className="px-2 py-3">
                        <Skeleton className="h-4 w-5" />
                      </td>
                    </tr>
                  ))
                : records.map((record) => (
                    <tr
                      key={record.id}
                      onClick={() => onRowClick(record)}
                      className={cn(
                        'cursor-pointer border-t border-border transition-colors hover:bg-accent/10',
                        selected.has(record.id) && 'bg-accent/20 hover:bg-accent/25'
                      )}
                    >
                      {selectable ? (
                        <td className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                          <input
                            type="checkbox"
                            checked={selected.has(record.id)}
                            onChange={() => toggleSelected(record.id)}
                            className="h-4 w-4"
                            aria-label={`Select record ${record.id}`}
                          />
                        </td>
                      ) : null}
                      <td className="px-4 py-3 font-mono text-xs text-muted-foreground" title={record.id}>
                        {truncate(record.id, 16)}
                      </td>
                      {visibleColumns.map((column) => (
                        <td key={column.key} className="px-4 py-3 align-top">
                          {renderRecordFieldValue(record[column.key], column.field, record, collections, collection)}
                        </td>
                      ))}
                      <td className="px-2 py-2 text-right" onClick={(event) => event.stopPropagation()}>
                        <DropdownMenu
                          width="w-44"
                          trigger={
                            <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="Record actions">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          }
                        >
                          {(close) => (
                            <>
                              <DropdownMenuItem
                                icon={collection.type === 'view' ? <Eye className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
                                onSelect={() => {
                                  close()
                                  onEdit(record)
                                }}
                              >
                                {collection.type === 'view' ? 'View' : 'Edit'}
                              </DropdownMenuItem>
                              {collection.type !== 'view' ? (
                                <>
                                  <DropdownMenuItem
                                    icon={<CopyPlus className="h-4 w-4" />}
                                    onSelect={() => {
                                      close()
                                      onDuplicate(record)
                                    }}
                                  >
                                    Duplicate
                                  </DropdownMenuItem>
                                  <DropdownMenuItem
                                    destructive
                                    icon={<Trash2 className="h-4 w-4" />}
                                    onSelect={() => {
                                      close()
                                      setDeleteTarget(record)
                                    }}
                                  >
                                    Delete
                                  </DropdownMenuItem>
                                </>
                              ) : null}
                            </>
                          )}
                        </DropdownMenu>
                      </td>
                    </tr>
                  ))}
            </tbody>
          </table>
        </div>
      )}

      <AlertDialog
        open={Boolean(deleteTarget)}
        title="Delete record?"
        description={
          <>
            This will permanently delete <code className="font-mono text-xs text-foreground">{deleteTarget?.id}</code>.
            This cannot be undone.
          </>
        }
        confirmLabel="Delete"
        destructive
        loading={singleDeleting}
        onConfirm={handleSingleDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      <AlertDialog
        open={bulkConfirmOpen}
        title={`Delete ${selected.size} record${selected.size === 1 ? '' : 's'}?`}
        description="All selected records will be permanently deleted in a single transaction. This cannot be undone."
        confirmLabel={`Delete ${selected.size}`}
        destructive
        loading={bulkDeleting}
        onConfirm={handleBulkDelete}
        onCancel={() => setBulkConfirmOpen(false)}
      />
    </div>
  )
}

function SortableHeader({
  label,
  columnKey,
  sortField,
  sortDir,
  onSort,
}: {
  label: string
  columnKey: string
  sortField: string
  sortDir: 'asc' | 'desc'
  onSort: (key: string) => void
}) {
  const active = sortField === columnKey
  return (
    <th className="px-4 py-3">
      <button
        type="button"
        onClick={() => onSort(columnKey)}
        className={cn(
          'inline-flex items-center gap-1 uppercase tracking-wide transition-colors hover:text-foreground',
          active && 'text-foreground'
        )}
        title={`Sort by ${label}`}
      >
        {label}
        {active ? (
          sortDir === 'asc' ? (
            <ChevronUp className="h-3.5 w-3.5" />
          ) : (
            <ChevronDown className="h-3.5 w-3.5" />
          )
        ) : null}
      </button>
    </th>
  )
}

function truncate(value: string, limit = 16) {
  return value.length > limit ? `${value.slice(0, limit)}…` : value
}
