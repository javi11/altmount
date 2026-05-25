import { AlertTriangle } from "lucide-react";
import { Component, type ErrorInfo, type ReactNode } from "react";

interface ErrorBoundaryProps {
	children: ReactNode;
	/** Optional custom fallback rendered instead of the default error card. */
	fallback?: ReactNode;
}

interface ErrorBoundaryState {
	error: Error | null;
}

/**
 * ErrorBoundary catches render-time exceptions in its subtree so a single
 * failing component shows a recoverable error card instead of unmounting the
 * whole application. React provides no hook equivalent, so this stays a class.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
	state: ErrorBoundaryState = { error: null };

	static getDerivedStateFromError(error: Error): ErrorBoundaryState {
		return { error };
	}

	componentDidCatch(error: Error, info: ErrorInfo) {
		console.error("Unhandled UI error:", error, info.componentStack);
	}

	private handleReset = () => {
		this.setState({ error: null });
	};

	render() {
		const { error } = this.state;
		if (!error) {
			return this.props.children;
		}
		if (this.props.fallback) {
			return this.props.fallback;
		}
		return (
			<div className="flex min-h-[60vh] items-center justify-center p-4">
				<div className="alert alert-error max-w-lg">
					<AlertTriangle className="h-6 w-6" aria-hidden="true" />
					<div>
						<div className="font-bold">Something went wrong</div>
						<div className="break-words text-sm">{error.message}</div>
					</div>
					<button type="button" className="btn btn-sm" onClick={this.handleReset}>
						Try again
					</button>
				</div>
			</div>
		);
	}
}
