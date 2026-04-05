import * as React from 'react'
import { useRouteError, isRouteErrorResponse, useNavigate } from 'react-router'
import { AlertCircle, Home, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

export function ErrorBoundary() {
  const error = useRouteError()
  const navigate = useNavigate()

  let errorMessage = 'An unexpected error occurred'
  let errorStatus = 'Error'

  if (isRouteErrorResponse(error)) {
    errorStatus = `${error.status} ${error.statusText}`
    errorMessage = error.data?.message || error.statusText
  } else if (error instanceof Error) {
    errorMessage = error.message
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/50 p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10">
            <AlertCircle className="h-6 w-6 text-destructive" />
          </div>
          <CardTitle className="text-2xl">{errorStatus}</CardTitle>
          <CardDescription>{errorMessage}</CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center gap-4">
          <Button variant="outline" onClick={() => window.location.reload()}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Retry
          </Button>
          <Button onClick={() => navigate('/dashboard')}>
            <Home className="mr-2 h-4 w-4" />
            Dashboard
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}

interface PageErrorBoundaryProps {
  children: React.ReactNode
  fallback?: React.ReactNode
}

interface PageErrorBoundaryState {
  hasError: boolean
  error?: Error
}

export class PageErrorBoundary extends React.Component<
  PageErrorBoundaryProps,
  PageErrorBoundaryState
> {
  constructor(props: PageErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false }
  }

  static getDerivedStateFromError(error: Error): PageErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('Page error:', error, errorInfo)
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }

      return (
        <div className="flex h-[400px] flex-col items-center justify-center rounded-lg border border-dashed">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <p className="mt-2 text-sm text-muted-foreground">
            Something went wrong loading this page
          </p>
          <Button
            variant="outline"
            className="mt-4"
            onClick={() => this.setState({ hasError: false })}
          >
            <RefreshCw className="mr-2 h-4 w-4" />
            Try Again
          </Button>
        </div>
      )
    }

    return this.props.children
  }
}
