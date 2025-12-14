import {
	Bar,
	BarChart,
	CartesianGrid,
	Cell,
	ResponsiveContainer,
	Tooltip,
	XAxis,
	YAxis,
} from "recharts";
import { useHealthStats } from "../../hooks/useApi";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function HealthChart() {
	const { data: stats, isLoading, error } = useHealthStats();

	if (isLoading) {
		return (
			<div className="flex h-64 items-center justify-center">
				<LoadingSpinner size="lg" />
			</div>
		);
	}

	if (error || !stats) {
		return (
			<div className="flex h-64 items-center justify-center text-error">
				Failed to load health statistics
			</div>
		);
	}

	// Include all categories to maintain consistent x-axis, even if zero
	const data = [
		{ name: "Healthy", value: stats.healthy, color: "#10b981" }, // success
		{ name: "Checking", value: stats.checking, color: "#3b82f6" }, // info
		{ name: "Pending", value: stats.pending, color: "#f59e0b" }, // warning
		{ name: "Repairing", value: stats.repair_triggered, color: "#8b5cf6" }, // purple
		{ name: "Corrupted", value: stats.corrupted, color: "#ef4444" }, // error
	];

	// Check if all values are zero
	if (data.every((item) => item.value === 0)) {
		return (
			<div className="flex h-64 flex-col items-center justify-center text-base-content/50">
				<p>No files tracked</p>
			</div>
		);
	}

	return (
		<ResponsiveContainer width="100%" height={300}>
			<BarChart data={data} margin={{ top: 20, right: 30, left: 0, bottom: 5 }}>
				<CartesianGrid strokeDasharray="3 3" vertical={false} stroke="hsl(var(--bc) / 0.1)" />
				<XAxis
					dataKey="name"
					tick={{ fontSize: 12 }}
					axisLine={false}
					tickLine={false}
					className="text-base-content"
				/>
				<YAxis
					tick={{ fontSize: 12 }}
					axisLine={false}
					tickLine={false}
					className="text-base-content"
					allowDecimals={false}
				/>
				<Tooltip
					cursor={{ fill: "hsl(var(--bc) / 0.05)" }}
					contentStyle={{
						backgroundColor: "hsl(var(--b1))",
						border: "1px solid hsl(var(--bc) / 0.2)",
						borderRadius: "0.5rem",
						color: "hsl(var(--bc))",
					}}
					itemStyle={{ color: "hsl(var(--bc))" }}
				/>
				<Bar dataKey="value" radius={[4, 4, 0, 0]}>
					{data.map((entry, index) => (
						<Cell key={`cell-${index}`} fill={entry.color} />
					))}
				</Bar>
			</BarChart>
		</ResponsiveContainer>
	);
}
