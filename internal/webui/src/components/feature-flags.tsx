import { useEffect, useState } from 'react'
import { AlertCircle, CheckCircle, XCircle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'

interface FeatureFlag {
  name: string
  enabled: boolean
  description?: string
}

const defaultFlags: FeatureFlag[] = [
  { name: 'websocket', enabled: true, description: 'Real-time updates via WebSocket' },
  { name: 'charts', enabled: true, description: 'Interactive charts and visualizations' },
  { name: 'export', enabled: true, description: 'Data export functionality' },
  { name: 'notifications', enabled: true, description: 'Notification center' },
  { name: 'mock_api', enabled: import.meta.env.DEV, description: 'Mock API for development' },
  { name: 'beta_features', enabled: false, description: 'Experimental features' }
]

export function useFeatureFlags() {
  const [flags, setFlags] = useState<FeatureFlag[]>(() => {
    const saved = localStorage.getItem('olb-feature-flags')
    if (saved) {
      try {
        return JSON.parse(saved)
      } catch {
        return defaultFlags
      }
    }
    return defaultFlags
  })

  useEffect(() => {
    localStorage.setItem('olb-feature-flags', JSON.stringify(flags))
  }, [flags])

  const isEnabled = (name: string): boolean => {
    return flags.find(f => f.name === name)?.enabled ?? false
  }

  const toggle = (name: string) => {
    setFlags(prev =>
      prev.map(f =>
        f.name === name ? { ...f, enabled: !f.enabled } : f
      )
    )
  }

  const enable = (name: string) => {
    setFlags(prev =>
      prev.map(f =>
        f.name === name ? { ...f, enabled: true } : f
      )
    )
  }

  const disable = (name: string) => {
    setFlags(prev =>
      prev.map(f =>
        f.name === name ? { ...f, enabled: false } : f
      )
    )
  }

  return { flags, isEnabled, toggle, enable, disable }
}

interface FeatureFlagBadgeProps {
  name: string
  className?: string
}

export function FeatureFlagBadge({ name, className }: FeatureFlagBadgeProps) {
  const { isEnabled } = useFeatureFlags()
  const enabled = isEnabled(name)

  return (
    <Badge variant={enabled ? 'success' : 'secondary'} className={className}>
      {enabled ? (
        <CheckCircle className="h-3 w-3 mr-1" />
      ) : (
        <XCircle className="h-3 w-3 mr-1" />
      )}
      {name}
    </Badge>
  )
}

interface FeatureGateProps {
  flag: string
  children: React.ReactNode
  fallback?: React.ReactNode
}

export function FeatureGate({ flag, children, fallback }: FeatureGateProps) {
  const { isEnabled } = useFeatureFlags()

  if (!isEnabled(flag)) {
    return fallback || (
      <div className="flex items-center justify-center p-8 text-muted-foreground">
        <AlertCircle className="h-5 w-5 mr-2" />
        <span>This feature is currently disabled</span>
      </div>
    )
  }

  return <>{children}</>
}
