import { Activity, BarChart2, CheckCircle2, XCircle } from "lucide-react";
import type { IndexerStat, IndexerSummary } from "./types";

interface IndexerHealthSummaryProps {
	stats: IndexerStat[];
	summary: IndexerSummary;
}

export function IndexerHealthSummary({ stats, summary }: IndexerHealthSummaryProps) {
	const overallColor =
		summary.overallRate >= 85
			? "text-success"
			: summary.overallRate >= 60
				? "text-warning"
				: "text-error";

	return (
		<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
			{/* Total Indexers Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-primary/20">
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						Tracked Indexers
					</span>
					<div className="font-extrabold font-mono text-2xl text-primary tracking-tight">
						{stats.length}
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">Active Integrations</div>
				</div>
				<div className="relative z-10 text-primary">
					<Activity className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
				</div>
			</div>

			{/* Overall Health Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-success/20">
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						System Health
					</span>
					<div className={`font-black font-mono text-3xl tracking-tight ${overallColor}`}>
						{summary.overallRate.toFixed(1)}%
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">Average success rate</div>
				</div>
				<div className={`relative z-10 ${overallColor}`}>
					<BarChart2 className="h-8 w-8 opacity-50" />
				</div>
			</div>

			{/* Successful Imports Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-success/20">
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						Successful Imports
					</span>
					<div className="font-extrabold font-mono text-2xl text-success tracking-tight">
						{summary.totalSuccess.toLocaleString()}
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">Imports completed</div>
				</div>
				<div className="relative z-10 text-success">
					<CheckCircle2 className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
				</div>
			</div>

			{/* Failed Imports Card */}
			<div className="card group relative flex flex-row items-center justify-between overflow-hidden border border-base-200 bg-base-100 p-5 shadow-xl backdrop-blur-md transition-all hover:border-error/20">
				<div className="relative z-10 space-y-1">
					<span className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
						Failed Imports
					</span>
					<div className="font-extrabold font-mono text-2xl text-error tracking-tight">
						{summary.totalFailed.toLocaleString()}
					</div>
					<div className="font-semibold text-[10px] text-base-content/50">
						Verification failures
					</div>
				</div>
				<div className="relative z-10 text-error">
					<XCircle className="h-8 w-8 opacity-45 transition-opacity group-hover:opacity-85" />
				</div>
			</div>
		</div>
	);
}
