interface HealthFiltersProps {
	searchTerm: string;
	statusFilter: string;
	onSearchChange: (value: string) => void;
	onStatusFilterChange: (value: string) => void;
	onUserInteractionStart: () => void;
	onUserInteractionEnd: () => void;
}

export function HealthFilters({
	searchTerm,
	statusFilter,
	onSearchChange,
	onStatusFilterChange,
	onUserInteractionStart,
	onUserInteractionEnd,
}: HealthFiltersProps) {
	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<div className="flex flex-col gap-4 sm:flex-row">
					{/* Search */}
					<fieldset className="fieldset flex-1">
						<legend className="fieldset-legend">Search Files</legend>
						<input
							type="text"
							placeholder="Search files..."
							className="input"
							value={searchTerm}
							onChange={(e) => onSearchChange(e.target.value)}
							onFocus={onUserInteractionStart}
							onBlur={onUserInteractionEnd}
						/>
					</fieldset>

					{/* Status Filter */}
					<fieldset className="fieldset sm:w-48">
						<legend className="fieldset-legend">Status</legend>
						<select
							className="select"
							value={statusFilter}
							onChange={(e) => onStatusFilterChange(e.target.value)}
							onFocus={onUserInteractionStart}
							onBlur={onUserInteractionEnd}
						>
							<option value="">All Statuses</option>
							<option value="pending">Pending</option>
							<option value="checking">Checking</option>
							<option value="healthy">Healthy</option>
							<option value="corrupted">Corrupted</option>
							<option value="repair_triggered">Repair Triggered</option>
						</select>
					</fieldset>
				</div>
			</div>
		</div>
	);
}
