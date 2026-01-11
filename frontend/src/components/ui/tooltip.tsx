import { ReactNode, useState, useRef, useEffect } from 'react'
import { cn } from '@/lib/utils'

interface TooltipProps {
  children: ReactNode
  content: string
  className?: string
}

export default function Tooltip({ children, content, className }: TooltipProps) {
  const [isVisible, setIsVisible] = useState(false)
  const [position, setPosition] = useState({ top: 0, left: 0 })
  const [isPositioned, setIsPositioned] = useState(false)
  const tooltipRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (isVisible && triggerRef.current && tooltipRef.current) {
      // Reset positioned state when visibility changes
      setIsPositioned(false)
      // Use double RAF to ensure tooltip is rendered and measured
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          if (triggerRef.current && tooltipRef.current) {
            const triggerRect = triggerRef.current.getBoundingClientRect()
            const tooltipRect = tooltipRef.current.getBoundingClientRect()

            let top = triggerRect.bottom + 8
            let left = triggerRect.left + (triggerRect.width / 2) - (tooltipRect.width / 2)

            // Adjust if tooltip goes off screen
            if (left < 8) left = 8
            if (left + tooltipRect.width > window.innerWidth - 8) {
              left = window.innerWidth - tooltipRect.width - 8
            }
            if (top + tooltipRect.height > window.innerHeight - 8) {
              top = triggerRect.top - tooltipRect.height - 8
            }

            setPosition({ top, left })
            setIsPositioned(true)
          }
        })
      })
    } else if (!isVisible) {
      // Reset positioned state when tooltip is hidden
      setIsPositioned(false)
    }
  }, [isVisible])

  return (
    <div
      ref={triggerRef}
      className="inline-block"
      onMouseEnter={() => setIsVisible(true)}
      onMouseLeave={() => setIsVisible(false)}
    >
      {children}
      {isVisible && (
        <div
          ref={tooltipRef}
          className={cn(
            'fixed z-50 px-2 py-1 text-xs text-white bg-gray-900 dark:bg-gray-700 rounded shadow-lg pointer-events-none whitespace-nowrap',
            className
          )}
          style={{
            top: `${position.top}px`,
            left: `${position.left}px`,
            opacity: isPositioned ? 1 : 0,
            transition: isPositioned ? 'opacity 0.1s ease-in' : 'none',
          }}
        >
          {content}
        </div>
      )}
    </div>
  )
}
