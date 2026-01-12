import { useState, useEffect, useRef } from 'react'
import { gatekeeperApiFunctions, isAxiosError, getErrorMessage, subscribeToIdentityUpdates, type IdentityData } from '@/lib/api'

interface User {
  id: number
  email: string
  username: string
  display_name?: string | null
  groups: Array<{ id: number; name: string }>
  scopes: Array<{ id: number; name: string }>
}

interface WhoamiResponse {
  user: User
  scopes: string[]
  groups: string[]
}

export function useAuth() {
  const [user, setUser] = useState<User | null>(null)
  const [scopes, setScopes] = useState<string[]>([])
  const [groups, setGroups] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const identityReceivedRef = useRef(false)
  const fallbackAttemptedRef = useRef(false)

  // Convert IdentityData to User format
  const updateFromIdentity = (identity: IdentityData | null) => {
    if (!identity) {
      // If identity is null, clear user data (user logged out or not authenticated)
      if (identityReceivedRef.current) {
        setUser(null)
        setScopes([])
        setGroups([])
      }
      return
    }

    identityReceivedRef.current = true
    setLoading(false)
    setError(null)

    // Convert identity data to User format
    const userData: User = {
      id: identity.userId ? parseInt(identity.userId, 10) : 0,
      email: identity.email || '',
      username: identity.username || '',
      display_name: null,
      groups: identity.groups.map((name, index) => ({ id: index + 1, name })),
      scopes: identity.scopes.map((name, index) => ({ id: index + 1, name })),
    }

    setUser(userData)
    setScopes(identity.scopes)
    setGroups(identity.groups)
  }

  // Subscribe to identity updates from API responses
  useEffect(() => {
    const unsubscribe = subscribeToIdentityUpdates(updateFromIdentity)
    return unsubscribe
  }, [])

  // Fallback: If no identity headers received after a short delay, try whoami endpoint
  useEffect(() => {
    const timeoutId = setTimeout(() => {
      if (!identityReceivedRef.current && !fallbackAttemptedRef.current) {
        fallbackAttemptedRef.current = true
        fetchUserFromWhoami()
      }
    }, 1000) // Wait 1 second for identity headers from first API call

    return () => clearTimeout(timeoutId)
  }, [])

  const fetchUserFromWhoami = async () => {
    try {
      setLoading(true)
      const response = await gatekeeperApiFunctions.whoami<WhoamiResponse>()
      setUser(response.data.user)
      setScopes(response.data.scopes)
      setGroups(response.data.groups)
      setError(null)
      identityReceivedRef.current = true
    } catch (err: unknown) {
      // Graceful degradation: Network errors or 404/401 are expected when Gatekeeper is unavailable
      if (isAxiosError(err)) {
        const status = err.response?.status
        // 404 or 401 means Gatekeeper not available or not authenticated - this is fine
        if (status === 404 || status === 401) {
          setUser(null)
          setScopes([])
          setGroups([])
          setError(null) // Don't set error for expected cases
        } else if (!err.response && err.request) {
          // Network error (ECONNREFUSED, timeout) - Gatekeeper unavailable
          setUser(null)
          setScopes([])
          setGroups([])
          setError(null) // Don't set error for network failures
        } else {
          // Unexpected error (500, etc.)
          setError(getErrorMessage(err, 'Failed to fetch user'))
          setUser(null)
          setScopes([])
          setGroups([])
        }
      } else {
        // Non-Axios error
        setError(getErrorMessage(err, 'Failed to fetch user'))
        setUser(null)
        setScopes([])
        setGroups([])
      }
    } finally {
      setLoading(false)
    }
  }

  const logout = async () => {
    try {
      await gatekeeperApiFunctions.logout()
      setUser(null)
      setScopes([])
      setGroups([])
      identityReceivedRef.current = false
      // Redirect to login page if logout succeeds
      window.location.href = '/login'
    } catch (err) {
      // Log error but don't crash - Gatekeeper might be unavailable
      console.error('Logout failed:', err)
      // Still clear local state
      setUser(null)
      setScopes([])
      setGroups([])
      identityReceivedRef.current = false
    }
  }

  const hasScope = (scopeName: string) => {
    return scopes.includes(scopeName)
  }

  const hasAnyScope = (scopeNames: string[]) => {
    return scopeNames.some((scope) => scopes.includes(scope))
  }

  const hasAllScopes = (scopeNames: string[]) => {
    return scopeNames.every((scope) => scopes.includes(scope))
  }

  return {
    user,
    scopes,
    groups,
    loading,
    error,
    logout,
    hasScope,
    hasAnyScope,
    hasAllScopes,
    refetch: fetchUserFromWhoami,
  }
}
