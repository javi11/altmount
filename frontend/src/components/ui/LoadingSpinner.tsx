/** biome-ignore-all lint/suspicious/noArrayIndexKey: Is a repeat */
import { cn } from "../../lib/utils";

interface LoadingSpinnerProps {
	size?: "sm" | "md" | "lg";
	className?: string;
}

export function LoadingSpinner({ size = "md", className }: LoadingSpinnerProps) {
	const sizeClasses = {
		sm: "loading-sm",
		md: "loading-md",
		lg: "loading-lg",
	};

	return <span className={cn("loading loading-spinner", sizeClasses[size], className)} />;
}

export function LoadingTable({ columns }: { columns: number }) {
	return (
		<div className="overflow-x-auto">
			<table className="table">
				<tbody>
					{Array.from({ length: 5 }).map((_, i) => (
						<tr key={`loading-row-${i}`}>
							{Array.from({ length: columns }).map((_, j) => (
								<td key={`loading-cell-${i}-${j}`}>
									<div className="h-4 animate-pulse rounded bg-base-300" />
								</td>
							))}
						</tr>
					))}
				</tbody>
			</table>
		</div>
	);
}
