import axios from 'axios'
import { arbiterApi } from './api'

// ── Types ────────────────────────────────────────────────────────────────────

export interface StatusBreakdown {
  c_401: number
  c_403: number
  c_404: number
  c_429: number
  c_5xx: number
}

export interface SummaryResponse {
  host: string
  window_minutes: number
  start_ts: number
  end_ts: number
  total: number
  unique_ips: number
  status: StatusBreakdown
}

export interface TopIPsResponse {
  host: string
  window_minutes: number
  start_ts: number
  end_ts: number
  items: Array<{ ip: string; total: number; status: StatusBreakdown }>
}

export interface TopPathsResponse {
  host: string
  ip: string
  window_minutes: number
  start_ts: number
  end_ts: number
  items: Array<{ path: string; total: number; status: StatusBreakdown }>
}

// ── Overview response types ─────────────────────────────────────────────────

export interface TopHostsOverviewResponse {
  window_minutes: number
  start_ts: number
  end_ts: number
  items: Array<{
    host: string
    total: number
    unique_ips: number
    avg_rps: number
    peak_rps: number
  }>
}

export interface SuspiciousScannersOverviewResponse {
  window_minutes: number
  start_ts: number
  end_ts: number
  items: Array<{
    host: string
    ip: string
    unique_paths: number
    total: number
    avg_rps: number
    peak_rps: number
    burstiness: number
    reasons: string[]
  }>
}

export interface SuspiciousSprayersOverviewResponse {
  window_minutes: number
  start_ts: number
  end_ts: number
  items: Array<{
    ip: string
    unique_hosts: number
    total: number
    avg_rps: number
    peak_rps: number
    burstiness: number
    reasons: string[]
  }>
}

export interface SuspiciousFloodersOverviewResponse {
  window_minutes: number
  start_ts: number
  end_ts: number
  items: Array<{
    host: string
    ip: string
    path: string
    unique_paths: number
    total: number
    avg_rps: number
    peak_rps: number
    burstiness: number
    reasons: string[]
  }>
}

// ── Overview config types ────────────────────────────────────────────────────

export interface ReasonFlagInfo {
  flag: string
  description: string
}

export interface DetectionConfig {
  description: string
  thresholds: Record<string, number>
  reason_flags: ReasonFlagInfo[]
}

export interface OverviewConfigResponse {
  scanners: DetectionConfig
  sprayers: DetectionConfig
  flooders: DetectionConfig
}

// ── Error helper ─────────────────────────────────────────────────────────────

function handleError(err: unknown): never {
  if (axios.isCancel(err)) {
    throw err // rethrow cancellation so caller can ignore
  }
  if (axios.isAxiosError(err) && err.response?.data) {
    const data = err.response.data as { error?: string; request_id?: string }
    if (data.error) {
      const wrapped = new Error(data.error)
      ;(wrapped as any).request_id = data.request_id || undefined
      throw wrapped
    }
  }
  throw err
}

// ── Query param builder ──────────────────────────────────────────────────────

function buildParams(
  windowMinutes: number,
  limit?: number,
  endTs?: number,
): Record<string, string> {
  const params: Record<string, string> = {
    window_minutes: String(windowMinutes),
  }
  if (limit !== undefined) {
    params.limit = String(limit)
  }
  if (endTs !== undefined) {
    params.end_ts = String(endTs)
  }
  return params
}

// ── API functions ────────────────────────────────────────────────────────────

export async function getSummary(
  host: string,
  windowMinutes: number,
  endTs?: number,
  signal?: AbortSignal,
): Promise<SummaryResponse> {
  try {
    const response = await arbiterApi.get<SummaryResponse>(
      `/telemetry/hosts/${encodeURIComponent(host)}/summary`,
      { params: buildParams(windowMinutes, undefined, endTs), signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}

export async function getTopIPs(
  host: string,
  windowMinutes: number,
  limit: number,
  endTs?: number,
  signal?: AbortSignal,
): Promise<TopIPsResponse> {
  try {
    const response = await arbiterApi.get<TopIPsResponse>(
      `/telemetry/hosts/${encodeURIComponent(host)}/top-ips`,
      { params: buildParams(windowMinutes, limit, endTs), signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}

export async function getTopPaths(
  host: string,
  ip: string,
  windowMinutes: number,
  limit: number,
  endTs?: number,
  signal?: AbortSignal,
): Promise<TopPathsResponse> {
  try {
    const response = await arbiterApi.get<TopPathsResponse>(
      `/telemetry/hosts/${encodeURIComponent(host)}/ips/${encodeURIComponent(ip)}/top-paths`,
      { params: buildParams(windowMinutes, limit, endTs), signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}

// ── Overview API functions ────────────────────────────────────────────────

export async function getOverviewTopHosts(
  windowMinutes: number,
  limit: number,
  endTs?: number,
  signal?: AbortSignal,
): Promise<TopHostsOverviewResponse> {
  try {
    const response = await arbiterApi.get<TopHostsOverviewResponse>(
      `/telemetry/overview/top-hosts`,
      { params: buildParams(windowMinutes, limit, endTs), signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}

export async function getOverviewSuspiciousScanners(
  windowMinutes: number,
  limit: number,
  endTs?: number,
  signal?: AbortSignal,
): Promise<SuspiciousScannersOverviewResponse> {
  try {
    const response = await arbiterApi.get<SuspiciousScannersOverviewResponse>(
      `/telemetry/overview/suspicious-scanners`,
      { params: buildParams(windowMinutes, limit, endTs), signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}

export async function getOverviewSuspiciousSprayers(
  windowMinutes: number,
  limit: number,
  endTs?: number,
  signal?: AbortSignal,
): Promise<SuspiciousSprayersOverviewResponse> {
  try {
    const response = await arbiterApi.get<SuspiciousSprayersOverviewResponse>(
      `/telemetry/overview/suspicious-sprayers`,
      { params: buildParams(windowMinutes, limit, endTs), signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}

export async function getOverviewSuspiciousFlooders(
  windowMinutes: number,
  limit: number,
  endTs?: number,
  signal?: AbortSignal,
): Promise<SuspiciousFloodersOverviewResponse> {
  try {
    const response = await arbiterApi.get<SuspiciousFloodersOverviewResponse>(
      `/telemetry/overview/suspicious-flooders`,
      { params: buildParams(windowMinutes, limit, endTs), signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}

export async function getOverviewConfig(
  signal?: AbortSignal,
): Promise<OverviewConfigResponse> {
  try {
    const response = await arbiterApi.get<OverviewConfigResponse>(
      `/telemetry/overview/config`,
      { signal },
    )
    return response.data
  } catch (err) {
    handleError(err)
  }
}
