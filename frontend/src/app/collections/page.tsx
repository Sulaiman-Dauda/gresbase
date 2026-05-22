'use client'

import { useEffect, useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  Plus, Database, Trash2, Eye, Pencil,
  Loader2, Search, ArrowUpDown, Table2,
  Columns, Filter, ChevronRight
} from 'lucide-react'
import Link from 'next/link'

interface CollectionData {
  id: string
  name: string
  type: string
  schema: Array<{ id: string; name: string; type: string; required?: boolean }>
  list_rule?: string
  create_rule?: string
  update_rule?: string
  delete_rule?: string
  system?: boolean
  created_at: string
  updated_at: string
}

export default function CollectionsPage() {
  const [collections, setCollections] = useState<CollectionData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [selectedType, setSelectedType] = useState('base')

  const fetchCollections = useCallback(async () => {
    setLoading(true)
    try {
      const data = await api.getCollections()
      setCollections(Array.isArray(data) ? data : [])
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchCollections() }, [fetchCollections])

  const handleCreate = async () => {
    if (!newName.trim()) return
    setCreating(true)
    try {
      await api.createCollection({
        name: newName.trim(),
        type: selectedType,
        schema: [
          { name: 'title', type: 'text', required: true },
          { name: 'description', type: 'text' },
        ],
      })
      setNewName('')
      setSelectedType('base')
      fetchCollections()
    } catch (err: any) {
      setError(err.message)
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (id: string, name: string) => {
    if (!confirm(`Delete collection "${name}" and all its records? This cannot be undone.`)) return
    try {
      await api.deleteCollection(id)
      fetchCollections()
    } catch (err: any) {
      setError(err.message)
    }
  }

  const filtered = collections.filter(c =>
    c.name.toLowerCase().includes(search.toLowerCase())
  )

  const typeColors: Record<string, string> = {
    base: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
    auth: 'bg-purple-500/10 text-purple-500 border-purple-500/20',
    view: 'bg-emerald-500/10 text-emerald-500 border-emerald-500/20',
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Collections</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {collections.length} collection{collections.length !== 1 ? 's' : ''} — manage your database tables
          </p>
        </div>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3">
            <p className="text-sm text-red-500">{error}</p>
          </CardContent>
        </Card>
      )}

      {/* Quick Create */}
      <Card>
        <CardContent className="flex items-end gap-3 pt-6">
          <div className="flex-1 space-y-2">
            <label className="text-xs font-medium text-muted-foreground">Create new collection</label>
            <Input
              placeholder="Collection name (e.g. posts, products)"
              value={newName}
              onChange={e => setNewName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleCreate()}
            />
          </div>
          <div className="space-y-2">
            <label className="text-xs font-medium text-muted-foreground">Type</label>
            <select
              value={selectedType}
              onChange={e => setSelectedType(e.target.value)}
              className="h-9 rounded-lg border border-border bg-background px-3 text-sm"
            >
              <option value="base">Base</option>
              <option value="auth">Auth</option>
              <option value="view">View</option>
            </select>
          </div>
          <Button onClick={handleCreate} disabled={creating || !newName.trim()} size="sm">
            {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
            Create
          </Button>
        </CardContent>
      </Card>

      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Filter collections..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>

      {/* Collection List */}
      <div className="space-y-2">
        {filtered.length === 0 && (
          <Card>
            <CardContent className="flex flex-col items-center justify-center py-12 text-center">
              <Database className="h-12 w-12 text-muted-foreground opacity-50 mb-4" />
              <h3 className="text-lg font-medium">No collections yet</h3>
              <p className="text-sm text-muted-foreground mt-1 max-w-sm">
                Create your first collection above to start storing data.
                Collections are PostgreSQL-backed tables with dynamic schemas.
              </p>
            </CardContent>
          </Card>
        )}

        {filtered.map((coll) => (
          <Card key={coll.id} className="transition-all hover:border-primary/30">
            <CardContent className="flex items-center gap-4 py-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
                {coll.type === 'view' ? (
                  <Eye className="h-5 w-5 text-primary" />
                ) : coll.type === 'auth' ? (
                  <ShieldIcon className="h-5 w-5 text-primary" />
                ) : (
                  <Table2 className="h-5 w-5 text-primary" />
                )}
              </div>

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <h3 className="font-medium truncate">{coll.name}</h3>
                  <Badge variant="outline" className={typeColors[coll.type] || ''}>
                    {coll.type}
                  </Badge>
                  {coll.system && (
                    <Badge variant="secondary" className="text-[10px]">system</Badge>
                  )}
                </div>
                <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                  <span className="flex items-center gap-1">
                    <Columns className="h-3 w-3" />
                    {coll.schema?.length || 0} fields
                  </span>
                  {coll.list_rule && (
                    <span className="flex items-center gap-1">
                      <Filter className="h-3 w-3" />
                      Rules set
                    </span>
                  )}
                  <span>Created {new Date(coll.created_at).toLocaleDateString()}</span>
                </div>
              </div>

              <div className="flex items-center gap-1">
                <Link href={`/collections/${coll.id}`}>
                  <Button variant="ghost" size="icon" title="View records">
                    <Eye className="h-4 w-4" />
                  </Button>
                </Link>
                <Button variant="ghost" size="icon" title="Edit schema">
                  <Pencil className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => handleDelete(coll.id, coll.name)}
                  title="Delete collection"
                  disabled={coll.system}
                >
                  <Trash2 className="h-4 w-4 text-red-500" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

function ShieldIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
    </svg>
  )
}
