import {
	ArrowDown,
	ArrowUp,
	ArrowUpDown,
	CheckCircle2,
	Clock,
	Trash2,
	XCircle,
} from "lucide-react";
import { formatRelativeTime } from "../../../lib/utils";
import type { IndexerStat, SortKey } from "./types";

interface IndexerHealthTableProps {
	items: IndexerStat[];
	sortKey: SortKey;
	sortAsc: boolean;
	onSort: (key: SortKey) => void;
	onDelete: (indexer: string) => void;
}

const SortIcon = ({
	field,
	sortKey,
	sortAsc,
}: {
	field: SortKey;
	sortKey: SortKey;
	sortAsc: boolean;
}) => {
	if (sortKey !== field) return <ArrowUpDown className="h-3 w-3 opacity-30" />;
	return sortAsc ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />;
};

function tierClasses(rate: number) {
	if (rate >= 85) {
		return {
			bar: "bg-success",
			text: "text-success",
			badge: "border-success/20 bg-success/10 text-success",
			label: "EXCELLENT",
		};
	}
	if (rate >= 50) {
		return {
			bar: "bg-warning",
			text: "text-warning",
			badge: "border-warning/20 bg-warning/10 text-warning",
			label: "MODERATE",
		};
	}
	return {
		bar: "bg-error",
		text: "text-error",
		badge: "border-error/20 bg-error/10 text-error",
		label: "POOR",
	};
}

export function IndexerHealthTable({
	items,
	sortKey,
	sortAsc,
	onSort,
	onDelete,
}: IndexerHealthTableProps) {
	const columns: { field: SortKey; label: string }[] = [
		{ field: "name", label: "Indexer" },
		{ field: "last_24h", label: "Grabs 24h" },
		{ field: "last_seen", label: "Last Seen" },
		{ field: "health", label: "Success Rate" },
		{ field: "success", label: "Completed" },
		{ field: "failed", label: "Failed" },
		{ field: "total", label: "Total" },
	];

	return (
		<div className="overflow-x-auto">
			<table className="table-zebra table border-collapse">
				<thead>
					<tr>
						{columns.map((col) => (
							<th
								key={col.field}
								className="cursor-pointer transition-colors hover:bg-base-200"
								onClick={() => onSort(col.field)}
							>
								<div className="flex items-center gap-1">
									{col.label} <SortIcon field={col.field} sortKey={sortKey} sortAsc={sortAsc} />
								</div>
							</th>
						))}
						<th>Actions</th>
					</tr>
				</thead>
				<tbody>
					{items.map((item) => {
						const tier = tierClasses(item.success_rate);
						return (
							<tr
								key={item.indexer}
								className="border-base-200/30 border-b transition-colors hover:bg-base-content/5"
							>
								<td>
									<div className="flex flex-col gap-1">
										<span className="font-bold text-base-content text-sm tracking-wide">
											{item.indexer}
										</span>
										<span
											className={`badge badge-xs w-fit border py-1.5 font-black text-[8px] tracking-wider ${tier.badge}`}
										>
											{tier.label}
										</span>
									</div>
								</td>

								<td>
									<span
										className={`font-mono text-sm tabular-nums ${
											(item.last_24h_count ?? 0) > 0
												? "font-semibold text-base-content"
												: "text-base-content/30"
										}`}
									>
										{(item.last_24h_count ?? 0).toLocaleString()}
									</span>
								</td>

								<td>
									<div className="flex items-center gap-1.5 text-base-content/60 text-xs">
										<Clock className="h-3 w-3 shrink-0" aria-hidden="true" />
										<span className="font-medium">{formatRelativeTime(item.last_seen_at)}</span>
									</div>
								</td>

								<td>
									<div className="flex items-center gap-2">
										<div className="h-2.5 w-20 overflow-hidden rounded-full border border-base-content/10 bg-base-200/50">
											<div
												className={`h-full rounded-full transition-all duration-500 ${tier.bar}`}
												style={{ width: `${Math.min(100, item.success_rate)}%` }}
												role="progressbar"
												aria-valuenow={Math.round(item.success_rate)}
												aria-valuemin={0}
												aria-valuemax={100}
												aria-label={`Success rate for ${item.indexer}`}
											/>
										</div>
										<span className={`font-bold font-mono text-xs tabular-nums ${tier.text}`}>
											{item.success_rate.toFixed(1)}%
										</span>
									</div>
								</td>

								<td>
									<span className="inline-flex items-center gap-1 font-mono font-semibold text-sm text-success tabular-nums">
										<CheckCircle2 className="h-3.5 w-3.5 shrink-0 opacity-70" aria-hidden="true" />
										{item.success_count.toLocaleString()}
									</span>
								</td>

								<td>
									{item.failed_count > 0 ? (
										<span className="inline-flex items-center gap-1 font-mono font-semibold text-error text-sm tabular-nums">
											<XCircle className="h-3.5 w-3.5 shrink-0 opacity-70" aria-hidden="true" />
											{item.failed_count.toLocaleString()}
										</span>
									) : (
										<span className="font-mono text-base-content/30 text-sm">0</span>
									)}
								</td>

								<td>
									<span className="font-bold font-mono text-base-content text-sm tabular-nums">
										{item.total_imports.toLocaleString()}
									</span>
								</td>

								<td>
									<button
										type="button"
										className="btn btn-ghost btn-sm border border-base-200 text-error hover:bg-error/10"
										onClick={() => onDelete(item.indexer)}
										aria-label={`Delete statistics for ${item.indexer}`}
										title="Delete indexer stats"
									>
										<Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
									</button>
								</td>
							</tr>
						);
					})}
				</tbody>
			</table>
		</div>
	);
}
