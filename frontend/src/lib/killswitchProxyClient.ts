import axios from 'axios'
import { arbiterApi } from './api'

// ── Types ────────────────────────────────────────────────────────────────────

export interface BlockIPRequest {
  ip: string
  method?: string   // default: "*"
  domain?: string   // default: "*"
  path?: string     // default: "*"
  expires_at: string // ISO 8601
  reason: string     // max 20 chars
}

export interface BlockIPResponse {
  success: boolean
  ip_address?: string
  method?: string
  domain?: string
  path?: string
  message?: string
  error?: string
}

// ── API function ─────────────────────────────────────────────────────────────

export async function blockIP(req: BlockIPRequest): Promise<BlockIPResponse> {
  try {
    const response = await arbiterApi.post<BlockIPResponse>(
      '/killswitch/block-ip',
      req,
    )
    return response.data
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.data) {
      const data = err.response.data as { error?: string }
      if (data.error) {
        throw new Error(data.error)
      }
    }
    throw err
  }
}
