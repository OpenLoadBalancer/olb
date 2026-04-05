import { cn } from '@/lib/utils'
import { CheckCircle, XCircle, AlertCircle, Clock, Activity } from 'lucide-react'
import { cva, type VariantProps } from 'class-variance-authority'

const healthIndicatorVariants = cva(
  'relative flex h-3 w-3 rounded-full',
  {
    variants: {
      status: {
        healthy: 'bg-green-500',
        unhealthy: 'bg-red-500',
        warning: 'bg-amber-500',
        unknown: 'bg-gray-400',
        pending: 'bg-blue-400 animate-pulse'
      }
    },
    defaultVariants: {
      status: 'unknown'
    }
  }
)

interface HealthIndicatorProps extends VariantProps<typeof healthIndicatorVariants> {
  className?: string
  showPulse?: boolean
}

export function HealthIndicator({ status, className, showPulse }: HealthIndicatorProps) {
  return (
    <span className={cn(healthIndicatorVariants({ status }), className)}>
      {showPulse && status === 'healthy' && (
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-75" />
      )}
    </span>
  )
}

interface HealthStatusBadgeProps {
  status: 'healthy' | 'unhealthy' | 'warning' | 'unknown' | 'up' | 'down' | 'draining' | 'starting'
  className?: string
  showIcon?: boolean
  size?: 'sm' | 'md' | 'lg'
}

const statusConfig = {
  healthy: { icon: CheckCircle, color: 'text-green-600', bg: 'bg-green-50', border: 'border-green-200' },
  up: { icon: CheckCircle, color: 'text-green-600', bg: 'bg-green-50', border: 'border-green-200' },
  unhealthy: { icon: XCircle, color: 'text-red-600', bg: 'bg-red-50', border: 'border-red-200' },
  down: { icon: XCircle, color: 'text-red-600', bg: 'bg-red-50', border: 'border-red-200' },
  warning: { icon: AlertCircle, color: 'text-amber-600', bg: 'bg-amber-50', border: 'border-amber-200' },
  draining: { icon: Clock, color: 'text-amber-600', bg: 'bg-amber-50', border: 'border-amber-200' },
  starting: { icon: Activity, color: 'text-blue-600', bg: 'bg-blue-50', border: 'border-blue-200' },
  unknown: { icon: AlertCircle, color: 'text-gray-600', bg: 'bg-gray-50', border: 'border-gray-200' }
}

const sizeConfig = {
  sm: 'text-xs px-2 py-0.5',
  md: 'text-sm px-2.5 py-0.5',
  lg: 'text-base px-3 py-1'
}

export function HealthStatusBadge({
  status,
  className,
  showIcon = true,
  size = 'md'
}: HealthStatusBadgeProps) {
  const config = statusConfig[status as keyof typeof statusConfig] || statusConfig.unknown
  const Icon = config.icon

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border font-medium',
        config.color,
        config.bg,
        config.border,
        sizeConfig[size],
        className
      )}
    >
      {showIcon && <Icon className={cn('h-3.5 w-3.5', size === 'lg' && 'h-4 w-4')} />}
      <span className="capitalize">{status}</span>
    </span>
  )
}

interface HealthScoreRingProps {
  score: number
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

export function HealthScoreRing({ score, size = 'md', className }: HealthScoreRingProps) {
  const sizeConfig = {
    sm: { width: 40, strokeWidth: 3, fontSize: '10px' },
    md: { width: 60, strokeWidth: 4, fontSize: '12px' },
    lg: { width: 80, strokeWidth: 5, fontSize: '14px' }
  }

  const { width, strokeWidth, fontSize } = sizeConfig[size]
  const radius = (width - strokeWidth) / 2
  const circumference = radius * 2 * Math.PI
  const offset = circumference - (score / 100) * circumference

  const getColor = (score: number) => {
    if (score >= 90) return '#22c55e'
    if (score >= 70) return '#f59e0b'
    return '#ef4444'
  }

  const color = getColor(score)

  return (
    <div className={cn('relative inline-flex items-center justify-center', className)}>
      <svg width={width} height={width} className="transform -rotate-90">
        {/* Background circle */}
        <circle
          cx={width / 2}
          cy={width / 2}
          r={radius}
          fill="none"
          stroke="hsl(var(--muted))"
          strokeWidth={strokeWidth}
        />
        {/* Progress circle */}
        <circle
          cx={width / 2}
          cy={width / 2}
          r={radius}
          fill="none"
          stroke={color}
          strokeWidth={strokeWidth}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          style={{ transition: 'stroke-dashoffset 0.5s ease' }}
        />
      </svg>
      <span
        className="absolute font-semibold"
        style={{ fontSize, color }}
      >
        {score}
      </span>
    </div>
  )
}

interface UptimeBadgeProps {
  uptime: number // in seconds
  className?: string
}

export function UptimeBadge({ uptime, className }: UptimeBadgeProps) {
  const formatUptime = (seconds: number): string => {
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)

    if (days > 0) return `${days}d ${hours}h`
    if (hours > 0) return `${hours}h ${minutes}m`
    return `${minutes}m`
  }

  return (
    <span className={cn('inline-flex items-center gap-1 text-sm text-muted-foreground', className)}>
      <Clock className="h-3.5 w-3.5" />
      {formatUptime(uptime)}
    </span>
  )
}
