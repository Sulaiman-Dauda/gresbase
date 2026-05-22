'use client'

import { useEffect, useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  Key, Plus, Trash2, Copy, Eye, EyeOff, Loader2,
  Clock, Shield, CheckCircle, XCircle, Code
} from 'lucide-react'

interface ApiKeyData {
  id: string
  name: string
  prefix: string
  created_at: string
  key?: string
}

export default function ApiKeysPage() {
  const [keys, setKeys] = useState<ApiKeyData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)
  const [newKey, setNewKey] = useState<string | null>(null)
  const [showKey, setShowKey] = useState<Record<string, boolean>>({})
  const [copied, setCopied] = useState<string | null>(null)

  const fetchKeys = useCallback(async () => {
    setLoading(true)
    try {
      const data = await api.getApiKeys()
      setKeys(Array.isArray(data) ? data : [])
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchKeys() }, [fetchKeys])

  const handleCreate = async () => {
    if (!newName.trim()) return
    setCreating(true)
    try {
      const result = await api.createApiKey(newName.trim())
      setNewName('')
      setShowCreate(false)
      setNewKey(result.key || null)
      fetchKeys()
    } catch (err: any) {
      setError(err.message)
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this API key? All clients using it will lose access.')) return
    try {
      await api.deleteApiKey(id)
      fetchKeys()
    } catch (err: any) {
      setError(err.message)
    }
  }

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(text)
      setTimeout(() => setCopied(null), 2000)
    })
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
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">API Keys</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {keys.length} key{keys.length !== 1 ? 's' : ''} — manage API access tokens
          </p>
        </div>
        <Button onClick={() => setShowCreate(!showCreate)} size="sm">
          <Plus className="mr-2 h-4 w-4" />
          Generate Key
        </Button>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3 flex items-center justify-between">
            <p className="text-sm text-red-500">{error}</p>
            <Button variant="ghost" size="sm" onClick={() => setError(null)}>
              <XCircle className="h-4 w-4" />
            </Button>
          </CardContent>
        </Card>
      )}

      {/* New Key Display */}
      {newKey && (
        <Card className="border-emerald-500/50 bg-emerald-500/5">
          <CardContent className="py-4 space-y-3">
            <div className="flex items-center gap-2">
              <CheckCircle className="h-5 w-5 text-emerald-500" />
              <span className="font-medium text-emerald-600">API Key Generated</span>
            </div>
            <p className="text-sm text-muted-foreground">
              Copy this key now. You won't be able to see it again.
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 rounded-lg bg-muted px-3 py-2 text-sm font-mono break-all">
                {showKey['new'] ? newKey : '•'.repeat(48)}
              </code>
              <Button
                variant="outline"
                size="icon"
                onClick={() => setShowKey(prev => ({ ...prev, new: !prev['new'] }))}
              >
                {showKey['new'] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => copyToClipboard(newKey)}
              >
                {copied === newKey ? (
                  <CheckCircle className="mr-2 h-4 w-4 text-emerald-500" />
                ) : (
                  <Copy className="mr-2 h-4 w-4" />
                )}
                {copied === newKey ? 'Copied' : 'Copy'}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {showCreate && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">Generate New API Key</CardTitle>
          </CardHeader>
          <CardContent className="flex items-end gap-3">
            <div className="flex-1 space-y-2">
              <label className="text-xs text-muted-foreground">Key Name</label>
              <Input
                placeholder="e.g. Mobile App, CI/CD Pipeline"
                value={newName}
                onChange={e => setNewName(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleCreate()}
              />
            </div>
            <Button onClick={handleCreate} disabled={creating || !newName.trim()} size="sm">
              {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
              Generate
            </Button>
          </CardContent>
        </Card>
      )}

      {/* API Key List */}
      <div className="space-y-2">
        {keys.length === 0 && (
          <Card>
            <CardContent className="flex flex-col items-center py-12 text-center">
              <Key className="h-12 w-12 text-muted-foreground opacity-50 mb-4" />
              <h3 className="text-lg font-medium">No API keys</h3>
              <p className="text-sm text-muted-foreground mt-1 max-w-sm">
                Generate an API key to authenticate external services and SDKs.
                Keys use the <code className="text-xs bg-muted px-1 rounded">gb_</code> prefix.
              </p>
            </CardContent>
          </Card>
        )}

        {keys.map((key) => (
          <Card key={key.id} className="transition-all hover:border-primary/30">
            <CardContent className="flex items-center gap-4 py-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-amber-500/10">
                <Key className="h-5 w-5 text-amber-500" />
              </div>

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <h3 className="font-medium">{key.name}</h3>
                  <Badge variant="outline" className="font-mono text-[10px]">
                    {key.prefix}...
                  </Badge>
                </div>
                <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                  <span className="flex items-center gap-1">
                    <Shield className="h-3 w-3" />
                    <code className="text-[10px] bg-muted px-1 rounded">gb_</code> prefix
                  </span>
                  <span className="flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    Created {new Date(key.created_at).toLocaleDateString()}
                  </span>
                </div>
              </div>

              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => copyToClipboard(key.id)}
                  title="Copy key ID"
                >
                  {copied === key.id ? (
                    <CheckCircle className="h-4 w-4 text-emerald-500" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => handleDelete(key.id)}
                  title="Revoke key"
                >
                  <Trash2 className="h-4 w-4 text-muted-foreground hover:text-red-500" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* API Usage Info */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Code className="h-4 w-4" />
            Usage
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div className="rounded-lg bg-muted/50 p-3">
            <p className="text-xs text-muted-foreground mb-2">Include the API key in requests:</p>
            <code className="text-xs bg-muted px-2 py-1 rounded font-mono block break-all">
              Authorization: Bearer gb_your_api_key_here
            </code>
          </div>
          <div className="rounded-lg bg-muted/50 p-3">
            <p className="text-xs text-muted-foreground mb-2">Using the TypeScript SDK:</p>
            <pre className="text-xs bg-muted px-2 py-1 rounded font-mono">{`import { GresbaseClient } from '@gresbase/sdk'
const client = new GresbaseClient({ url: '...' })
client.auth.setToken('gb_your_api_key_here')`}</pre>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
