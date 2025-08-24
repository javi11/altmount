import { cn, getStatusColor } from "../../lib/utils";

interface StatusBadgeProps {
	status: string;
	className?: string;
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
	const colorClass = getStatusColor(status);

	return (
		<div className={cn(`badge badge-${colorClass}`, className)}>{status}</div>
	);
}

export function HealthBadge({ status, className }: StatusBadgeProps) {
	const icons = {
		healthy: "✓",
		partial: "⚠",
		corrupted: "✗",
		degraded: "⚠",
		unhealthy: "✗",
		checking: "🔄",
	};

	const icon = icons[status.toLowerCase() as keyof typeof icons] || "?";
	const colorClass = getStatusColor(status);

	return (
		<div className={cn(`badge badge-${colorClass}`, className)}>
			{status.toLowerCase() === "checking" ? (
				<span className="loading loading-spinner loading-xs mr-1" />
			) : (
				<span className="mr-1">{icon}</span>
			)}
			{status}
		</div>
	);
}
