import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { useHealthStats, useQueueStats } from "../../hooks/useApi";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function QueueChart() {
	const { data: stats, isLoading, error } = useQueueStats();

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
				Failed to load queue statistics
			</div>
		);
	}

	const data = [
		{ name: "Queued", value: stats.total_queued, fill: "#f59e0b" },
		{ name: "Processing", value: stats.total_processing, fill: "#3b82f6" },
		{ name: "Completed", value: stats.total_completed, fill: "#10b981" },
		{ name: "Failed", value: stats.total_failed, fill: "#ef4444" },
	];

	return (
		<ResponsiveContainer width="100%" height={300}>
			<BarChart data={data}>
				<CartesianGrid strokeDasharray="3 3" />
				<XAxis dataKey="name" tick={{ fontSize: 12 }} className="text-base-content" />
				<YAxis tick={{ fontSize: 12 }} className="text-base-content" />
				<Tooltip
					contentStyle={{
						backgroundColor: "hsl(var(--b1))",
						border: "1px solid hsl(var(--bc) / 0.2)",
						borderRadius: "0.5rem",
						color: "hsl(var(--bc))",
					}}
				/>
				<Bar dataKey="value" />
			</BarChart>
		</ResponsiveContainer>
	);
}

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

	const data = [
		{ name: "Healthy", value: stats.healthy, fill: "#10b981" },
		{ name: "Partial", value: stats.partial, fill: "#f59e0b" },
		{ name: "Corrupted", value: stats.corrupted, fill: "#ef4444" },
	];

	return (
		<ResponsiveContainer width="100%" height={300}>
			<BarChart data={data}>
				<CartesianGrid strokeDasharray="3 3" />
				<XAxis dataKey="name" tick={{ fontSize: 12 }} className="text-base-content" />
				<YAxis tick={{ fontSize: 12 }} className="text-base-content" />
				<Tooltip
					contentStyle={{
						backgroundColor: "hsl(var(--b1))",
						border: "1px solid hsl(var(--bc) / 0.2)",
						borderRadius: "0.5rem",
						color: "hsl(var(--bc))",
					}}
				/>
				<Bar dataKey="value" />
			</BarChart>
		</ResponsiveContainer>
	);
}
