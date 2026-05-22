'use client'

import { useEffect, useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  ScrollText, Search, Filter, Loader2, RefreshCw,
  Clock, User, Database, Shield, AlertCircle,
  CheckCircle, XCircle, ChevronDown
} from 'lucide-react'

interface LogEntry {
  id: number
  tenant_id?: string
  admin_id?: string
  action: string
  resource: string
  resource_id?: string
  data?: any
  ip?: string
  user_agent?: string
  created_at: string
}

const ACTION_COLORS: Record<string, string> = {
  'auth.login': 'bg-emerald-500/10 text-emerald-500',
  'auth.login.failed': 'bg-red-500/10 text-red-500',
  'auth.logout': 'bg-slate-500/10 text-slate-400',
  'auth.oauth': 'bg-blue-500/10 text-blue-500',
  'record.create': 'bg-emerald-500/10 text-emerald-500',
  'record.update': 'bg-amber-500/10 text-amber-500',
  'record.delete': 'bg-red-500/10 text-red-500',
  'collection.create': 'bg-purple-500/10 text-purple-500',
  'collection.delete': 'bg-red-500/10 text-red-500',
  'certificate.issue': 'bg-indigo-500/10 text-indigo-500',
  'backup.create': 'bg-cyan-500/10 text-cyan-500',
}

export default function LogsPage() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [page, setPage] = useState(1)

  const fetchLogs = useCallback(async () => {
    setLoading(true)
    try {
      const params: Record<string, string> = { page: String(page), perPage: '50' }
      if (actionFilter) params.filter = `action = "${actionFilter}"`
      const data = await api.getLogs()
      if (data && Array.isArray(data.items)) {
        setLogs(data.items)
      } else if (Array.isArray(data)) {
        setLogs(data)
      }
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [page, actionFilter])

  useEffect(() => { fetchLogs() }, [fetchLogs])

  const filtered = logs.filter(log =>
    !search ||
    log.action.toLowerCase().includes(search.toLowerCase()) ||
    log.resource.toLowerCase().includes(search.toLowerCase()) ||
    (log.admin_id && log.admin_id.includes(search)) ||
    (log.ip && log.ip.includes(search))
  )

  const getActionBadge = (action: string) => {
    const color = ACTION_COLORS[action] || 'bg-muted text-muted-foreground'
    return <Badge variant="outline" className={color}>{action}</Badge>
  }

  const getStatusIcon = (action: string) => {
    if (action.includes('failed') || action.includes('error')) {
      return <XCircle className="h-4 w-4 text-red-500" />
    }
    if (action.includes('login') || action.includes('delete')) {
      return <AlertCircle className="h-4 w-4 text-amber-500" />
    }
    return <CheckCircle className="h-4 w-4 text-emerald-500" />
  }

  const actions = [...new Set(logs.map(l => l.action))].sort()

  if (loading && logs.length === 0) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Audit Logs</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {logs.length} entries — track all platform activity
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={fetchLogs} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3"><p className="text-sm text-red-500">{error}</p></CardContent>
        </Card>
      )}

      {/* Filters */}
      <div className="flex gap-3">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search logs by action, resource, IP or user ID..."
            value={search}
            onChange={e => setSearch(e.target.value)}
            className="pl-9"
          />
        </div>
        <select
          value={actionFilter}
          onChange={e => { setActionFilter(e.target.value); setPage(1) }}
          className="h-9 rounded-lg border border-border bg-background px-3 text-sm min-w-[160px]"
        >
          <option value="">All Actions</option>
          {actions.map(a => (
            <option key={a} value={a}>{a}</option>
          ))}
        </select>
      </div>

      {/* Log Entries */}
      <div className="space-y-1">
        {filtered.length === 0 && (
          <Card>
            <CardContent className="flex flex-col items-center py-12 text-center">
              <ScrollText className="h-12 w-12 text-muted-foreground opacity-50 mb-4" />
              <h3 className="text-lg font-medium">No logs found</h3>
              <p className="text-sm text-muted-foreground mt-1">
                {search ? 'No logs match your search.' : 'Activity will appear here as users interact with the platform.'}
              </p>
            </CardContent>
          </Card>
        )}

        {filtered.map((log) => (
          <div
            key={log.id}
            className="flex items-center gap-3 rounded-lg border border-border bg-card px-4 py-3 hover:bg-accent/30 transition-colors"
          >
            {getStatusIcon(log.action)}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                {getActionBadge(log.action)}
                <span className="text-sm font-medium">
                  {log.resource}
                  {log.resource_id && (
                    <code className="ml-1 text-[10px] bg-muted px-1 rounded font-mono">
                      {log.resource_id.slice(0, 8)}
                    </code>
                  )}
                </span>
              </div>
              {log.data && Object.keys(log.data).length > 0 && (
                <div className="text-xs text-muted-foreground mt-1">
                  {JSON.stringify(log.data).slice(0, 100)}
                </div>
              )}
            </div>
            <div className="flex items-center gap-4 text-xs text-muted-foreground shrink-0">
              {log.ip && (
                <span className="flex items-center gap-1">
                  <Shield className="h-3 w-3" />
                  {log.ip}
                </span>
              )}
              {log.admin_id && (
                <span className="flex items-center gap-1">
                  <User className="h-3 w-3" />
                  {log.admin_id.slice(0, 8)}
                </span>
              )}
              <span className="flex items-center gap-1">
                <Clock className="h-3 w-3" />
                {new Date(log.created_at).toLocaleString()}
              </span>
            </div>
          </div>
        ))}
      </div>

      {/* Pagination */}
      {logs.length >= 50 && (
        <div className="flex items-center justify-between">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPage(p => Math.max(1, p - 1))}
            disabled={page === 1}
          >
            Previous
          </Button>
          <span className="text-sm text-muted-foreground">Page {page}</span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPage(p => p + 1)}
          >
            Next
          </Button>
        </div>
      )}
    </div>
  )
}
