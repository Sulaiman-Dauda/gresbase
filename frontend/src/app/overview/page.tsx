'use client'

import { useEffect, useState, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  Database, Users, Activity, FileKey, HardDrive,
  Server, Shield, Key, ArrowUpRight, Loader2,
  Zap, Clock, Globe, RefreshCw, TrendingUp
} from 'lucide-react'
import Link from 'next/link'

interface Stats {
  collections: number
  users: number
  apiKeys: number
  certificates: number
  storageFiles: number
  realtimeClients: number
  logsToday: number
}

export default function OverviewPage() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [loading, setLoading] = useState(true)
  const [health, setHealth] = useState<any>(null)
  const [error, setError] = useState<string | null>(null)
  const router = useRouter()

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [healthData, collections, admins, apiKeys, certs] = await Promise.all([
        api.health().catch(() => ({ status: 'degraded', database: false })),
        api.getCollections().catch(() => []),
        api.getAdmins().catch(() => []),
        api.getApiKeys().catch(() => []),
        api.getCertificates().catch(() => []),
      ])

      setHealth(healthData)
      setStats({
        collections: Array.isArray(collections) ? collections.length : 0,
        users: Array.isArray(admins) ? admins.length : 0,
        apiKeys: Array.isArray(apiKeys) ? apiKeys.length : 0,
        certificates: Array.isArray(certs) ? certs.length : 0,
        storageFiles: 0,
        realtimeClients: 0,
        logsToday: 0,
      })
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const statCards = [
    { label: 'Collections', value: stats?.collections ?? '—', icon: Database, href: '/collections', color: 'text-blue-500' },
    { label: 'Admin Users', value: stats?.users ?? '—', icon: Users, href: '/users', color: 'text-emerald-500' },
    { label: 'API Keys', value: stats?.apiKeys ?? '—', icon: Key, href: '/api-keys', color: 'text-amber-500' },
    { label: 'Certificates', value: stats?.certificates ?? '—', icon: FileKey, href: '/certificates', color: 'text-purple-500' },
  ]

  const statusCards = [
    { label: 'Server Status', value: health?.status || 'connecting...', icon: Server, color: health?.status === 'healthy' ? 'text-emerald-500' : 'text-amber-500' },
    { label: 'Database', value: health?.database ? 'Connected' : 'Disconnected', icon: Database, color: health?.database ? 'text-emerald-500' : 'text-red-500' },
    { label: 'Version', value: health?.version || '0.2.0', icon: Zap, color: 'text-blue-500' },
    { label: 'Uptime', value: '—', icon: Clock, color: 'text-slate-500' },
  ]

  const quickActions = [
    { label: 'Create Collection', href: '/collections', icon: Database, description: 'Define a new database collection' },
    { label: 'Issue Certificate', href: '/certificates', icon: FileKey, description: 'Issue TLS cert via ACME CA' },
    { label: 'Create API Key', href: '/api-keys', icon: Key, description: 'Generate a new API key' },
    { label: 'View API Docs', href: '/apis', icon: Globe, description: 'Explore REST API endpoints' },
  ]

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-8 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Overview</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Your Gresbase platform at a glance
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={fetchData} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3">
            <p className="text-sm text-red-500">Failed to load data: {error}</p>
          </CardContent>
        </Card>
      )}

      {/* Server Status */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {statusCards.map((card) => (
          <Card key={card.label}>
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">
                {card.label}
              </CardTitle>
              <card.icon className={`h-4 w-4 ${card.color}`} />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold capitalize">{card.value}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Stats Grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {statCards.map((card) => (
          <Link key={card.label} href={card.href}>
            <Card className="transition-all hover:border-primary/50 hover:shadow-sm cursor-pointer">
              <CardHeader className="flex flex-row items-center justify-between pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">
                  {card.label}
                </CardTitle>
                <card.icon className={`h-4 w-4 ${card.color}`} />
              </CardHeader>
              <CardContent>
                <div className="flex items-center justify-between">
                  <div className="text-2xl font-bold">{card.value}</div>
                  <ArrowUpRight className="h-4 w-4 text-muted-foreground opacity-50" />
                </div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>

      {/* Quick Actions */}
      <div>
        <h2 className="text-lg font-semibold mb-4">Quick Actions</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          {quickActions.map((action) => (
            <Link key={action.label} href={action.href}>
              <Card className="transition-all hover:border-primary/50 hover:shadow-sm cursor-pointer h-full">
                <CardContent className="flex flex-col gap-2 pt-6">
                  <action.icon className="h-8 w-8 text-primary" />
                  <div>
                    <div className="font-medium">{action.label}</div>
                    <p className="text-xs text-muted-foreground mt-1">{action.description}</p>
                  </div>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      </div>

      {/* Realtime Status */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Activity className="h-4 w-4 text-emerald-500" />
            Realtime Engine
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-4 text-sm">
            <Badge variant="outline" className="gap-1">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
              </span>
              SSE Active
            </Badge>
            <Badge variant="outline" className="gap-1">
              <Zap className="h-3 w-3" />
              WebSocket Ready
            </Badge>
            <span className="text-muted-foreground">
              Connect at <code className="text-xs bg-muted px-1 py-0.5 rounded">/api/v1/realtime</code>
            </span>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
