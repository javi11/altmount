import { MoreHorizontal, PlayCircle, Trash2, Wrench, X, EyeOff } from "lucide-react";
import type { FileHealth } from "../../../../types/api";

interface HealthItemActionsMenuProps {
	item: FileHealth;
	isCancelPending: boolean;
	isDirectCheckPending: boolean;
	isRepairPending: boolean;
	isDeletePending: boolean;
	isIgnorePending: boolean;
	onCancelCheck: (id: number) => void;
	onManualCheck: (id: number) => void;
	onRepair: (id: number) => void;
	onDelete: (id: number) => void;
	onIgnore: (id: number) => void;
}

export function HealthItemActionsMenu({
	item,
	isCancelPending,
	isDirectCheckPending,
	isRepairPending,
	isDeletePending,
	isIgnorePending,
	onCancelCheck,
	onManualCheck,
	onRepair,
	onDelete,
	onIgnore,
}: HealthItemActionsMenuProps) {
	return (
		<div className="dropdown dropdown-end">
			<button tabIndex={0} type="button" className="btn btn-ghost btn-sm">
				<MoreHorizontal className="h-4 w-4" />
			</button>
			<ul className="dropdown-content menu w-48 rounded-box bg-base-100 shadow-lg">
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
				{item.status !== "ignored" && (
					<li>
						<button
							type="button"
							onClick={() => onIgnore(item.id)}
							disabled={isIgnorePending}
							className="text-base-content/60"
						>
							<EyeOff className="h-4 w-4" />
							Ignore
						</button>
					</li>
				)}
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
