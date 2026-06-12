'use client'

import { useEffect, useState, useCallback } from 'react'
import { AppLayout } from '@/components/layout/app-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import {
  Settings as SettingsIcon, Shield, Save, Loader2, RefreshCw, HardDrive, Mail,
  MailOpen, ChevronDown, ChevronRight, RotateCcw,
} from 'lucide-react'

interface SettingsData {
  app_name: string
  app_url: string
  sender_name: string
  sender_address: string
  smtp: {
    enabled: boolean
    host: string
    port: number
    username: string
    password?: string
    tls: boolean
  }
  s3: {
    enabled: boolean
    bucket: string
    region: string
    endpoint: string
    access_key: string
    secret_key?: string
    force_path_style: boolean
  }
  security: {
    min_password_length: number
    auth_token_expiry: number
    refresh_token_expiry: number
    max_failed_login_attempts: number
    mfa_enabled: boolean
    allow_registration: boolean
  }
  // Keyed by template id; an entry with empty subject AND body means
  // "use the built-in default" (the backend drops it on save).
  email_templates?: Record<string, { subject: string; body: string }>
}

interface EmailTemplateInfo {
  id: string
  name: string
  description: string
  placeholders: string[]
  defaultSubject: string
  defaultBody: string
  customSubject?: string
  customBody?: string
}

const SECTIONS = [
  { key: 'general', title: 'General', icon: SettingsIcon, description: 'Application identity and email sender' },
  { key: 'security', title: 'Security', icon: Shield, description: 'Password policy and token lifetimes' },
  { key: 'smtp', title: 'Email (SMTP)', icon: Mail, description: 'SMTP server for transactional email' },
  { key: 'email_templates', title: 'Email templates', icon: MailOpen, description: 'Customize the subject and body of transactional emails' },
  { key: 's3', title: 'Storage (S3)', icon: HardDrive, description: 'Optional S3-compatible file storage' },
] as const

