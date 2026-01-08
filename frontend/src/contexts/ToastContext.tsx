import { createContext, useContext, useState, useCallback, ReactNode } from 'react'
import { Toast, ToastVariant } from '@/components/ui/toast'
import ToastComponent from '@/components/ui/toast'

interface ToastContextType {
  toasts: Toast[]
  addToast: (message: string, variant?: ToastVariant, duration?: number) => void
  removeToast: (id: string) => void
  success: (message: string, duration?: number) => void
  error: (message: string, duration?: number) => void
  warning: (message: string, duration?: number) => void
  info: (message: string, duration?: number) => void
}

const ToastContext = createContext<ToastContextType | undefined>(undefined)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const addToast = useCallback((message: string, variant: ToastVariant = 'info', duration?: number) => {
    const id = Math.random().toString(36).substring(2, 9)
    const newToast: Toast = { id, message, variant, duration }
    setToasts((prev) => {
      const updated = [...prev, newToast]
      // Limit to 5 toasts maximum
      return updated.slice(-5)
    })

    // Auto-remove after duration (default 5 seconds)
    if (duration !== 0) {
      const timeout = duration || 5000
      setTimeout(() => {
        setToasts((prev) => prev.filter((toast) => toast.id !== id))
      }, timeout)
    }
  }, [])

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id))
  }, [])

  const success = useCallback((message: string, duration?: number) => {
    addToast(message, 'success', duration)
  }, [addToast])

  const error = useCallback((message: string, duration?: number) => {
    addToast(message, 'error', duration)
  }, [addToast])

  const warning = useCallback((message: string, duration?: number) => {
    addToast(message, 'warning', duration)
  }, [addToast])

  const info = useCallback((message: string, duration?: number) => {
    addToast(message, 'info', duration)
  }, [addToast])

  return (
    <ToastContext.Provider value={{ toasts, addToast, removeToast, success, error, warning, info }}>
      {children}
    </ToastContext.Provider>
  )
}

export function useToastContext() {
  const context = useContext(ToastContext)
  if (context === undefined) {
    throw new Error('useToastContext must be used within a ToastProvider')
  }
  return context
}

export function ToastContainer() {
  const { toasts, removeToast } = useToastContext()

  return (
    <div className="fixed top-4 left-1/2 -translate-x-1/2 right-4 z-50 flex flex-col gap-2 md:left-auto md:translate-x-0 md:right-4">
      {toasts.map((toast) => (
        <ToastComponent key={toast.id} toast={toast} onClose={() => removeToast(toast.id)} />
      ))}
    </div>
  )
}
