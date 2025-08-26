import { FileClock, FileScan, FileWarning, FileX, Heart } from "lucide-react";
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
	healthy: <Heart className="inline-block" />,
	partial: <FileWarning className="inline-block" />,
	corrupted: <FileX className="inline-block" />,
	pending: <FileClock className="inline-block" />,
	checking: <FileScan className="inline-block" />,
};

export function HealthBadge({ status, className }: StatusBadgeProps) {
	const fileIcon = icons[status.toLowerCase() as keyof typeof icons];
	const colorClass = getStatusColor(status);

	return (
		<div className={cn(`badge badge-${colorClass}`, className)}>
			{status.toLowerCase() === "checking" ? (
				<span className="loading loading-spinner loading-xs mr-1" />
			) : (
				<span className="mr-1">{fileIcon}</span>
			)}
			{status}
		</div>
	);
}
