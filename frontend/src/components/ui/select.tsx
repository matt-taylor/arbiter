import { SelectHTMLAttributes, forwardRef, ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
  error?: string
  children: ReactNode
}

const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ className, label, error, children, ...props }, ref) => {
    return (
      <div className="w-full">
        {label && (
          <label className="block text-sm font-medium mb-1 text-foreground">
            {label}
            {props.required && <span className="text-red-500 ml-1">*</span>}
          </label>
        )}
        <select
          className={cn(
            'flex h-12 md:h-10 w-full rounded-md border border-input bg-background px-4 md:px-3 py-3 md:py-2 text-base md:text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
            error && 'border-red-500 focus-visible:ring-red-500',
            className
          )}
          ref={ref}
          {...props}
        >
          {children}
        </select>
        {error && <p className="mt-1 text-sm text-red-500">{error}</p>}
      </div>
    )
  }
)

Select.displayName = 'Select'

export default Select
