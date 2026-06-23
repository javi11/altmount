import { Search } from "lucide-react";

type StatusFilter = "all" | "excellent" | "moderate" | "poor";

interface IndexerHealthFiltersProps {
	searchQuery: string;
	onSearchChange: (value: string) => void;
	statusFilter: StatusFilter;
	onStatusFilterChange: (filter: StatusFilter) => void;
	filteredCount: number;
}

export function IndexerHealthFilters({
	searchQuery,
	onSearchChange,
	statusFilter,
	onStatusFilterChange,
	filteredCount,
}: IndexerHealthFiltersProps) {
	return (
		<div className="flex flex-col gap-3 rounded-2xl border border-base-200 bg-base-100 p-3 backdrop-blur-md md:flex-row md:items-center md:justify-between">
			<div className="relative max-w-sm flex-1">
				<input
					type="text"
					placeholder="Search indexers..."
					value={searchQuery}
					onChange={(e) => onSearchChange(e.target.value)}
					className="input input-bordered input-sm w-full border-base-300 bg-base-200/50 pl-8 font-medium text-base-content placeholder-base-content/40 focus:border-primary/50"
					aria-label="Search indexers"
				/>
				<div className="-translate-y-1/2 absolute top-1/2 left-2.5 text-base-content/40">
					<Search className="h-4 w-4" aria-hidden="true" />
				</div>
			</div>

			<div className="flex flex-wrap items-center gap-3">
				<fieldset className="flex flex-wrap items-center gap-1.5" aria-label="Status Filters">
					<span className="mr-1 font-bold text-[10px] text-base-content/40 uppercase tracking-wider">
						Filter
					</span>
					{(["all", "excellent", "moderate", "poor"] as const).map((filter) => {
						const active = statusFilter === filter;
						let btnClass =
							"btn-ghost text-base-content/60 hover:text-base-content hover:bg-base-content/5 border-transparent";
						if (active) {
							if (filter === "excellent") btnClass = "bg-success/15 border-success/30 text-success";
							else if (filter === "moderate")
								btnClass = "bg-warning/15 border-warning/30 text-warning";
							else if (filter === "poor") btnClass = "bg-error/15 border-error/30 text-error";
							else btnClass = "bg-primary/15 border-primary/30 text-primary";
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
				</fieldset>
				<span className="font-semibold text-base-content/40 text-xs">
					{filteredCount} Indexer{filteredCount !== 1 ? "s" : ""} active
				</span>
			</div>
		</div>
	);
}
