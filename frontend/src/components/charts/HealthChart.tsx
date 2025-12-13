import { Cell, Legend, Pie, PieChart, ResponsiveContainer, Tooltip } from "recharts";
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

	// Filter out zero values to avoid clutter
	const data = [
		{ name: "Healthy", value: stats.healthy, color: "#10b981" }, // success
		{ name: "Checking", value: stats.checking, color: "#3b82f6" }, // info
		{ name: "Pending", value: stats.pending, color: "#f59e0b" }, // warning
		{ name: "Repairing", value: stats.repair_triggered, color: "#8b5cf6" }, // purple
		{ name: "Corrupted", value: stats.corrupted, color: "#ef4444" }, // error
	].filter((item) => item.value > 0);

	if (data.length === 0) {
		return (
			<div className="flex h-64 flex-col items-center justify-center text-base-content/50">
				<p>No files tracked</p>
			</div>
		);
	}

	return (
		<ResponsiveContainer width="100%" height={300}>
			<PieChart>
				<Pie
					data={data}
					cx="50%"
					cy="50%"
					innerRadius={60}
					outerRadius={80}
					paddingAngle={5}
					dataKey="value"
				>
					{data.map((entry, index) => (
						<Cell key={`cell-${index}`} fill={entry.color} />
					))}
				</Pie>
				<Tooltip
					contentStyle={{
						backgroundColor: "hsl(var(--b1))",
						border: "1px solid hsl(var(--bc) / 0.2)",
						borderRadius: "0.5rem",
						color: "hsl(var(--bc))",
					}}
					itemStyle={{ color: "hsl(var(--bc))" }}
				/>
				<Legend />
			</PieChart>
		</ResponsiveContainer>
	);
}
