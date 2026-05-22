'use client'

import { useEffect, useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  Settings as SettingsIcon, Server, Shield, Database, Globe,
  Save, Loader2, RefreshCw, HardDrive, Activity, Mail,
  Key, Zap, Wifi
} from 'lucide-react'

const SETTINGS_SECTIONS = [
  {
    key: 'general',
    title: 'General',
    icon: SettingsIcon,
    description: 'Application name, URL, and branding settings',
    fields: [
      { key: 'app_name', label: 'Application Name', type: 'text', placeholder: 'Gresbase' },
      { key: 'app_url', label: 'Application URL', type: 'text', placeholder: 'http://localhost:8080' },
      { key: 'data_dir', label: 'Data Directory', type: 'text', placeholder: './gresbase_data', disabled: true },
    ],
  },
  {
    key: 'auth',
    title: 'Authentication',
    icon: Shield,
    description: 'Password policy, token expiry, and session settings',
    fields: [
      { key: 'access_token_expiry', label: 'Access Token Expiry (minutes)', type: 'number', placeholder: '15' },
      { key: 'refresh_token_expiry', label: 'Refresh Token Expiry (days)', type: 'number', placeholder: '7' },
      { key: 'password_min_length', label: 'Min Password Length', type: 'number', placeholder: '8' },
      { key: 'rate_limit_enabled', label: 'Rate Limiting', type: 'checkbox' },
      { key: 'multi_tenant', label: 'Multi-Tenant Mode', type: 'checkbox' },
    ],
  },
  {
    key: 'storage',
    title: 'Storage',
    icon: HardDrive,
    description: 'File storage backend, S3 credentials, and limits',
    fields: [
      { key: 'storage_backend', label: 'Storage Backend', type: 'select', options: ['local', 's3'], placeholder: 'local' },
      { key: 'storage_local_path', label: 'Local Storage Path', type: 'text', placeholder: './storage' },
      { key: 's3_endpoint', label: 'S3 Endpoint', type: 'text', placeholder: 'https://s3.amazonaws.com' },
      { key: 's3_bucket', label: 'S3 Bucket', type: 'text', placeholder: 'my-bucket' },
      { key: 's3_region', label: 'S3 Region', type: 'text', placeholder: 'us-east-1' },
    ],
  },
  {
    key: 'email',
    title: 'Email (SMTP)',
    icon: Mail,
    description: 'SMTP server for sending transactional emails',
    fields: [
      { key: 'smtp_host', label: 'SMTP Host', type: 'text', placeholder: 'smtp.example.com' },
      { key: 'smtp_port', label: 'SMTP Port', type: 'number', placeholder: '587' },
      { key: 'smtp_username', label: 'SMTP Username', type: 'text', placeholder: 'user@example.com' },
      { key: 'smtp_password', label: 'SMTP Password', type: 'password', placeholder: '••••••••' },
      { key: 'smtp_from', label: 'From Address', type: 'text', placeholder: 'noreply@example.com' },
    ],
  },
  {
    key: 'realtime',
    title: 'Realtime',
    icon: Activity,
    description: 'WebSocket and SSE connection limits',
    fields: [
      { key: 'realtime_max_connections', label: 'Max Connections', type: 'number', placeholder: '10000' },
      { key: 'realtime_idle_timeout', label: 'Idle Timeout (seconds)', type: 'number', placeholder: '300' },
      { key: 'realtime_max_message_size', label: 'Max Message Size (bytes)', type: 'number', placeholder: '65536' },
    ],
  },
]

