'use client'

import { useQuery } from '@tanstack/react-query'
import { AppLayout } from '@/components/layout/app-layout'
import { api } from '@/lib/api'
import { Shield, Key, Link2, Smartphone, Mail, Loader2 } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'

export default function AuthPage() {
  const { data: admins } = useQuery({ queryKey: ['admins'], queryFn: api.getAdmins })

  const methods = [
    { title: 'Email / Password', desc: 'Standard email and password authentication', icon: Mail, enabled: true },
    { title: 'OAuth Providers', desc: 'Google, GitHub, Apple, Microsoft (coming soon)', icon: Shield, enabled: false },
    { title: 'Magic Links', desc: 'Passwordless email magic link authentication', icon: Link2, enabled: false },
    { title: 'OTP Codes', desc: 'One-time password via email', icon: Smartphone, enabled: false },
    { title: 'API Keys', desc: 'Programmatic access with API tokens', icon: Key, enabled: true },
  ]

  return (
    <AppLayout>
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Authentication</h1>
          <p className="text-sm text-muted-foreground mt-1">Configure authentication providers and sessions</p>
        </div>

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {methods.map(m => (
            <Card key={m.title}>
              <CardHeader className="flex flex-row items-center gap-4 space-y-0 pb-2">
                <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-accent"><m.icon className="h-5 w-5 text-muted-foreground"/></div>
                <div><CardTitle className="text-sm">{m.title}</CardTitle><CardDescription className="text-xs">{m.enabled ? 'Enabled' : 'Coming soon'}</CardDescription></div>
              </CardHeader>
              <CardContent><p className="text-xs text-muted-foreground">{m.desc}</p></CardContent>
            </Card>
          ))}
        </div>

        <Card>
          <CardHeader><CardTitle className="text-base">Session Management</CardTitle><CardDescription>Token configuration</CardDescription></CardHeader>
          <CardContent className="space-y-3">
            {[
              { label: 'Access Token Expiry', value: '15 minutes' },
              { label: 'Refresh Token Expiry', value: '7 days' },
              { label: 'Login Rate Limit', value: '10 / minute' },
              { label: 'Admin Users', value: `${admins?.length ?? 0}` },
            ].map(s => (
              <div key={s.label} className="flex items-center justify-between rounded-lg bg-accent/50 px-3 py-2">
                <p className="text-sm">{s.label}</p>
                <span className="text-sm font-mono font-medium">{s.value}</span>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </AppLayout>
  )
}
