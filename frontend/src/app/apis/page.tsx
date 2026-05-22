'use client'

import { useQuery } from '@tanstack/react-query'
import { AppLayout } from '@/components/layout/app-layout'
import { api } from '@/lib/api'
import { Code2, Copy, Loader2 } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'

const endpoints = [
  { method: 'GET', path: '/api/v1/health', desc: 'Health check', auth: false },
  { method: 'POST', path: '/api/v1/auth/login', desc: 'Admin login', auth: false },
  { method: 'POST', path: '/api/v1/auth/refresh', desc: 'Refresh token', auth: false },
  { method: 'GET', path: '/api/v1/collections', desc: 'List collections', auth: true },
  { method: 'POST', path: '/api/v1/collections', desc: 'Create collection', auth: true },
  { method: 'GET', path: '/api/v1/records/&#123;collection&#125;', desc: 'List records', auth: false },
  { method: 'POST', path: '/api/v1/records/&#123;collection&#125;', desc: 'Create record', auth: false },
  { method: 'GET', path: '/api/v1/files/&#123;collection&#125;/&#123;id&#125;/&#123;file&#125;', desc: 'Download file', auth: false },
  { method: 'GET', path: '/api/v1/realtime', desc: 'WebSocket realtime', auth: false },
  { method: 'GET', path: '/api/v1/acme/directory', desc: 'ACME directory', auth: false },
  { method: 'GET', path: '/api/v1/certificates', desc: 'List certificates', auth: true },
]

const methodColors: Record<string, string> = {
  GET: 'text-emerald-500', POST: 'text-blue-500', PUT: 'text-amber-500',
  PATCH: 'text-amber-500', DELETE: 'text-red-500',
}

export default function APIsPage() {
  const { data: health } = useQuery({ queryKey: ['health'], queryFn: api.health })

  return (
    <AppLayout>
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">APIs</h1>
          <p className="text-sm text-muted-foreground mt-1">REST API reference — base URL: <code className="rounded bg-accent px-1.5 py-0.5 text-xs font-mono">/api/v1</code></p>
        </div>

        <Card>
          <CardHeader><CardTitle className="text-base flex items-center gap-2"><Code2 className="h-4 w-4"/>Endpoints</CardTitle></CardHeader>
          <CardContent className="p-0">
            <div className="divide-y divide-border">
              {endpoints.map((ep, i) => (
                <div key={i} className="flex items-center gap-3 px-6 py-2.5 hover:bg-accent/50 transition-colors">
                  <span className={`min-w-[48px] text-xs font-mono font-bold ${methodColors[ep.method]}`}>{ep.method}</span>
                  <code className="text-xs font-mono text-muted-foreground flex-1">{ep.path}</code>
                  <span className="hidden lg:block text-xs text-muted-foreground max-w-[180px] truncate">{ep.desc}</span>
                  {ep.auth && <Badge variant="secondary" className="text-[10px]">AUTH</Badge>}
                  <button onClick={() => { navigator.clipboard.writeText(ep.path); toast.success('Copied') }}
                    className="opacity-0 group-hover:opacity-100 p-1 hover:bg-accent rounded"><Copy className="h-3 w-3"/></button>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="text-base">SDKs</CardTitle><CardDescription>Type-safe client libraries</CardDescription></CardHeader>
          <CardContent className="space-y-2">
            {[
              { lang: 'TypeScript', desc: 'Fully typed client for Node.js and browser', pkg: '@gresbase/sdk' },
              { lang: 'Go', desc: 'Native Go client for server-side integration', pkg: 'github.com/gresbase/sdk' },
            ].map(sdk => (
              <div key={sdk.lang} className="flex items-center justify-between rounded-lg bg-accent/50 px-4 py-2.5">
                <div className="flex items-center gap-3">
                  <Badge variant="outline">{sdk.lang}</Badge>
                  <div><p className="text-sm font-medium">{sdk.pkg}</p><p className="text-xs text-muted-foreground">{sdk.desc}</p></div>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </AppLayout>
  )
}