export default function SettingsPage() {
  const [settings, setSettings] = useState<Record<string, any>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)
  const [activeSection, setActiveSection] = useState('general')
  const [health, setHealth] = useState<any>(null)

  const fetchSettings = useCallback(async () => {
    setLoading(true)
    try {
      const [data, h] = await Promise.all([
        api.getSettings().catch(() => ({})),
        api.health().catch(() => ({ status: 'unknown' })),
      ])
      setSettings(data || {})
      setHealth(h)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchSettings() }, [fetchSettings])

  const updateField = (key: string, value: any) => {
    setSettings(prev => ({ ...prev, [key]: value }))
    setSuccess(false)
  }

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.request('/api/v1/settings', {
        method: 'PUT',
        body: JSON.stringify(settings),
      })
      setSuccess(true)
      setTimeout(() => setSuccess(false), 3000)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const systemInfo = [
    { label: 'Server Status', value: health?.status || 'Loading...', icon: Server },
    { label: 'Database', value: health?.database ? 'Connected' : 'Disconnected', icon: Database },
    { label: 'Version', value: health?.version || '0.2.0', icon: Zap },
    { label: 'Environment', value: settings?.dev_mode ? 'Development' : 'Production', icon: Globe },
    { label: 'Rate Limiting', value: settings?.rate_limit_enabled ? 'Enabled' : 'Disabled', icon: Wifi },
    { label: 'Multi-Tenant', value: settings?.multi_tenant ? 'Enabled' : 'Disabled', icon: Key },
  ]

  const activeFields = SETTINGS_SECTIONS.find(s => s.key === activeSection)

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
          <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Configure your Gresbase instance
          </p>
        </div>
        <div className="flex items-center gap-2">
          {success && (
            <span className="text-sm text-emerald-500 flex items-center gap-1">
              <CheckCircle className="h-4 w-4" /> Saved
            </span>
          )}
          <Button size="sm" variant="outline" onClick={fetchSettings} disabled={loading}>
            <RefreshCw className="mr-2 h-4 w-4" /> Reset
          </Button>
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
            Save Changes
          </Button>
        </div>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3"><p className="text-sm text-red-500">{error}</p></CardContent>
        </Card>
      )}

      {/* System Info */}
      <div className="grid gap-4 md:grid-cols-3 lg:grid-cols-6">
        {systemInfo.map((info) => (
          <Card key={info.label}>
            <CardContent className="flex items-center gap-3 py-4">
              <info.icon className="h-4 w-4 text-muted-foreground" />
              <div>
                <div className="text-xs text-muted-foreground">{info.label}</div>
                <div className="text-sm font-medium capitalize">{info.value}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Settings Sections */}
      <div className="flex gap-6">
        {/* Section Navigation */}
        <div className="w-48 shrink-0 space-y-1">
          {SETTINGS_SECTIONS.map((section) => (
            <button
              key={section.key}
              onClick={() => setActiveSection(section.key)}
              className={`w-full flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
                activeSection === section.key
                  ? 'bg-accent text-accent-foreground font-medium'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
              }`}
            >
              <section.icon className="h-4 w-4" />
              {section.title}
            </button>
          ))}
        </div>

        {/* Section Content */}
        <div className="flex-1">
          {activeFields && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base flex items-center gap-2">
                  <activeFields.icon className="h-5 w-5" />
                  {activeFields.title}
                </CardTitle>
                <p className="text-sm text-muted-foreground">{activeFields.description}</p>
              </CardHeader>
              <CardContent className="space-y-4">
                {activeFields.fields.map((field) => (
                  <div key={field.key} className="space-y-2">
                    <label className="text-sm font-medium">
                      {field.label}
                      {field.disabled && (
                        <Badge variant="secondary" className="ml-2 text-[10px]">read-only</Badge>
                      )}
                    </label>
                    {field.type === 'checkbox' ? (
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={!!settings[field.key]}
                          onChange={e => updateField(field.key, e.target.checked)}
                          className="h-4 w-4 rounded border-border"
                        />
                        <span className="text-sm text-muted-foreground">Enable {field.label.toLowerCase()}</span>
                      </label>
                    ) : field.type === 'select' ? (
                      <select
                        value={settings[field.key] || (field as any).placeholder || ''}
                        onChange={e => updateField(field.key, e.target.value)}
                        disabled={field.disabled}
                        className="w-full h-9 rounded-lg border border-border bg-background px-3 text-sm"
                      >
                        {(field as any).options?.map((opt: string) => (
                          <option key={opt} value={opt}>{opt}</option>
                        ))}
                      </select>
                    ) : field.type === 'password' ? (
                      <Input
                        type="password"
                        value={settings[field.key] || ''}
                        onChange={e => updateField(field.key, e.target.value)}
                        placeholder={(field as any).placeholder}
                        disabled={field.disabled}
                      />
                    ) : field.type === 'number' ? (
                      <Input
                        type="number"
                        value={settings[field.key] || ''}
                        onChange={e => updateField(field.key, parseInt(e.target.value) || 0)}
                        placeholder={(field as any).placeholder}
                        disabled={field.disabled}
                      />
                    ) : (
                      <Input
                        type="text"
                        value={settings[field.key] || ''}
                        onChange={e => updateField(field.key, e.target.value)}
                        placeholder={(field as any).placeholder}
                        disabled={field.disabled}
                      />
                    )}
                  </div>
                ))}
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  )
}

function CheckCircle({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
      <polyline points="22 4 12 14.01 9 11.01" />
    </svg>
  )
}
