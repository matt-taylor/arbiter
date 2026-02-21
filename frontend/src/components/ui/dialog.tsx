import { ReactNode } from 'react'
import { X } from 'lucide-react'

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
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 md:p-6">
      <div className="fixed inset-0 bg-black/50" onClick={onClose} />
      <div className="relative z-50 w-full max-w-lg max-h-[calc(100vh-2rem)] rounded-lg bg-card shadow-lg flex flex-col">
        {/* Header - fixed */}
        <div className="flex items-center justify-between p-4 md:p-6 border-b flex-shrink-0">
          <h2 className="text-lg font-semibold text-card-foreground">{title}</h2>
          <button
            onClick={onClose}
            className="rounded-md p-1 hover:bg-accent transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        {/* Content - scrollable */}
        <div className="flex-1 overflow-y-auto p-4 md:p-6 text-card-foreground">
          {children}
        </div>
        {/* Footer - fixed */}
        {footer && (
          <div className="p-4 md:p-6 border-t flex justify-end gap-2 flex-shrink-0 bg-card">
            {footer}
          </div>
        )}
      </div>
    </div>
  )
}
