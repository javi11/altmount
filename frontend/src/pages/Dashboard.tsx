import { AlertTriangle, Network } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";
import { PoolMetricsCard } from "../components/system/PoolMetricsCard";
import { ProviderCard } from "../components/system/ProviderCard";
import { QueueHistoricalStatsCard } from "../components/queue/QueueHistoricalStatsCard";
import { ActivityHub } from "../components/system/ActivityHub";
import { ImportStatusCard } from "../components/system/ImportStatusCard";
import { HealthStatusCard } from "../components/system/HealthStatusCard";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { useToast } from "../contexts/ToastContext";
import { useHealthStats, usePoolMetrics, useQueueStats } from "../hooks/useApi";

export function Dashboard() {
	const { data: queueStats, error: queueError } = useQueueStats();
	const { data: healthStats, error: healthError } = useHealthStats();
	const { data: poolMetrics } = usePoolMetrics();
	const { showToast } = useToast();
	const warnedProvidersRef = useRef<Set<string>>(new Set());

	const hasError = queueError || healthError;

	// Memoized queue metrics computation
	const queueMetrics = useMemo(() => {
		if (!queueStats) return null;

		return {
			hasFailures: queueStats.total_failed > 0,
			failedCount: queueStats.total_failed,
		};
	}, [queueStats]);

	// Fire warning toast when server reports missing_warning for a provider
	useEffect(() => {
		if (!poolMetrics?.providers) return;
		const warned = warnedProvidersRef.current;

		for (const provider of poolMetrics.providers) {
			if (provider.missing_warning && !warned.has(provider.id)) {
				warned.add(provider.id);
				showToast({
					type: "warning",
					title: "High Missing Article Rate",
					message: `${provider.host} has ~${Math.round(provider.missing_rate_per_minute)}/min missing articles. Consider using a backup provider.`,
					duration: 10000,
				});
			} else if (!provider.missing_warning && warned.has(provider.id)) {
				warned.delete(provider.id);
			}
		}
	}, [poolMetrics?.providers, showToast]);

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
			<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
				{/* Import Status (Active Work) */}
				<ImportStatusCard />

				{/* Health Status (Library Integrity) */}
				<HealthStatusCard />

				{/* Pool Metrics */}
				<PoolMetricsCard />
			</div>

			{/* Detailed Status */}
			<div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
				{/* Activity Hub (Tabs for Playback & Imports) */}
				<ActivityHub />

				<QueueHistoricalStatsCard />
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
