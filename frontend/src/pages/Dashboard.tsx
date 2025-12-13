import { AlertTriangle, CheckCircle, Download, Network } from "lucide-react";
import { useMemo } from "react";
import { HealthChart } from "../components/charts/HealthChart";
import { QueueChart } from "../components/charts/QueueChart";
import { ActiveStreamsCard } from "../components/system/ActiveStreamsCard";
import { PoolMetricsCard } from "../components/system/PoolMetricsCard";
import { ProviderCard } from "../components/system/ProviderCard";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner } from "../components/ui/LoadingSpinner";
import { StatusBadge } from "../components/ui/StatusBadge";
import { useHealthStats, usePoolMetrics, useQueueStats } from "../hooks/useApi";

export function Dashboard() {
	const { data: queueStats, error: queueError } = useQueueStats();
	const { data: healthStats, error: healthError } = useHealthStats();
	const { data: poolMetrics } = usePoolMetrics();

	const hasError = queueError || healthError;

	// Memoized queue metrics computation
	const queueMetrics = useMemo(() => {
		if (!queueStats) return null;

		const totalItems =
			queueStats.total_processing + queueStats.total_completed + queueStats.total_failed;
		const pendingItems = queueStats.total_queued;
		const completedAndFailed = queueStats.total_completed + queueStats.total_failed;

		// Build progress text
		const progressParts: string[] = [];
		if (pendingItems > 0) progressParts.push(`${pendingItems} pending`);
		if (queueStats.total_processing > 0)
			progressParts.push(`${queueStats.total_processing} processing`);
		if (queueStats.total_failed > 0) progressParts.push(`${queueStats.total_failed} failed`);

		return {
			totalItems,
			pendingItems,
			completedAndFailed,
			progressText: progressParts.join(", "),
			progressDisplay: `${completedAndFailed} / ${totalItems}`,
			hasFailures: queueStats.total_failed > 0,
			failedCount: queueStats.total_failed,
			processingCount: queueStats.total_processing,
			completedCount: queueStats.total_completed,
		};
	}, [queueStats]);

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
								{queueMetrics ? (
									<div className="font-bold text-2xl">{queueMetrics.progressDisplay}</div>
								) : (
									<LoadingSpinner size="sm" />
								)}
							</div>
							<Download className="h-8 w-8 text-primary" />
						</div>
						{queueMetrics && (
							<div className="mt-2">
								<div className="text-base-content/70 text-sm">{queueMetrics.progressText}</div>
								<progress
									className="progress progress-primary mt-2 w-full"
									value={queueMetrics.completedAndFailed}
									max={queueMetrics.totalItems}
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
										{healthStats.corrupted} / {healthStats.total}
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

				{/* Active Streams */}
				<ActiveStreamsCard />
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
						{queueMetrics ? (
							<div className="space-y-3">
								<div className="flex items-center justify-between">
									<span>Queued</span>
									<StatusBadge status={`${queueMetrics.pendingItems} items`} />
								</div>
								<div className="flex items-center justify-between">
									<span>Processing</span>
									<StatusBadge status={`${queueMetrics.processingCount} items`} />
								</div>
								<div className="flex items-center justify-between">
									<span>Completed</span>
									<StatusBadge status={`${queueMetrics.completedCount} items`} />
								</div>
								<div className="flex items-center justify-between">
									<span>Failed</span>
									<StatusBadge status={`${queueMetrics.failedCount} items`} />
								</div>
							</div>
						) : (
							<LoadingSpinner />
						)}
					</div>
				</div>
			</div>

			{/* Provider Status */}
			{poolMetrics?.providers && poolMetrics.providers.length > 0 && (
				<div>
					<h2 className="mb-4 flex items-center gap-2 font-semibold text-xl">
						<Network className="h-6 w-6" />
						NNTP Providers
					</h2>
					<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
						{poolMetrics.providers.map((provider) => (
							<ProviderCard key={provider.id} provider={provider} />
						))}
					</div>
				</div>
			)}

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
			{queueMetrics?.hasFailures || (healthStats && healthStats.corrupted > 0) ? (
				<div className="alert alert-warning">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Attention Required</div>
						<div className="text-sm">
							{queueMetrics?.hasFailures && `${queueMetrics.failedCount} failed queue items. `}
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
