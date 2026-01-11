import { useState } from 'react'
import { Search, Sun, Moon, Monitor } from 'lucide-react'
import { policiesApi } from '@/lib/api'
import type { EffectivePolicy } from '@/lib/types'
import { useToast } from '@/hooks/useToast'
import { useTheme } from '@/hooks/useTheme'
import Button from '@/components/ui/button'
import Input from '@/components/ui/input'
import Badge from '@/components/ui/badge'
import Loading from '@/components/ui/loading'
import Tooltip from '@/components/ui/tooltip'

export default function EffectivePage() {
  const [host, setHost] = useState('')
  const [loading, setLoading] = useState(false)
  const [policy, setPolicy] = useState<EffectivePolicy | null>(null)
  const { error } = useToast()
  const { theme, setTheme } = useTheme()

  const handleCheck = async () => {
    if (!host.trim()) {
      error('Please enter a host')
      return
    }

    try {
      setLoading(true)
      const data = await policiesApi.effective(host.trim())
      setPolicy(data)
    } catch (err) {
      error('Failed to check effective policy')
      setPolicy(null)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-b bg-card">
        <div className="container mx-auto px-4 py-4 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <img
              src="/favicon.svg"
              alt="Arbiter"
              className="w-8 h-8"
            />
            <h1 className="text-2xl font-bold">Arbiter</h1>
            {/* Theme Selector */}
            <div className="flex gap-1 border rounded-md p-1 bg-background ml-2">
              <Button
                variant={theme === 'light' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('light')}
                className="h-8 w-8 p-0"
                aria-label="Light theme"
                title="Light"
              >
                <Sun className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === 'dark' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('dark')}
                className="h-8 w-8 p-0"
                aria-label="Dark theme"
                title="Dark"
              >
                <Moon className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === 'system' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('system')}
                className="h-8 w-8 p-0"
                aria-label="System theme"
                title="System"
              >
                <Monitor className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      </header>

      <div className="container mx-auto px-4 py-6 max-w-4xl">
        <h2 className="text-2xl font-bold mb-6">Effective Policy Check</h2>

      <div className="bg-card rounded-lg border p-6 mb-6">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2">Host</label>
            <div className="flex gap-2">
              <Input
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="example.com"
                onKeyDown={(e) => e.key === 'Enter' && handleCheck()}
              />
              <Button onClick={handleCheck} disabled={loading}>
                <Search className="h-4 w-4 mr-2" />
                Check
              </Button>
            </div>
          </div>
        </div>
      </div>

      {loading && <Loading />}

      {policy && (
        <div className="bg-card rounded-lg border p-6">
          <h2 className="text-xl font-semibold mb-4">Effective Policy</h2>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Host:</span>
              <span className="font-mono text-sm">{policy.host}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Killswitch Required:</span>
              {policy.killswitch_required ? (
                <Tooltip content="Killswitch service is required for this host. Requests must pass through Killswitch before proceeding.">
                  <Badge variant="success">Required</Badge>
                </Tooltip>
              ) : (
                <Tooltip content="Killswitch service is not required for this host. Requests can proceed without Killswitch checks.">
                  <Badge variant="danger">－</Badge>
                </Tooltip>
              )}
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Gatekeeper Required:</span>
              {policy.gatekeeper_required ? (
                <Tooltip content="Gatekeeper service is required for this host. Requests must pass through Gatekeeper authorization before proceeding.">
                  <Badge variant="success">Required</Badge>
                </Tooltip>
              ) : (
                <Tooltip content="Gatekeeper service is not required for this host. Requests can proceed without Gatekeeper authorization checks.">
                  <Badge variant="danger">－</Badge>
                </Tooltip>
              )}
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Source:</span>
              <Badge variant={policy.source === 'forced' ? 'warning' : 'default'}>
                {policy.source}
              </Badge>
            </div>
            {policy.forced_killswitch && (
              <div className="text-sm text-warning">
                Killswitch requirement forced to false (public host)
              </div>
            )}
            {policy.forced_gatekeeper && (
              <div className="text-sm text-warning">
                Gatekeeper requirement forced to false (public host)
              </div>
            )}
            {policy.killswitch_public_host && (
              <div className="text-sm text-muted-foreground">
                Killswitch Public Host: {policy.killswitch_public_host}
              </div>
            )}
            {policy.gatekeeper_public_host && (
              <div className="text-sm text-muted-foreground">
                Gatekeeper Public Host: {policy.gatekeeper_public_host}
              </div>
            )}
          </div>
        </div>
      )}
      </div>
    </div>
  )
}
