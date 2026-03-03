import { CheckCircle, EyeOff, FileClock, FileScan, FileX, Wrench } from "lucide-react";
import { cn, getStatusColor } from "../../lib/utils";

interface StatusBadgeProps {
	status: string;
	className?: string;
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
	const colorClass = getStatusColor(status);

	return <div className={cn(`badge badge-${colorClass}`, className)}>{status}</div>;
}

const icons = {
	corrupted: <FileX className="inline-block" />,
	pending: <FileClock className="inline-block" />,
	checking: <FileScan className="inline-block" />,
	healthy: <CheckCircle className="inline-block" />,
	repair_triggered: <Wrench className="inline-block" />,
	masked: <EyeOff className="inline-block" />,
};

interface HealthBadgeProps extends StatusBadgeProps {
	isMasked?: boolean;
}

export function HealthBadge({ status, isMasked, className }: HealthBadgeProps) {
	const fileIcon = isMasked ? icons.masked : icons[status.toLowerCase() as keyof typeof icons];
	const colorClass = isMasked ? "warning" : getStatusColor(status);

	return (
		<div className={cn(`badge badge-${colorClass}`, className)}>
			{!isMasked && status.toLowerCase() === "checking" ? (
				<span className="loading loading-spinner loading-xs mr-1" />
			) : !isMasked && status.toLowerCase() === "repair_triggered" ? (
				<span className="loading loading-spinner loading-xs mr-1" />
			) : (
				<span className="mr-1">{fileIcon}</span>
			)}
			{isMasked ? "MASKED" : status}
		</div>
	);
}
