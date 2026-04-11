import { Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"

interface LoadingProps {
  className?: string
  size?: "sm" | "md" | "lg"
  text?: string
}

export function Loading({ className, size = "md", text }: LoadingProps) {
  const sizeClasses = {
    sm: "h-4 w-4",
    md: "h-8 w-8",
    lg: "h-12 w-12",
  }

  return (
    <div role="status" className={cn("flex flex-col items-center justify-center gap-2", className)}>
      <Loader2 className={cn("animate-spin text-primary", sizeClasses[size])} aria-hidden="true" />
      {text && <p className="text-sm text-muted-foreground">{text}</p>}
      <span className="sr-only">Loading...</span>
    </div>
  )
}

interface LoadingOverlayProps {
  className?: string
  text?: string
}

export function LoadingOverlay({ className, text = "Loading..." }: LoadingOverlayProps) {
  return (
    <div role="status" className={cn("absolute inset-0 flex items-center justify-center bg-background/80 backdrop-blur-sm z-50", className)}>
      <Loading text={text} />
    </div>
  )
}

interface LoadingCardProps {
  className?: string
}

export function LoadingCard({ className }: LoadingCardProps) {
  return (
    <div aria-hidden="true" className={cn("p-6 rounded-lg border bg-card", className)}>
      <div className="space-y-3">
        <div className="h-4 w-1/3 bg-muted rounded animate-pulse" />
        <div className="h-8 w-2/3 bg-muted rounded animate-pulse" />
        <div className="h-4 w-full bg-muted rounded animate-pulse" />
      </div>
    </div>
  )
}

interface LoadingTableProps {
  rows?: number
  className?: string
}

export function LoadingTable({ rows = 5, className }: LoadingTableProps) {
  return (
    <div aria-hidden="true" className={cn("space-y-2", className)}>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex gap-4 p-3 border rounded-lg">
          <div className="h-4 w-1/4 bg-muted rounded animate-pulse" />
          <div className="h-4 w-1/4 bg-muted rounded animate-pulse" />
          <div className="h-4 w-1/4 bg-muted rounded animate-pulse" />
          <div className="h-4 w-1/4 bg-muted rounded animate-pulse" />
        </div>
      ))}
    </div>
  )
}
