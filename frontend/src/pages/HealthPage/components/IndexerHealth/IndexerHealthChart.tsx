import { BarChart2 } from "lucide-react";
import { Bar, BarChart, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { IndexerStat } from "./types";

const ChartTooltip = ({
	active,
	payload,
}: {
	active?: boolean;
	payload?: { value: number; payload: { name: string } }[];
}) => {
	if (!active || !payload || payload.length === 0) return null;
	const data = payload[0];
	const val = data.value;
	const name = data.payload.name;

	const isExcellent = val >= 85;
	const isModerate = val >= 50 && val < 85;
	const statusText = isExcellent
		? "Excellent"
		: isModerate
			? "Moderate"
			: "Poor";
	const badgeColor = isExcellent
		? "bg-teal-500/10 text-teal-400 border-teal-500/20"
		: isModerate
			? "bg-amber-500/10 text-amber-500 border-amber-500/20"
			: "bg-rose-500/10 text-rose-400 border-rose-500/20";

	return (
		<div className="z-50 rounded-xl border border-base-200 bg-base-100/95 p-3 text-base-content text-xs shadow-2xl backdrop-blur-md">
			<p className="mb-1.5 font-extrabold leading-tight">{name}</p>
			<div className="flex items-center gap-2">
				<span
					className={`badge badge-xs border ${badgeColor} py-1.5 font-bold uppercase tracking-wider`}
				>
					{statusText}
				</span>
				<span className="font-extrabold font-mono text-sm">{val.toFixed(1)}%</span>
			</div>
		</div>
	);
};

interface IndexerHealthChartProps {
	sorted: IndexerStat[];
}

export function IndexerHealthChart({ sorted }: IndexerHealthChartProps) {
	return (
		<div className="card overflow-hidden border border-base-200 bg-base-100/60 p-5 shadow-xl backdrop-blur-md transition-all duration-300">
			<div className="mb-4 flex items-center justify-between border-base-200 border-b pb-2">
				<div>
					<h4 className="flex items-center gap-2 font-bold text-base-content text-sm">
						<BarChart2
							className="h-4 w-4 animate-pulse text-teal-500 dark:text-teal-400"
							aria-hidden="true"
						/>
						Usenet Indexer Success Comparison
					</h4>
					<p className="font-medium text-[10px] text-base-content/50">
						Comparative efficiency rating across active indexers (%)
					</p>
				</div>
			</div>
			<div className="h-64 w-full">
				<ResponsiveContainer width="100%" height="100%">
					<BarChart
						data={sorted.map((s) => ({
							name: s.indexer,
							health: Math.round(s.success_rate * 10) / 10,
						}))}
						margin={{ top: 10, right: 10, left: -20, bottom: 5 }}
					>
						<XAxis
							dataKey="name"
							stroke="currentColor"
							className="text-[10px] text-base-content/40"
							tick={false}
							tickLine={false}
							axisLine={false}
						/>
						<YAxis
							domain={[0, 100]}
							stroke="currentColor"
							className="text-[10px] text-base-content/40"
							tickLine={false}
							axisLine={false}
							tickFormatter={(v) => `${v}%`}
						/>
						<Tooltip
							content={<ChartTooltip />}
							cursor={{ fill: "rgba(255, 255, 255, 0.05)", radius: 4 }}
						/>
						<Bar dataKey="health" radius={[4, 4, 0, 0]} barSize={36}>
							{sorted.map((entry, index) => {
								const isExcellent = entry.success_rate >= 85;
								const isModerate = entry.success_rate >= 50 && entry.success_rate < 85;
								const color = isExcellent
									? "#0d9488"
									: isModerate
										? "#d97706"
										: "#e11d48";
								return <Cell key={`cell-${index}`} fill={color} />;
							})}
						</Bar>
					</BarChart>
				</ResponsiveContainer>
			</div>
		</div>
	);
}
