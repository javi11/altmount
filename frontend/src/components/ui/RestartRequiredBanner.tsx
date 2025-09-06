import { AlertTriangle, RefreshCw, X } from "lucide-react";

interface RestartRequiredBannerProps {
	restartRequiredConfigs: string[];
	onDismiss: () => void;
	isDismissed: boolean;
}

export function RestartRequiredBanner({
	restartRequiredConfigs,
	onDismiss,
	isDismissed,
}: RestartRequiredBannerProps) {
	if (restartRequiredConfigs.length === 0 || isDismissed) {
		return null;
	}

	return (
		<div className="alert alert-warning mb-6">
			<AlertTriangle className="h-6 w-6" />
			<div className="flex-1">
				<div className="font-bold">Server restart required</div>
				<div className="text-sm">
					The following settings have been updated and require a server restart to take effect:
					<span className="ml-1 font-medium">{restartRequiredConfigs.join(", ")}</span>
				</div>
			</div>
			<div className="flex items-center space-x-2">
				<div className="flex items-center text-sm">
					<RefreshCw className="mr-1 h-4 w-4" />
					Restart the server to apply changes
				</div>
				<button
					type="button"
					className="btn btn-ghost btn-sm"
					onClick={onDismiss}
					aria-label="Dismiss restart notification"
				>
					<X className="h-4 w-4" />
				</button>
			</div>
		</div>
	);
}
