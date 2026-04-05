import { useEffect, useState } from 'react'
import { Wifi, WifiOff } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'

export function useNetworkStatus() {
  const [isOnline, setIsOnline] = useState(navigator.onLine)
  const [connectionType, setConnectionType] = useState<string>('unknown')

  useEffect(() => {
    const handleOnline = () => {
      setIsOnline(true)
      toast.success('Back online', { description: 'Connection restored' })
    }

    const handleOffline = () => {
      setIsOnline(false)
      toast.error('Offline', {
        description: 'You are currently offline. Some features may not work.',
        duration: 5000
      })
    }

    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)

    // Check connection type if available
    const connection = (navigator as any).connection
    if (connection) {
      setConnectionType(connection.effectiveType || 'unknown')
      connection.addEventListener('change', () => {
        setConnectionType(connection.effectiveType || 'unknown')
      })
    }

    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
    }
  }, [])

  return { isOnline, connectionType }
}

interface NetworkStatusBadgeProps {
  showLabel?: boolean
  className?: string
}

export function NetworkStatusBadge({ showLabel, className }: NetworkStatusBadgeProps) {
  const { isOnline } = useNetworkStatus()

  if (isOnline) {
    return (
      <Badge variant="secondary" className={className}>
        <Wifi className="h-3 w-3 mr-1" />
        {showLabel && 'Online'}
      </Badge>
    )
  }

  return (
    <Badge variant="destructive" className={className}>
      <WifiOff className="h-3 w-3 mr-1" />
      {showLabel && 'Offline'}
    </Badge>
  )
}