export default function SettingsPage() {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [emailTemplates, setEmailTemplates] = useState<EmailTemplateInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [activeSection, setActiveSection] = useState<(typeof SECTIONS)[number]['key']>('general')
  const [expandedTemplate, setExpandedTemplate] = useState<string | null>(null)

  const fetchSettings = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [data, templates] = await Promise.all([api.getSettings(), api.getEmailTemplates()])
      setSettings(data)
      setEmailTemplates(templates)
    } catch (err: any) {
      setError(err.message || 'Failed to load settings')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchSettings() }, [fetchSettings])

  const patch = (updater: (current: SettingsData) => SettingsData) => {
    setSettings((current) => (current ? updater(current) : current))
  }

  const handleSave = async () => {
    if (!settings) return
    setSaving(true)
    setError(null)
    try {
      // Blank secrets mean "keep the stored value" — omit them entirely.
      const payload: any = JSON.parse(JSON.stringify(settings))
      if (!payload.smtp.password) delete payload.smtp.password
      if (!payload.s3.secret_key) delete payload.s3.secret_key
      delete payload.id
      delete payload.created_at
      delete payload.updated_at
      delete payload.meta
      await api.updateSettings(payload)
      toast.success('Settings saved')
    } catch (err: any) {
      setError(err.message || 'Failed to save settings')
      toast.error(err.message || 'Failed to save settings')
    } finally {
      setSaving(false)
    }
  }

  if (loading || !settings) {
    return (
      <AppLayout>
        <div className="flex min-h-[60vh] items-center justify-center">
          {error ? <p className="text-sm text-red-500">{error}</p> : <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />}
        </div>
      </AppLayout>
    )
  }

  const section = SECTIONS.find((s) => s.key === activeSection)!

  return (
    <AppLayout>
      <div className="animate-fade-in space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
            <p className="mt-1 text-sm text-muted-foreground">Configure your Gresbase instance</p>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={fetchSettings} disabled={loading}>
              <RefreshCw className="mr-2 h-4 w-4" /> Reset
            </Button>
            <Button size="sm" onClick={handleSave} disabled={saving}>
              {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
              Save changes
            </Button>
          </div>
        </div>

        {error ? (
          <Card className="border-red-500/50 bg-red-500/5">
            <CardContent className="py-3"><p className="text-sm text-red-500">{error}</p></CardContent>
          </Card>
        ) : null}

        <div className="flex gap-6">
          <div className="w-48 shrink-0 space-y-1">
            {SECTIONS.map((s) => (
              <button
                key={s.key}
                onClick={() => setActiveSection(s.key)}
                className={`flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
                  activeSection === s.key
                    ? 'bg-accent font-medium text-accent-foreground'
                    : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                }`}
              >
                <s.icon className="h-4 w-4" />
                {s.title}
              </button>
            ))}
          </div>

          <div className="flex-1">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <section.icon className="h-5 w-5" />
                  {section.title}
                </CardTitle>
                <p className="text-sm text-muted-foreground">{section.description}</p>
              </CardHeader>
              <CardContent className="space-y-4">
                {activeSection === 'general' ? (
                  <>
                    <Field label="Application name">
                      <Input value={settings.app_name} onChange={(e) => patch((c) => ({ ...c, app_name: e.target.value }))} placeholder="Gresbase" />
                    </Field>
                    <Field label="Application URL" hint="Used in emails and OAuth redirects.">
                      <Input value={settings.app_url} onChange={(e) => patch((c) => ({ ...c, app_url: e.target.value }))} placeholder="https://api.example.com" />
                    </Field>
                    <Field label="Sender name">
                      <Input value={settings.sender_name} onChange={(e) => patch((c) => ({ ...c, sender_name: e.target.value }))} placeholder="Gresbase" />
                    </Field>
                    <Field label="Sender address">
                      <Input value={settings.sender_address} onChange={(e) => patch((c) => ({ ...c, sender_address: e.target.value }))} placeholder="noreply@example.com" />
                    </Field>
                  </>
                ) : null}

                {activeSection === 'security' ? (
                  <>
                    <Field label="Minimum password length">
                      <Input type="number" min={6} value={settings.security.min_password_length} onChange={(e) => patch((c) => ({ ...c, security: { ...c.security, min_password_length: Number(e.target.value) || 0 } }))} />
                    </Field>
                    <Field label="Auth token expiry (seconds)" hint="86400 = 24 hours">
                      <Input type="number" min={1} value={settings.security.auth_token_expiry} onChange={(e) => patch((c) => ({ ...c, security: { ...c.security, auth_token_expiry: Number(e.target.value) || 0 } }))} />
                    </Field>
                    <Field label="Refresh token expiry (seconds)" hint="604800 = 7 days">
                      <Input type="number" min={1} value={settings.security.refresh_token_expiry} onChange={(e) => patch((c) => ({ ...c, security: { ...c.security, refresh_token_expiry: Number(e.target.value) || 0 } }))} />
                    </Field>
                    <Field label="Max failed login attempts">
                      <Input type="number" min={1} value={settings.security.max_failed_login_attempts} onChange={(e) => patch((c) => ({ ...c, security: { ...c.security, max_failed_login_attempts: Number(e.target.value) || 0 } }))} />
                    </Field>
                    <Toggle
                      label="Allow admin registration"
                      hint="When off, new admins can only be created by a super admin."
                      checked={settings.security.allow_registration}
                      onChange={(v) => patch((c) => ({ ...c, security: { ...c.security, allow_registration: v } }))}
                    />
                    <Toggle
                      label="Multi-factor authentication"
                      hint="Require a second factor for admin logins."
                      checked={settings.security.mfa_enabled}
                      onChange={(v) => patch((c) => ({ ...c, security: { ...c.security, mfa_enabled: v } }))}
                    />
                  </>
                ) : null}

                {activeSection === 'smtp' ? (
                  <>
                    <Toggle
                      label="Enable SMTP"
                      hint="When off, emails are logged instead of sent."
                      checked={settings.smtp.enabled}
                      onChange={(v) => patch((c) => ({ ...c, smtp: { ...c.smtp, enabled: v } }))}
                    />
                    <Field label="Host">
                      <Input value={settings.smtp.host} onChange={(e) => patch((c) => ({ ...c, smtp: { ...c.smtp, host: e.target.value } }))} placeholder="smtp.example.com" />
                    </Field>
                    <Field label="Port">
                      <Input type="number" value={settings.smtp.port} onChange={(e) => patch((c) => ({ ...c, smtp: { ...c.smtp, port: Number(e.target.value) || 0 } }))} placeholder="587" />
                    </Field>
                    <Field label="Username">
                      <Input value={settings.smtp.username} onChange={(e) => patch((c) => ({ ...c, smtp: { ...c.smtp, username: e.target.value } }))} />
                    </Field>
                    <Field label="Password" hint="Leave blank to keep the current password.">
                      <Input type="password" value={settings.smtp.password || ''} onChange={(e) => patch((c) => ({ ...c, smtp: { ...c.smtp, password: e.target.value } }))} placeholder="••••••••" />
                    </Field>
                    <Toggle
                      label="TLS"
                      checked={settings.smtp.tls}
                      onChange={(v) => patch((c) => ({ ...c, smtp: { ...c.smtp, tls: v } }))}
                    />
                  </>
                ) : null}

                {activeSection === 'email_templates' ? (
                  <>
                    <p className="text-xs text-muted-foreground">
                      Overrides are Go templates. Leave a field unchanged (or reset it) to use the built-in default.
                      Changes are applied with the global &quot;Save changes&quot; button.
                    </p>
                    {emailTemplates.map((tpl) => (
                      <EmailTemplateEditor
                        key={tpl.id}
                        template={tpl}
                        override={settings.email_templates?.[tpl.id]}
                        expanded={expandedTemplate === tpl.id}
                        onToggle={() => setExpandedTemplate(expandedTemplate === tpl.id ? null : tpl.id)}
                        onChange={(subject, body) =>
                          patch((c) => ({
                            ...c,
                            email_templates: { ...(c.email_templates || {}), [tpl.id]: { subject, body } },
                          }))
                        }
                      />
                    ))}
                  </>
                ) : null}

                {activeSection === 's3' ? (
                  <>
                    <Toggle
                      label="Enable S3 storage"
                      hint="When off, files are stored on the local filesystem."
                      checked={settings.s3.enabled}
                      onChange={(v) => patch((c) => ({ ...c, s3: { ...c.s3, enabled: v } }))}
                    />
                    <Field label="Bucket">
                      <Input value={settings.s3.bucket} onChange={(e) => patch((c) => ({ ...c, s3: { ...c.s3, bucket: e.target.value } }))} placeholder="my-bucket" />
                    </Field>
                    <Field label="Region">
                      <Input value={settings.s3.region} onChange={(e) => patch((c) => ({ ...c, s3: { ...c.s3, region: e.target.value } }))} placeholder="us-east-1" />
                    </Field>
                    <Field label="Endpoint" hint="Leave blank for AWS S3; set for MinIO/R2/B2.">
                      <Input value={settings.s3.endpoint} onChange={(e) => patch((c) => ({ ...c, s3: { ...c.s3, endpoint: e.target.value } }))} placeholder="https://s3.amazonaws.com" />
                    </Field>
                    <Field label="Access key">
                      <Input value={settings.s3.access_key} onChange={(e) => patch((c) => ({ ...c, s3: { ...c.s3, access_key: e.target.value } }))} />
                    </Field>
                    <Field label="Secret key" hint="Leave blank to keep the current secret.">
                      <Input type="password" value={settings.s3.secret_key || ''} onChange={(e) => patch((c) => ({ ...c, s3: { ...c.s3, secret_key: e.target.value } }))} placeholder="••••••••" />
                    </Field>
                    <Toggle
                      label="Force path style"
                      hint="Required by most non-AWS S3 providers."
                      checked={settings.s3.force_path_style}
                      onChange={(v) => patch((c) => ({ ...c, s3: { ...c.s3, force_path_style: v } }))}
                    />
                  </>
                ) : null}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </AppLayout>
  )
}

