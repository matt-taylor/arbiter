import { useState, useEffect, useRef, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import axios from 'axios'
import {
  getSummary,
  getTopIPs,
  getTopPaths,
  getOverviewTopHosts,
  getOverviewSuspiciousScanners,
  getOverviewSuspiciousSprayers,
  type SummaryResponse,
  type TopIPsResponse,
  type TopPathsResponse,
  type TopHostsOverviewResponse,
  type SuspiciousScannersOverviewResponse,
  type SuspiciousSprayersOverviewResponse,
  type StatusBreakdown,
} from '@/lib/telemetryClient'
import Button from '@/components/ui/button'
import Input from '@/components/ui/input'
import Select from '@/components/ui/select'
import Loading from '@/components/ui/loading'
import Badge from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { RefreshCw, ChevronDown, ChevronUp, X } from 'lucide-react'

// ── Constants ────────────────────────────────────────────────────────────────

const MAX_WINDOW = 60
const MAX_LIMIT = 100
const TOP_IPS_LIMIT = 20
const TOP_PATHS_LIMIT = 50
const DEBOUNCE_MS = 300
const AUTO_REFRESH_MS = 10_000

// ── Helpers ──────────────────────────────────────────────────────────────────

function clampWindow(v: number): number {
  if (!Number.isFinite(v) || v < 1) return 5
  return Math.min(v, MAX_WINDOW)
}

function clampLimit(v: number, max = MAX_LIMIT): number {
  if (!Number.isFinite(v) || v < 1) return 20
  return Math.min(v, max)
}

function parseEndTs(raw: string | null): number | undefined {
  if (!raw) return undefined
  const n = parseInt(raw, 10)
  if (!Number.isFinite(n) || n <= 0) return undefined
  const now = Math.floor(Date.now() / 1000)
  if (n > now + 60) return now
  return n
}

function errorWithRequestId(err: unknown): { message: string; requestId?: string } {
  if (err instanceof Error) {
    return { message: err.message, requestId: (err as any).request_id }
  }
  return { message: String(err) }
}

// ── Status column helpers ────────────────────────────────────────────────────

const STATUS_COLS: Array<{ key: keyof StatusBreakdown; label: string }> = [
  { key: 'c_401', label: '401' },
  { key: 'c_403', label: '403' },
  { key: 'c_404', label: '404' },
  { key: 'c_429', label: '429' },
  { key: 'c_5xx', label: '5xx' },
]

// ── Types ─────────────────────────────────────────────────────────────────────

type TelemetryMode = 'overview' | 'host'

// ── Component ────────────────────────────────────────────────────────────────

export default function TelemetryPage() {
  const [searchParams, setSearchParams] = useSearchParams()

  // ── State from URL ───────────────────────────────────────────────────────
  const [selectedHost, setSelectedHost] = useState(() => searchParams.get('host') || '')
  const [windowMinutes, setWindowMinutes] = useState(() => clampWindow(parseInt(searchParams.get('window') || '5', 10)))
  const [endTs, setEndTs] = useState<number | undefined>(() => parseEndTs(searchParams.get('end_ts')))
  const [selectedIP, setSelectedIP] = useState<string | null>(() => searchParams.get('ip') || null)
  const [autoRefresh, setAutoRefresh] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(() => searchParams.has('end_ts'))

  // ── Mode state ─────────────────────────────────────────────────────────
  const [mode, setMode] = useState<TelemetryMode>(() =>
    searchParams.get('host') ? 'host' : 'overview'
  )

  // ── Data state (host drilldown) ────────────────────────────────────────
  const [summary, setSummary] = useState<SummaryResponse | null>(null)
  const [topIPs, setTopIPs] = useState<TopIPsResponse | null>(null)
  const [topPaths, setTopPathsData] = useState<TopPathsResponse | null>(null)

  // ── Data state (overview) ──────────────────────────────────────────────
  const [overviewTopHosts, setOverviewTopHosts] = useState<TopHostsOverviewResponse | null>(null)
  const [overviewScanners, setOverviewScanners] = useState<SuspiciousScannersOverviewResponse | null>(null)
  const [overviewSprayers, setOverviewSprayers] = useState<SuspiciousSprayersOverviewResponse | null>(null)

  // ── Loading / error ──────────────────────────────────────────────────────
  const [batchLoading, setBatchLoading] = useState(false)
  const [pathsLoading, setPathsLoading] = useState(false)
  const [batchError, setBatchError] = useState<{ message: string; requestId?: string } | null>(null)
  const [pathsError, setPathsError] = useState<{ message: string; requestId?: string } | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(false)
  const [overviewError, setOverviewError] = useState<{ message: string; requestId?: string } | null>(null)

  // ── Refs ──────────────────────────────────────────────────────────────────
  const batchAbortRef = useRef<AbortController | null>(null)
  const pathsAbortRef = useRef<AbortController | null>(null)
  const overviewAbortRef = useRef<AbortController | null>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const overviewDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const batchInFlightRef = useRef(false)
  const pathsInFlightRef = useRef(false)
  const overviewInFlightRef = useRef(false)
  const prevHostRef = useRef(selectedHost)
  const pathsSectionRef = useRef<HTMLDivElement>(null)

  // ── URL sync (write) ─────────────────────────────────────────────────────
  useEffect(() => {
    const next = new URLSearchParams()
    if (selectedHost) next.set('host', selectedHost)
    if (windowMinutes !== 5) next.set('window', String(windowMinutes))
    if (endTs !== undefined) next.set('end_ts', String(endTs))
    if (selectedIP) next.set('ip', selectedIP)

    const current = searchParams.toString()
    const nextStr = next.toString()
    if (current !== nextStr) {
      setSearchParams(next, { replace: true })
    }
  }, [selectedHost, windowMinutes, endTs, selectedIP]) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Fetch batch (summary + top IPs) ──────────────────────────────────────
  const fetchBatch = useCallback(async (host: string, win: number, ets?: number) => {
    if (!host.trim()) return

    // Abort previous
    batchAbortRef.current?.abort()
    const controller = new AbortController()
    batchAbortRef.current = controller

    setBatchLoading(true)
    setBatchError(null)
    batchInFlightRef.current = true

    try {
      const [summaryRes, ipsRes] = await Promise.all([
        getSummary(host, win, ets, controller.signal),
        getTopIPs(host, win, clampLimit(TOP_IPS_LIMIT), ets, controller.signal),
      ])
      setSummary(summaryRes)
      setTopIPs(ipsRes)
    } catch (err) {
      if (axios.isCancel(err)) return
      setBatchError(errorWithRequestId(err))
      setSummary(null)
      setTopIPs(null)
    } finally {
      setBatchLoading(false)
      batchInFlightRef.current = false
    }
  }, [])

  // ── Fetch top paths ──────────────────────────────────────────────────────
  const fetchPaths = useCallback(async (host: string, ip: string, win: number, ets?: number) => {
    if (!host.trim() || !ip) return

    pathsAbortRef.current?.abort()
    const controller = new AbortController()
    pathsAbortRef.current = controller

    setPathsLoading(true)
    setPathsError(null)
    pathsInFlightRef.current = true

    try {
      const res = await getTopPaths(host, ip, win, clampLimit(TOP_PATHS_LIMIT), ets, controller.signal)
      setTopPathsData(res)
    } catch (err) {
      if (axios.isCancel(err)) return
      setPathsError(errorWithRequestId(err))
      setTopPathsData(null)
    } finally {
      setPathsLoading(false)
      pathsInFlightRef.current = false
    }
  }, [])

  // ── Fetch overview (all 3 endpoints) ────────────────────────────────────
  const fetchOverview = useCallback(async (win: number, ets?: number) => {
    overviewAbortRef.current?.abort()
    const controller = new AbortController()
    overviewAbortRef.current = controller

    setOverviewLoading(true)
    setOverviewError(null)
    overviewInFlightRef.current = true

    try {
      const [hostsRes, scannersRes, sprayersRes] = await Promise.all([
        getOverviewTopHosts(win, 20, ets, controller.signal),
        getOverviewSuspiciousScanners(win, 50, ets, controller.signal),
        getOverviewSuspiciousSprayers(win, 50, ets, controller.signal),
      ])
      setOverviewTopHosts(hostsRes)
      setOverviewScanners(scannersRes)
      setOverviewSprayers(sprayersRes)
    } catch (err) {
      if (axios.isCancel(err)) return
      setOverviewError(errorWithRequestId(err))
    } finally {
      setOverviewLoading(false)
      overviewInFlightRef.current = false
    }
  }, [])

  // ── Mode-switch side effects ─────────────────────────────────────────────
  useEffect(() => {
    if (mode === 'overview') {
      // Abort host-mode in-flight requests, clear errors (keep cached data)
      batchAbortRef.current?.abort()
      pathsAbortRef.current?.abort()
      setBatchError(null)
      setPathsError(null)
    } else {
      // Abort overview in-flight requests, clear errors (keep cached data)
      overviewAbortRef.current?.abort()
      setOverviewError(null)
    }
  }, [mode])

  // ── Debounced fetch on host/window/endTs change (host mode) ─────────────
  useEffect(() => {
    if (mode !== 'host') return

    // If host changed, clear selected IP to avoid stale drilldown
    if (prevHostRef.current !== selectedHost) {
      prevHostRef.current = selectedHost
      setSelectedIP(null)
      setTopPathsData(null)
      setPathsError(null)
    }

    if (debounceRef.current) clearTimeout(debounceRef.current)

    debounceRef.current = setTimeout(() => {
      fetchBatch(selectedHost, windowMinutes, endTs)
    }, DEBOUNCE_MS)

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [selectedHost, windowMinutes, endTs, fetchBatch, mode])

  // ── Debounced fetch on window/endTs change (overview mode) ──────────────
  useEffect(() => {
    if (mode !== 'overview') return

    if (overviewDebounceRef.current) clearTimeout(overviewDebounceRef.current)

    overviewDebounceRef.current = setTimeout(() => {
      fetchOverview(windowMinutes, endTs)
    }, DEBOUNCE_MS)

    return () => {
      if (overviewDebounceRef.current) clearTimeout(overviewDebounceRef.current)
    }
  }, [windowMinutes, endTs, fetchOverview, mode])

  // ── Fetch paths when selectedIP changes (host mode) ─────────────────────
  useEffect(() => {
    if (mode !== 'host') return

    if (selectedIP) {
      fetchPaths(selectedHost, selectedIP, windowMinutes, endTs)
      // Scroll to paths section
      setTimeout(() => {
        pathsSectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      }, 100)
    } else {
      pathsAbortRef.current?.abort()
      setTopPathsData(null)
      setPathsError(null)
    }
  }, [selectedIP, mode]) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Auto-refresh ─────────────────────────────────────────────────────────
  useEffect(() => {
    if (!autoRefresh) return

    if (mode === 'host' && !selectedHost.trim()) return

    const id = setInterval(() => {
      if (mode === 'overview') {
        if (overviewInFlightRef.current) return
        fetchOverview(windowMinutes, endTs)
      } else {
        if (batchInFlightRef.current || pathsInFlightRef.current) return
        fetchBatch(selectedHost, windowMinutes, endTs)
        if (selectedIP) {
          fetchPaths(selectedHost, selectedIP, windowMinutes, endTs)
        }
      }
    }, AUTO_REFRESH_MS)

    return () => clearInterval(id)
  }, [autoRefresh, mode, selectedHost, windowMinutes, endTs, selectedIP, fetchBatch, fetchPaths, fetchOverview])

  // ── Cleanup on unmount ───────────────────────────────────────────────────
  useEffect(() => {
    return () => {
      batchAbortRef.current?.abort()
      pathsAbortRef.current?.abort()
      overviewAbortRef.current?.abort()
    }
  }, [])

  // ── Handlers ─────────────────────────────────────────────────────────────
  const handleRefresh = () => {
    if (mode === 'overview') {
      if (overviewDebounceRef.current) clearTimeout(overviewDebounceRef.current)
      fetchOverview(windowMinutes, endTs)
    } else {
      if (debounceRef.current) clearTimeout(debounceRef.current)
      fetchBatch(selectedHost, windowMinutes, endTs)
      if (selectedIP) {
        fetchPaths(selectedHost, selectedIP, windowMinutes, endTs)
      }
    }
  }

  const handleIPClick = (ip: string) => {
    setSelectedIP(ip)
  }

  const handleClearIP = () => {
    setSelectedIP(null)
  }

  const handleEndTsChange = (raw: string) => {
    if (!raw.trim()) {
      setEndTs(undefined)
      return
    }
    const parsed = parseInt(raw, 10)
    if (Number.isFinite(parsed) && parsed > 0) {
      const now = Math.floor(Date.now() / 1000)
      setEndTs(parsed > now + 60 ? now : parsed)
    }
  }

  // ── Sorted items ─────────────────────────────────────────────────────────
  const sortedIPs = topIPs?.items ? [...topIPs.items].sort((a, b) => b.total - a.total) : []
  const sortedPaths = topPaths?.items ? [...topPaths.items].sort((a, b) => b.total - a.total) : []

  // ── Render ───────────────────────────────────────────────────────────────
  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Telemetry</h1>

      {/* ── Controls ──────────────────────────────────────────────────── */}
      <div className="border rounded-lg p-6 bg-card mb-6">
        {/* Mode toggle */}
        <div className="flex items-center gap-2 mb-4">
          <Button
            variant={mode === 'overview' ? 'default' : 'outline'}
            size="sm"
            onClick={() => setMode('overview')}
          >
            Overview
          </Button>
          <Button
            variant={mode === 'host' ? 'default' : 'outline'}
            size="sm"
            onClick={() => setMode('host')}
          >
            Host drilldown
          </Button>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-4 gap-4 items-end">
          <Input
            label="Host"
            value={selectedHost}
            onChange={(e) => setSelectedHost(e.target.value)}
            placeholder="example.com"
          />
          <Select
            label="Window"
            value={String(windowMinutes)}
            onChange={(e) => setWindowMinutes(clampWindow(parseInt(e.target.value, 10)))}
          >
            <option value="5">5 min</option>
            <option value="15">15 min</option>
            <option value="60">60 min</option>
          </Select>
          <div className="flex items-end gap-2">
            <Button
              onClick={handleRefresh}
              disabled={
                (mode === 'host' && !selectedHost.trim()) ||
                (mode === 'host' && batchLoading) ||
                (mode === 'overview' && overviewLoading)
              }
            >
              <RefreshCw className={cn('h-4 w-4 mr-2', (mode === 'host' ? batchLoading : overviewLoading) && 'animate-spin')} />
              Refresh
            </Button>
            <Button
              variant={autoRefresh ? 'default' : 'outline'}
              onClick={() => setAutoRefresh(!autoRefresh)}
              title={autoRefresh ? 'Disable auto-refresh' : 'Enable auto-refresh (10s)'}
            >
              {autoRefresh ? 'Auto: On' : 'Auto: Off'}
            </Button>
          </div>
          <div className="flex items-end">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setAdvancedOpen(!advancedOpen)}
            >
              Advanced
              {advancedOpen ? <ChevronUp className="h-4 w-4 ml-1" /> : <ChevronDown className="h-4 w-4 ml-1" />}
            </Button>
          </div>
        </div>

        {advancedOpen && (
          <div className="mt-4 pt-4 border-t">
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4 items-end">
              <Input
                label="End Timestamp (epoch seconds)"
                value={endTs !== undefined ? String(endTs) : ''}
                onChange={(e) => handleEndTsChange(e.target.value)}
                placeholder="Leave empty for now"
                type="number"
              />
              {endTs !== undefined && (
                <div className="flex items-end">
                  <Button variant="ghost" size="sm" onClick={() => setEndTs(undefined)}>
                    <X className="h-4 w-4 mr-1" /> Clear
                  </Button>
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      {/* ── Overview mode ───────────────────────────────────────────────── */}
      {mode === 'overview' && (
        <>
          {/* Overview error */}
          {overviewError && (
            <div className="border border-red-500 rounded-lg p-4 bg-card mb-6">
              <p className="text-sm text-red-500 font-medium">{overviewError.message}</p>
              {overviewError.requestId && (
                <p className="text-xs text-muted-foreground mt-1">
                  Request ID: <code className="bg-muted px-1 py-0.5 rounded">{overviewError.requestId}</code>
                </p>
              )}
            </div>
          )}

          {/* Overview loading */}
          {overviewLoading && <Loading />}

          {/* Top Hosts table */}
          {!overviewLoading && !overviewError && overviewTopHosts && (
            <div className="mb-6">
              <h2 className="text-lg font-semibold mb-3">Top Hosts</h2>
              {overviewTopHosts.items.length === 0 ? (
                <div className="border rounded-lg p-6 bg-card text-center text-muted-foreground">
                  No data in this window.
                </div>
              ) : (
                <div className="border rounded-lg bg-card overflow-x-auto">
                  <table className="min-w-full text-sm">
                    <thead>
                      <tr className="border-b">
                        <th className="text-left px-4 py-3 font-medium text-muted-foreground">Host</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Total</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Unique IPs</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Avg RPS</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Peak RPS</th>
                      </tr>
                    </thead>
                    <tbody>
                      {overviewTopHosts.items.map((row) => (
                        <tr key={row.host} className="border-b last:border-b-0">
                          <td className="px-4 py-3 font-mono">{row.host}</td>
                          <td className="text-right px-4 py-3 font-semibold">{row.total.toLocaleString()}</td>
                          <td className="text-right px-4 py-3">{row.unique_ips.toLocaleString()}</td>
                          <td className="text-right px-4 py-3">{row.avg_rps.toFixed(2)}</td>
                          <td className="text-right px-4 py-3">{row.peak_rps.toFixed(2)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}

          {/* Suspicious Scanners table */}
          {!overviewLoading && !overviewError && overviewScanners && (
            <div className="mb-6">
              <h2 className="text-lg font-semibold mb-3">Suspicious Scanners</h2>
              {overviewScanners.items.length === 0 ? (
                <div className="border rounded-lg p-6 bg-card text-center text-muted-foreground">
                  No data in this window.
                </div>
              ) : (
                <div className="border rounded-lg bg-card overflow-x-auto">
                  <table className="min-w-full text-sm">
                    <thead>
                      <tr className="border-b">
                        <th className="text-left px-4 py-3 font-medium text-muted-foreground">Host</th>
                        <th className="text-left px-4 py-3 font-medium text-muted-foreground">IP</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Unique Paths</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Avg RPS</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Peak RPS</th>
                        <th className="text-left px-4 py-3 font-medium text-muted-foreground">Reasons</th>
                      </tr>
                    </thead>
                    <tbody>
                      {overviewScanners.items.map((row, idx) => (
                        <tr key={`${row.host}-${row.ip}-${idx}`} className="border-b last:border-b-0">
                          <td className="px-4 py-3 font-mono">{row.host}</td>
                          <td className="px-4 py-3 font-mono">{row.ip}</td>
                          <td className="text-right px-4 py-3">{row.unique_paths.toLocaleString()}</td>
                          <td className="text-right px-4 py-3">{row.avg_rps.toFixed(2)}</td>
                          <td className="text-right px-4 py-3">{row.peak_rps.toFixed(2)}</td>
                          <td className="px-4 py-3">
                            <div className="flex flex-wrap gap-1">
                              {row.reasons.map((r, i) => (
                                <Badge key={r + i} className="text-xs">{r}</Badge>
                              ))}
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}

          {/* Suspicious Sprayers table */}
          {!overviewLoading && !overviewError && overviewSprayers && (
            <div className="mb-6">
              <h2 className="text-lg font-semibold mb-3">Suspicious Sprayers</h2>
              {overviewSprayers.items.length === 0 ? (
                <div className="border rounded-lg p-6 bg-card text-center text-muted-foreground">
                  No data in this window.
                </div>
              ) : (
                <div className="border rounded-lg bg-card overflow-x-auto">
                  <table className="min-w-full text-sm">
                    <thead>
                      <tr className="border-b">
                        <th className="text-left px-4 py-3 font-medium text-muted-foreground">IP</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Unique Hosts</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Avg RPS</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Peak RPS</th>
                        <th className="text-left px-4 py-3 font-medium text-muted-foreground">Reasons</th>
                      </tr>
                    </thead>
                    <tbody>
                      {overviewSprayers.items.map((row, idx) => (
                        <tr key={`${row.ip}-${idx}`} className="border-b last:border-b-0">
                          <td className="px-4 py-3 font-mono">{row.ip}</td>
                          <td className="text-right px-4 py-3">{row.unique_hosts.toLocaleString()}</td>
                          <td className="text-right px-4 py-3">{row.avg_rps.toFixed(2)}</td>
                          <td className="text-right px-4 py-3">{row.peak_rps.toFixed(2)}</td>
                          <td className="px-4 py-3">
                            <div className="flex flex-wrap gap-1">
                              {row.reasons.map((r, i) => (
                                <Badge key={r + i} className="text-xs">{r}</Badge>
                              ))}
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* ── Host drilldown mode ────────────────────────────────────────── */}
      {mode === 'host' && (
        <>
          {/* ── No host message ───────────────────────────────────────── */}
          {!selectedHost.trim() && (
            <div className="border rounded-lg p-6 bg-card text-center text-muted-foreground">
              Enter a host to view telemetry data.
            </div>
          )}

          {/* ── Batch error ───────────────────────────────────────────── */}
          {batchError && (
            <div className="border border-red-500 rounded-lg p-4 bg-card mb-6">
              <p className="text-sm text-red-500 font-medium">{batchError.message}</p>
              {batchError.requestId && (
                <p className="text-xs text-muted-foreground mt-1">
                  Request ID: <code className="bg-muted px-1 py-0.5 rounded">{batchError.requestId}</code>
                </p>
              )}
            </div>
          )}

          {/* ── Batch loading ─────────────────────────────────────────── */}
          {batchLoading && selectedHost.trim() && <Loading />}

          {/* ── Summary panel ─────────────────────────────────────────── */}
          {!batchLoading && !batchError && summary && selectedHost.trim() && (
            <div className="mb-6">
              <h2 className="text-lg font-semibold mb-3">Summary</h2>
              {summary.total === 0 ? (
                <div className="border rounded-lg p-6 bg-card text-center text-muted-foreground">
                  No data in this window.
                </div>
              ) : (
                <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-7 gap-3">
                  <StatCard label="Total" value={summary.total} />
                  <StatCard label="Unique IPs" value={summary.unique_ips} />
                  {STATUS_COLS.map((col) => (
                    <StatCard
                      key={col.key}
                      label={col.label}
                      value={summary.status[col.key]}
                      muted={summary.status[col.key] === 0}
                    />
                  ))}
                </div>
              )}
            </div>
          )}

          {/* ── Top IPs table ─────────────────────────────────────────── */}
          {!batchLoading && !batchError && topIPs && selectedHost.trim() && sortedIPs.length > 0 && (
            <div className="mb-6">
              <h2 className="text-lg font-semibold mb-3">Top IPs</h2>
              <div className="border rounded-lg bg-card overflow-x-auto">
                <table className="min-w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left px-4 py-3 font-medium text-muted-foreground">IP</th>
                      <th className="text-right px-4 py-3 font-medium text-muted-foreground">Total</th>
                      {STATUS_COLS.map((col) => (
                        <th key={col.key} className="text-right px-4 py-3 font-medium text-muted-foreground">{col.label}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {sortedIPs.map((row) => (
                      <tr
                        key={row.ip}
                        onClick={() => handleIPClick(row.ip)}
                        className={cn(
                          'border-b last:border-b-0 cursor-pointer transition-colors hover:bg-accent',
                          selectedIP === row.ip && 'bg-accent'
                        )}
                      >
                        <td className="px-4 py-3 font-mono">{row.ip}</td>
                        <td className="text-right px-4 py-3 font-semibold">{row.total.toLocaleString()}</td>
                        {STATUS_COLS.map((col) => (
                          <td key={col.key} className={cn('text-right px-4 py-3', row.status[col.key] === 0 && 'text-muted-foreground')}>
                            {row.status[col.key].toLocaleString()}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* ── Top Paths table (IP drilldown) ────────────────────────── */}
          {selectedIP && selectedHost.trim() && (
            <div ref={pathsSectionRef}>
              <div className="flex items-center gap-3 mb-3">
                <h2 className="text-lg font-semibold">
                  Top Paths for <Badge className="font-mono ml-1">{selectedIP}</Badge>
                </h2>
                <Button variant="ghost" size="sm" onClick={handleClearIP}>
                  <X className="h-4 w-4 mr-1" /> Clear
                </Button>
              </div>

              {pathsError && (
                <div className="border border-red-500 rounded-lg p-4 bg-card mb-4">
                  <p className="text-sm text-red-500 font-medium">{pathsError.message}</p>
                  {pathsError.requestId && (
                    <p className="text-xs text-muted-foreground mt-1">
                      Request ID: <code className="bg-muted px-1 py-0.5 rounded">{pathsError.requestId}</code>
                    </p>
                  )}
                </div>
              )}

              {pathsLoading && <Loading />}

              {!pathsLoading && !pathsError && topPaths && sortedPaths.length === 0 && (
                <div className="border rounded-lg p-6 bg-card text-center text-muted-foreground">
                  No data in this window.
                </div>
              )}

              {!pathsLoading && !pathsError && sortedPaths.length > 0 && (
                <div className="border rounded-lg bg-card overflow-x-auto">
                  <table className="min-w-full text-sm">
                    <thead>
                      <tr className="border-b">
                        <th className="text-left px-4 py-3 font-medium text-muted-foreground">Path</th>
                        <th className="text-right px-4 py-3 font-medium text-muted-foreground">Total</th>
                        {STATUS_COLS.map((col) => (
                          <th key={col.key} className="text-right px-4 py-3 font-medium text-muted-foreground">{col.label}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {sortedPaths.map((row, idx) => (
                        <tr key={`${row.path}-${idx}`} className="border-b last:border-b-0">
                          <td className="px-4 py-3 font-mono break-all">{row.path}</td>
                          <td className="text-right px-4 py-3 font-semibold">{row.total.toLocaleString()}</td>
                          {STATUS_COLS.map((col) => (
                            <td key={col.key} className={cn('text-right px-4 py-3', row.status[col.key] === 0 && 'text-muted-foreground')}>
                              {row.status[col.key].toLocaleString()}
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}

// ── StatCard sub-component ───────────────────────────────────────────────────

function StatCard({ label, value, muted }: { label: string; value: number; muted?: boolean }) {
  return (
    <div className="border rounded-lg p-4 bg-card">
      <p className="text-xs font-medium text-muted-foreground mb-1">{label}</p>
      <p className={cn('text-xl font-bold', muted && 'text-muted-foreground')}>
        {value.toLocaleString()}
      </p>
    </div>
  )
}
