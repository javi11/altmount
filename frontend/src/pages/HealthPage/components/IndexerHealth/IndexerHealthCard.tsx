import { CheckCircle2, Clock, Radio, Trash2, XCircle } from "lucide-react";
import { formatRelativeTime } from "../../../../lib/utils";
import type { IndexerStat } from "./types";

function generatePulseMatrix(success: number, _failed: number, total: number) {
	const dots: ("success" | "failed" | "neutral")[] = [];
	const totalAvailable = Math.min(24, total);

	const successRatio = total > 0 ? success / total : 0;
	const successDotsCount = Math.round(totalAvailable * successRatio);
	const failedDotsCount = totalAvailable - successDotsCount;

	for (let i = 0; i < totalAvailable; i++) {
		if (failedDotsCount > 0 && (i * failedDotsCount) % totalAvailable < failedDotsCount) {
			dots.push("failed");
		} else {
			dots.push("success");
		}
	}

	while (dots.length < 24) {
		dots.push("neutral");
	}
	return dots.reverse();
}

interface IndexerHealthCardProps {
	item: IndexerStat;
	onDelete: (indexer: string) => void;
}

export function IndexerHealthCard({ item, onDelete }: IndexerHealthCardProps) {
	const isExcellent = item.success_rate >= 85;
	const isModerate = item.success_rate >= 50 && item.success_rate < 85;

	// Map the three performance tiers onto AltMount's daisyUI theme tokens so the
	// colors follow the active theme: success (excellent), warning (moderate),
	// error (poor).
	const accentColor = isExcellent
		? "border-success/15 hover:border-success/40"
		: isModerate
			? "border-warning/15 hover:border-warning/40"
			: "border-error/15 hover:border-error/40";

	const barSuccessWidth =
		item.total_imports > 0 ? (item.success_count / item.total_imports) * 100 : 0;
	const barFailWidth = item.total_imports > 0 ? (item.failed_count / item.total_imports) * 100 : 0;

	// Mute the failure bar on excellent indexers so a tiny failure sliver doesn't
	// read as an alarm; moderate/poor tiers keep their warning/error color.
	const barFailClass = isExcellent
		? "h-full bg-base-content/15 transition-all duration-700"
		: isModerate
			? "h-full bg-warning transition-all duration-700"
			: "h-full bg-error transition-all duration-700";

	const topLineGradient = isExcellent
		? "from-success/40 to-success/10"
		: isModerate
			? "from-warning/40 to-warning/10"
			: "from-error/40 to-error/10";

	const statusBadgeColor = isExcellent
		? "bg-success/10 text-success border-success/20"
		: isModerate
			? "bg-warning/10 text-warning border-warning/20"
			: "bg-error/10 text-error border-error/20";

	const statusText = isExcellent ? "EXCELLENT" : isModerate ? "MODERATE" : "POOR";

	const percentColor = isExcellent ? "text-success" : isModerate ? "text-warning" : "text-error";

	return (
		<div
			className={`group card relative overflow-hidden border ${accentColor} hover:-translate-y-1.5 bg-base-100/60 shadow-md backdrop-blur-md transition-all duration-500 ease-out hover:scale-[1.01]`}
		>
			<div
				className={`absolute top-0 right-0 left-0 h-[2px] bg-gradient-to-r ${topLineGradient}`}
			/>

			<div className="absolute top-3 right-3 z-10">
				<button
					type="button"
					className="btn btn-ghost btn-xs p-1 text-error opacity-0 transition-all duration-200 hover:bg-error/20 group-hover:opacity-100"
					onClick={() => onDelete(item.indexer)}
					aria-label={`Delete statistics for ${item.indexer}`}
				>
					<Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
				</button>
			</div>

			<div className="card-body p-5">
				<div className="flex items-center justify-between gap-3">
					<div className="min-w-0 flex-1 space-y-2 py-0.5">
						<h4 className="truncate pr-6 font-extrabold text-[17px] text-base-content leading-tight tracking-tight sm:text-lg">
							{item.indexer}
						</h4>
						<div className="flex flex-wrap items-center gap-2">
							<span
								className={`badge badge-xs border ${statusBadgeColor} py-2 font-black text-[9px] tracking-wider`}
							>
								{statusText}
							</span>
							<div className="flex items-center gap-1 font-semibold text-[10px] text-base-content/40">
								<Clock className="h-2.5 w-2.5 shrink-0" aria-hidden="true" />
								<span>Seen {formatRelativeTime(item.last_seen_at)}</span>
							</div>
						</div>
					</div>

					<div className="flex shrink-0 flex-col items-end pl-2">
						<span
							className={`flex items-baseline font-black font-mono text-[17px] leading-none tracking-tight sm:text-lg ${percentColor}`}
						>
							{item.success_rate.toFixed(1)}
							<span className="ml-0.5 font-semibold text-[9px] opacity-50 sm:text-[10px]">%</span>
						</span>
						<span className="mt-1.5 font-black text-[8px] text-base-content/40 uppercase tracking-widest">
							SUCCESS
						</span>
					</div>
				</div>

				{/* Progress bar */}
				<div className="mt-4 space-y-1">
					<div className="relative flex h-2 w-full overflow-hidden rounded-full border border-base-200 bg-base-200/40">
						<div
							className="h-full bg-success transition-all duration-700"
							style={{ width: `${barSuccessWidth}%` }}
							role="progressbar"
							aria-valuenow={barSuccessWidth}
							aria-valuemin={0}
							aria-valuemax={100}
							aria-label="Success rate percentage"
						/>
						<div
							className={barFailClass}
							style={{ width: `${barFailWidth}%` }}
							role="progressbar"
							aria-valuenow={barFailWidth}
							aria-valuemin={0}
							aria-valuemax={100}
							aria-label="Failure rate percentage"
						/>
					</div>
					<div className="flex justify-between font-bold text-[9px] uppercase tracking-wide">
						<span className="text-success">{item.success_count} OK</span>
						<span className="text-error">{item.failed_count} FAILED</span>
					</div>
				</div>

				{/* Import Pulse Stream */}
				<div className="mt-4 space-y-1.5">
					<div className="flex items-center gap-1.5 font-bold text-[9px] text-base-content/40 uppercase tracking-wider">
						<Radio className="h-3 w-3 animate-pulse text-primary" />
						Import Pulse Stream (Last 24)
					</div>
					<div className="flex flex-wrap items-center gap-1 rounded-xl border border-base-200 bg-base-200/50 p-2">
						{generatePulseMatrix(item.success_count, item.failed_count, item.total_imports).map(
							(dot, idx) => {
								const dotColor =
									dot === "success"
										? "bg-success/80 hover:bg-success"
										: dot === "failed"
											? "bg-error/80 hover:bg-error"
											: "bg-base-200/30 border border-base-200";
								const dotTip =
									dot === "success"
										? "Import OK"
										: dot === "failed"
											? "Import Failed"
											: "No Activity";
								return (
									<div
										key={idx}
										className={`h-2.5 w-2.5 cursor-pointer rounded-sm transition-all duration-300 hover:scale-125 ${dotColor} tooltip tooltip-top`}
										data-tip={dotTip}
									/>
								);
							},
						)}
					</div>
				</div>

				{/* Telemetry Grid */}
				<div className="mt-4 grid grid-cols-3 gap-1.5 rounded-xl border border-base-200 bg-base-200/50 p-3 text-center">
					<div className="space-y-0.5">
						<div className="font-extrabold text-base-content text-sm tabular-nums">
							{item.total_imports.toLocaleString()}
						</div>
						<div className="font-bold text-[8px] text-base-content/40 uppercase tracking-wider">
							Total
						</div>
					</div>
					<div className="space-y-0.5">
						<div className="flex items-center justify-center gap-0.5 font-extrabold text-sm text-success tabular-nums">
							<CheckCircle2 className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
							{item.success_count.toLocaleString()}
						</div>
						<div className="font-bold text-[8px] text-base-content/40 uppercase tracking-wider">
							Success
						</div>
					</div>
					<div className="space-y-0.5">
						<div className="flex items-center justify-center gap-0.5 font-extrabold text-error text-sm tabular-nums">
							<XCircle className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
							{item.failed_count.toLocaleString()}
						</div>
						<div className="font-bold text-[8px] text-base-content/40 uppercase tracking-wider">
							Failed
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
