import { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface TableCardProps {
  children: ReactNode
  className?: string
}

export function TableCard({ children, className }: TableCardProps) {
  return (
    <div className={cn('border rounded-lg bg-card p-4 space-y-3', className)}>
      {children}
    </div>
  )
}

interface TableCardRowProps {
  label: string
  value: ReactNode
  className?: string
}

export function TableCardRow({ label, value, className }: TableCardRowProps) {
  return (
    <div className={cn('flex flex-col gap-1', className)}>
      <dt className="text-sm font-medium text-muted-foreground">{label}</dt>
      <dd className="text-sm">{value}</dd>
    </div>
  )
}

interface TableCardActionsProps {
  children: ReactNode
  className?: string
}

export function TableCardActions({ children, className }: TableCardActionsProps) {
  return (
    <div className={cn('flex items-center gap-2 pt-2 border-t', className)}>
      {children}
    </div>
  )
}
