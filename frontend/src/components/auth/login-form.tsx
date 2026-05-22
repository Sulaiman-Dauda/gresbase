'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useStore } from '@/lib/store'
import { toast } from 'sonner'
import { Eye, EyeOff, Loader2, Sparkles } from 'lucide-react'

export function LoginForm() {
  const router = useRouter()
  const { login, isAuthenticated } = useStore()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [loading, setLoading] = useState(false)
  const [setupRequired, setSetupRequired] = useState(false)
  const [checkingSetup, setCheckingSetup] = useState(true)

  // Check if first-time setup is needed
  useEffect(() => {
    fetch('/api/v1/setup')
      .then(r => r.json())
      .then(data => {
        setSetupRequired(data.setup_required === true)
        setCheckingSetup(false)
      })
      .catch(() => setCheckingSetup(false))
  }, [])

  useEffect(() => {
    if (isAuthenticated) router.push('/overview')
  }, [isAuthenticated, router])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      if (setupRequired) {
        // First-run setup — create the initial admin
        const res = await fetch('/api/v1/setup', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email, password }),
        })
        const data = await res.json()
        if (!res.ok) throw new Error(data.message)

        // Store token and redirect
        localStorage.setItem('gresbase_token', data.token)
        toast.success('Setup complete! Welcome to Gresbase.')
        router.push('/overview')
      } else {
        await login(email, password)
        toast.success('Signed in')
        router.push('/overview')
      }
    } catch (err: any) {
      toast.error(err.message || 'Invalid credentials')
    } finally {
      setLoading(false)
    }
  }

  if (checkingSetup) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {setupRequired && (
        <div className="rounded-lg bg-blue-500/10 border border-blue-500/20 p-3 text-sm text-blue-600 dark:text-blue-400">
          <div className="flex items-center gap-2 font-medium mb-1">
            <Sparkles className="h-4 w-4" />
            Welcome! Create your admin account
          </div>
          <p className="text-xs opacity-80">No admins exist yet. Set up your first super admin to get started.</p>
        </div>
      )}
      <div className="space-y-2">
        <label className="text-sm font-medium">Email</label>
        <Input type="email" placeholder="admin@example.com" value={email}
          onChange={(e) => setEmail(e.target.value)} required autoFocus />
      </div>
      <div className="space-y-2">
        <label className="text-sm font-medium">Password</label>
        <div className="relative">
          <Input type={show ? 'text' : 'password'} placeholder="••••••••" value={password}
            onChange={(e) => setPassword(e.target.value)} required className="pr-10" />
          <button type="button" onClick={() => setShow(!show)}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
            {show ? <EyeOff className="h-4 w-4"/> : <Eye className="h-4 w-4"/>}
          </button>
        </div>
      </div>
      <Button type="submit" className="w-full" disabled={loading}>
        {loading ? <><Loader2 className="mr-2 h-4 w-4 animate-spin"/>{setupRequired ? 'Setting up...' : 'Signing in...'}</> :
          setupRequired ? <>Create admin account</> : 'Sign in'}
      </Button>
    </form>
  )
}
