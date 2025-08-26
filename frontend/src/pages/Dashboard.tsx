import { AlertTriangle, CheckCircle, Download } from "lucide-react";
import { HealthChart, QueueChart } from "../components/charts/QueueChart";
import { PoolMetricsCard } from "../components/system/PoolMetricsCard";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner } from "../components/ui/LoadingSpinner";
import { StatusBadge } from "../components/ui/StatusBadge";
import { useHealthStats, useQueueStats } from "../hooks/useApi";

export function Dashboard() {
	const { data: queueStats, error: queueError } = useQueueStats();
	const { data: healthStats, error: healthError } = useHealthStats();

	const hasError = queueError || healthError;

	if (hasError) {
		return (
			<div className="space-y-4">
				<h1 className="font-bold text-3xl">Dashboard</h1>
				<ErrorAlert error={hasError as Error} onRetry={() => window.location.reload()} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<div className="flex items-center justify-between">
				<h1 className="font-bold text-3xl">Dashboard</h1>
			</div>

			{/* System Stats Cards */}
			<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-4">
				{/* Queue Status */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<div className="flex items-center justify-between">
							<div>
								<h2 className="card-title font-medium text-base-content/70 text-sm">
									Queue Status
								</h2>
								{queueStats ? (
									<div className="font-bold text-2xl">
										{queueStats.total_processing} / {queueStats.total_queued}
									</div>
								) : (
									<LoadingSpinner size="sm" />
								)}
							</div>
							<Download className="h-8 w-8 text-primary" />
						</div>
						{queueStats && (
							<div className="mt-2">
								<div className="text-base-content/70 text-sm">
									{queueStats.total_completed} completed, {queueStats.total_failed} failed
								</div>
								<progress
									className="progress progress-primary mt-2 w-full"
									value={queueStats.total_completed}
									max={queueStats.total_failed}
								/>
							</div>
						)}
					</div>
				</div>

				{/* Health Status */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<div className="flex items-center justify-between">
							<div>
								<h2 className="card-title font-medium text-base-content/70 text-sm">File Health</h2>
								{healthStats ? (
									<div className="font-bold text-2xl text-success">
										{healthStats.healthy} / {healthStats.total}
									</div>
								) : (
									<LoadingSpinner size="sm" />
								)}
							</div>
							<CheckCircle className="h-8 w-8 text-success" />
						</div>
						{healthStats && healthStats.corrupted > 0 && (
							<div className="mt-2">
								<div className="text-error text-sm">{healthStats.corrupted} corrupted files</div>
							</div>
						)}
					</div>
				</div>

				{/* Pool Metrics */}
				<PoolMetricsCard />
			</div>

			{/* Detailed Status */}
			<div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
				{/* Queue Details */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Download className="h-5 w-5" />
							Queue Status
						</h2>
						{queueStats ? (
							<div className="space-y-3">
								<div className="flex items-center justify-between">
									<span>Pending</span>
									<StatusBadge status={`${queueStats.total_queued} items`} />
								</div>
								<div className="flex items-center justify-between">
									<span>Processing</span>
									<StatusBadge status={`${queueStats.total_processing} items`} />
								</div>
								<div className="flex items-center justify-between">
									<span>Completed</span>
									<StatusBadge status={`${queueStats.total_completed} items`} />
								</div>
								<div className="flex items-center justify-between">
									<span>Failed</span>
									<StatusBadge status={`${queueStats.total_failed} items`} />
								</div>
							</div>
						) : (
							<LoadingSpinner />
						)}
					</div>
				</div>
			</div>

			{/* Charts */}
			<div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Download className="h-5 w-5" />
							Queue Distribution
						</h2>
						<QueueChart />
					</div>
				</div>

				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<CheckCircle className="h-5 w-5" />
							File Health Status
						</h2>
						<HealthChart />
					</div>
				</div>
			</div>

			{/* Issues Alert */}
			{(queueStats && queueStats.total_failed > 0) || (healthStats && healthStats.corrupted > 0) ? (
				<div className="alert alert-warning">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Attention Required</div>
						<div className="text-sm">
							{queueStats &&
								queueStats.total_failed > 0 &&
								`${queueStats.total_failed} failed queue items. `}
							{healthStats &&
								healthStats.corrupted > 0 &&
								`${healthStats.corrupted} corrupted files detected.`}
						</div>
					</div>
				</div>
			) : null}
		</div>
	);
}
