import { useState, useEffect } from 'react'
import Dialog from '@/components/ui/dialog'
import Button from '@/components/ui/button'
import Input from '@/components/ui/input'
import Select from '@/components/ui/select'
import { blockIP } from '@/lib/killswitchProxyClient'

// ── Types ────────────────────────────────────────────────────────────────────

export type SuspiciousType = 'scanner' | 'sprayer' | 'flooder'

export interface BlockIPModalData {
  type: SuspiciousType
  ip: string
  host?: string    // available for scanners and flooders
  path?: string    // available for flooders (the most-hit path)
}

interface BlockIPModalProps {
  open: boolean
  onClose: () => void
  data: BlockIPModalData | null
  onSuccess: (message: string) => void
  onError: (message: string) => void
}

// ── Expiration presets ───────────────────────────────────────────────────────

const EXPIRATION_OPTIONS = [
  { label: '1 hour', hours: 1 },
  { label: '6 hours', hours: 6 },
  { label: '12 hours', hours: 12 },
  { label: '24 hours', hours: 24 },
  { label: '48 hours', hours: 48 },
  { label: '72 hours', hours: 72 },
] as const

function computeExpiresAt(hours: number): string {
  const d = new Date(Date.now() + hours * 60 * 60 * 1000)
  return d.toISOString()
}

// ── Component ────────────────────────────────────────────────────────────────

export default function BlockIPModal({ open, onClose, data, onSuccess, onError }: BlockIPModalProps) {
  // ── State ────────────────────────────────────────────────────────────────
  const [method, setMethod] = useState('*')
  const [domain, setDomain] = useState('*')
  const [path, setPath] = useState('*')
  const [expirationHours, setExpirationHours] = useState(24)
  const [reason, setReason] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  // ── Reset on open ────────────────────────────────────────────────────────
  useEffect(() => {
    if (!open || !data) return

    setError('')
    setSubmitting(false)
    setExpirationHours(24)
    setReason('')

    switch (data.type) {
      case 'scanner':
        // Scanners: IP + host known, path is open
        setMethod('*')
        setDomain(data.host || '*')
        setPath('*')
        break
      case 'sprayer':
        // Sprayers: IP only, everything else global
        setMethod('*')
        setDomain('*')
        setPath('*')
        break
      case 'flooder':
        // Flooders: IP + host + path known
        setMethod('*')
        setDomain(data.host || '*')
        setPath(data.path || '*')
        break
    }
  }, [open, data])

  // ── Computed field states ─────────────────────────────────────────────────
  const ipDisabled = true
  const methodDisabled = data?.type === 'sprayer' || data?.type === 'flooder'
  const domainDisabled = data?.type !== undefined // all types: disabled (pre-populated or global)
  const pathDisabled = data?.type === 'sprayer'
  const pathEditable = data?.type === 'flooder' || data?.type === 'scanner'

  // ── Submit ───────────────────────────────────────────────────────────────
  const handleSubmit = async () => {
    if (!data) return

    // Validate
    const trimmedReason = reason.trim()
    if (!trimmedReason) {
      setError('Reason is required')
      return
    }
    if (trimmedReason.length > 20) {
      setError('Reason must be 20 characters or less')
      return
    }

    setError('')
    setSubmitting(true)

    try {
      await blockIP({
        ip: data.ip,
        method: method || '*',
        domain: domain || '*',
        path: path || '*',
        expires_at: computeExpiresAt(expirationHours),
        reason: trimmedReason,
      })

      const scopeDesc = domain === '*' ? 'globally' : `on ${domain}`
      onSuccess(`Blocked ${data.ip} ${scopeDesc} for ${expirationHours}h`)
      onClose()
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to block IP'
      setError(msg)
      onError(msg)
    } finally {
      setSubmitting(false)
    }
  }

  if (!data) return null

  const typeLabel = data.type === 'scanner' ? 'Scanner' : data.type === 'sprayer' ? 'Sprayer' : 'Flooder'

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={`Block IP — ${typeLabel}`}
      footer={
        <>
          <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button variant="danger" size="sm" onClick={handleSubmit} disabled={submitting}>
            {submitting ? 'Blocking...' : 'Block IP'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {/* IP Address — always locked */}
        <Input
          label="IP Address"
          value={data.ip}
          disabled={ipDisabled}
          readOnly
        />

        {/* Method */}
        <Select
          label="HTTP Method"
          value={method}
          onChange={(e) => setMethod(e.target.value)}
          disabled={methodDisabled}
        >
          <option value="*">* (all methods)</option>
          <option value="GET">GET</option>
          <option value="POST">POST</option>
          <option value="PUT">PUT</option>
          <option value="PATCH">PATCH</option>
          <option value="DELETE">DELETE</option>
          <option value="HEAD">HEAD</option>
          <option value="OPTIONS">OPTIONS</option>
        </Select>

        {/* Domain */}
        <Input
          label="Domain"
          value={domain}
          onChange={(e) => setDomain(e.target.value)}
          disabled={domainDisabled}
          readOnly={domainDisabled}
          placeholder="* (all domains)"
        />

        {/* Path */}
        <div>
          <Input
            label="Path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            disabled={pathDisabled}
            readOnly={pathDisabled}
            placeholder="* (all paths)"
          />
          {data.type === 'flooder' && data.path && pathEditable && (
            <p className="mt-1 text-xs text-muted-foreground">
              Flooder is targeting: <code className="bg-muted px-1 py-0.5 rounded font-mono">{data.path}</code>
            </p>
          )}
        </div>

        {/* Expiration */}
        <Select
          label="Expiration"
          value={String(expirationHours)}
          onChange={(e) => setExpirationHours(Number(e.target.value))}
          required
        >
          {EXPIRATION_OPTIONS.map((opt) => (
            <option key={opt.hours} value={String(opt.hours)}>
              {opt.label}
            </option>
          ))}
        </Select>

        {/* Reason */}
        <Input
          label="Reason"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder="e.g. bot, abuse, scanner"
          maxLength={20}
          required
        />
        <p className="text-xs text-muted-foreground -mt-2">
          {reason.length}/20 characters
        </p>

        {/* Error display */}
        {error && (
          <div className="rounded-md border border-red-500 bg-red-500/10 px-3 py-2">
            <p className="text-sm text-red-500">{error}</p>
          </div>
        )}
      </div>
    </Dialog>
  )
}
