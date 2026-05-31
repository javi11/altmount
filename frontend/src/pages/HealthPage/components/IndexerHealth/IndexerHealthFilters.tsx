import { ArrowUpDown, Search } from "lucide-react";
import type { SortKey } from "./types";

type StatusFilter = "all" | "excellent" | "good" | "moderate" | "operational";

interface IndexerHealthFiltersProps {
	searchQuery: string;
	onSearchChange: (value: string) => void;
	statusFilter: StatusFilter;
	onStatusFilterChange: (filter: StatusFilter) => void;
	sortKey: SortKey;
	sortAsc: boolean;
	onSort: (key: SortKey) => void;
	filteredCount: number;
}

export function IndexerHealthFilters({
	searchQuery,
	onSearchChange,
	statusFilter,
	onStatusFilterChange,
	sortKey,
	sortAsc: _sortAsc,
	onSort,
	filteredCount,
}: IndexerHealthFiltersProps) {
	return (
		<div className="space-y-3">
			{/* Search & Status Filter */}
			<div className="flex flex-col gap-3 rounded-2xl border border-base-200 bg-base-100 p-3 backdrop-blur-md md:flex-row md:items-center md:justify-between">
				<div className="relative max-w-sm flex-1">
					<input
						type="text"
						placeholder="Search indexers..."
						value={searchQuery}
						onChange={(e) => onSearchChange(e.target.value)}
						className="input input-bordered input-sm w-full border-base-300 bg-base-200/50 pl-8 font-medium text-base-content placeholder-base-content/40 focus:border-teal-500/50"
						aria-label="Search indexers"
					/>
					<div className="absolute top-1/2 left-2.5 -translate-y-1/2 text-base-content/40">
						<Search className="h-4 w-4" aria-hidden="true" />
					</div>
				</div>

				<div
					className="flex flex-wrap items-center gap-1.5"
					role="group"
					aria-label="Status Filters"
				>
					<span className="mr-1 font-bold text-[10px] text-base-content/40 uppercase tracking-wider">
						Filter
					</span>
					{(["all", "excellent", "good", "moderate", "operational"] as const).map((filter) => {
						const active = statusFilter === filter;
						let btnClass =
							"btn-ghost text-base-content/60 hover:text-base-content hover:bg-base-content/5 border-transparent";
						if (active) {
							if (filter === "excellent")
								btnClass =
									"bg-teal-500/15 border-teal-500/30 text-teal-400 shadow-[0_0_8px_rgba(20,184,166,0.25)]";
							else if (filter === "good")
								btnClass =
									"bg-emerald-500/15 border-emerald-500/30 text-emerald-400 shadow-[0_0_8px_rgba(16,185,129,0.25)]";
							else if (filter === "moderate")
								btnClass =
									"bg-amber-500/15 border-amber-500/30 text-amber-500 shadow-[0_0_8px_rgba(245,158,11,0.25)]";
							else if (filter === "operational")
								btnClass =
									"bg-slate-500/15 border-slate-500/30 text-slate-400 shadow-[0_0_8px_rgba(148,163,184,0.25)]";
							else
								btnClass =
									"bg-primary/15 border-primary/30 text-primary shadow-[0_0_8px_rgba(59,130,246,0.25)]";
						}
						return (
							<button
								key={filter}
								type="button"
								onClick={() => onStatusFilterChange(filter)}
								className={`btn btn-xs rounded-lg border font-bold capitalize tracking-wide transition-all duration-200 ${btnClass}`}
							>
								{filter}
							</button>
						);
					})}
				</div>
			</div>

			{/* Sort Toolbar */}
			<div className="flex items-center gap-3">
				<span className="font-bold text-[10px] text-base-content/50 uppercase tracking-wider">
					Sort by
				</span>
				<div
					className="join rounded-xl border border-base-200 bg-base-200/30 p-0.5"
					role="group"
					aria-label="Sort options"
				>
					{(["health", "total", "name"] as SortKey[]).map((key) => (
						<button
							key={key}
							type="button"
							onClick={() => onSort(key)}
							className={`btn btn-xs join-item border-none font-bold capitalize tracking-wide transition-all duration-200 ${
								sortKey === key
									? "btn-primary shadow-[0_0_8px_rgba(59,130,246,0.25)]"
									: "btn-ghost text-base-content/60 hover:bg-base-content/5 hover:text-base-content"
							}`}
							aria-label={`Sort by ${key === "health" ? "Health" : key === "total" ? "Volume" : "Name"}`}
						>
							{key === "health" ? "Health %" : key === "total" ? "Volume" : "Name"}
							{sortKey === key && (
								<ArrowUpDown className="ml-1 h-3 w-3 transition-transform" aria-hidden="true" />
							)}
						</button>
					))}
				</div>
				<span className="ml-auto font-semibold text-base-content/40 text-xs">
					{filteredCount} Indexer{filteredCount !== 1 ? "s" : ""} active
				</span>
			</div>
		</div>
	);
}
