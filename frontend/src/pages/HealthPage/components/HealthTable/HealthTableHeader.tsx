import { ChevronDown, ChevronUp } from "lucide-react";
import type { SortBy, SortOrder } from "../../types";

interface HealthTableHeaderProps {
	isAllSelected: boolean;
	isIndeterminate: boolean;
	sortBy: SortBy;
	sortOrder: SortOrder;
	onSelectAll: (checked: boolean) => void;
	onSelectAllPages: () => void;
	onSort: (column: SortBy) => void;
	allowSelectAllPages: boolean;
}

export function HealthTableHeader({
	isAllSelected,
	isIndeterminate,
	sortBy,
	sortOrder,
	onSelectAll,
	onSelectAllPages,
	onSort,
	allowSelectAllPages,
}: HealthTableHeaderProps) {
	return (
		<thead>
			<tr>
				<th className="w-16">
					<div className="dropdown">
						<label className="flex cursor-pointer items-center gap-1">
							<input
								type="checkbox"
								className="checkbox checkbox-sm"
								checked={isAllSelected}
								ref={(input) => {
									if (input) input.indeterminate = Boolean(isIndeterminate);
								}}
								onChange={(e) => onSelectAll(e.target.checked)}
							/>
							<ChevronDown className="h-3 w-3" />
						</label>
						<ul className="dropdown-content menu z-[1] w-52 rounded-box bg-base-100 p-2 shadow">
							<li>
								<button type="button" onClick={() => onSelectAll(true)}>
									Select all on page
								</button>
							</li>
							{allowSelectAllPages && (
								<li>
									<button type="button" onClick={() => onSelectAllPages()}>
										Select all pages
									</button>
								</li>
							)}
							<li>
								<button type="button" onClick={() => onSelectAll(false)}>
									Clear selection
								</button>
							</li>
						</ul>
					</div>
				</th>
				<th>
					<button
						type="button"
						className="flex items-center gap-1 hover:text-primary"
						onClick={() => onSort("file_path")}
					>
						File Path
						{sortBy === "file_path" &&
							(sortOrder === "asc" ? (
								<ChevronUp className="h-4 w-4" />
							) : (
								<ChevronDown className="h-4 w-4" />
							))}
					</button>
				</th>
				<th>Library Path</th>
				<th>
					<button
						type="button"
						className="flex items-center gap-1 hover:text-primary"
						onClick={() => onSort("status")}
					>
						Status
						{sortBy === "status" &&
							(sortOrder === "asc" ? (
								<ChevronUp className="h-4 w-4" />
							) : (
								<ChevronDown className="h-4 w-4" />
							))}
					</button>
				</th>
				<th>
					<button
						type="button"
						className="flex items-center gap-1 hover:text-primary"
						onClick={() => onSort("priority")}
					>
						Rank
						{sortBy === "priority" &&
							(sortOrder === "asc" ? (
								<ChevronUp className="h-4 w-4" />
							) : (
								<ChevronDown className="h-4 w-4" />
							))}
					</button>
				</th>
				<th>
					<button
						type="button"
						className="flex items-center gap-1 hover:text-primary"
						onClick={() => onSort("last_checked")}
					>
						Check Times
						{sortBy === "last_checked" &&
							(sortOrder === "asc" ? (
								<ChevronUp className="h-4 w-4" />
							) : (
								<ChevronDown className="h-4 w-4" />
							))}
					</button>
				</th>
				<th>
					<button
						type="button"
						className="flex items-center gap-1 hover:text-primary"
						onClick={() => onSort("created_at")}
					>
						Added
						{sortBy === "created_at" &&
							(sortOrder === "asc" ? (
								<ChevronUp className="h-4 w-4" />
							) : (
								<ChevronDown className="h-4 w-4" />
							))}
					</button>
				</th>
				<th>Actions</th>
			</tr>
		</thead>
	);
}
