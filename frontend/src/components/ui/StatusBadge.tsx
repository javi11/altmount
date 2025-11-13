import { CheckCircle, FileClock, FileScan, FileX, Wrench } from "lucide-react";
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
};

export function HealthBadge({ status, className }: StatusBadgeProps) {
	const fileIcon = icons[status.toLowerCase() as keyof typeof icons];
	const colorClass = getStatusColor(status);

	return (
		<div className={cn(`badge badge-${colorClass}`, className)}>
			{status.toLowerCase() === "checking" ? (
				<span className="loading loading-spinner loading-xs mr-1" />
			) : status.toLowerCase() === "repair_triggered" ? (
				<span className="loading loading-spinner loading-xs mr-1" />
			) : (
				<span className="mr-1">{fileIcon}</span>
			)}
			{status}
		</div>
	);
}
