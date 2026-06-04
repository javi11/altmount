import { AlertCircle, RefreshCw } from "lucide-react";

interface ErrorAlertProps {
	error: Error;
	onRetry?: () => void;
	className?: string;
}

export function ErrorAlert({ error, onRetry, className }: ErrorAlertProps) {
	return (
		<div className={`alert alert-error ${className}`}>
			<AlertCircle className="h-6 w-6" />
			<div>
				<div className="font-bold">Something went wrong</div>
				<div className="text-sm">{error.message}</div>
			</div>
			{onRetry && (
				<div>
					<button type="button" className="btn btn-sm btn-outline" onClick={onRetry}>
						<RefreshCw className="h-4 w-4" />
						Retry
					</button>
				</div>
			)}
		</div>
	);
}
