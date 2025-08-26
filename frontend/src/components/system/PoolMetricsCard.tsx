import { Network } from "lucide-react";
import { usePoolMetrics } from "../../hooks/useApi";
import { BytesDisplay } from "../ui/BytesDisplay";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface PoolMetricsCardProps {
	className?: string;
}

export function PoolMetricsCard({ className }: PoolMetricsCardProps) {
	const { data: poolMetrics, isLoading, error } = usePoolMetrics();
	console.log(poolMetrics);
	// Helper function to format speed in human readable format
	const _formatSpeed = (bytesPerSec: number) => {
		const mbPerSec = bytesPerSec / (1024 * 1024);
		return `${mbPerSec.toFixed(1)} MB/s`;
	};

	// Helper function to format percentage
	const formatPercentage = (value: number) => {
		return `${value.toFixed(1)}%`;
	};

	if (error) {
		return (
			<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
				<div className="card-body">
					<div className="flex items-center justify-between">
						<div>
							<h2 className="card-title font-medium text-base-content/70 text-sm">Pool Metrics</h2>
							<div className="text-error text-sm">Failed to load</div>
						</div>
						<Network className="h-8 w-8 text-error" />
					</div>
				</div>
			</div>
		);
	}

	return (
		<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
			<div className="card-body">
				<div className="flex items-center justify-between">
					<div>
						<h2 className="card-title font-medium text-base-content/70 text-sm">Active Connections</h2>
						{isLoading ? (
							<LoadingSpinner size="sm" />
						) : poolMetrics ? (
							<div className="font-bold text-2xl text-primary">
								{poolMetrics.active_connections}
							</div>
						) : (
							<div className="font-bold text-2xl text-base-content/50">--</div>
						)}
					</div>
					<Network className="h-8 w-8 text-primary" />
				</div>

				{poolMetrics && (
					<div className="mt-4 space-y-2">
						{/* Total Downloaded */}
						<div className="flex items-center justify-between text-sm">
							<span className="text-base-content/70">Downloaded</span>
							<span className="font-medium">
								<BytesDisplay bytes={poolMetrics.total_bytes_downloaded} />
							</span>
						</div>

						{/* Success Rate */}
						<div className="flex items-center justify-between text-sm">
							<span className="text-base-content/70">Success Rate</span>
							<span
								className={`font-medium ${poolMetrics.command_success_rate_percent >= 95 ? "text-success" : poolMetrics.command_success_rate_percent >= 90 ? "text-warning" : "text-error"}`}
							>
								{formatPercentage(poolMetrics.command_success_rate_percent)}
							</span>
						</div>

						{/* Error Rate - Only show if > 0 */}
						{poolMetrics.error_rate_percent > 0 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Error Rate</span>
								<span className="font-medium text-error">
									{formatPercentage(poolMetrics.error_rate_percent)}
								</span>
							</div>
						)}

						{/* Connection Wait Time - Only show if > 100ms */}
						{poolMetrics.acquire_wait_time_ms > 100 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Avg Wait Time</span>
								<span
									className={`font-medium ${poolMetrics.acquire_wait_time_ms > 1000 ? "text-warning" : "text-base-content"}`}
								>
									{poolMetrics.acquire_wait_time_ms}ms
								</span>
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
