import { Heart } from "lucide-react";
import { HealthBadge } from "../../../../components/ui/StatusBadge";
import { formatFutureTime, formatRelativeTime, truncateText } from "../../../../lib/utils";
import type { FileHealth } from "../../../../types/api";
import { HealthItemActionsMenu } from "./HealthItemActionsMenu";

interface HealthTableRowProps {
	item: FileHealth;
	isSelected: boolean;
	isCancelPending: boolean;
	isDirectCheckPending: boolean;
	isRepairPending: boolean;
	isDeletePending: boolean;
	onSelectChange: (filePath: string, checked: boolean) => void;
	onCancelCheck: (id: number) => void;
	onManualCheck: (id: number) => void;
	onRepair: (id: number) => void;
	onDelete: (id: number) => void;
}

export function HealthTableRow({
	item,
	isSelected,
	isCancelPending,
	isDirectCheckPending,
	isRepairPending,
	isDeletePending,
	onSelectChange,
	onCancelCheck,
	onManualCheck,
	onRepair,
	onDelete,
}: HealthTableRowProps) {
	return (
		<tr key={item.id} className={`hover ${isSelected ? "bg-base-200" : ""}`}>
			<td>
				<label className="cursor-pointer">
					<input
						type="checkbox"
						className="checkbox"
						checked={isSelected}
						onChange={(e) => onSelectChange(item.file_path, e.target.checked)}
					/>
				</label>
			</td>
			<td>
				<div className="flex items-center space-x-3">
					<Heart className="h-4 w-4 text-primary" />
					<div>
						<div className="font-bold">
							{truncateText(item.file_path.split("/").pop() || "", 40)}
						</div>
						<div className="tooltip text-base-content/70 text-sm" data-tip={item.file_path}>
							{truncateText(item.file_path, 60)}
						</div>
					</div>
				</div>
			</td>
			<td>
				<div className="tooltip text-sm" data-tip={item.library_path}>
					{truncateText(item.library_path?.split("/").pop() || "", 40)}
				</div>
			</td>
			<td>
				<div className="flex items-center gap-2">
					<HealthBadge status={item.status} />
				</div>
				{/* Show last_error for repair failures and general errors */}
				{item.last_error && (
					<div className="mt-1">
						<div className="tooltip tooltip-bottom text-left" data-tip={item.last_error}>
							<div className="cursor-help text-error text-xs">
								{truncateText(item.last_error, 50)}
							</div>
						</div>
					</div>
				)}
				{/* Show error_details for additional technical details */}
				{item.error_details && item.error_details !== item.last_error && (
					<div className="mt-1">
						<div className="tooltip tooltip-bottom text-left" data-tip={item.error_details}>
							<div className="cursor-help text-warning text-xs">
								Technical: {truncateText(item.error_details, 40)}
							</div>
						</div>
					</div>
				)}
			</td>
			<td>
				<div className="flex flex-col gap-1">
					<span
						className={`badge badge-sm ${item.retry_count > 0 ? "badge-warning" : "badge-ghost"}`}
						title="Health check retries"
					>
						H: {item.retry_count}/{item.max_retries}
					</span>
					{(item.status === "repair_triggered" || item.repair_retry_count > 0) && (
						<span
							className={`badge badge-sm ${item.repair_retry_count > 0 ? "badge-info" : "badge-ghost"}`}
							title="Repair retries"
						>
							R: {item.repair_retry_count}/{item.max_repair_retries}
						</span>
					)}
				</div>
			</td>
			<td>
				<span className="text-base-content/70 text-sm">
					{item.last_checked ? formatRelativeTime(item.last_checked) : "Never"}
				</span>
			</td>
			<td>
				<span className="text-base-content/70 text-sm">
					{item.scheduled_check_at ? formatFutureTime(item.scheduled_check_at) : "Not scheduled"}
				</span>
			</td>
			<td>
				<span className="text-base-content/70 text-sm">{formatRelativeTime(item.created_at)}</span>
			</td>
			<td>
				<HealthItemActionsMenu
					item={item}
					isCancelPending={isCancelPending}
					isDirectCheckPending={isDirectCheckPending}
					isRepairPending={isRepairPending}
					isDeletePending={isDeletePending}
					onCancelCheck={onCancelCheck}
					onManualCheck={onManualCheck}
					onRepair={onRepair}
					onDelete={onDelete}
				/>
			</td>
		</tr>
	);
}
