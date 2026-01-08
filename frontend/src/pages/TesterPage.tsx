import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { policiesApi } from '@/lib/api'
import type { TestCheckResponse } from '@/lib/types'
import { useToast } from '@/hooks/useToast'
import { useTheme } from '@/hooks/useTheme'
import Button from '@/components/ui/button'
import Input from '@/components/ui/input'
import Select from '@/components/ui/select'
import Badge from '@/components/ui/badge'
import Loading from '@/components/ui/loading'
import { ArrowLeft, CheckCircle2, XCircle, AlertCircle, Sun, Moon, Monitor } from 'lucide-react'

export default function TesterPage() {
  const navigate = useNavigate()
  const { error: showError } = useToast()
  const { theme, setTheme } = useTheme()
  const [host, setHost] = useState('')
  const [method, setMethod] = useState('GET')
  const [uri, setUri] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<TestCheckResponse | null>(null)
  const [errors, setErrors] = useState<{ host?: string; method?: string; uri?: string }>({})

  const validate = (): boolean => {
    const newErrors: { host?: string; method?: string; uri?: string } = {}

    if (!host.trim()) {
      newErrors.host = 'Host is required'
    }
    if (!method.trim()) {
      newErrors.method = 'Method is required'
    }
    if (!uri.trim()) {
      newErrors.uri = 'URI is required'
    }

    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!validate()) {
      return
    }

    setLoading(true)
    setResult(null)
    setErrors({})

    try {
      const data = await policiesApi.testCheck(host.trim(), method.trim(), uri.trim())
      // Ensure latency_ms exists, default to 0 if missing
      if (data.latency_ms === undefined) {
        data.latency_ms = 0.0
      }
      setResult(data)
    } catch (err: any) {
      const errorMessage = err?.response?.data?.error || err?.response?.data?.message || err?.message || 'Failed to test arbiter check'
      showError(errorMessage)
      // Set error result
      setResult({
        decision: 'error',
        status: 500,
        reason: errorMessage,
        source: 'none',
        policy: 'none',
        trace_id: '',
        normalized: {
          host: host.trim(),
          method: method.trim(),
          uri: '',
        },
        latency_ms: 0.0,
        total_latency_ms: 0.0,
        killswitch_latency_ms: 0.0,
        gatekeeper_latency_ms: 0.0,
        nginx_headers: {
          'X-Auth-Decision': 'error',
          'X-Auth-Reason': errorMessage,
          'X-Auth-Source': 'none',
          'X-Auth-Policy': 'none',
          'X-Auth-Trace': '',
        },
      })
    } finally {
      setLoading(false)
    }
  }

  const getDecisionBadge = (decision: string) => {
    switch (decision) {
      case 'allow':
        return <Badge variant="success">ALLOW</Badge>
      case 'unauth':
        return <Badge variant="warning">UNAUTH</Badge>
      case 'forbid':
        return <Badge variant="danger">FORBID</Badge>
      case 'killswitch':
        return <Badge variant="danger">KILLSWITCH</Badge>
      case 'error':
        return <Badge variant="danger">ERROR</Badge>
      default:
        return <Badge>{decision.toUpperCase()}</Badge>
    }
  }

  const getDecisionIcon = (decision: string) => {
    switch (decision) {
      case 'allow':
        return <CheckCircle2 className="h-5 w-5 text-green-500" />
      case 'unauth':
      case 'forbid':
      case 'killswitch':
        return <XCircle className="h-5 w-5 text-red-500" />
      case 'error':
        return <AlertCircle className="h-5 w-5 text-red-500" />
      default:
        return null
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
            <h1 className="text-2xl font-bold">Arbiter Tester</h1>
          </div>
          <div className="flex items-center gap-4">
            <Button
              variant="outline"
              onClick={() => navigate('/')}
            >
              <ArrowLeft className="h-4 w-4 mr-2" />
              Back to Policies
            </Button>
            {/* Theme Selector */}
            <div className="flex gap-1 border rounded-md p-1 bg-background">
              <button
                onClick={() => setTheme('light')}
                className={`p-1.5 rounded ${theme === 'light' ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'}`}
                aria-label="Light theme"
              >
                <Sun className="h-4 w-4" />
              </button>
              <button
                onClick={() => setTheme('dark')}
                className={`p-1.5 rounded ${theme === 'dark' ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'}`}
                aria-label="Dark theme"
              >
                <Moon className="h-4 w-4" />
              </button>
              <button
                onClick={() => setTheme('system')}
                className={`p-1.5 rounded ${theme === 'system' ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'}`}
                aria-label="System theme"
              >
                <Monitor className="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>
      </header>

      <div className="container mx-auto px-4 py-6 max-w-4xl">
        {/* Form Section */}
        <div className="border rounded-lg p-6 bg-card mb-6">
          <h2 className="text-xl font-semibold mb-4">Test Arbiter Check</h2>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <Input
                label="Host"
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="example.com"
                required
                error={errors.host}
              />
              <Select
                label="Method"
                value={method}
                onChange={(e) => setMethod(e.target.value)}
                required
                error={errors.method}
              >
                <option value="GET">GET</option>
                <option value="POST">POST</option>
                <option value="PUT">PUT</option>
                <option value="PATCH">PATCH</option>
                <option value="DELETE">DELETE</option>
                <option value="HEAD">HEAD</option>
                <option value="OPTIONS">OPTIONS</option>
              </Select>
              <Input
                label="URI"
                value={uri}
                onChange={(e) => setUri(e.target.value)}
                placeholder="/api/users?foo=bar"
                required
                error={errors.uri}
              />
            </div>
            <div className="text-sm text-muted-foreground">
              <p>Note: Your current session cookies (gk_sid and gk_csrf) will be automatically included in the test request.</p>
            </div>
            <Button
              type="submit"
              disabled={loading}
              className="w-full md:w-auto"
            >
              {loading ? (
                <>
                  <span className="mr-2">
                    <Loading />
                  </span>
                  Checking...
                </>
              ) : (
                'Check'
              )}
            </Button>
          </form>
        </div>

        {/* Results Section */}
        {result && (
          <div className="border rounded-lg p-6 bg-card">
            <h2 className="text-xl font-semibold mb-4">Results</h2>
            <div className="space-y-4">
              {/* Decision */}
              <div className="flex items-center gap-3">
                <span className="text-sm font-medium">Decision:</span>
                <div className="flex items-center gap-2">
                  {getDecisionIcon(result.decision)}
                  {getDecisionBadge(result.decision)}
                </div>
              </div>

              {/* Status */}
              <div className="flex items-center gap-3">
                <span className="text-sm font-medium">Would return:</span>
                <Badge variant="default" className="font-mono">
                  HTTP {result.status}
                </Badge>
              </div>

              {/* Latency */}
              <div className="border-t pt-4">
                <span className="text-sm font-medium mb-3 block">Latency Breakdown:</span>
                <div className="space-y-2 text-sm">
                  <div className="flex items-center justify-between gap-4">
                    <span className="font-medium">Total:</span>
                    <Badge variant="default" className="font-mono">
                      {(result.total_latency_ms ?? result.latency_ms ?? 0).toFixed(3)} ms
                    </Badge>
                  </div>
                  <div className="flex items-center justify-between gap-4">
                    <span className="font-medium text-muted-foreground">Killswitch:</span>
                    <Badge variant="default" className="font-mono">
                      {(result.killswitch_latency_ms ?? 0).toFixed(3)} ms
                    </Badge>
                  </div>
                  <div className="flex items-center justify-between gap-4">
                    <span className="font-medium text-muted-foreground">Gatekeeper:</span>
                    <Badge variant="default" className="font-mono">
                      {(result.gatekeeper_latency_ms ?? 0).toFixed(3)} ms
                    </Badge>
                  </div>
                </div>
              </div>

              {/* Source */}
              <div>
                <span className="text-sm font-medium">Source:</span>
                <div className="mt-1">
                  <code className="text-sm bg-muted px-2 py-1 rounded">
                    {result.source || 'none'}
                  </code>
                </div>
              </div>

              {/* Policy */}
              <div>
                <span className="text-sm font-medium">Policy:</span>
                <div className="mt-1">
                  {result.policy ? (
                    <code className="text-sm bg-muted px-2 py-1 rounded">
                      {result.policy}
                    </code>
                  ) : (
                    <span className="text-sm text-muted-foreground">None</span>
                  )}
                </div>
              </div>

              {/* Trace ID */}
              {result.trace_id && (
                <div>
                  <span className="text-sm font-medium">Trace ID:</span>
                  <div className="mt-1">
                    <code className="text-sm bg-muted px-2 py-1 rounded">
                      {result.trace_id}
                    </code>
                  </div>
                </div>
              )}

              {/* Reason */}
              <div>
                <span className="text-sm font-medium">Reason:</span>
                <div className="mt-1">
                  {result.reason ? (
                    <p className="text-sm">{result.reason}</p>
                  ) : (
                    <span className="text-sm text-muted-foreground">—</span>
                  )}
                </div>
              </div>

              {/* Normalized Values */}
              <div className="border-t pt-4">
                <span className="text-sm font-medium mb-2 block">Normalized values:</span>
                <div className="space-y-2 text-sm">
                  <div className="flex gap-2">
                    <span className="font-medium w-20">Host:</span>
                    <code className="bg-muted px-2 py-1 rounded flex-1">
                      {result.normalized.host || <span className="text-muted-foreground">—</span>}
                    </code>
                  </div>
                  <div className="flex gap-2">
                    <span className="font-medium w-20">Method:</span>
                    <code className="bg-muted px-2 py-1 rounded flex-1">
                      {result.normalized.method || <span className="text-muted-foreground">—</span>}
                    </code>
                  </div>
                  <div className="flex gap-2">
                    <span className="font-medium w-20">URI:</span>
                    <code className="bg-muted px-2 py-1 rounded flex-1">
                      {result.normalized.uri || <span className="text-muted-foreground">—</span>}
                    </code>
                  </div>
                </div>
              </div>

              {/* Identity Headers */}
              {result.identity_headers && Object.keys(result.identity_headers).length > 0 && (
                <div className="border-t pt-4">
                  <span className="text-sm font-medium mb-2 block">Identity Headers:</span>
                  <div className="space-y-2 text-sm">
                    {Object.entries(result.identity_headers).map(([key, value]) => (
                      <div key={key} className="flex gap-2">
                        <span className="font-medium w-48">{key}:</span>
                        <code className="bg-muted px-2 py-1 rounded flex-1">
                          {value}
                        </code>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Killswitch Headers */}
              {result.killswitch_headers && Object.keys(result.killswitch_headers).length > 0 && (
                <div className="border-t pt-4">
                  <span className="text-sm font-medium mb-2 block">Killswitch Headers:</span>
                  <div className="space-y-2 text-sm">
                    {Object.entries(result.killswitch_headers).map(([key, value]) => (
                      <div key={key} className="flex gap-2">
                        <span className="font-medium w-48">{key}:</span>
                        <code className="bg-muted px-2 py-1 rounded flex-1">
                          {value}
                        </code>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* NGINX Headers - All headers that would be sent back to NGINX */}
              {result.nginx_headers && Object.keys(result.nginx_headers).length > 0 && (
                <div className="border-t pt-4">
                  <span className="text-sm font-medium mb-2 block">NGINX Response Headers:</span>
                  <div className="text-xs text-muted-foreground mb-2">
                    All headers that would be sent back to NGINX via auth_request
                  </div>
                  <div className="space-y-2 text-sm">
                    {Object.entries(result.nginx_headers).map(([key, value]) => (
                      <div key={key} className="flex gap-2">
                        <span className="font-medium w-48">{key}:</span>
                        <code className="bg-muted px-2 py-1 rounded flex-1">
                          {value}
                        </code>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
