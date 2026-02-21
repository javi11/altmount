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

	const handleResetStats = async (duration?: string) => {
		const durationLabel = !duration || duration === "all" ? "all time" : `last ${duration}`;
		if (
			confirm(
				`Are you sure you want to reset all NNTP errors and import history stats for ${durationLabel}?`,
			)
		) {
			try {
				await resetStats.mutateAsync(duration);
				showToast({
					type: "success",
					title: "Statistics Reset",
					message: `System statistics and import history for ${durationLabel} have been reset.`,
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

	const handleCustomReset = async () => {
		const customDuration = prompt(
			"Enter duration to reset (e.g., 12h, 2d, 1w):\nUse 'h' for hours, 'd' for days.",
			"12h",
		);
		if (customDuration && customDuration.trim() !== "") {
			await handleResetStats(customDuration.trim().toLowerCase());
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
					<div tabIndex={0} role="button" className="btn btn-outline btn-sm gap-2">
						{resetStats.isPending ? (
							<span className="loading loading-spinner loading-xs" />
						) : (
							<RotateCcw className="h-4 w-4" />
						)}
						Reset Stats
						<ChevronDown className="h-3 w-3 opacity-50" />
					</div>
					<ul className="dropdown-content menu z-[50] mt-1 w-52 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
						<li>
							<button type="button" onClick={() => handleResetStats("1h")}>
								Last 1 Hour
							</button>
						</li>
						<li>
							<button type="button" onClick={() => handleResetStats("2h")}>
								Last 2 Hours
							</button>
						</li>
						<li>
							<button type="button" onClick={() => handleResetStats("24h")}>
								Last 24 Hours
							</button>
						</li>
						<li>
							<button
								type="button"
								onClick={handleCustomReset}
								className="font-medium text-info italic"
							>
								Custom Range...
							</button>
						</li>
						<div className="divider my-1" />
						<li>
							<button
								type="button"
								onClick={() => handleResetStats("all")}
								className="font-bold text-error italic"
							>
								All Time (Full Reset)
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
