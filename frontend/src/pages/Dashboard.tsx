import {
	Activity,
	AlertTriangle,
	CheckCircle,
	Clock,
	Download,
	Server,
} from "lucide-react";
import { HealthChart, QueueChart } from "../components/charts/QueueChart";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner } from "../components/ui/LoadingSpinner";
import { HealthBadge, StatusBadge } from "../components/ui/StatusBadge";
import {
	useHealthStats,
	useQueueStats,
	useSystemHealth,
	useSystemStats,
} from "../hooks/useApi";
import { formatRelativeTime } from "../lib/utils";

export function Dashboard() {
	const { data: queueStats, error: queueError } = useQueueStats();
	const { data: healthStats, error: healthError } = useHealthStats();
	const { data: systemStats, error: systemError } = useSystemStats();
	const { data: systemHealth, error: healthCheckError } = useSystemHealth();

	const hasError = queueError || healthError || systemError || healthCheckError;

	if (hasError) {
		return (
			<div className="space-y-4">
				<h1 className="text-3xl font-bold">Dashboard</h1>
				<ErrorAlert
					error={hasError as Error}
					onRetry={() => window.location.reload()}
				/>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<div className="flex items-center justify-between">
				<h1 className="text-3xl font-bold">Dashboard</h1>
				{systemHealth && (
					<HealthBadge status={systemHealth.status} className="badge-lg" />
				)}
			</div>

			{/* System Stats Cards */}
			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
				{/* Queue Status */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<div className="flex items-center justify-between">
							<div>
								<h2 className="card-title text-sm font-medium text-base-content/70">
									Queue Status
								</h2>
								{queueStats ? (
									<div className="text-2xl font-bold">
										{queueStats.processing} / {queueStats.total}
									</div>
								) : (
									<LoadingSpinner size="sm" />
								)}
							</div>
							<Download className="h-8 w-8 text-primary" />
						</div>
						{queueStats && (
							<div className="mt-2">
								<div className="text-sm text-base-content/70">
									{queueStats.pending} pending, {queueStats.failed} failed
								</div>
								<progress
									className="progress progress-primary w-full mt-2"
									value={queueStats.completed}
									max={queueStats.total}
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
								<h2 className="card-title text-sm font-medium text-base-content/70">
									File Health
								</h2>
								{healthStats ? (
									<div className="text-2xl font-bold text-success">
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
								<div className="text-sm text-error">
									{healthStats.corrupted} corrupted files
								</div>
							</div>
						)}
					</div>
				</div>

				{/* System Uptime */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<div className="flex items-center justify-between">
							<div>
								<h2 className="card-title text-sm font-medium text-base-content/70">
									Uptime
								</h2>
								{systemStats ? (
									<div className="text-2xl font-bold">{systemStats.uptime}</div>
								) : (
									<LoadingSpinner size="sm" />
								)}
							</div>
							<Clock className="h-8 w-8 text-info" />
						</div>
						{systemStats && (
							<div className="mt-2">
								<div className="text-sm text-base-content/70">
									Since {formatRelativeTime(systemStats.start_time)}
								</div>
							</div>
						)}
					</div>
				</div>

				{/* System Health */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<div className="flex items-center justify-between">
							<div>
								<h2 className="card-title text-sm font-medium text-base-content/70">
									System Health
								</h2>
								{systemHealth ? (
									<div className="text-2xl font-bold">
										{Object.keys(systemHealth.components).length}
									</div>
								) : (
									<LoadingSpinner size="sm" />
								)}
							</div>
							<Activity className="h-8 w-8 text-secondary" />
						</div>
						{systemHealth && (
							<div className="mt-2">
								<div className="text-sm text-base-content/70">
									Components checked
								</div>
							</div>
						)}
					</div>
				</div>
			</div>

			{/* Detailed Status */}
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
				{/* Queue Details */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Download className="h-5 w-5" />
							Queue Status
						</h2>
						{queueStats ? (
							<div className="space-y-3">
								<div className="flex justify-between items-center">
									<span>Pending</span>
									<StatusBadge status={`${queueStats.pending} items`} />
								</div>
								<div className="flex justify-between items-center">
									<span>Processing</span>
									<StatusBadge status={`${queueStats.processing} items`} />
								</div>
								<div className="flex justify-between items-center">
									<span>Completed</span>
									<StatusBadge status={`${queueStats.completed} items`} />
								</div>
								<div className="flex justify-between items-center">
									<span>Failed</span>
									<StatusBadge status={`${queueStats.failed} items`} />
								</div>
								<div className="flex justify-between items-center">
									<span>Retrying</span>
									<StatusBadge status={`${queueStats.retrying} items`} />
								</div>
							</div>
						) : (
							<LoadingSpinner />
						)}
					</div>
				</div>

				{/* System Components */}
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<h2 className="card-title">
							<Server className="h-5 w-5" />
							System Components
						</h2>
						{systemHealth ? (
							<div className="space-y-3">
								{Object.entries(systemHealth.components).map(
									([name, component]) => (
										<div
											key={name}
											className="flex justify-between items-center"
										>
											<span className="capitalize">
												{name.replace("_", " ")}
											</span>
											<HealthBadge status={component.status} />
										</div>
									),
								)}
								<div className="divider"></div>
								<div className="text-sm text-base-content/70">
									Last checked: {formatRelativeTime(systemHealth.timestamp)}
								</div>
							</div>
						) : (
							<LoadingSpinner />
						)}
					</div>
				</div>
			</div>

			{/* Charts */}
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
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
			{(queueStats && queueStats.failed > 0) ||
			(healthStats && healthStats.corrupted > 0) ? (
				<div className="alert alert-warning">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Attention Required</div>
						<div className="text-sm">
							{queueStats &&
								queueStats.failed > 0 &&
								`${queueStats.failed} failed queue items. `}
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
