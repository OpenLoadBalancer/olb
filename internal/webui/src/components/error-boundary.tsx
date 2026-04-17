import { Component, type ReactNode } from 'react'
import { AlertTriangle, RefreshCw } from 'lucide-react'

interface Props {
	children: ReactNode
}

interface State {
	hasError: boolean
	error: Error | null
}

export class PageErrorBoundary extends Component<Props, State> {
	state: State = { hasError: false, error: null }

	static getDerivedStateFromError(error: Error): State {
		return { hasError: true, error }
	}

	componentDidCatch(error: Error, info: React.ErrorInfo) {
		console.error('[PageErrorBoundary]', error, info.componentStack)
	}

	handleRetry = () => {
		this.setState({ hasError: false, error: null })
	}

	render() {
		if (this.state.hasError) {
			return (
				<div className="flex flex-col items-center justify-center min-h-[60vh] gap-4 p-8">
					<AlertTriangle className="h-12 w-12 text-destructive"  aria-hidden="true" />
					<h2 className="text-xl font-semibold">Something went wrong</h2>
					<p className="text-muted-foreground text-center max-w-md">
						This page encountered an unexpected error. You can try again or navigate to a different page.
					</p>
					{this.state.error && (
						<pre className="text-xs text-muted-foreground bg-muted p-3 rounded-md max-w-lg overflow-auto">
							{this.state.error.message}
						</pre>
					)}
					<button
						onClick={this.handleRetry}
						className="inline-flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors"
					>
						<RefreshCw className="h-4 w-4"  aria-hidden="true" />
						Retry
					</button>
				</div>
			)
		}

		return this.props.children
	}
}
