import { History } from "lucide-react";
import { useState } from "react";
import { useQueueHistory } from "../../hooks/useApi";
import type { DailyStat } from "../../types/api";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function QueueHistoricalStatsCard() {
	const [days, setDays] = useState(30);
	const { data: history, isLoading } = useQueueHistory(days);

	const ranges = [
		{ label: "24h", value: 1 },
		{ label: "7d", value: 7 },
		{ label: "30d", value: 30 },
		{ label: "365d", value: 365 },
	];

	const currentRangeData = () => {
		if (!history) return null;
		if (days === 1) return history.last_24_hours;
		if (days === 7) return history.last_7_days;
		if (days === 30) return history.last_30_days;
		return history.last_365_days;
	};

	const data = currentRangeData();

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<div className="flex flex-wrap items-center justify-between gap-4">
					<h2 className="card-title">
						<History className="h-5 w-5" />
						Import History
					</h2>
					<div className="flex items-center gap-2">
						<div className="join">
							{ranges.map((range) => (
								<button
									key={range.label}
									type="button"
									className={`btn btn-sm join-item ${days === range.value ? "btn-primary" : "btn-outline"}`}
									onClick={() => setDays(range.value)}
								>
									{range.label}
								</button>
							))}
						</div>
						<div className="flex items-center gap-1">
							<input
								type="number"
								className="input input-bordered input-sm w-16"
								value={days}
								onChange={(e) =>
									setDays(Math.max(1, Math.min(365, Number.parseInt(e.target.value, 10) || 1)))
								}
								min="1"
								max="365"
							/>
							<span className="text-base-content/70 text-xs">days</span>
						</div>
					</div>
				</div>

				{isLoading ? (
					<div className="flex h-32 items-center justify-center">
						<LoadingSpinner />
					</div>
				) : data ? (
					<div className="mt-4 space-y-4">
						<div className="flex items-end justify-between gap-4">
							<div className="flex flex-col">
								<span className="text-base-content/70 text-sm">Success Rate</span>
								<span className="font-bold text-3xl">{data.percentage.toFixed(1)}%</span>
							</div>
							<div className="flex flex-col text-right">
								<span className="text-base-content/70 text-sm">Processed</span>
								<span className="font-medium text-lg">{data.completed + data.failed} items</span>
							</div>
						</div>

						<div className="space-y-2">
							<div className="flex items-center justify-between text-xs">
								<span className="text-success">{data.completed} Successful</span>
								<span className="text-error">{data.failed} Failed</span>
							</div>
							<div className="flex h-4 w-full overflow-hidden rounded-full bg-base-200">
								<div
									className="h-full bg-success transition-all duration-500"
									style={{ width: `${data.percentage}%` }}
								/>
								<div
									className="h-full bg-error transition-all duration-500"
									style={{ width: `${100 - data.percentage}%` }}
								/>
							</div>
						</div>

						{history.daily.length > 0 && (
							<div className="mt-4 flex h-16 items-end gap-1 overflow-x-auto pb-1">
								{history.daily.slice(-30).map((day: DailyStat) => {
									const total = day.completed + day.failed;
									const height = total > 0 ? Math.min(100, (total / 50) * 100) : 2;
									return (
										<div
											key={day.day}
											className="group relative flex-1"
											title={`${day.day}: ${day.completed} success, ${day.failed} fail`}
										>
											<div
												className="w-full rounded-t-sm bg-primary/20 transition-all hover:bg-primary"
												style={{ height: `${height}%` }}
											/>
											<div
												className="absolute bottom-0 w-full bg-success"
												style={{ height: `${total > 0 ? (day.completed / total) * height : 0}%` }}
											/>
										</div>
									);
								})}
							</div>
						)}
					</div>
				) : (
					<div className="py-8 text-center text-base-content/50">
						No historical data available yet
					</div>
				)}
			</div>
		</div>
	);
}
