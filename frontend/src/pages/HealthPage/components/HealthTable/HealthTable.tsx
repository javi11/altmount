import { Shield } from "lucide-react";
import { LoadingTable } from "../../../../components/ui/LoadingSpinner";
import type { FileHealth, HealthPriority } from "../../../../types/api";
import type { SortBy, SortOrder } from "../../types";
import { HealthTableHeader } from "./HealthTableHeader";
import { HealthTableRow } from "./HealthTableRow";

interface HealthTableProps {
	data: FileHealth[] | undefined;
	isLoading: boolean;
	selectedItems: Set<string>;
	sortBy: SortBy;
	sortOrder: SortOrder;
	searchTerm: string;
	statusFilter: string;
	isCancelPending: boolean;
	isDirectCheckPending: boolean;
	isRepairPending: boolean;
	isDeletePending: boolean;
	onSelectItem: (filePath: string, checked: boolean) => void;
	onSelectAll: (checked: boolean) => void;
	onSort: (column: SortBy) => void;
	onCancelCheck: (id: number) => void;
	onManualCheck: (id: number) => void;
	onRepair: (id: number) => void;
	onDelete: (id: number) => void;
	onSetPriority: (id: number, priority: HealthPriority) => void;
}

export function HealthTable({
	data,
	isLoading,
	selectedItems,
	sortBy,
	sortOrder,
	searchTerm,
	statusFilter,
	isCancelPending,
	isDirectCheckPending,
	isRepairPending,
	isDeletePending,
	onSelectItem,
	onSelectAll,
	onSort,
	onCancelCheck,
	onManualCheck,
	onRepair,
	onDelete,
	onSetPriority,
}: HealthTableProps) {
	// Helper functions for select all checkbox state
	const isAllSelected =
		data && data.length > 0 && data.every((item) => selectedItems.has(item.file_path));
	const isIndeterminate = data && selectedItems.size > 0 && !isAllSelected;

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body p-0">
				{isLoading ? (
					<LoadingTable columns={9} />
				) : data && data.length > 0 ? (
					<div>
						<table className="table-zebra table">
							<HealthTableHeader
								isAllSelected={Boolean(isAllSelected)}
								isIndeterminate={Boolean(isIndeterminate)}
								sortBy={sortBy}
								sortOrder={sortOrder}
								onSelectAll={onSelectAll}
								onSort={onSort}
							/>
							<tbody>
								{data.map((item: FileHealth) => (
									<HealthTableRow
										key={item.id}
										item={item}
										isSelected={selectedItems.has(item.file_path)}
										isCancelPending={isCancelPending}
										isDirectCheckPending={isDirectCheckPending}
										isRepairPending={isRepairPending}
										isDeletePending={isDeletePending}
										onSelectChange={onSelectItem}
										onCancelCheck={onCancelCheck}
										onManualCheck={onManualCheck}
										onRepair={onRepair}
										onDelete={onDelete}
										onSetPriority={onSetPriority}
									/>
								))}
							</tbody>
						</table>
					</div>
				) : (
					<div className="flex flex-col items-center justify-center py-12">
						<Shield className="mb-4 h-12 w-12 text-base-content/30" />
						<h3 className="font-semibold text-base-content/70 text-lg">No health records found</h3>
						<p className="text-base-content/50">
							{searchTerm || statusFilter
								? "Try adjusting your filters"
								: "No files are currently being health checked"}
						</p>
					</div>
				)}
			</div>
		</div>
	);
}
