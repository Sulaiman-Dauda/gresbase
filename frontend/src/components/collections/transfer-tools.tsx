'use client'

import * as React from 'react'
import { Download, Loader2, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export function CollectionsTransferTools({ onImported }: { onImported?: () => void }) {
  const [importPayload, setImportPayload] = React.useState('')
  const [deleteMissing, setDeleteMissing] = React.useState(false)
  const [busy, setBusy] = React.useState<'export' | 'import' | null>(null)

  const handleExport = async () => {
    setBusy('export')
    try {
      const result = await api.exportCollections()
      const payload = JSON.stringify(result.collections || [], null, 2)
      setImportPayload(payload)
      const blob = new Blob([payload], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = 'gresbase-collections.json'
      anchor.click()
      URL.revokeObjectURL(url)
      toast.success('Collections exported')
    } catch (err: any) {
      toast.error(err.message || 'Failed to export collections')
    } finally {
      setBusy(null)
    }
  }

  const handleImport = async () => {
    setBusy('import')
    try {
      const parsed = JSON.parse(importPayload)
      const collections = Array.isArray(parsed) ? parsed : parsed.collections
      if (!Array.isArray(collections)) {
        throw new Error('Expected a collections array')
      }
      await api.importCollections(collections, deleteMissing)
      toast.success('Collections imported')
      onImported?.()
    } catch (err: any) {
      toast.error(err.message || 'Failed to import collections')
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Import / export</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" onClick={handleExport} disabled={busy !== null}>
            {busy === 'export' ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
            Export collections
          </Button>
          <Button onClick={handleImport} disabled={busy !== null || !importPayload.trim()}>
            {busy === 'import' ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Upload className="mr-2 h-4 w-4" />}
            Import collections
          </Button>
        </div>

        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input type="checkbox" checked={deleteMissing} onChange={(e) => setDeleteMissing(e.target.checked)} />
          Delete collections missing from the import payload
        </label>

        <textarea
          value={importPayload}
          onChange={(e) => setImportPayload(e.target.value)}
          rows={12}
          className="w-full rounded-lg border border-input bg-background px-3 py-2 font-mono text-sm"
          placeholder='Paste exported collections JSON here'
        />
      </CardContent>
    </Card>
  )
}
