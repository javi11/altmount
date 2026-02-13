import { Activity, AlertTriangle, CheckCircle2, Wifi, WifiOff } from "lucide-react";
import { usePoolMetrics } from "../../../../hooks/useApi";
import { formatBytes, formatRelativeTime } from "../../../../lib/utils";

export function ProviderHealth() {
	const { data, isLoading, error } = usePoolMetrics();

	if (isLoading) {
		return (
			<div className="flex items-center justify-center p-8">
				<span className="loading loading-spinner loading-lg text-primary" />
			</div>
		);
	}

	if (error) {
		return (
			<div className="alert alert-error">
				<AlertTriangle className="h-6 w-6" />
				<span>Failed to load provider metrics: {(error as Error).message}</span>
			</div>
		);
	}

	if (!data) {
		return null;
	}

	const totalMaxConnections = data.providers.reduce(
		(sum, provider) => sum + provider.max_connections,
		0,
	);
	const totalUsedConnections = data.providers.reduce((sum, provider) => {
		if (provider.state === "connected" || provider.state === "active") {
			return sum + provider.used_connections;
		}
		return sum;
	}, 0);

	return (
		<div className="space-y-6">
			{/* Global Metrics Cards */}
			<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-primary">
						<Activity className="h-8 w-8" />
					</div>
					<div className="stat-title">Download Traffic</div>
					<div className="stat-value text-2xl text-primary">
						{formatBytes(data.bytes_downloaded)}
					</div>
					<div className="stat-desc font-mono">
						{formatBytes(data.download_speed_bytes_per_sec)}/s
					</div>
				</div>

				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-secondary">
						<Wifi className="h-8 w-8" />
					</div>
					<div className="stat-title">Articles</div>
					<div className="stat-value text-2xl text-secondary">
						{data.articles_downloaded.toLocaleString()}
					</div>
					<div className="stat-desc">Downloaded</div>
				</div>

				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-error">
						<AlertTriangle className="h-8 w-8" />
					</div>
					<div className="stat-title">Total Errors</div>
					<div className="stat-value text-2xl text-error">{data.total_errors.toLocaleString()}</div>
					<div className="stat-desc">Across all providers</div>
				</div>

				<div className="stat rounded-box bg-base-100 shadow">
					<div className="stat-figure text-info">
						<CheckCircle2 className="h-8 w-8" />
					</div>
					<div className="stat-title">Active Connections</div>
					<div className="stat-value text-2xl text-info">
						{totalUsedConnections}
						<span className="text-base-content/50 text-lg"> / {totalMaxConnections}</span>
					</div>
				</div>
			</div>

			{/* Provider Table */}
			<div className="card bg-base-100 shadow-xl">
				<div className="card-body p-0">
					<div className="border-base-200 border-b p-4">
						<h2 className="card-title text-lg">Provider Performance</h2>
					</div>
					<div className="overflow-x-auto">
						<table className="table-zebra table">
							<thead>
								<tr>
									<th>Provider Host</th>
									<th>State</th>
									<th>Connections</th>
									<th>Missing</th>
																   <th>Current Speed</th>
																   <th>Top Speed</th>
																   </tr>
																</thead>
																<tbody>
																   {data.providers.map((provider) => (
																   <tr key={provider.id}>
																   <td className="font-medium">
																   <div className="flex flex-col">
																   <span>{provider.host}</span>
																   <span className="cursor-pointer font-mono text-base-content/50 text-xs blur-sm transition-all hover:blur-none">
																   {provider.username}
																   </span>
																   </div>
																   </td>
																   <td>
																   <div className="flex items-center gap-2">
																   {provider.state === "connected" || provider.state === "active" ? (
																   <span className="badge badge-success badge-sm gap-1">
																   <Wifi className="h-3 w-3" /> Connected
																   </span>
																   ) : provider.state === "disconnected" ? (
																   <span className="badge badge-ghost badge-sm gap-1">
																   <WifiOff className="h-3 w-3" /> Disconnected
																   </span>
																   ) : (
																   <span className="badge badge-warning badge-sm">{provider.state}</span>
																   )}
																   </div>
																   </td>
																   <td>
																   <div className="flex items-center gap-2">
																   <progress
																   className="progress progress-primary w-20"
																   value={provider.used_connections}
																   max={provider.max_connections}
																   />
																   <span className="font-mono text-sm">
																   {provider.used_connections}/{provider.max_connections}
																   </span>
																   </div>
																   </td>
																   <td>
																   {provider.missing_count > 0 ? (
																   <div className="flex flex-col">
																   <span
																   className={`font-medium ${provider.missing_warning ? "text-error" : "text-warning"}`}
																   >
																   {provider.missing_count.toLocaleString()}
																   </span>
																   {provider.missing_rate_per_minute > 0 && (
																   <span
																   className={`text-xs ${provider.missing_warning ? "text-error/70" : "text-base-content/50"}`}
																   >
																   ~{Math.round(provider.missing_rate_per_minute)}/min
																   </span>
																   )}
																   </div>
																   ) : (
																   <span className="text-base-content/50">0</span>
																   )}
																   </td>
																   <td>
																   {provider.current_speed_bytes_per_sec > 0 ? (
																   <span className="font-medium font-mono text-info">
																   {formatBytes(provider.current_speed_bytes_per_sec)}/s
																   </span>
																   ) : (
																   <span className="text-base-content/50">-</span>
																   )}
																   </td>
																   <td>
																   {provider.last_speed_test_mbps > 0 ? (
																   <div className="flex flex-col">
																   <span className="font-medium text-success">
																   {provider.last_speed_test_mbps.toFixed(2)} Mbps
																   </span>
																   {provider.last_speed_test_time && (
																   <span className="text-base-content/50 text-xs">
																   {formatRelativeTime(provider.last_speed_test_time)}
																   </span>
																   )}
																   </div>
																   ) : (
																   <span className="text-base-content/50">-</span>
																   )}
																   </td>
																   </tr>
																   ))}
																</tbody>
									
						</table>
					</div>
				</div>
			</div>
		</div>
	);
}
