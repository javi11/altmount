import { formatDistanceToNowStrict } from "date-fns";
import { AlertTriangle, CheckCircle, Network, XCircle } from "lucide-react";
import type { ProviderStatus } from "../../types/api";

interface ProviderCardProps {
	provider: ProviderStatus;
	className?: string;
}

export function ProviderCard({ provider, className }: ProviderCardProps) {
	// Calculate connection usage percentage
	const usagePercentage =
		provider.max_connections > 0
			? Math.round((provider.used_connections / provider.max_connections) * 100)
			: 0;

	// Determine state badge color and icon
	const getStateBadge = () => {
		const state = provider.state.toLowerCase();

		switch (state) {
			case "active":
				return {
					color: "badge-success",
					icon: <CheckCircle className="h-3 w-3" />,
					text: "Active",
				};
			case "failed":
			case "failing":
				return {
					color: "badge-error",
					icon: <XCircle className="h-3 w-3" />,
					text: "Failed",
				};
			case "pending":
			case "connecting":
				return {
					color: "badge-warning",
					icon: <AlertTriangle className="h-3 w-3" />,
					text: "Pending",
				};
			default:
				return {
					color: "badge-ghost",
					icon: <Network className="h-3 w-3" />,
					text: state,
				};
		}
	};

	const stateBadge = getStateBadge();

	// Determine progress bar color based on usage
	const getProgressColor = () => {
		if (usagePercentage >= 90) return "progress-error";
		if (usagePercentage >= 70) return "progress-warning";
		return "progress-success";
	};

	return (
		<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
			<div className="card-body">
				{/* Header with host and state badge */}
				<div className="flex items-start justify-between">
					<div className="min-w-0 flex-1">
						<div className="flex items-center gap-2">
							<div
								className={`h-2 w-2 shrink-0 rounded-full ${
									provider.state.toLowerCase() === "active"
										? provider.error_count > 10
											? "animate-pulse bg-warning"
											: "bg-success"
										: provider.state.toLowerCase() === "failed"
											? "bg-error"
											: "bg-base-300"
								}`}
							/>
							<h3 className="card-title truncate font-medium text-base">{provider.host}</h3>
						</div>
						{provider.username && (
							<p className="cursor-pointer truncate text-base-content/60 text-sm blur-sm transition-all hover:blur-none">
								@{provider.username}
							</p>
						)}
					</div>
					<div className={`badge ${stateBadge.color} gap-1`}>
						{stateBadge.icon}
						{stateBadge.text}
					</div>
				</div>

				{/* Connection usage */}
				<div className="mt-3 space-y-2">
					<div className="flex items-center justify-between text-sm">
						<span className="text-base-content/70">Connections</span>
						<span className="font-medium">
							{provider.used_connections} / {provider.max_connections}
						</span>
					</div>
					<progress
						className={`progress w-full ${getProgressColor()}`}
						value={provider.used_connections}
						max={provider.max_connections}
					/>
				</div>

				{/* Missing Articles */}
				{provider.missing_count > 0 && (
					<div className="mt-2 space-y-1">
						<div className="flex items-center justify-between text-sm">
							<span className="text-base-content/70">Missing Articles</span>
							<div className="text-right">
								<span
									className={`font-medium ${provider.missing_warning ? "text-error" : "text-warning"}`}
								>
									{provider.missing_count.toLocaleString()}
								</span>
								{provider.missing_rate_per_minute > 0 && (
									<span className="ml-1 text-base-content/60 text-xs">
										~{Math.round(provider.missing_rate_per_minute)}/min
									</span>
								)}
							</div>
						</div>
						{provider.missing_warning && (
							<div className="alert alert-warning py-2">
								<AlertTriangle className="h-4 w-4" />
								<span className="text-sm">Consider using a backup provider</span>
							</div>
						)}
					</div>
				)}

				{/* Speed Test Info */}
				{provider.last_speed_test_mbps > 0 && (
					<div className="mt-2 flex items-center justify-between text-xs">
						<span className="text-base-content/60">Last Speed Test:</span>
						<div className="text-right">
							<span className="font-medium text-success">
								{provider.last_speed_test_mbps.toFixed(2)} MB/s
							</span>
							{provider.last_speed_test_time && (
								<div className="text-base-content/50">
									{formatDistanceToNowStrict(new Date(provider.last_speed_test_time), {
										addSuffix: true,
									})}
								</div>
							)}
						</div>
					</div>
				)}

				{/* Failure reason - only show if present */}
				{provider.failure_reason && provider.failure_reason !== "" && (
					<div className="mt-2">
						<div className="alert alert-warning py-2">
							<AlertTriangle className="h-4 w-4" />
							<span className="text-sm">{provider.failure_reason}</span>
						</div>
					</div>
				)}
			</div>
		</div>
	);
}
