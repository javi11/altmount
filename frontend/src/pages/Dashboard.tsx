import { ChevronDown, Network, RotateCcw } from "lucide-react";
import { useEffect, useRef } from "react";
import { QueueHistoricalStatsCard } from "../components/queue/QueueHistoricalStatsCard";
import { ActivityHub } from "../components/system/ActivityHub";
import { HealthStatusCard } from "../components/system/HealthStatusCard";
import { ImportStatusCard } from "../components/system/ImportStatusCard";
import { PoolMetricsCard } from "../components/system/PoolMetricsCard";
import { ProviderCard } from "../components/system/ProviderCard";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { useToast } from "../contexts/ToastContext";
import {
	useHealthStats,
	usePoolMetrics,
	useQueueStats,
	useResetSystemStats,
} from "../hooks/useApi";

export function Dashboard() {
	const { error: queueError } = useQueueStats();
	const { error: healthError } = useHealthStats();
	const { data: poolMetrics } = usePoolMetrics();
	const { showToast } = useToast();
	const resetStats = useResetSystemStats();
	const warnedProvidersRef = useRef<Set<string>>(new Set());

	const hasError = queueError || healthError;

	const handleResetStats = async (options: {
		duration?: string;
		reset_peak?: boolean;
		reset_totals?: boolean;
		reset_history?: boolean;
		reset_queue?: boolean;
		label: string;
	}) => {
		if (confirm(`Are you sure you want to reset ${options.label}?`)) {
			try {
				await resetStats.mutateAsync({
					duration: options.duration,
					reset_peak: options.reset_peak,
					reset_totals: options.reset_totals,
					reset_history: options.reset_history,
					reset_queue: options.reset_queue,
				});
				showToast({
					type: "success",
					title: "Statistics Reset",
					message: `${options.label} have been reset.`,
				});
			} catch (error) {
				showToast({
					type: "error",
					title: "Reset Failed",
					message: error instanceof Error ? error.message : "Failed to reset statistics",
				});
			}
		}
	};

	const handleCustomHistoryReset = async () => {
		const customDuration = prompt(
			"Enter duration to reset history (e.g., 12h, 2d, 1w):\nUse 'h' for hours, 'd' for days.",
			"12h",
		);
		if (customDuration && customDuration.trim() !== "") {
			await handleResetStats({
				duration: customDuration.trim().toLowerCase(),
				reset_history: true,
				label: `import history for last ${customDuration}`,
			});
		}
	};

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
				<div className="dropdown dropdown-end">
					<button type="button" className="btn btn-outline btn-sm gap-2">
						{resetStats.isPending ? (
							<span className="loading loading-spinner loading-xs" />
						) : (
							<RotateCcw className="h-4 w-4" />
						)}
						Reset Stats
						<ChevronDown className="h-3 w-3 text-base-content/70" />
					</button>
					<ul className="dropdown-content menu z-[50] mt-1 w-64 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
						<div className="px-3 py-1 font-bold text-base-content/50 text-xs uppercase">
							Download Metrics
						</div>
						<li>
							<button
								type="button"
								onClick={() => handleResetStats({ reset_peak: true, label: "peak download speed" })}
							>
								Reset Peak Speed
							</button>
						</li>
						<li>
							<button
								type="button"
								onClick={() =>
									handleResetStats({ reset_totals: true, label: "total download metrics" })
								}
							>
								Reset Download Totals
							</button>
						</li>
						<div className="divider my-1" />
						<div className="px-3 py-1 font-bold text-base-content/50 text-xs uppercase">
							Import History
						</div>
						<li>
							<button
								type="button"
								onClick={() =>
									handleResetStats({
										duration: "24h",
										reset_history: true,
										label: "import history for last 24h",
									})
								}
							>
								Last 24 Hours
							</button>
						</li>
						<li>
							<button
								type="button"
								onClick={handleCustomHistoryReset}
								className="font-medium text-info italic"
							>
								Custom Range...
							</button>
						</li>
						<li>
							<button
								type="button"
								onClick={() =>
									handleResetStats({ reset_history: true, label: "all import history" })
								}
							>
								Reset All History
							</button>
						</li>
						<div className="divider my-1" />
						<li>
							<button
								type="button"
								onClick={() =>
									handleResetStats({
										reset_peak: true,
										reset_totals: true,
										reset_history: true,
										label: "ALL system statistics",
									})
								}
								className="font-bold text-error italic"
							>
								Full Reset (Everything)
							</button>
						</li>
					</ul>
				</div>
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
				<div className="space-y-4">
					<h2 className="flex items-center gap-2 font-semibold text-xl">
						<Network className="h-6 w-6" />
						NNTP Providers
					</h2>
					<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
						{poolMetrics.providers.map((provider) => (
							<ProviderCard key={provider.id} provider={provider} />
						))}
					</div>
				</div>
			)}
		</div>
	);
}
