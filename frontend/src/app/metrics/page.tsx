'use client'

import { useEffect, useState, useCallback } from 'react'
import { AppLayout } from '@/components/layout/app-layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  Activity, Database, Server, Wifi, Loader2, RefreshCw,
} from 'lucide-react'

interface MetricsReport {
  status: string
  version: string
  timestamp: string
  database: {
    status: string
    mode: string
    open_connections?: number
    acquired_connections?: number
    idle_connections?: number
    max_connections?: number
    acquire_count?: number
    acquire_duration_ms?: number
  }
  realtime: { clients: number }
  system: { go_version: string; num_goroutines: number }
}

export default function MetricsPage() {
  const [report, setReport] = useState<MetricsReport | null>(null)
  const [collections, setCollections] = useState<number>(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null)

  const fetchMetrics = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [metrics, colls] = await Promise.all([
        api.metrics(),
        api.getCollections().catch(() => []),
      ])
      setReport(metrics)
      setCollections(Array.isArray(colls) ? colls.length : 0)
      setLastRefreshed(new Date())
    } catch (err: any) {
      setError(err.message || 'Failed to load metrics')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchMetrics()
    const interval = setInterval(fetchMetrics, 10_000)
    return () => clearInterval(interval)
  }, [fetchMetrics])

  if (loading && !report) {
    return (
      <AppLayout>
        <div className="flex min-h-[60vh] items-center justify-center">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </AppLayout>
    )
  }

  const db = report?.database

  return (
    <AppLayout>
      <div className="animate-fade-in space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">Metrics</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Live server health{lastRefreshed ? ` — refreshed ${lastRefreshed.toLocaleTimeString()}` : ''} (auto-refreshes every 10s)
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={fetchMetrics} disabled={loading}>
            <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
        </div>

        {error ? (
          <Card className="border-red-500/50 bg-red-500/5">
            <CardContent className="py-3"><p className="text-sm text-red-500">{error}</p></CardContent>
          </Card>
        ) : null}

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard
            icon={<Server className="h-5 w-5 text-emerald-500" />}
            label="Server"
            value={report?.status === 'healthy' ? 'Healthy' : report?.status || 'Unknown'}
            detail={`v${report?.version || '?'} · ${report?.system.go_version || ''}`}
          />
          <StatCard
            icon={<Database className="h-5 w-5 text-blue-500" />}
            label="Database"
            value={db?.status === 'connected' ? 'Connected' : db?.status || 'Unknown'}
            detail={db?.mode === 'embedded' ? 'Embedded PostgreSQL' : 'External PostgreSQL'}
          />
          <StatCard
            icon={<Wifi className="h-5 w-5 text-violet-500" />}
            label="Realtime clients"
            value={String(report?.realtime.clients ?? 0)}
            detail="Connected SSE/WebSocket clients"
          />
          <StatCard
            icon={<Activity className="h-5 w-5 text-amber-500" />}
            label="Goroutines"
            value={String(report?.system.num_goroutines ?? 0)}
            detail={`${collections} collection${collections === 1 ? '' : 's'}`}
          />
        </div>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Connection pool</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {[
                ['Open connections', db?.open_connections],
                ['Acquired', db?.acquired_connections],
                ['Idle', db?.idle_connections],
                ['Max connections', db?.max_connections],
                ['Total acquires', db?.acquire_count],
                ['Avg acquire (ms)', db?.acquire_duration_ms != null ? Number(db.acquire_duration_ms).toFixed(2) : undefined],
              ].map(([label, value]) => (
                <div key={String(label)} className="flex items-center justify-between rounded-lg bg-accent/50 px-3 py-2">
                  <span className="text-sm text-muted-foreground">{label}</span>
                  <span className="font-mono text-sm font-medium">{value ?? '—'}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Badge variant="outline" className="text-[10px]">GET /api/v1/metrics</Badge>
          All values come directly from the server — nothing here is simulated.
        </div>
      </div>
    </AppLayout>
  )
}

function StatCard({ icon, label, value, detail }: { icon: React.ReactNode; label: string; value: string; detail?: string }) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3 py-4">
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-muted">{icon}</div>
        <div className="min-w-0">
          <div className="truncate text-lg font-bold">{value}</div>
          <div className="text-xs text-muted-foreground">{label}</div>
          {detail ? <div className="truncate text-[11px] text-muted-foreground/70">{detail}</div> : null}
        </div>
      </CardContent>
    </Card>
  )
}
