import { ReactNode } from 'react'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import Button from './button'

interface DialogProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  footer?: ReactNode
}

export default function Dialog({ open, onClose, title, children, footer }: DialogProps) {
  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="fixed inset-0 bg-black/50" onClick={onClose} />
      <div className="relative z-50 w-full max-w-lg rounded-lg bg-card p-6 shadow-lg">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-card-foreground">{title}</h2>
          <button
            onClick={onClose}
            className="rounded-md p-1 hover:bg-accent transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="text-card-foreground">{children}</div>
        {footer && <div className="mt-4 flex justify-end gap-2">{footer}</div>}
      </div>
    </div>
  )
}
