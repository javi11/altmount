import { AlertTriangle, Gauge, RotateCcw } from "lucide-react";
import { formatSpeed } from "../../lib/utils";
import type { ProviderStatus } from "../../types/api";
import { BytesDisplay } from "../ui/BytesDisplay";

function getQuotaProgressColor(provider: ProviderStatus): string {
	if (provider.quota_exceeded) {
		return "progress-error";
	}
	if ((provider.quota_used ?? 0) / (provider.quota_bytes ?? 1) >= 0.9) {
		return "progress-warning";
	}
	return "progress-success";
}

interface ProviderCardProps {
	provider: ProviderStatus;
	className?: string;
	onResetQuota?: (providerId: string) => void;
}

const MS_PER_MINUTE = 1000 * 60;
const MS_PER_HOUR = MS_PER_MINUTE * 60;

function QuotaResetCountdown({ resetAt }: { resetAt: string }) {
	const diffMs = new Date(resetAt).getTime() - Date.now();

	if (diffMs <= 0) {
		return <span>resetting...</span>;
	}

	const hours = Math.floor(diffMs / MS_PER_HOUR);
	const minutes = Math.floor((diffMs % MS_PER_HOUR) / MS_PER_MINUTE);

	if (hours > 24) {
		const days = Math.floor(hours / 24);
		return (
			<span>
				in {days}d {hours % 24}h
			</span>
		);
	}

	return (
		<span>
			in {hours}h {minutes}m
		</span>
	);
}

export function ProviderCard({ provider, className, onResetQuota }: ProviderCardProps) {
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
					text: "Active",
				};
			case "failed":
			case "failing":
				return {
					color: "badge-error",
					text: "Failed",
				};
			case "pending":
			case "connecting":
				return {
					color: "badge-warning",
					text: "Pending",
				};
			default:
				return {
					color: "badge-ghost",
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
		<article
			className={`card bg-base-100 shadow-sm transition-shadow ${className || ""}`}
			aria-labelledby={`provider-${provider.host}`}
		>
			<div className="card-body p-4">
				{/* Header with host and state badge */}
				<div className="flex items-start justify-between">
					<div className="min-w-0 flex-1">
						<div className="flex items-center gap-1.5">
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
							<h3
								id={`provider-${provider.host}`}
								className="card-title truncate font-medium text-sm"
							>
								{provider.host}
							</h3>
						</div>
						{provider.username && (
							<p className="cursor-pointer truncate text-base-content/40 text-xs blur-[1px] transition-all hover:blur-none">
								@{provider.username}
							</p>
						)}
					</div>
					<div className={`badge ${stateBadge.color} badge-xs font-bold uppercase`}>
						{stateBadge.text}
					</div>
				</div>

				{/* Connection usage */}
				<div className="mt-2 space-y-1">
					<div className="flex items-center justify-between text-xs">
						<span className="text-base-content/50 uppercase tracking-tight">Pool Usage</span>
						<span className="font-mono font-semibold">
							{provider.used_connections} / {provider.max_connections}
						</span>
					</div>
					<progress
						className={`progress h-1 w-full ${getProgressColor()}`}
						value={provider.used_connections}
						max={provider.max_connections}
					/>
				</div>

				{/* Performance Stats */}
				<div className="mt-3 grid grid-cols-3 gap-1 border-base-200 border-t pt-3 text-center">
					<div className="space-y-0.5">
						<div className="text-[8px] text-base-content/40 uppercase tracking-widest">Speed</div>
						<div className="truncate font-bold font-mono text-primary text-xs">
							{provider.current_speed_bytes_per_sec !== undefined
								? formatSpeed(provider.current_speed_bytes_per_sec)
								: "0 B/s"}
						</div>
					</div>
					<div className="space-y-0.5">
						<div className="text-[8px] text-base-content/40 uppercase tracking-widest">Ping</div>
						<div className="font-bold font-mono text-info text-xs">{provider.ping_ms}ms</div>
					</div>
					<div className="space-y-0.5">
						<div className="text-[8px] text-base-content/40 uppercase tracking-widest">Errors</div>
						<div
							className={`font-bold font-mono text-xs ${provider.error_count > 0 ? "text-error" : "text-base-content/20"}`}
						>
							{provider.error_count}
						</div>
					</div>
				</div>

				{/* Total Bytes per provider */}
				<div className="mt-2 space-y-1 border-base-200 border-t pt-2">
					<div className="flex items-center justify-between text-[10px]">
						<span className="text-base-content/50 uppercase tracking-tight">Total Downloaded</span>
						<span className="font-bold font-mono text-base-content/70">
							<BytesDisplay bytes={provider.byte_count} />
						</span>
					</div>
					<div className="flex items-center justify-between text-[10px]">
						<span className="text-base-content/50 uppercase tracking-tight">Last 24h</span>
						<span className="font-bold font-mono text-primary">
							<BytesDisplay bytes={provider.byte_count_24h} />
						</span>
					</div>
				</div>

				{/* Download Quota */}
				{provider.quota_bytes != null && provider.quota_bytes > 0 && (
					<div className="mt-2 space-y-1 border-base-200 border-t pt-2">
						<div className="flex items-center justify-between text-[10px]">
							<span className="flex items-center gap-1 text-base-content/50 uppercase tracking-tight">
								<Gauge className="h-3 w-3" />
								Quota
								{onResetQuota && (
									<button
										type="button"
										className="btn btn-ghost btn-xs ml-1 h-4 min-h-0 w-4 p-0"
										title="Reset quota"
										onClick={() => onResetQuota(provider.id)}
									>
										<RotateCcw className="h-2.5 w-2.5" />
									</button>
								)}
							</span>
							<span
								className={`font-bold font-mono ${provider.quota_exceeded ? "text-error" : "text-base-content/70"}`}
							>
								<BytesDisplay bytes={provider.quota_used ?? 0} /> /{" "}
								<BytesDisplay bytes={provider.quota_bytes} />
							</span>
						</div>
						<progress
							className={`progress h-1 w-full ${getQuotaProgressColor(provider)}`}
							value={provider.quota_used ?? 0}
							max={provider.quota_bytes}
						/>
						{provider.quota_reset_at && (
							<div className="flex items-center justify-between text-[10px]">
								<span className="text-base-content/40">Resets</span>
								<span className="font-mono text-base-content/50">
									<QuotaResetCountdown resetAt={provider.quota_reset_at} />
								</span>
							</div>
						)}
					</div>
				)}

				{/* Missing Articles */}
				{provider.missing_count > 0 && (
					<div className="mt-2 border-base-200 border-t pt-2">
						<div className="flex items-center justify-between text-xs">
							<span className="text-base-content/50">Missing</span>
							<div className="text-right">
								<span
									className={`font-mono font-semibold ${provider.missing_warning ? "text-error" : "text-warning"}`}
								>
									{provider.missing_count.toLocaleString()}
								</span>
								{provider.missing_rate_per_minute > 0 && (
									<span className="ml-0.5 text-base-content/40 text-xs">
										~{Math.round(provider.missing_rate_per_minute)}/min
									</span>
								)}
							</div>
						</div>
					</div>
				)}

				{/* Failure reason - only show if present */}
				{provider.failure_reason && provider.failure_reason !== "" && (
					<div className="mt-2">
						<div className="alert alert-error rounded-md px-2 py-1.5">
							<AlertTriangle className="h-3 w-3" />
							<span className="text-xs">{provider.failure_reason}</span>
						</div>
					</div>
				)}
			</div>
		</article>
	);
}
