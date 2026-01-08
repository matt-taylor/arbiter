import { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

interface BadgeProps extends HTMLAttributes<HTMLDivElement> {
  variant?: 'default' | 'success' | 'warning' | 'danger'
}

export default function Badge({ className, variant = 'default', ...props }: BadgeProps) {
  const variants = {
    default: 'bg-secondary text-secondary-foreground',
    success: 'bg-green-600 text-white',
    warning: 'bg-yellow-600 text-white',
    danger: 'bg-destructive text-destructive-foreground',
  }

  return (
    <div
      className={cn(
        'inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold transition-colors',
        variants[variant],
        className
      )}
      {...props}
    />
  )
}
