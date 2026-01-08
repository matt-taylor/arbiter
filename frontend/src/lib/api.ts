import axios from 'axios'
import type { HostPolicy, EffectivePolicy, CreatePolicyRequest, TestCheckResponse } from './types'

// Arbiter API instance (uses configurable base URL)
const arbiterBaseURL = import.meta.env.VITE_ARBITER_API_BASE_URL || '/api/v1'
const api = axios.create({
  baseURL: arbiterBaseURL,
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true, // Important for cookies (gk_sid, gk_csrf)
})

// Add response interceptor for error handling
api.interceptors.response.use(
  (response) => response,
  (error) => {
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

export const policiesApi = {
  list: async (): Promise<HostPolicy[]> => {
    const response = await api.get<HostPolicy[]>('/policies')
    return response.data
  },

  get: async (id: number): Promise<HostPolicy> => {
    const response = await api.get<HostPolicy>(`/policies/${id}`)
    return response.data
  },

  create: async (policy: CreatePolicyRequest): Promise<HostPolicy> => {
    const response = await api.post<HostPolicy>('/policies', policy)
    return response.data
  },

  update: async (id: number, policy: Partial<CreatePolicyRequest>): Promise<HostPolicy> => {
    const response = await api.patch<HostPolicy>(`/policies/${id}`, policy)
    return response.data
  },

  delete: async (id: number): Promise<void> => {
    await api.delete(`/policies/${id}`)
  },

  effective: async (host: string): Promise<EffectivePolicy> => {
    const response = await api.get<EffectivePolicy>('/effective', {
      params: { host },
    })
    return response.data
  },

  testCheck: async (host: string, method: string, uri: string): Promise<TestCheckResponse> => {
    const response = await api.post<TestCheckResponse>('/test/check', { host, method, uri })
    return response.data
  },
}

export default api
