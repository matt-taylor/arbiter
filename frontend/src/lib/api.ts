import axios from 'axios'
import type { HostPolicy, EffectivePolicy, CreatePolicyRequest, TestCheckResponse } from './types'

// Identity header extraction and notification system
export interface IdentityData {
  userId?: string
  email?: string
  username?: string
  scopes: string[]
  groups: string[]
}

type IdentityUpdateCallback = (identity: IdentityData | null) => void

const identityCallbacks = new Set<IdentityUpdateCallback>()

export function subscribeToIdentityUpdates(callback: IdentityUpdateCallback): () => void {
  identityCallbacks.add(callback)
  return () => {
    identityCallbacks.delete(callback)
  }
}

function extractIdentityFromHeaders(headers: Record<string, string>): IdentityData | null {
  const email = headers['x-identity-email']
  const username = headers['x-identity-username']
  const userId = headers['x-identity-user-id']
  const scopesHeader = headers['x-identity-scopes']
  const groupsHeader = headers['x-identity-groups']

  // If we have at least email or username, we have identity data
  if (!email && !username) {
    return null
  }

  const scopes = scopesHeader ? scopesHeader.split(',').map(s => s.trim()).filter(Boolean) : []
  const groups = groupsHeader ? groupsHeader.split(',').map(g => g.trim()).filter(Boolean) : []

  return {
    userId,
    email: email || undefined,
    username: username || undefined,
    scopes,
    groups,
  }
}

function notifyIdentityCallbacks(identity: IdentityData | null) {
  identityCallbacks.forEach(callback => {
    try {
      callback(identity)
    } catch (error) {
      console.error('Error in identity update callback:', error)
    }
  })
}

// Get CSRF token from cookie
function getCsrfToken(): string | null {
  const cookies = document.cookie.split(';')
  for (const cookie of cookies) {
    const [name, value] = cookie.trim().split('=')
    if (name === 'gk_csrf') {
      return value
    }
  }
  return null
}

// Build headers for Gatekeeper API requests
function buildGatekeeperHeaders(): Record<string, string> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  // Add CSRF token if available (required by Gatekeeper endpoints)
  const csrfToken = getCsrfToken()
  if (csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
  }
  return headers
}

// Extract base domain helper function
// For .home.arpa domains, use last 3 parts (e.g., arbiter.lan.home.arpa -> lan.home.arpa)
// For .domostack.me domains, use last 2 parts (e.g., arbiter.domostack.me -> domostack.me)
function getBaseDomain(): string {
  const currentHostname = window.location.hostname
  const parts = currentHostname.split('.')

  // Handle IP addresses or localhost - use as-is
  if (parts[parts.length - 1].match(/^\d+$/) || currentHostname === 'localhost') {
    return currentHostname
  }

  // For .home.arpa domains, take last 3 parts (e.g., lan.home.arpa)
  if (currentHostname.endsWith('.home.arpa') || currentHostname === 'home.arpa') {
    if (parts.length >= 3) {
      return parts.slice(-3).join('.')
    }
    // Fallback if not enough parts
    return parts.slice(-2).join('.')
  }

  // For .domostack.me domains, take last 2 parts (e.g., domostack.me)
  if (currentHostname.endsWith('.domostack.me') || currentHostname === 'domostack.me') {
    return parts.slice(-2).join('.')
  }

  // Default: take last 2 parts for standard domains
  if (parts.length >= 2) {
    return parts.slice(-2).join('.')
  }

  // Fallback: use as-is
  return currentHostname
}

// Generate Gatekeeper whoami URL using subdomain pattern
export function getGatekeeperWhoamiUrl(): string {
  const protocol = window.location.protocol
  const baseDomain = getBaseDomain()
  return `${protocol}//gatekeeper.${baseDomain}/api/v1/whoami`
}

// Helper function to check if error is Axios error
export function isAxiosError(error: unknown): error is import('axios').AxiosError {
  return axios.isAxiosError(error)
}

// Helper function to get error message
export function getErrorMessage(error: unknown, defaultMessage: string = 'An error occurred'): string {
  if (isAxiosError(error)) {
    if (error.response?.data) {
      if (typeof error.response.data === 'string') {
        return error.response.data
      }
      if (typeof error.response.data === 'object' && error.response.data !== null) {
        const data = error.response.data as { error?: string; message?: string }
        return data.error || data.message || defaultMessage
      }
    }
    return error.message || defaultMessage
  }
  if (error instanceof Error) {
    return error.message
  }
  return defaultMessage
}

