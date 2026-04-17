import { useRouteError, isRouteErrorResponse, Link } from "react-router"
import { AlertTriangle, Home, ArrowLeft } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function ErrorBoundary() {
  const error = useRouteError()

  if (isRouteErrorResponse(error)) {
    if (error.status === 404) {
      return <NotFoundPage />
    }

    return (
      <div className="min-h-screen flex items-center justify-center p-4">
        <Card className="max-w-md w-full">
          <CardHeader className="text-center">
            <div className="mx-auto w-12 h-12 rounded-full bg-destructive/10 flex items-center justify-center mb-4">
              <AlertTriangle className="h-6 w-6 text-destructive"  aria-hidden="true" />
            </div>
            <CardTitle className="text-2xl">Error {error.status}</CardTitle>
            <CardDescription>
              {error.statusText || "Something went wrong"}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {error.data && (
              <div className="text-sm text-muted-foreground bg-muted p-3 rounded-lg">
                {error.data}
              </div>
            )}
            <div className="flex gap-2">
              <Button variant="outline" className="flex-1" onClick={() => window.history.back()}>
                <ArrowLeft className="mr-2 h-4 w-4"  aria-hidden="true" />
                Go Back
              </Button>
              <Button className="flex-1" asChild>
                <Link to="/">
                  <Home className="mr-2 h-4 w-4"  aria-hidden="true" />
                  Dashboard
                </Link>
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    )
  }

  // Handle other errors
  const errorMessage = error instanceof Error ? error.message : "An unexpected error occurred"

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <Card className="max-w-md w-full">
        <CardHeader className="text-center">
          <div className="mx-auto w-12 h-12 rounded-full bg-destructive/10 flex items-center justify-center mb-4">
            <AlertTriangle className="h-6 w-6 text-destructive"  aria-hidden="true" />
          </div>
          <CardTitle className="text-2xl">Unexpected Error</CardTitle>
          <CardDescription>
            Something went wrong while loading this page
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="text-sm text-muted-foreground bg-muted p-3 rounded-lg">
            {errorMessage}
          </div>
          <div className="flex gap-2">
            <Button variant="outline" className="flex-1" onClick={() => window.location.reload()}>
              Retry
            </Button>
            <Button className="flex-1" asChild>
              <Link to="/">
                <Home className="mr-2 h-4 w-4"  aria-hidden="true" />
                Dashboard
              </Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

export function NotFoundPage() {
  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <Card className="max-w-md w-full text-center">
        <CardHeader>
          <div className="text-6xl font-bold text-muted-foreground mb-4">404</div>
          <CardTitle className="text-2xl">Page Not Found</CardTitle>
          <CardDescription>
            The page you're looking for doesn't exist or has been moved.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex gap-2 justify-center">
            <Button variant="outline" onClick={() => window.history.back()}>
              <ArrowLeft className="mr-2 h-4 w-4"  aria-hidden="true" />
              Go Back
            </Button>
            <Button asChild>
              <Link to="/">
                <Home className="mr-2 h-4 w-4"  aria-hidden="true" />
                Go Home
              </Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
