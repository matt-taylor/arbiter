export interface HostPolicy {
  id: number
  host: string
  killswitch_required: boolean
  gatekeeper_required: boolean
  notes?: string | null
  created_at: string
  updated_at: string
}

export interface EffectivePolicy {
  host: string
  killswitch_required: boolean
  gatekeeper_required: boolean
  source: string
  forced_killswitch: boolean
  forced_gatekeeper: boolean
  killswitch_public_host?: string
  gatekeeper_public_host?: string
}

export interface CreatePolicyRequest {
  host: string
  killswitch_required: boolean
  gatekeeper_required: boolean
  notes?: string | null
}

export interface TestCheckResponse {
  decision: 'allow' | 'unauth' | 'forbid' | 'killswitch' | 'error'
  status: number
  reason: string
  source: string
  policy: string
  trace_id: string
  normalized: {
    host: string
    method: string
    uri: string
  }
  latency_ms: number // Deprecated: use total_latency_ms
  total_latency_ms: number
  killswitch_latency_ms: number
  gatekeeper_latency_ms: number
  nginx_headers?: Record<string, string>
  identity_headers?: Record<string, string>
  killswitch_headers?: Record<string, string>
}
