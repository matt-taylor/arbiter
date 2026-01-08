import { useToastContext } from '@/contexts/ToastContext'

export function useToast() {
  const { success, error, warning, info } = useToastContext()
  return { success, error, warning, info }
}
