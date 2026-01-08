import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

export type ToastVariant = 'success' | 'error' | 'warning' | 'info'

export interface Toast {
  id: string
  message: string
  variant: ToastVariant
  duration?: number
}

interface ToastProps {
  toast: Toast
  onClose: () => void
}

const variantStyles: Record<ToastVariant, string> = {
  success: 'bg-green-600 text-white',
  error: 'bg-destructive text-destructive-foreground',
  warning: 'bg-yellow-600 text-white',
  info: 'bg-primary text-primary-foreground',
}

export default function ToastComponent({ toast, onClose }: ToastProps) {
  return (
    <div
      className={cn(
        'flex items-center gap-3 rounded-md px-4 py-3 shadow-lg min-w-[300px] max-w-md',
        variantStyles[toast.variant]
      )}
    >
      <p className="flex-1 text-sm font-medium">{toast.message}</p>
      <button
        onClick={onClose}
        className="rounded-md p-1 hover:bg-black/20 transition-colors"
        aria-label="Close"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}
