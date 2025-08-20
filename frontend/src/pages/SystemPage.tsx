import {
	Activity,
	AlertTriangle,
	CheckCircle,
	Cpu,
	Database,
	RefreshCw,
	Server,
	Settings,
	Shield,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner } from "../components/ui/LoadingSpinner";
import { HealthBadge } from "../components/ui/StatusBadge";
import {
	useHealthStats,
	useQueueStats,
	useSystemCleanup,
	useSystemHealth,
	useSystemStats,
} from "../hooks/useApi";
import { formatRelativeTime } from "../lib/utils";

export function SystemPage() {
	const [cleanupLoading, setCleanupLoading] = useState(false);

	const {
		data: systemStats,
		isLoading: statsLoading,
		error: statsError,
		refetch: refetchStats,
	} = useSystemStats();

	const {
		data: systemHealth,
		isLoading: healthLoading,
		error: healthError,
		refetch: refetchHealth,
	} = useSystemHealth();

	const { data: queueStats } = useQueueStats();
	const { data: healthMetrics } = useHealthStats();
	const systemCleanup = useSystemCleanup();

	const handleSystemCleanup = async () => {
		const olderThan = new Date(
			Date.now() - 7 * 24 * 60 * 60 * 1000,
		).toISOString();

		if (
			confirm(
				"Are you sure you want to cleanup old system data? This will remove completed queue items and old health records older than 7 days.",
			)
		) {
			setCleanupLoading(true);
			try {
				await systemCleanup.mutateAsync({
					queue_older_than: olderThan,
					health_older_than: olderThan,
				});
			} finally {
				setCleanupLoading(false);
			}
		}
	};

	const handleRefreshAll = () => {
		refetchStats();
		refetchHealth();
	};

	const isLoading = statsLoading || healthLoading;
	const hasError = statsError || healthError;

	if (hasError) {
		return (
			<div className="space-y-4">
				<h1 className="text-3xl font-bold">System Management</h1>
				<ErrorAlert error={hasError as Error} onRetry={handleRefreshAll} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
				<div>
					<h1 className="text-3xl font-bold">System Management</h1>
					<p className="text-base-content/70">
						Monitor system health and manage resources
					</p>
				</div>
				<div className="flex gap-2">
					<button
						type="button"
						className="btn btn-outline"
						onClick={handleRefreshAll}
						disabled={isLoading}
					>
						<RefreshCw
							className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`}
						/>
						Refresh
					</button>
					<button
						type="button"
						className="btn btn-warning"
						onClick={handleSystemCleanup}
						disabled={cleanupLoading}
					>
						<Trash2 className="h-4 w-4" />
						System Cleanup
					</button>
				</div>
			</div>

			{/* System Overview */}
			<div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
				{/* System Info */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Server className="h-5 w-5" />
							System Information
						</h2>
						{systemStats ? (
							<div className="space-y-3">
								<div className="flex justify-between">
									<span className="text-base-content/70">Go Version</span>
									<span className="font-mono text-sm">
										{systemStats.go_version}
									</span>
								</div>
								<div className="flex justify-between">
									<span className="text-base-content/70">Uptime</span>
									<span className="font-mono text-sm">
										{systemStats.uptime}
									</span>
								</div>
								<div className="flex justify-between">
									<span className="text-base-content/70">Started</span>
									<span className="text-sm">
										{formatRelativeTime(systemStats.start_time)}
									</span>
								</div>
							</div>
						) : (
							<LoadingSpinner />
						)}
					</div>
				</div>

				{/* System Health */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Activity className="h-5 w-5" />
							System Health
						</h2>
						{systemHealth ? (
							<div className="space-y-3">
								<div className="flex justify-between items-center">
									<span className="text-base-content/70">Overall Status</span>
									<HealthBadge status={systemHealth.status} />
								</div>
								<div className="flex justify-between">
									<span className="text-base-content/70">Components</span>
									<span>{Object.keys(systemHealth.components).length}</span>
								</div>
								<div className="flex justify-between">
									<span className="text-base-content/70">Last Check</span>
									<span className="text-sm">
										{formatRelativeTime(systemHealth.timestamp)}
									</span>
								</div>
								<div className="divider my-2"></div>
								<div className="space-y-2">
									{Object.entries(systemHealth.components).map(
										([name, component]) => (
											<div
												key={name}
												className="flex justify-between items-center"
											>
												<span className="text-sm capitalize">
													{name.replace("_", " ")}
												</span>
												<div className="flex items-center space-x-2">
													<HealthBadge
														status={component.status}
														className="badge-sm"
													/>
													{component.details && (
														<div
															className="tooltip tooltip-left"
															data-tip={component.details}
														>
															<AlertTriangle className="h-3 w-3 text-warning" />
														</div>
													)}
												</div>
											</div>
										),
									)}
								</div>
							</div>
						) : (
							<LoadingSpinner />
						)}
					</div>
				</div>

				{/* Resource Usage */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Cpu className="h-5 w-5" />
							Resource Overview
						</h2>
						<div className="space-y-4">
							{/* Queue Usage */}
							{queueStats && (
								<div className="space-y-2">
									<div className="flex justify-between">
										<span className="text-base-content/70">Queue Usage</span>
										<span className="text-sm">{queueStats.total} items</span>
									</div>
									<progress
										className="progress progress-primary w-full"
										value={queueStats.processing + queueStats.completed}
										max={queueStats.total}
									/>
									<div className="text-xs text-base-content/50">
										{queueStats.processing} processing, {queueStats.completed}{" "}
										completed
									</div>
								</div>
							)}

							{/* Health Monitoring */}
							{healthMetrics && (
								<div className="space-y-2">
									<div className="flex justify-between">
										<span className="text-base-content/70">File Health</span>
										<span className="text-sm">{healthMetrics.total} files</span>
									</div>
									<progress
										className="progress progress-success w-full"
										value={healthMetrics.healthy}
										max={healthMetrics.total}
									/>
									<div className="text-xs text-base-content/50">
										{healthMetrics.healthy} healthy, {healthMetrics.corrupted}{" "}
										corrupted
									</div>
								</div>
							)}
						</div>
					</div>
				</div>
			</div>

			{/* System Statistics */}
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
				{/* Performance Metrics */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Database className="h-5 w-5" />
							Performance Metrics
						</h2>
						<div className="stats stats-vertical w-full">
							{queueStats && (
								<>
									<div className="stat">
										<div className="stat-title">Active Queue Items</div>
										<div className="stat-value text-primary">
											{queueStats.processing}
										</div>
										<div className="stat-desc">Currently being processed</div>
									</div>
									<div className="stat">
										<div className="stat-title">Queue Backlog</div>
										<div className="stat-value text-warning">
											{queueStats.pending}
										</div>
										<div className="stat-desc">
											{queueStats.failed > 0 &&
												`${queueStats.failed} failed items`}
										</div>
									</div>
								</>
							)}
						</div>
					</div>
				</div>

				{/* System Actions */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Settings className="h-5 w-5" />
							System Actions
						</h2>
						<div className="space-y-4">
							<div className="form-control">
								<label className="label" htmlFor="system-maintenance">
									<span className="label-text">System Maintenance</span>
								</label>
								<button
								type="button"
									className="btn btn-outline btn-warning"
									onClick={handleSystemCleanup}
									disabled={cleanupLoading}
								>
									<Trash2 className="h-4 w-4" />
									{cleanupLoading ? "Cleaning up..." : "Cleanup Old Data"}
								</button>
								<label className="label" htmlFor="system-maintenance">
									<span className="label-text-alt">
										Remove completed queue items and health records older than 7
										days
									</span>
								</label>
							</div>

							<div className="form-control">
								<label className="label" htmlFor="system-health">
									<span className="label-text">System Health</span>
								</label>
								<button
									type="button"
									className="btn btn-outline btn-info"
									onClick={() => refetchHealth()}
									disabled={healthLoading}
								>
									<Shield className="h-4 w-4" />
									{healthLoading ? "Checking..." : "Run Health Check"}
								</button>
								<label className="label" htmlFor="system-health">
									<span className="label-text-alt">
										Perform comprehensive system health validation
									</span>
								</label>
							</div>
						</div>
					</div>
				</div>
			</div>

			{/* Component Status Details */}
			{systemHealth && Object.keys(systemHealth.components).length > 0 && (
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<CheckCircle className="h-5 w-5" />
							Component Status Details
						</h2>
						<div className="overflow-x-auto">
							<table className="table table-zebra">
								<thead>
									<tr>
										<th>Component</th>
										<th>Status</th>
										<th>Message</th>
										<th>Details</th>
									</tr>
								</thead>
								<tbody>
									{Object.entries(systemHealth.components).map(
										([name, component]) => (
											<tr key={name}>
												<td className="font-medium capitalize">
													{name.replace("_", " ")}
												</td>
												<td>
													<HealthBadge status={component.status} />
												</td>
												<td>{component.message}</td>
												<td>
													{component.details ? (
														<span className="text-sm text-base-content/70">
															{component.details}
														</span>
													) : (
														<span className="text-base-content/50">—</span>
													)}
												</td>
											</tr>
										),
									)}
								</tbody>
							</table>
						</div>
					</div>
				</div>
			)}

			{/* Alerts */}
			{systemHealth?.status !== "healthy" && (
				<div className="alert alert-warning">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">System Health Warning</div>
						<div className="text-sm">
							Some system components are not operating optimally. Check the
							component details above.
						</div>
					</div>
				</div>
			)}
		</div>
	);
}
