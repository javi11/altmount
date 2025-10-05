import { Network } from "lucide-react";
import { usePoolMetrics } from "../../hooks/useApi";
import { BytesDisplay } from "../ui/BytesDisplay";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface PoolMetricsCardProps {
	className?: string;
}

export function PoolMetricsCard({ className }: PoolMetricsCardProps) {
	const { data: poolMetrics, isLoading, error } = usePoolMetrics();

	// Helper function to format speed
	const formatSpeed = (bytesPerSec: number) => {
		if (bytesPerSec === 0) return "0 B/s";
		const units = ["B/s", "KB/s", "MB/s", "GB/s"];
		const index = Math.floor(Math.log(bytesPerSec) / Math.log(1024));
		const value = bytesPerSec / 1024 ** index;
		return `${value.toFixed(1)} ${units[index]}`;
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
						<h2 className="card-title font-medium text-base-content/70 text-sm">
							Articles Downloaded
						</h2>
						{isLoading ? (
							<LoadingSpinner size="sm" />
						) : poolMetrics ? (
							<div className="font-bold text-2xl text-primary">
								{poolMetrics.articles_downloaded.toLocaleString()}
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
								<BytesDisplay bytes={poolMetrics.bytes_downloaded} />
							</span>
						</div>

						{/* Download Speed */}
						<div className="flex items-center justify-between text-sm">
							<span className="text-base-content/70">Download Speed</span>
							<span
								className={`font-medium ${poolMetrics.download_speed_bytes_per_sec > 0 ? "text-success" : "text-base-content"}`}
							>
								{formatSpeed(poolMetrics.download_speed_bytes_per_sec)}
							</span>
						</div>

						{/* Upload Speed - Only show if > 0 */}
						{poolMetrics.upload_speed_bytes_per_sec > 0 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Upload Speed</span>
								<span className="font-medium text-info">
									{formatSpeed(poolMetrics.upload_speed_bytes_per_sec)}
								</span>
							</div>
						)}

						{/* Total Errors - Only show if > 0 */}
						{poolMetrics.total_errors > 0 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Total Errors</span>
								<span className="font-medium text-error">
									{poolMetrics.total_errors.toLocaleString()}
								</span>
							</div>
						)}

						{/* Provider Errors - Only show if there are any */}
						{poolMetrics.provider_errors && Object.keys(poolMetrics.provider_errors).length > 0 && (
							<div className="space-y-2">
								<span className="font-medium text-base-content/70 text-sm">Provider Errors</span>
								<div className="space-y-1">
									{Object.entries(poolMetrics.provider_errors).map(([provider, errorCount]) => (
										<div key={provider} className="flex items-center justify-between pl-2 text-sm">
											<span className="text-base-content/60">
												<span className="badge badge-ghost badge-sm mr-2">{provider}</span>
											</span>
											<span className="font-medium text-error">{errorCount.toLocaleString()}</span>
										</div>
									))}
								</div>
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
