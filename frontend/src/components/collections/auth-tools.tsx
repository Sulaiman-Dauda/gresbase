'use client'

import * as React from 'react'
import { KeyRound, Loader2, MailCheck, RefreshCcw, Shield, Smartphone } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import type { Collection } from '@/lib/types'

interface OAuthProviderOption {
  name: string
  displayName: string
  authURL?: string
  authUrl?: string
  redirectURL?: string
  state?: string
  codeVerifier?: string
}

export function AuthTools({ collection }: { collection: Collection }) {
  const [methods, setMethods] = React.useState<any | null>(null)
  const [loadingMethods, setLoadingMethods] = React.useState(true)
  const [identity, setIdentity] = React.useState('')
  const [password, setPassword] = React.useState('')
  const [email, setEmail] = React.useState('')
  const [otpEmail, setOtpEmail] = React.useState('')
  const [otpId, setOtpId] = React.useState('')
  const [otpCode, setOtpCode] = React.useState('')
  const [authResult, setAuthResult] = React.useState<any | null>(null)
  const [busyAction, setBusyAction] = React.useState<string | null>(null)

  const loadMethods = React.useCallback(async () => {
    setLoadingMethods(true)
    try {
      const response = await api.getRecordAuthMethods(collection.name)
      setMethods(response)
    } catch (err: any) {
      toast.error(err.message || 'Failed to load auth methods')
      setMethods(null)
    } finally {
      setLoadingMethods(false)
    }
  }, [collection.name])

  React.useEffect(() => {
    loadMethods()
  }, [loadMethods])

  const oauthProviders = React.useMemo<OAuthProviderOption[]>(() => {
    const rawProviders = methods?.oauth2?.providers
    if (!Array.isArray(rawProviders)) return []
    return rawProviders
      .map((provider: any) => {
        if (typeof provider === 'string') {
          return {
            name: provider,
            displayName: provider,
            authURL: `/api/v1/collections/${collection.name}/auth/oauth2/${provider}`,
          }
        }
        const name = String(provider?.name || '').trim()
        if (!name) return null
        return {
          name,
          displayName: String(provider?.displayName || provider?.display_name || name),
          authURL: provider?.authURL || provider?.authUrl,
          authUrl: provider?.authUrl,
          redirectURL: provider?.redirectURL,
          state: provider?.state,
          codeVerifier: provider?.codeVerifier,
        }
      })
      .filter(Boolean) as OAuthProviderOption[]
  }, [collection.name, methods])

  const run = async (action: string, fn: () => Promise<any>) => {
    setBusyAction(action)
    try {
      const result = await fn()
      return result
    } catch (err: any) {
      toast.error(err.message || 'Request failed')
      throw err
    } finally {
      setBusyAction(null)
    }
  }

  const startOAuth = async (provider: OAuthProviderOption) => {
    const action = `oauth:${provider.name}`
    setBusyAction(action)
    try {
      const authURL = provider.authURL || provider.authUrl || `/api/v1/collections/${collection.name}/auth/oauth2/${provider.name}`
      const redirectURL = provider.redirectURL || `${window.location.origin}/api/oauth2-redirect`
      window.localStorage.removeItem('gresbase_oauth2_redirect_result')
      const popup = window.open(authURL, `gresbase-oauth-${provider.name}`, 'width=620,height=760')
      if (!popup) {
        throw new Error('OAuth popup was blocked')
      }

      const payload = await new Promise<{ code?: string; state?: string; error?: string; errorDescription?: string }>((resolve, reject) => {
        let settled = false
        const storageKey = 'gresbase_oauth2_redirect_result'
        const timeoutId = window.setTimeout(() => finish(new Error('OAuth login timed out')), 120000)
        const closedPoll = window.setInterval(() => {
          try {
            const raw = window.localStorage.getItem(storageKey)
            if (raw) {
              window.localStorage.removeItem(storageKey)
              finish(null, JSON.parse(raw))
              return
            }
          } catch {}
          if (popup.closed) finish(new Error('OAuth popup was closed before completion'))
        }, 400)

        const finish = (error?: Error | null, data?: { code?: string; state?: string; error?: string; errorDescription?: string }) => {
          if (settled) return
          settled = true
          window.clearTimeout(timeoutId)
          window.clearInterval(closedPoll)
          window.removeEventListener('message', onMessage)
          if (error) reject(error)
          else resolve(data || {})
        }

        const onMessage = (event: MessageEvent) => {
          if (event.origin !== window.location.origin) return
          const message = event.data
          if (!message || message.type !== 'gresbase:oauth2-redirect') return
          const data = (message.payload || {}) as { code?: string; state?: string; error?: string; errorDescription?: string }
          if (data.error) {
            finish(new Error(data.errorDescription || data.error))
            return
          }
          finish(null, data)
        }

        window.addEventListener('message', onMessage)
      })

      if (!payload.code || !payload.state) {
        throw new Error('OAuth redirect did not provide code/state')
      }

      const result = await api.recordAuthWithOAuth2(
        collection.name,
        provider.name,
        payload.code,
        payload.state,
        redirectURL,
        provider.codeVerifier
      )
      setAuthResult(result)
      toast.success(`Authenticated with ${provider.displayName}`)
    } catch (err: any) {
      toast.error(err?.message || 'OAuth authentication failed')
    } finally {
      setBusyAction(null)
    }
  }

  return (
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
      <div className="space-y-6">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <div>
              <CardTitle className="text-base">Auth methods</CardTitle>
            </div>
            <Button variant="outline" size="sm" onClick={loadMethods} disabled={loadingMethods}>
              {loadingMethods ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RefreshCcw className="mr-2 h-4 w-4" />}
              Refresh
            </Button>
          </CardHeader>
          <CardContent>
            {loadingMethods ? (
              <div className="py-6 text-sm text-muted-foreground">Loading auth capabilities…</div>
            ) : methods ? (
              <div className="grid gap-3 md:grid-cols-2">
                <MethodCard title="Email / password" enabled={Boolean(methods.emailPassword)} description="Primary record authentication flow" />
                <MethodCard title="Username / password" enabled={Boolean(methods.usernamePassword)} description="Identity field can also be username" />
                <MethodCard title="OTP" enabled={Boolean(methods.otp?.enabled)} description="One-time password via email" />
                <MethodCard title="OAuth2" enabled={Boolean(methods.oauth2?.enabled)} description={`${methods.oauth2?.providers?.length || 0} provider(s)`} />
                <MethodCard title="MFA" enabled={Boolean(methods.mfa?.enabled)} description="Second-factor support for record sessions" />
                <MethodCard title="Verified only" enabled={Boolean(methods.onlyVerified)} description="Restrict auth to verified records" />
              </div>
            ) : (
              <div className="py-6 text-sm text-muted-foreground">No auth method data available.</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Test password auth</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <Input value={identity} onChange={(e) => setIdentity(e.target.value)} placeholder="Email or username" />
            <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" placeholder="Password" />
            <Button
              onClick={async () => {
                const result = await run('auth', () => api.recordAuthWithPassword(collection.name, identity, password))
                setAuthResult(result)
                toast.success('Authenticated successfully')
              }}
              disabled={busyAction === 'auth' || !identity || !password}
            >
              {busyAction === 'auth' ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Shield className="mr-2 h-4 w-4" />}
              Authenticate
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Test OTP auth</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <Input value={otpEmail} onChange={(e) => setOtpEmail(e.target.value)} type="email" placeholder="user@example.com" />
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                onClick={async () => {
                  const result = await run('otp-request', () => api.recordAuthOTPRequest(collection.name, otpEmail))
                  setOtpId(result?.otpId || '')
                  toast.success('OTP sent')
                }}
                disabled={busyAction === 'otp-request' || !otpEmail}
              >
                {busyAction === 'otp-request' ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <MailCheck className="mr-2 h-4 w-4" />}
                Request OTP
              </Button>
              <Input value={otpId} onChange={(e) => setOtpId(e.target.value)} placeholder="otpId" />
              <Input value={otpCode} onChange={(e) => setOtpCode(e.target.value)} placeholder="123456" />
              <Button
                onClick={async () => {
                  const result = await run('otp-verify', () => api.recordAuthOTPVerify(collection.name, otpId, otpCode))
                  setAuthResult(result)
                  toast.success('OTP authenticated')
                }}
                disabled={busyAction === 'otp-verify' || !otpId || !otpCode}
              >
                {busyAction === 'otp-verify' ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Smartphone className="mr-2 h-4 w-4" />}
                Verify OTP
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">OAuth providers</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {methods?.oauth2?.enabled && oauthProviders.length ? (
              <div className="flex flex-wrap gap-2">
                {oauthProviders.map((provider) => {
                  const action = `oauth:${provider.name}`
                  return (
                    <Button
                      key={provider.name}
                      variant="outline"
                      onClick={() => startOAuth(provider)}
                      disabled={busyAction === action}
                      className="justify-start"
                    >
                      {busyAction === action ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Shield className="mr-2 h-4 w-4" />}
                      Continue with {provider.displayName}
                    </Button>
                  )
                })}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No OAuth providers are currently configured for this collection.</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Verification & reset</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" placeholder="user@example.com" />
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                onClick={async () => {
                  await run('verify', () => api.requestRecordVerification(collection.name, email))
                  toast.success('Verification request sent')
                }}
                disabled={busyAction === 'verify' || !email}
              >
                {busyAction === 'verify' ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <MailCheck className="mr-2 h-4 w-4" />}
                Send verification
              </Button>
              <Button
                variant="outline"
                onClick={async () => {
                  await run('reset', () => api.requestRecordPasswordReset(collection.name, email))
                  toast.success('Password reset request sent')
                }}
                disabled={busyAction === 'reset' || !email}
              >
                {busyAction === 'reset' ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <KeyRound className="mr-2 h-4 w-4" />}
                Request reset
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              These actions hit the real record auth endpoints for this collection — the full Gresbase auth tooling available directly in the workbench.
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Latest auth result</CardTitle>
          </CardHeader>
          <CardContent>
            {authResult ? (
              <pre className="overflow-x-auto rounded-lg border border-border bg-accent/10 p-4 text-xs text-muted-foreground">
                {JSON.stringify(authResult, null, 2)}
              </pre>
            ) : (
              <p className="text-sm text-muted-foreground">No authentication attempt yet.</p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function MethodCard({ title, enabled, description }: { title: string; enabled: boolean; description: string }) {
  return (
    <div className="rounded-lg border border-border p-4">
      <div className="flex items-center gap-2">
        <div className="font-medium">{title}</div>
        <Badge variant={enabled ? 'secondary' : 'outline'}>{enabled ? 'enabled' : 'disabled'}</Badge>
      </div>
      <p className="mt-2 text-sm text-muted-foreground">{description}</p>
    </div>
  )
}