function EmailTemplateEditor({
  template,
  override,
  expanded,
  onToggle,
  onChange,
}: {
  template: EmailTemplateInfo
  override?: { subject: string; body: string }
  expanded: boolean
  onToggle: () => void
  onChange: (subject: string, body: string) => void
}) {
  const customized = !!(override && (override.subject || override.body))
  const subjectValue = override?.subject || template.defaultSubject
  const bodyValue = override?.body || template.defaultBody

  // Text identical to the default is stored as "" (= use the default), so a
  // template only counts as customized while it actually differs.
  const handleSubject = (v: string) =>
    onChange(v === template.defaultSubject ? '' : v, override?.body || '')
  const handleBody = (v: string) =>
    onChange(override?.subject || '', v === template.defaultBody ? '' : v)

  return (
    <div className="rounded-lg border border-border">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-2 px-3 py-2.5 text-left transition-colors hover:bg-accent/50"
      >
        {expanded ? <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" /> : <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />}
        <span className="flex-1">
          <span className="block text-sm font-medium">{template.name}</span>
          <span className="block text-xs text-muted-foreground">{template.description}</span>
        </span>
        {customized ? <Badge variant="secondary">Customized</Badge> : null}
      </button>

      {expanded ? (
        <div className="space-y-4 border-t border-border px-3 py-4">
          <Field label="Subject">
            <Input
              value={subjectValue}
              onChange={(e) => handleSubject(e.target.value)}
              className="font-mono text-xs"
            />
          </Field>
          <Field label="Body (HTML)">
            <textarea
              value={bodyValue}
              onChange={(e) => handleBody(e.target.value)}
              spellCheck={false}
              rows={14}
              className="flex w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            />
          </Field>
          <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
            <span>Available placeholders:</span>
            {template.placeholders.map((p) => (
              <code key={p} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{p}</code>
            ))}
          </div>
          <div>
            <Button
              size="sm"
              variant="outline"
              disabled={!customized}
              onClick={() => onChange('', '')}
            >
              <RotateCcw className="mr-2 h-3.5 w-3.5" /> Reset to default
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <label className="text-sm font-medium">{label}</label>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

function Toggle({ label, hint, checked, onChange }: { label: string; hint?: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-border px-3 py-2.5">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-0.5 h-4 w-4 rounded border-border"
      />
      <span>
        <span className="block text-sm font-medium">{label}</span>
        {hint ? <span className="block text-xs text-muted-foreground">{hint}</span> : null}
      </span>
    </label>
  )
}
