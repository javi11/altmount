import {
	AlertCircle,
	ChevronDown,
	ChevronUp,
	Clock,
	Heart,
	HeartCrack,
	Loader,
	Wrench,
} from "lucide-react";
import { useState } from "react";
import { HealthBadge } from "../../../../components/ui/StatusBadge";
import { formatFutureTime, formatRelativeTime, truncateText } from "../../../../lib/utils";
import { type FileHealth, HealthPriority } from "../../../../types/api";
import { HealthItemActionsMenu } from "./HealthItemActionsMenu";

interface HealthItemCardProps {
	item: FileHealth;
	isSelected: boolean;
	onSelectChange: (filePath: string, checked: boolean) => void;
	onSetPriority: (id: number, priority: HealthPriority) => void;
	onCancelCheck: (id: number) => void;
	onManualCheck: (id: number) => void;
	onRepair: (id: number) => void;
	onDelete: (id: number) => void;
	onUnmask: (id: number) => void;
	isCancelPending: boolean;
	isDirectCheckPending: boolean;
	isRepairPending: boolean;
	isDeletePending: boolean;
	isUnmaskPending: boolean;
}

export function HealthItemCard({
	item,
	isSelected,
	onSelectChange,
	onSetPriority,
	onCancelCheck,
	onManualCheck,
	onRepair,
	onDelete,
	onUnmask,
	isCancelPending,
	isDirectCheckPending,
	isRepairPending,
	isDeletePending,
	isUnmaskPending,
}: HealthItemCardProps) {
	const [isExpanded, setIsExpanded] = useState(false);

	// Reuse status icon logic from HealthTableRow
	const getNextPriority = (current: HealthPriority): HealthPriority => {
		switch (current) {
			case HealthPriority.Normal:
				return HealthPriority.High;
			case HealthPriority.High:
				return HealthPriority.Next;
			case HealthPriority.Next:
				return HealthPriority.Normal;
			default:
				return HealthPriority.Normal;
		}
	};

	let statusIcon: React.ReactNode;
	let iconColorClass = "text-base-content/50";

	switch (item.status) {
		case "healthy":
			statusIcon = <Heart className="h-4 w-4" />;
			iconColorClass = "text-success";
			break;
		case "corrupted":
			statusIcon = <HeartCrack className="h-4 w-4" />;
			iconColorClass = "text-error";
			break;
		case "repair_triggered":
			statusIcon = <Wrench className="h-4 w-4 animate-spin-slow" />;
			iconColorClass = "text-info";
			break;
		case "checking":
			statusIcon = <Loader className="h-4 w-4 animate-spin" />;
			iconColorClass = "text-warning";
			break;
		default:
			statusIcon = <Clock className="h-4 w-4" />;
			iconColorClass = "text-base-content/50";
			break;
	}

	return (
		<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
			<div className="card-body space-y-3 p-4">
				{/* Header Row: Checkbox + File Info + Actions */}
				<div className="flex items-start gap-3">
					<label className="flex h-11 w-11 shrink-0 cursor-pointer items-center justify-center">
						<input
							type="checkbox"
							className="checkbox"
							checked={isSelected}
							onChange={(e) => onSelectChange(item.file_path, e.target.checked)}
						/>
					</label>

					<div className="min-w-0 flex-1">
						<div className="flex items-center gap-2">
							<span className={iconColorClass}>{statusIcon}</span>
							<div className="truncate font-bold text-sm">
								{truncateText(item.file_path.split("/").pop() || "", 40)}
							</div>
						</div>

						{/* Quick Info Pills */}
						<div className="mt-2 flex flex-wrap gap-2">
							<HealthBadge status={item.status} isMasked={item.is_masked} />

							<button
								type="button"
								className="badge badge-sm cursor-pointer transition-transform hover:scale-110"
								onClick={() => onSetPriority(item.id, getNextPriority(item.priority))}
								onKeyDown={(e) => {
									if (e.key === "Enter" || e.key === " ") {
										onSetPriority(item.id, getNextPriority(item.priority));
									}
								}}
								title="Click to cycle priority"
							>
								{item.priority === HealthPriority.Next
									? "Next"
									: item.priority === HealthPriority.High
										? "High"
										: "Normal"}
							</button>

							<span className="badge badge-ghost badge-xs">
								{item.last_checked ? formatRelativeTime(item.last_checked) : "Never checked"}
							</span>

							{item.retry_count > 0 && (
								<span className="badge badge-warning badge-xs" title="Health check retries">
									H: {item.retry_count}/{item.max_retries}
								</span>
							)}

							{(item.status === "repair_triggered" || item.repair_retry_count > 0) && (
								<span className="badge badge-info badge-xs" title="Repair retries">
									R: {item.repair_retry_count}/{item.max_repair_retries}
								</span>
							)}
						</div>
					</div>

					<div className="shrink-0">
						<HealthItemActionsMenu
							item={item}
							isCancelPending={isCancelPending}
							isDirectCheckPending={isDirectCheckPending}
							isRepairPending={isRepairPending}
							isDeletePending={isDeletePending}
							isUnmaskPending={isUnmaskPending}
							onCancelCheck={onCancelCheck}
							onManualCheck={onManualCheck}
							onRepair={onRepair}
							onDelete={onDelete}
							onUnmask={onUnmask}
						/>
					</div>
				</div>

				{/* Error Messages */}
				{item.last_error && (
					<div className="alert alert-error px-3 py-2">
						<AlertCircle className="h-4 w-4 shrink-0" />
						<span className="text-xs">{truncateText(item.last_error, 100)}</span>
					</div>
				)}

				{item.error_details && item.error_details !== item.last_error && (
					<div className="alert alert-warning px-3 py-2">
						<AlertCircle className="h-4 w-4 shrink-0" />
						<span className="text-xs">Technical: {truncateText(item.error_details, 80)}</span>
					</div>
				)}

				{/* Expandable Details */}
				<button
					type="button"
					className="btn btn-ghost btn-sm w-full justify-between"
					onClick={() => setIsExpanded(!isExpanded)}
				>
					<span className="text-xs">Full Details</span>
					{isExpanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
				</button>

				{isExpanded && (
					<div className="space-y-2 border-t pt-3 text-xs">
						<div>
							<span className="opacity-70">File Path:</span>
							<div className="mt-1 break-all font-mono text-xs">{item.file_path}</div>
						</div>
						{item.library_path && (
							<div>
								<span className="opacity-70">Library Path:</span>
								<div className="mt-1 break-all font-mono text-xs">{item.library_path}</div>
							</div>
						)}
						<div>
							<span className="opacity-70">Next Check:</span>
							<div className="mt-1 text-xs">
								{item.scheduled_check_at
									? formatFutureTime(item.scheduled_check_at)
									: "Not scheduled"}
							</div>
						</div>
						<div>
							<span className="opacity-70">Created:</span>
							<div className="mt-1 text-xs">{formatRelativeTime(item.created_at)}</div>
						</div>
					</div>
				)}
			</div>
		</div>
	);
}
