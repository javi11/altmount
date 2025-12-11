import { RefreshCw, Trash2, Wrench } from "lucide-react";

interface BulkActionsToolbarProps {
	selectedCount: number;
	isRestartPending: boolean;
	isDeletePending: boolean;
	isRepairPending: boolean;
	onClearSelection: () => void;
	onBulkRestart: () => void;
	onBulkDelete: () => void;
	onBulkRepair: () => void;
}

export function BulkActionsToolbar({
	selectedCount,
	isRestartPending,
	isDeletePending,
	isRepairPending,
	onClearSelection,
	onBulkRestart,
	onBulkDelete,
	onBulkRepair,
}: BulkActionsToolbarProps) {
	if (selectedCount === 0) {
		return null;
	}

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-4">
						<span className="font-semibold text-sm">
							{selectedCount} record{selectedCount !== 1 ? "s" : ""} selected
						</span>
						<button type="button" className="btn btn-ghost btn-sm" onClick={onClearSelection}>
							Clear Selection
						</button>
					</div>
					<div className="flex items-center gap-2">
						<button
							type="button"
							className="btn btn-warning btn-sm"
							onClick={onBulkRepair}
							disabled={isRepairPending}
						>
							<Wrench className="h-4 w-4" />
							Repair Selected
						</button>
						<button
							type="button"
							className="btn btn-info btn-sm"
							onClick={onBulkRestart}
							disabled={isRestartPending}
						>
							<RefreshCw className="h-4 w-4" />
							Restart Checks
						</button>
						<button
							type="button"
							className="btn btn-error btn-sm"
							onClick={onBulkDelete}
							disabled={isDeletePending}
						>
							<Trash2 className="h-4 w-4" />
							Delete Selected
						</button>
					</div>
				</div>
			</div>
		</div>
	);
}
