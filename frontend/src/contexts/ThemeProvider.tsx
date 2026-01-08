import { ReactNode } from 'react'
import { useTheme } from '@/hooks/useTheme'

interface ThemeProviderProps {
  children: ReactNode
}

export function ThemeProvider({ children }: ThemeProviderProps) {
  // Initialize theme hook to apply theme on mount
  useTheme()
  return <>{children}</>
}
