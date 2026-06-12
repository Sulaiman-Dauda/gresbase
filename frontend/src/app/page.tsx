import { LoginForm } from '@/components/auth/login-form'
import { Metadata } from 'next'

export const metadata: Metadata = {
  title: 'Sign In',
}

export default function LoginPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      {/* Subtle grid background */}
      <div className="absolute inset-0 bg-[linear-gradient(rgba(255,255,255,0.02)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,0.02)_1px,transparent_1px)] bg-[size:64px_64px] [mask-image:radial-gradient(ellipse_80%_50%_at_50%_50%,black_40%,transparent_70%)]" />
      
      <div className="relative w-full max-w-sm space-y-6 rounded-xl border border-border bg-card/80 p-8 shadow-2xl shadow-black/5 backdrop-blur-sm">
        <div className="space-y-2 text-center">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-foreground shadow-lg shadow-foreground/10">
            <span className="text-xl font-bold text-background">G</span>
          </div>
          <h1 className="text-xl font-semibold tracking-tight">Sign in to Gresbase</h1>
          <p className="text-sm text-muted-foreground">
            Enter your credentials to access the dashboard
          </p>
        </div>
        <LoginForm />
      </div>
    </div>
  )
}