// Arbiter API instance (uses configurable base URL)
const arbiterBaseURL = import.meta.env.VITE_ARBITER_API_BASE_URL || '/api/v1'
export const arbiterApi = axios.create({
  baseURL: arbiterBaseURL,
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true, // Important for cookies (gk_sid, gk_csrf)
})

// Extract identity headers from Arbiter API responses
arbiterApi.interceptors.response.use(
  (response) => {
    // Extract identity headers from response
    const headers = response.headers
    const identity = extractIdentityFromHeaders(headers as Record<string, string>)

    if (identity) {
      notifyIdentityCallbacks(identity)
    }

    return response
  },
  (error) => {
    // On error, check if we can extract identity from error response headers
    if (isAxiosError(error) && error.response?.headers) {
      const headers = error.response.headers as Record<string, string>
      const identity = extractIdentityFromHeaders(headers)

      if (identity) {
        notifyIdentityCallbacks(identity)
      }
    }

    // Handle errors globally
    // If the error response is plain text, convert it to a structured error
    if (error.response) {
      if (typeof error.response.data === 'string') {
        error.response.data = {
          error: error.response.data,
          message: error.response.data,
        }
      }
    }
    return Promise.reject(error)
  }
)

// Gatekeeper API instance (uses configurable base URL)
const gatekeeperBaseURL = import.meta.env.VITE_GATEKEEPER_API_BASE_URL || '/api/v1'
export const gatekeeperApi = axios.create({
  baseURL: gatekeeperBaseURL,
  withCredentials: true, // Important for cookies
  headers: {
    'Content-Type': 'application/json',
  },
})

// Add CSRF token to Gatekeeper requests (except login)
gatekeeperApi.interceptors.request.use((config) => {
  // Skip CSRF token for login endpoint - user doesn't have a token yet
  if (config.url === '/session' && config.method?.toLowerCase() === 'post') {
    return config
  }

  // Add CSRF token to all requests (GET, POST, PUT, PATCH, DELETE) that require auth
  const headers = buildGatekeeperHeaders()
  Object.assign(config.headers, headers)
  return config
})

// Handle errors for Gatekeeper API (non-fatal)
gatekeeperApi.interceptors.response.use(
  (response) => response,
  (error) => {
    // Don't throw for network errors or 404/401 - these are expected when Gatekeeper is unavailable
    // Only throw for unexpected errors (500, etc.)
    if (isAxiosError(error)) {
      const status = error.response?.status
      if (status === 404 || status === 401) {
        // Gatekeeper not available or not authenticated - this is fine
        return Promise.reject(error)
      }
      // Network errors (ECONNREFUSED, timeout) are also fine
      if (!error.response && error.request) {
        // Network error - Gatekeeper unavailable
        return Promise.reject(error)
      }
    }
    return Promise.reject(error)
  }
)

export const policiesApi = {
  list: async (): Promise<HostPolicy[]> => {
    const response = await arbiterApi.get<HostPolicy[]>('/policies')
    return response.data
  },

  get: async (id: number): Promise<HostPolicy> => {
    const response = await arbiterApi.get<HostPolicy>(`/policies/${id}`)
    return response.data
  },

  create: async (policy: CreatePolicyRequest): Promise<HostPolicy> => {
    const response = await arbiterApi.post<HostPolicy>('/policies', policy)
    return response.data
  },

  update: async (id: number, policy: Partial<CreatePolicyRequest>): Promise<HostPolicy> => {
    const response = await arbiterApi.patch<HostPolicy>(`/policies/${id}`, policy)
    return response.data
  },

  delete: async (id: number): Promise<void> => {
    await arbiterApi.delete(`/policies/${id}`)
  },

  effective: async (host: string): Promise<EffectivePolicy> => {
    const response = await arbiterApi.get<EffectivePolicy>('/effective', {
      params: { host },
    })
    return response.data
  },

  testCheck: async (host: string, method: string, uri: string): Promise<TestCheckResponse> => {
    const response = await arbiterApi.post<TestCheckResponse>('/test/check', { host, method, uri })
    return response.data
  },
}

// Gatekeeper API functions
export const gatekeeperApiFunctions = {
  whoami: <T = unknown>() => {
    // Use dynamic gatekeeper subdomain URL instead of relative path
    // Use same headers as other Gatekeeper API calls (includes CSRF token)
    const url = getGatekeeperWhoamiUrl()
    const headers = buildGatekeeperHeaders()
    return axios.get<T>(url, {
      withCredentials: true,
      headers,
    })
  },
  logout: () => gatekeeperApi.delete('/session'),
}

// Default export for backward compatibility (arbiter API)
export default arbiterApi
