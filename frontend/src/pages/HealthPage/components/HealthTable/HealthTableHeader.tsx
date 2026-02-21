import { ChevronDown, ChevronUp } from "lucide-react";
import type { SortBy, SortOrder } from "../../types";

interface HealthTableHeaderProps {
	isAllSelected: boolean;
	isIndeterminate: boolean;
	sortBy: SortBy;
	sortOrder: SortOrder;
	onSelectAll: (checked: boolean) => void;
	onSort: (column: SortBy) => void;
}

export function HealthTableHeader({
	isAllSelected,
	isIndeterminate,
	sortBy,
	sortOrder,
	onSelectAll,
	onSort,
}: HealthTableHeaderProps) {
	return (
		<thead>
			<tr>
				<th className="w-12">
					<label className="cursor-pointer">
						<input
							type="checkbox"
							className="checkbox"
							checked={isAllSelected}
							ref={(input) => {
								if (input) input.indeterminate = Boolean(isIndeterminate);
							}}
							onChange={(e) => onSelectAll(e.target.checked)}
						/>
					</label>
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
