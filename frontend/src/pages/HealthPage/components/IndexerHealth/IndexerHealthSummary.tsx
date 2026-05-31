import { Activity, BarChart2, CheckCircle2, TrendingDown, TrendingUp, XCircle } from "lucide-react";
import type { IndexerStat, IndexerSummary } from "./types";

interface IndexerHealthSummaryProps {
	stats: IndexerStat[];
	summary: IndexerSummary;
}

export function IndexerHealthSummary({ stats, summary }: IndexerHealthSummaryProps) {
	return (
		<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
			{/* Total Indexers Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-teal-500/20">
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						Tracked Indexers
					</span>
					<div className="font-extrabold font-mono text-2xl text-teal-600 tracking-tight dark:text-teal-400">
						{stats.length}
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">
						Active Integrations
					</div>
				</div>
				<div className="relative z-10 text-teal-500 dark:text-teal-400">
					<Activity className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
				</div>
			</div>

			{/* Overall Health Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-primary/20">
				{summary.overallRate >= 85 && (
					<div className="absolute inset-0 animate-pulse bg-gradient-to-tr from-teal-500/5 via-transparent to-transparent opacity-60" />
				)}
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						System Health
					</span>
					<div
						className={`font-black font-mono text-3xl tracking-tight ${
							summary.overallRate >= 85
								? "text-teal-600 dark:text-teal-400"
								: summary.overallRate >= 60
									? "text-amber-600 dark:text-amber-500"
									: "text-rose-600 dark:text-rose-400"
						}`}
					>
						{summary.overallRate.toFixed(1)}%
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">
						Average success rate
					</div>
				</div>
				<div
					className={`relative z-10 ${
						summary.overallRate >= 85
							? "text-teal-600 shadow-[0_0_12px_rgba(13,148,136,0.3)] dark:text-teal-400"
							: summary.overallRate >= 60
								? "text-amber-600 shadow-[0_0_12px_rgba(245,158,11,0.3)] dark:text-amber-500"
								: "text-rose-600 shadow-[0_0_12px_rgba(225,29,72,0.3)] dark:text-rose-400"
					}`}
				>
					<BarChart2 className="h-8 w-8 opacity-50" />
				</div>
			</div>

			{/* Successful Imports Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-emerald-500/20">
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						Successful Imports
					</span>
					<div className="font-extrabold font-mono text-2xl text-emerald-600 tracking-tight dark:text-emerald-400">
						{summary.totalSuccess.toLocaleString()}
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">
						Imports completed
					</div>
				</div>
				<div className="relative z-10 text-emerald-600 dark:text-emerald-400">
					<CheckCircle2 className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
				</div>
			</div>

			{/* Failed Imports Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-rose-500/20">
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						Failed Imports
					</span>
					<div className="font-extrabold font-mono text-2xl text-rose-600 tracking-tight dark:text-rose-400">
						{summary.totalFailed.toLocaleString()}
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">
						Verification failures
					</div>
				</div>
				<div className="relative z-10 text-rose-600 dark:text-rose-400">
					<XCircle className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
				</div>
			</div>

			{/* Best / Worst performer chips */}
			{stats.length > 1 && (
				<>
					<div className="col-span-1 flex items-center gap-3 rounded-xl border border-teal-500/10 bg-teal-500/5 px-3 py-2.5 transition-all duration-300 hover:bg-teal-500/10 md:col-span-2">
						<TrendingUp className="h-4 w-4 shrink-0 text-teal-400" aria-hidden="true" />
						<div className="min-w-0">
							<div className="truncate font-bold text-teal-400 text-xs">
								{summary.best.indexer}
							</div>
							<div className="mt-0.5 font-semibold text-[10px] text-base-content/50">
								Highest efficiency rating · {summary.best.success_rate.toFixed(1)}%
							</div>
						</div>
					</div>
					<div className="col-span-1 flex items-center gap-3 rounded-xl border border-rose-500/10 bg-rose-500/5 px-3 py-2.5 transition-all duration-300 hover:bg-rose-500/10 md:col-span-2">
						<TrendingDown className="h-4 w-4 shrink-0 text-rose-400" aria-hidden="true" />
						<div className="min-w-0">
							<div className="truncate font-bold text-rose-400 text-xs">
								{summary.worst.indexer}
							</div>
							<div className="mt-0.5 font-semibold text-[10px] text-base-content/50">
								Needs telemetry inspection · {summary.worst.success_rate.toFixed(1)}%
							</div>
						</div>
					</div>
				</>
			)}
		</div>
	);
}
