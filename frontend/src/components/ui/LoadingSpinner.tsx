/** biome-ignore-all lint/suspicious/noArrayIndexKey: Is a repeat */
import { cn } from "../../lib/utils";

interface LoadingSpinnerProps {
	size?: "sm" | "md" | "lg";
	className?: string;
}

export function LoadingSpinner({
	size = "md",
	className,
}: LoadingSpinnerProps) {
	const sizeClasses = {
		sm: "loading-sm",
		md: "loading-md",
		lg: "loading-lg",
	};

	return (
		<span
			className={cn("loading loading-spinner", sizeClasses[size], className)}
		/>
	);
}

export function LoadingCard({ children }: { children?: React.ReactNode }) {
	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body items-center text-center">
				<LoadingSpinner size="lg" />
				<h2 className="card-title">Loading...</h2>
				{children}
			</div>
		</div>
	);
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
									<div className="h-4 bg-base-300 rounded animate-pulse" />
								</td>
							))}
						</tr>
					))}
				</tbody>
			</table>
		</div>
	);
}
