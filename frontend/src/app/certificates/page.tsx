'use client'

import { useEffect, useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  FileKey, Plus, Trash2, Loader2, RefreshCw, Globe,
  Shield, Clock, CheckCircle, XCircle, AlertTriangle,
  Calendar, Lock, Server, ExternalLink
} from 'lucide-react'

interface CertData {
  id: string
  domain: string
  status: string
  issuer?: string
  not_before?: string
  not_after?: string
  auto_renew?: boolean
  challenge_type?: string
  created_at?: string
}

export default function CertificatesPage() {
  const [certs, setCerts] = useState<CertData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showIssue, setShowIssue] = useState(false)
  const [newDomain, setNewDomain] = useState('')
  const [challengeType, setChallengeType] = useState('http-01')
  const [issuing, setIssuing] = useState(false)
  const [dnsInstructions, setDnsInstructions] = useState<any>(null)

  const fetchCerts = useCallback(async () => {
    setLoading(true)
    try {
      const data = await api.getCertificates()
      setCerts(Array.isArray(data) ? data : [])
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchCerts() }, [fetchCerts])

  useEffect(() => {
    const interval = setInterval(fetchCerts, 60000)
    return () => clearInterval(interval)
  }, [fetchCerts])

  const handleIssue = async () => {
    if (!newDomain.trim()) return
    setIssuing(true)
    setError(null)
    try {
      const result = await api.issueCertificate(newDomain.trim())
      setNewDomain('')
      setShowIssue(false)
      if (result.challenge_type === 'dns-01' && result.instructions) {
        setDnsInstructions(result)
      }
      fetchCerts()
    } catch (err: any) {
      setError(err.message)
    } finally {
      setIssuing(false)
    }
  }

  const handleRevoke = async (id: string) => {
    if (!confirm('Revoke this certificate? The domain will lose HTTPS.')) return
    try {
      await api.revokeCertificate(id)
      fetchCerts()
    } catch (err: any) {
      setError(err.message)
    }
  }

  const getStatusBadge = (status: string) => {
    const colors: Record<string, string> = {
      active: 'bg-emerald-500/10 text-emerald-500 border-emerald-500/20',
      revoked: 'bg-red-500/10 text-red-500 border-red-500/20',
      replaced: 'bg-amber-500/10 text-amber-500 border-amber-500/20',
      pending: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
      expired: 'bg-slate-500/10 text-slate-400 border-slate-500/20',
    }
    return <Badge variant="outline" className={colors[status] || ''}>{status}</Badge>
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'active': return <CheckCircle className="h-4 w-4 text-emerald-500" />
      case 'revoked': return <XCircle className="h-4 w-4 text-red-500" />
      case 'expired': return <AlertTriangle className="h-4 w-4 text-amber-500" />
      default: return <Clock className="h-4 w-4 text-blue-500" />
    }
  }

  const daysUntilExpiry = (notAfter?: string): number | null => {
    if (!notAfter) return null
    const expiry = new Date(notAfter).getTime()
    const now = Date.now()
    return Math.ceil((expiry - now) / (1000 * 60 * 60 * 24))
  }

  const activeCerts = certs.filter(c => c.status === 'active')
  const expiringCerts = activeCerts.filter(c => {
    const days = daysUntilExpiry(c.not_after)
    return days !== null && days <= 30
  })

  if (loading && certs.length === 0) {
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
          <h1 className="text-2xl font-bold tracking-tight">Certificates</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {certs.length} certificate{certs.length !== 1 ? 's' : ''} — managed by embedded ACME CA
          </p>
        </div>
        <Button onClick={() => setShowIssue(!showIssue)} size="sm">
          <Plus className="mr-2 h-4 w-4" />
          Issue Certificate
        </Button>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3"><p className="text-sm text-red-500">{error}</p></CardContent>
        </Card>
      )}

      {dnsInstructions && (
        <Card className="border-blue-500/50 bg-blue-500/5">
          <CardContent className="py-4 space-y-2">
            <div className="flex items-center gap-2">
              <Globe className="h-5 w-5 text-blue-500" />
              <span className="font-medium">DNS-01 Challenge — Manual Setup Required</span>
            </div>
            <p className="text-sm text-muted-foreground">
              Create the following DNS record to prove domain ownership:
            </p>
            <div className="rounded-lg bg-muted p-3 font-mono text-xs space-y-1">
              <div>Type: <span className="text-blue-400">TXT</span></div>
              <div>Name: <span className="text-emerald-400">{dnsInstructions.record_name}</span></div>
              <div>Value: <span className="text-amber-400 break-all">{dnsInstructions.record_value}</span></div>
            </div>
            <div className="flex gap-2">
              <Button size="sm" onClick={() => setDnsInstructions(null)}>Done</Button>
              <Button size="sm" variant="outline" onClick={() => navigator.clipboard.writeText(dnsInstructions.record_value)}>
                Copy Value
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Expiry Warnings */}
      {expiringCerts.length > 0 && (
        <Card className="border-amber-500/50 bg-amber-500/5">
          <CardContent className="py-3">
            <div className="flex items-center gap-2 text-amber-500">
              <AlertTriangle className="h-4 w-4" />
              <span className="text-sm font-medium">
                {expiringCerts.length} certificate{expiringCerts.length > 1 ? 's' : ''} expiring within 30 days
              </span>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Issue Form */}
      {showIssue && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">Issue New Certificate</CardTitle>
          </CardHeader>
          <CardContent className="flex items-end gap-3">
            <div className="flex-1 space-y-2">
              <label className="text-xs text-muted-foreground">Domain Name</label>
              <Input
                placeholder="example.com"
                value={newDomain}
                onChange={e => setNewDomain(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleIssue()}
              />
            </div>
            <div className="space-y-2">
              <label className="text-xs text-muted-foreground">Challenge</label>
              <select
                value={challengeType}
                onChange={e => setChallengeType(e.target.value)}
                className="h-9 rounded-lg border border-border bg-background px-3 text-sm"
              >
                <option value="http-01">HTTP-01</option>
                <option value="dns-01">DNS-01</option>
              </select>
            </div>
            <Button onClick={handleIssue} disabled={issuing || !newDomain.trim()} size="sm">
              {issuing ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Shield className="mr-2 h-4 w-4" />}
              Issue
            </Button>
          </CardContent>
        </Card>
      )}

      {/* Stats */}
      <div className="grid gap-4 sm:grid-cols-3">
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <CheckCircle className="h-5 w-5 text-emerald-500" />
            <div>
              <div className="text-lg font-bold">{activeCerts.length}</div>
              <div className="text-xs text-muted-foreground">Active Certs</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <Server className="h-5 w-5 text-blue-500" />
            <div>
              <div className="text-lg font-bold">ECDSA P-384</div>
              <div className="text-xs text-muted-foreground">Key Algorithm</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <Lock className="h-5 w-5 text-purple-500" />
            <div>
              <div className="text-lg font-bold">90 days</div>
              <div className="text-xs text-muted-foreground">Validity Period</div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Certificate List */}
      <div className="space-y-2">
        {certs.length === 0 && (
          <Card>
            <CardContent className="flex flex-col items-center py-12 text-center">
              <FileKey className="h-12 w-12 text-muted-foreground opacity-50 mb-4" />
              <h3 className="text-lg font-medium">No certificates</h3>
              <p className="text-sm text-muted-foreground mt-1 max-w-sm">
                Issue your first TLS certificate using the built-in ACME CA. Supports HTTP-01 and DNS-01 challenges.
              </p>
            </CardContent>
          </Card>
        )}

        {certs.map((cert) => {
          const days = daysUntilExpiry(cert.not_after)
          return (
            <Card key={cert.id} className="transition-all hover:border-primary/30">
              <CardContent className="flex items-center gap-4 py-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-muted">
                  <Globe className="h-5 w-5 text-muted-foreground" />
                </div>

                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <h3 className="font-medium">{cert.domain}</h3>
                    {getStatusBadge(cert.status)}
                    {cert.auto_renew && (
                      <Badge variant="secondary" className="text-[10px]">auto-renew</Badge>
                    )}
                  </div>
                  <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                    <span className="flex items-center gap-1">
                      {getStatusIcon(cert.status)}
                      {cert.issuer || 'Gresbase Internal CA'}
                    </span>
                    {cert.challenge_type && (
                      <Badge variant="outline" className="text-[10px]">
                        {cert.challenge_type.toUpperCase()}
                      </Badge>
                    )}
                    {cert.not_after && (
                      <span className="flex items-center gap-1">
                        <Calendar className="h-3 w-3" />
                        Expires {new Date(cert.not_after).toLocaleDateString()}
                        {days !== null && (
                          <span className={days <= 30 ? 'text-amber-500' : 'text-muted-foreground'}>
                            ({days}d)
                          </span>
                        )}
                      </span>
                    )}
                  </div>
                </div>

                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => handleRevoke(cert.id)}
                  title="Revoke certificate"
                  disabled={cert.status !== 'active'}
                >
                  <Trash2 className="h-4 w-4 text-muted-foreground hover:text-red-500" />
                </Button>
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
