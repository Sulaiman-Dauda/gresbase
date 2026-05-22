'use client'

import { useEffect, useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  BarChart3, Activity, Database, Wifi, HardDrive, Server,
  Loader2, RefreshCw, TrendingUp, Clock, Zap, Users,
  MessageSquare, ArrowUp, ArrowDown, Minus
} from 'lucide-react'

interface MetricsData {
  collections: number
  records: number
  admins: number
  apikeys: number
  realtime_clients: number
  storage_files: number
  storage_bytes: number
  certificates: number
  uptime_seconds: number
  requests_total: number
  requests_last_hour: number
  avg_response_ms: number
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function TrendBadge({ value, unit }: { value: number; unit?: string }) {
  const trend = value > 0 ? 'up' : value < 0 ? 'down' : 'flat'
  return (
    <span className={`inline-flex items-center gap-1 text-xs ${
      trend === 'up' ? 'text-emerald-500' : trend === 'down' ? 'text-red-500' : 'text-muted-foreground'
    }`}>
      {trend === 'up' ? <ArrowUp className="h-3 w-3" /> :
       trend === 'down' ? <ArrowDown className="h-3 w-3" /> :
       <Minus className="h-3 w-3" />}
      {Math.abs(value)}{unit || ''}
    </span>
  )
}

export default function MetricsPage() {
  const [metrics, setMetrics] = useState<MetricsData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchMetrics = useCallback(async () => {
    setLoading(true)
    try {
      const [health, collections, admins, apiKeys, certs] = await Promise.all([
        api.health().catch(() => ({})),
        api.getCollections().catch(() => []),
        api.getAdmins().catch(() => []),
        api.getApiKeys().catch(() => []),
        api.getCertificates().catch(() => []),
      ])

      setMetrics({
        collections: Array.isArray(collections) ? collections.length : 0,
        records: 0,
        admins: Array.isArray(admins) ? admins.length : 0,
        apikeys: Array.isArray(apiKeys) ? apiKeys.length : 0,
        realtime_clients: 0,
        storage_files: 0,
        storage_bytes: 0,
        certificates: Array.isArray(certs) ? certs.length : 0,
        uptime_seconds: 0,
        requests_total: 0,
        requests_last_hour: 0,
        avg_response_ms: 0,
      })
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchMetrics() }, [fetchMetrics])

  useEffect(() => {
    const interval = setInterval(fetchMetrics, 30000)
    return () => clearInterval(interval)
  }, [fetchMetrics])

  if (loading && !metrics) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const m = metrics!

  const cards = [
    { label: 'Collections', value: m.collections, icon: Database, color: 'text-blue-500', bg: 'bg-blue-500/10' },
    { label: 'Admin Users', value: m.admins, icon: Users, color: 'text-emerald-500', bg: 'bg-emerald-500/10' },
    { label: 'API Keys', value: m.apikeys, icon: Activity, color: 'text-amber-500', bg: 'bg-amber-500/10' },
    { label: 'Certificates', value: m.certificates, icon: Server, color: 'text-purple-500', bg: 'bg-purple-500/10' },
    { label: 'Realtime Clients', value: m.realtime_clients, icon: Wifi, color: 'text-cyan-500', bg: 'bg-cyan-500/10' },
    { label: 'Storage Used', value: formatBytes(m.storage_bytes), icon: HardDrive, color: 'text-pink-500', bg: 'bg-pink-500/10' },
    { label: 'Uptime', value: formatUptime(m.uptime_seconds), icon: Clock, color: 'text-indigo-500', bg: 'bg-indigo-500/10' },
    { label: 'Requests/hr', value: m.requests_last_hour, icon: Zap, color: 'text-orange-500', bg: 'bg-orange-500/10' },
  ]

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Metrics</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Real-time platform metrics and resource usage
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={fetchMetrics} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3"><p className="text-sm text-red-500">{error}</p></CardContent>
        </Card>
      )}

      {/* Metric Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {cards.map((card) => (
          <Card key={card.label}>
            <CardContent className="flex items-start gap-4 pt-6">
              <div className={`flex h-10 w-10 items-center justify-center rounded-lg ${card.bg}`}>
                <card.icon className={`h-5 w-5 ${card.color}`} />
              </div>
              <div>
                <div className="text-2xl font-bold">{card.value}</div>
                <div className="text-sm text-muted-foreground">{card.label}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Performance Charts (Placeholder) */}
      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <TrendingUp className="h-4 w-4" />
              Request Volume
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-end justify-between h-32 gap-1">
              {Array.from({ length: 24 }).map((_, i) => {
                const height = Math.max(8, Math.random() * 100 + 10)
                return (
                  <div
                    key={i}
                    className="flex-1 rounded-t bg-primary/30 hover:bg-primary/50 transition-colors"
                    style={{ height: `${height}%` }}
                    title={`Hour ${i}: ~${Math.round(height * m.requests_last_hour / 100)} req`}
                  />
                )
              })}
            </div>
            <div className="flex justify-between mt-2 text-[10px] text-muted-foreground">
              <span>00:00</span>
              <span>06:00</span>
              <span>12:00</span>
              <span>18:00</span>
              <span>Now</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <MessageSquare className="h-4 w-4" />
              Response Time
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              <div>
                <div className="flex justify-between text-sm mb-1">
                  <span>Average</span>
                  <span className="font-mono">{m.avg_response_ms}ms</span>
                </div>
                <div className="h-2 rounded-full bg-muted overflow-hidden">
                  <div
                    className="h-full rounded-full bg-emerald-500 transition-all"
                    style={{ width: `${Math.min(100, m.avg_response_ms / 10)}%` }}
                  />
                </div>
              </div>
              <div className="grid grid-cols-3 gap-4 pt-2">
                <div className="text-center">
                  <div className="text-lg font-bold text-emerald-500">—</div>
                  <div className="text-[10px] text-muted-foreground">p50</div>
                </div>
                <div className="text-center">
                  <div className="text-lg font-bold text-amber-500">—</div>
                  <div className="text-[10px] text-muted-foreground">p95</div>
                </div>
                <div className="text-center">
                  <div className="text-lg font-bold text-red-500">—</div>
                  <div className="text-[10px] text-muted-foreground">p99</div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Database Stats */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Database className="h-4 w-4" />
            Database
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="space-y-1">
              <div className="text-xs text-muted-foreground">Engine</div>
              <div className="font-medium">PostgreSQL 16</div>
            </div>
            <div className="space-y-1">
              <div className="text-xs text-muted-foreground">Connections</div>
              <div className="font-medium">— active</div>
            </div>
            <div className="space-y-1">
              <div className="text-xs text-muted-foreground">Size</div>
              <div className="font-medium">—</div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
