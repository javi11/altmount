import { Network, RotateCcw } from "lucide-react";
import { usePoolMetrics, useResetSystemStats } from "../../hooks/useApi";
import { BytesDisplay } from "../ui/BytesDisplay";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { formatSpeed } from "../../lib/utils";

interface PoolMetricsCardProps {
	className?: string;
}

export function PoolMetricsCard({ className }: PoolMetricsCardProps) {
	const { data: poolMetrics, isLoading, error } = usePoolMetrics();
	const resetStats = useResetSystemStats();

	const handleReset = () => {
		if (window.confirm("Are you sure you want to reset all cumulative download statistics?")) {
			resetStats.mutate();
		}
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
					<div className="flex-1">
						<div className="flex items-center gap-2">
							<h2 className="card-title font-medium text-base-content/70 text-sm">
								Articles Downloaded
							</h2>
							<button
								type="button"
								onClick={handleReset}
								className={`btn btn-ghost btn-xs ${resetStats.isPending ? "loading" : ""}`}
								title="Reset cumulative statistics"
								disabled={resetStats.isPending}
							>
								{!resetStats.isPending && <RotateCcw className="h-3 w-3" />}
							</button>
						</div>
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

						{/* Max Download Speed - Only show if > 0 */}
						{poolMetrics.max_download_speed_bytes_per_sec > 0 && (
							<div className="flex items-center justify-between text-sm">
								<span className="text-base-content/70">Top Pool Speed</span>
								<span className="font-medium text-success">
									{formatSpeed(poolMetrics.max_download_speed_bytes_per_sec)}
								</span>
							</div>
						)}

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
												</div>
											)}
										</div>
									</div>
								);
							}
							