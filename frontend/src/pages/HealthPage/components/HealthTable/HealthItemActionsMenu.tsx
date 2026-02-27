import { Eye, MoreHorizontal, PlayCircle, Trash2, Wrench, X } from "lucide-react";
import type { FileHealth } from "../../../../types/api";

interface HealthItemActionsMenuProps {
	item: FileHealth;
	isCancelPending: boolean;
	isDirectCheckPending: boolean;
	isRepairPending: boolean;
	isDeletePending: boolean;
	isUnmaskPending: boolean;
	onCancelCheck: (id: number) => void;
	onManualCheck: (id: number) => void;
	onRepair: (id: number) => void;
	onDelete: (id: number) => void;
	onUnmask: (id: number) => void;
}

export function HealthItemActionsMenu({
	item,
	isCancelPending,
	isDirectCheckPending,
	isRepairPending,
	isDeletePending,
	isUnmaskPending,
	onCancelCheck,
	onManualCheck,
	onRepair,
	onDelete,
	onUnmask,
}: HealthItemActionsMenuProps) {
	return (
		<div className="dropdown dropdown-end">
			<button tabIndex={0} type="button" className="btn btn-ghost btn-sm">
				<MoreHorizontal className="h-4 w-4" />
			</button>
			<ul className="dropdown-content menu z-[50] w-48 rounded-box border border-base-300 bg-base-100 p-2 shadow-xl">
				{item.is_masked && (
					<li>
						<button
							type="button"
							onClick={() => onUnmask(item.id)}
							disabled={isUnmaskPending}
							className="text-success"
						>
							<Eye className="h-4 w-4" />
							Unmask File
						</button>
					</li>
				)}
				{item.status === "checking" ? (
					<li>
						<button
							type="button"
							onClick={() => onCancelCheck(item.id)}
							disabled={isCancelPending}
							className="text-warning"
						>
							<X className="h-4 w-4" />
							Cancel Check
						</button>
					</li>
				) : (
					<li>
						<button
							type="button"
							onClick={() => onManualCheck(item.id)}
							disabled={isDirectCheckPending}
						>
							<PlayCircle className="h-4 w-4" />
							Retry Check
						</button>
					</li>
				)}
				<li>
					<button
						type="button"
						onClick={() => onRepair(item.id)}
						disabled={isRepairPending}
						className="text-info"
					>
						<Wrench className="h-4 w-4" />
						Trigger Repair
					</button>
				</li>
				<li>
					<button
						type="button"
						onClick={() => onDelete(item.id)}
						disabled={isDeletePending}
						className="text-error"
					>
						<Trash2 className="h-4 w-4" />
						Delete Record
					</button>
				</li>
			</ul>
		</div>
	);
}
