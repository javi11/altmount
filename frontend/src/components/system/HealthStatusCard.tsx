import { Activity, Search, Shield, ShieldAlert, ShieldCheck, Wrench } from "lucide-react";
import { useMemo } from "react";
import { useHealthStats, useHealthWorkerStatus } from "../../hooks/useApi";

interface HealthStatusCardProps {
	className?: string;
}

interface HealthMetrics {
	isEmpty: boolean;
	total: number;
	healthy: number;
	corrupted: number;
	repairing: number;
	checking: number;
	pending: number;
	healthyPercent: number;
	corruptedPercent: number;
	checkingPercent: number;
	pendingPercent: number;
	isWorking: boolean;
	workerStatus: string;
}

export function HealthStatusCard({ className }: HealthStatusCardProps) {
	const { data: stats } = useHealthStats();
	const { data: worker } = useHealthWorkerStatus();

	const healthMetrics = useMemo<HealthMetrics | { isEmpty: true } | null>(() => {
		if (!stats) return null;

		const total = stats.total || 0;
		if (total === 0) return { isEmpty: true };

		const healthyPercent = Math.round((stats.healthy / total) * 100);
		const corruptedPercent = Math.round((stats.corrupted / total) * 100);
		const checkingPercent = Math.round(((stats.checking + stats.repair_triggered) / total) * 100);
		const pendingPercent = 100 - healthyPercent - corruptedPercent - checkingPercent;

		const isWorking = worker?.status === "running" || stats.checking > 0;

		return {
			isEmpty: false,
			total,
			healthy: stats.healthy,
			corrupted: stats.corrupted,
			repairing: stats.repair_triggered,
			checking: stats.checking,
			pending: stats.pending,
			healthyPercent,
			corruptedPercent,
			checkingPercent,
			pendingPercent,
			isWorking,
			workerStatus: worker?.status || "idle",
		};
	}, [stats, worker]);

	if (!healthMetrics) {
		return (
			<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
				<div className="card-body">
					<div className="flex items-center justify-between">
						<h2 className="card-title font-medium text-base-content/70 text-sm">File Health</h2>
						<Shield className="h-8 w-8 text-base-content/20" />
					</div>
					<div className="loading loading-spinner loading-md mt-2" />
				</div>
			</div>
		);
	}

	if (healthMetrics.isEmpty) {
		return (
			<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
				<div className="card-body">
					<div className="flex items-center justify-between">
						<div>
							<h2 className="card-title font-medium text-base-content/70 text-sm">File Health</h2>
							<div className="mt-1 text-base-content/30 text-sm italic">No files monitored</div>
						</div>
						<Shield className="h-8 w-8 text-base-content/20" />
					</div>
				</div>
			</div>
		);
	}

	const metrics = healthMetrics as HealthMetrics;

	return (
		<div className={`card bg-base-100 shadow-lg ${className || ""}`}>
			<div className="card-body">
				<div className="flex items-start justify-between">
					<div className="min-w-0 flex-1">
						<h2 className="card-title font-medium text-base-content/70 text-sm">Library Health</h2>
						<div className="flex items-baseline gap-2">
							<div
								className={`font-bold text-2xl ${metrics.corrupted > 0 ? "text-error" : "text-success"}`}
							>
								{metrics.corrupted > 0
									? metrics.corrupted
									: metrics.pending > 0 || metrics.checking > 0
										? `${metrics.healthyPercent}%`
										: "100%"}
							</div>
							<div className="font-semibold text-base-content/40 text-sm">
								{metrics.corrupted > 0
									? "Corrupted"
									: metrics.pending > 0
										? `(${metrics.pending} unverified)`
										: "Healthy"}
							</div>
						</div>
					</div>
					<div className="shrink-0">
						{metrics.corrupted > 0 ? (
							<ShieldAlert className="h-8 w-8 text-error" />
						) : metrics.isWorking ? (
							<Activity className="h-8 w-8 animate-pulse text-info" />
						) : (
							<ShieldCheck className="h-8 w-8 text-success" />
						)}
					</div>
				</div>

				{/* Segmented Progress Bar */}
				<div className="mt-4">
					<div className="flex h-1.5 w-full overflow-hidden rounded-full bg-base-300">
						<div
							className="h-full bg-success transition-all duration-500"
							style={{ width: `${metrics.healthyPercent}%` }}
						/>
						<div
							className="h-full bg-error transition-all duration-500"
							style={{ width: `${metrics.corruptedPercent}%` }}
						/>
						<div
							className="h-full bg-info transition-all duration-500"
							style={{ width: `${metrics.checkingPercent}%` }}
						/>
					</div>

					{/* Legend / Stats */}
					<div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 font-bold text-base-content/80 text-xs uppercase tracking-wider">
						<div className="flex items-center gap-1">
							<div className="h-1.5 w-1.5 rounded-full bg-success" />
							<span>{metrics.healthy} Healthy</span>
						</div>
						{metrics.corrupted > 0 && (
							<div className="flex items-center gap-1">
								<div className="h-1.5 w-1.5 rounded-full bg-error" />
								<span className="text-error">{metrics.corrupted} Corrupted</span>
							</div>
						)}
						{metrics.isWorking && (
							<div className="flex items-center gap-1 text-info">
								<Search className="h-1.5 w-1.5" />
								<span className="animate-pulse">Scanning {metrics.checking}</span>
							</div>
						)}
						{metrics.repairing > 0 && (
							<div className="flex items-center gap-1 text-warning">
								<Wrench className="h-1.5 w-1.5" />
								<span>{metrics.repairing} Repairing</span>
							</div>
						)}
					</div>
				</div>
			</div>
		</div>
	);
}
