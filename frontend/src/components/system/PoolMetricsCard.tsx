import { Network } from "lucide-react";
import { usePoolMetrics } from "../../hooks/useApi";
import { formatSpeed } from "../../lib/utils";
import { BytesDisplay } from "../ui/BytesDisplay";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface PoolMetricsCardProps {
	className?: string;
}

export function PoolMetricsCard({ className }: PoolMetricsCardProps) {
	const { data: poolMetrics, isLoading, error } = usePoolMetrics();

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

	const activeConnections =
		poolMetrics?.providers?.reduce((sum, p) => sum + p.used_connections, 0) ?? 0;
	const maxConnections =
		poolMetrics?.providers?.reduce((sum, p) => sum + p.max_connections, 0) ?? 0;

	return (
		<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
			<div className="card-body">
				<div className="flex items-center justify-between">
					<div className="flex-1">
						<h2 className="card-title font-medium text-base-content/70 text-sm">Connections</h2>
						{isLoading ? (
							<LoadingSpinner size="sm" />
						) : poolMetrics ? (
							<div className="font-bold text-2xl text-primary">
								{activeConnections}
								{maxConnections > 0 && (
									<span className="ml-1 font-normal text-base text-base-content/40">
										/ {maxConnections}
									</span>
								)}
							</div>
						) : (
							<div className="font-bold text-2xl text-base-content/50">--</div>
						)}
					</div>
					<Network className="h-8 w-8 text-primary" />
				</div>

				{poolMetrics && (
					<div className="mt-4 space-y-2">
						{/* Download Speed */}
						<div className="flex items-center justify-between text-sm">
							<span className="text-base-content/70">Download Speed</span>
							<span className="font-medium text-primary">
								{formatSpeed(poolMetrics.download_speed_bytes_per_sec)}
							</span>
						</div>

						{/* Total Downloaded */}
						<div className="flex items-center justify-between text-sm">
							<span className="text-base-content/70">Total Bytes</span>
							<span className="font-medium">
								<BytesDisplay bytes={poolMetrics.bytes_downloaded} />
							</span>
						</div>

						{/* Last 24h Downloaded */}
						<div className="mb-2 flex items-center justify-between border-base-200 border-b pb-2 text-sm">
							<span className="text-base-content/70">Last 24h</span>
							<span className="font-bold text-primary">
								<BytesDisplay bytes={poolMetrics.bytes_downloaded_24h} />
							</span>
						</div>

						{/* Max Speed - Show as secondary */}
						{poolMetrics.max_download_speed_bytes_per_sec > 0 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Peak Speed</span>
								<span className="font-medium text-success">
									{formatSpeed(poolMetrics.max_download_speed_bytes_per_sec)}
								</span>
							</div>
						)}

						{/* Upload Speed - Only show if > 0 */}
						{poolMetrics.upload_speed_bytes_per_sec > 0 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Upload</span>
								<span className="font-medium text-info">
									{formatSpeed(poolMetrics.upload_speed_bytes_per_sec)}
								</span>
							</div>
						)}

						{/* Total Errors - Only show if > 0 */}
						{poolMetrics.total_errors > 0 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Errors</span>
								<span className="font-medium text-error">
									{poolMetrics.total_errors.toLocaleString()}
								</span>
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
