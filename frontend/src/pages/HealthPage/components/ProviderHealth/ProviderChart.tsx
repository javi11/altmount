import { BarChart3 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
	Area,
	AreaChart,
	CartesianGrid,
	Cell,
	Legend,
	Pie,
	PieChart,
	ResponsiveContainer,
	Tooltip,
	XAxis,
	YAxis,
} from "recharts";
import { LoadingSpinner } from "../../../../components/ui/LoadingSpinner";
import { usePoolMetrics, useProviderHistoricalStats } from "../../../../hooks/useApi";
import { formatBytes } from "../../../../lib/utils";

const COLORS = ["#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#ec4899", "#06b6d4"];

const CustomTooltip = ({
	active,
	payload,
	label,
}: {
	active?: boolean;
	payload?: { value: number; dataKey: string; stroke: string }[];
	label?: string;
}) => {
	if (!active || !payload || payload.length === 0) return null;

	const sortedPayload = [...payload].sort((a, b) => b.value - a.value);
	const sum = payload.reduce((acc, p) => acc + p.value, 0);

	return (
		<div className="z-50 min-w-[220px] rounded-xl border border-base-200/50 bg-base-100/90 p-4 text-xs shadow-2xl backdrop-blur-md">
			<p className="mb-2 border-base-200/30 border-b pb-1.5 font-bold text-base-content/80">
				{label}
			</p>
			<div className="max-h-48 space-y-1.5 overflow-y-auto pr-1">
				{sortedPayload.map((p) => (
					<div key={p.dataKey} className="flex items-center justify-between gap-4 py-0.5">
						<div className="flex items-center gap-1.5">
							<span className="h-2 w-2 rounded-full" style={{ backgroundColor: p.stroke }} />
							<span className="font-medium text-base-content/75">{p.dataKey}:</span>
						</div>
						<span className="font-mono font-semibold text-base-content">
							{formatBytes(p.value)}
						</span>
					</div>
				))}
			</div>
			<div className="mt-3 flex justify-between border-base-200/30 border-t pt-2 font-bold text-sm">
				<span className="text-base-content/70">Total:</span>
				<span className="font-mono text-info">{formatBytes(sum)}</span>
			</div>
		</div>
	);
};

export function ProviderChart() {
	const [days, setDays] = useState(30);
	const [activeProviders, setActiveProviders] = useState<Record<string, boolean>>({});
	const { data: poolData } = usePoolMetrics();

	// Dynamically match aggregation interval to timeframe for premium snappiness
	const interval = useMemo(() => {
		if (days <= 7) return "daily";
		if (days <= 60) return "daily";
		if (days <= 180) return "weekly";
		return "monthly";
	}, [days]);

	const { data: response, isLoading } = useProviderHistoricalStats(days, interval);

	const { chartData, providers, totalUsage, providerTotals } = useMemo(() => {
		const groupedByTime: Record<string, Record<string, string | number>> = {};
		const pTotals: Record<string, number> = {};
		let total = 0;

		if (poolData?.providers) {
			poolData.providers.forEach((p: any) => {
				pTotals[p.host] = 0;
			});
		}

		if (response?.stats && response.stats.length > 0) {
			for (const stat of response.stats) {
			const dateObj = new Date(stat.timestamp);
			const timeKey = dateObj.toISOString();

			let timeLabel = "";
			if (interval === "daily") {
				timeLabel = dateObj.toLocaleString(undefined, { month: "short", day: "numeric" });
			} else if (interval === "weekly") {
				timeLabel = `Wk of ${dateObj.toLocaleString(undefined, { month: "short", day: "numeric" })}`;
			} else {
				timeLabel = dateObj.toLocaleString(undefined, { month: "short", year: "2-digit" });
			}

			const provider = poolData?.providers?.find((p: any) => p.id === stat.provider_id || stat.provider_id.startsWith(p.host));
			const normalizedID = provider ? provider.host : stat.provider_id.split(":")[0];

			if (!groupedByTime[timeKey]) groupedByTime[timeKey] = { name: timeLabel };

			const currentVal = groupedByTime[timeKey][normalizedID];
			groupedByTime[timeKey][normalizedID] =
				(typeof currentVal === "number" ? currentVal : 0) + stat.bytes_downloaded;

			pTotals[normalizedID] = (pTotals[normalizedID] || 0) + stat.bytes_downloaded;
			total += stat.bytes_downloaded;
		}
		}

		const sortedProviders = Object.keys(pTotals).sort((a, b) => pTotals[b] - pTotals[a]);

		return {
			chartData: Object.values(groupedByTime),
			providers: sortedProviders,
			totalUsage: total,
			providerTotals: pTotals,
		};
	}, [response, interval, poolData]);

	// Initialize active providers when providers load
	useEffect(() => {
		if (providers.length > 0) {
			setActiveProviders((prev) => {
				const next = { ...prev };
				let changed = false;
				for (const p of providers) {
					if (next[p] === undefined) {
						next[p] = true;
						changed = true;
					}
				}
				return changed ? next : prev;
			});
		}
	}, [providers]);

	if (isLoading)
		return (
			<div className="flex h-64 items-center justify-center">
				<LoadingSpinner size="lg" />
			</div>
		);

	const toggleProvider = (provider: string) => {
		setActiveProviders((prev) => ({
			...prev,
			[provider]: !prev[provider],
		}));
	};

	const pieData = providers
		.map((p) => ({
			name: p,
			value: providerTotals[p],
		}))
		.filter((d) => activeProviders[d.name]);

	return (
		<div className="card rounded-2xl border border-base-200/60 bg-base-100/40 shadow-xl backdrop-blur-sm">
			<div className="card-body p-6">
				<div className="mb-6 flex flex-col items-start justify-between gap-4 lg:flex-row lg:items-center">
					<div>
						<h2 className="card-title flex items-center gap-2 font-bold text-lg">
							<BarChart3 className="h-5 w-5 animate-pulse text-info" />
							Data Usage Trends
						</h2>
						<p className="mt-0.5 text-base-content/60 text-xs">
							Total volume: {formatBytes(totalUsage)} in the last {days} days
						</p>
					</div>

					{/* Premium Segmented Timeline Controller */}
					<div className="join rounded-xl border border-base-200/40 bg-base-200/50 p-0.5">
						{[
							{ label: "7d", value: 7 },
							{ label: "30d", value: 30 },
							{ label: "90d", value: 90 },
							{ label: "All Time", value: 365 },
						].map((tab) => (
							<button
								key={tab.label}
								className={`join-item btn btn-sm btn-ghost rounded-lg px-3.5 font-bold text-xs transition-all ${
									days === tab.value
										? "bg-info text-info-content shadow hover:bg-info"
										: "text-base-content/60 hover:text-base-content"
								}`}
								onClick={() => setDays(tab.value)}
							>
								{tab.label}
							</button>
						))}
					</div>
				</div>

				<div className="flex h-80 w-full flex-col gap-6 lg:flex-row">
					<div className="h-full w-full flex-grow lg:w-3/4">
						<ResponsiveContainer width="100%" height="100%">
							<AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
								<defs>
									{providers.map((p, i) => (
										<linearGradient key={`color${p}`} id={`color${p}`} x1="0" y1="0" x2="0" y2="1">
											<stop offset="5%" stopColor={COLORS[i % COLORS.length]} stopOpacity={0.45} />
											<stop offset="95%" stopColor={COLORS[i % COLORS.length]} stopOpacity={0.02} />
										</linearGradient>
									))}
								</defs>
								<CartesianGrid strokeDasharray="3 3" opacity={0.04} vertical={false} />
								<XAxis
									dataKey="name"
									tick={{ fontSize: 10, fill: "currentColor", opacity: 0.7 }}
									axisLine={false}
									tickLine={false}
								/>
								<YAxis
									tick={{ fontSize: 10, fill: "currentColor", opacity: 0.7 }}
									axisLine={false}
									tickLine={false}
									tickFormatter={formatBytes}
								/>
								<Tooltip
									content={<CustomTooltip />}
									cursor={{ stroke: "rgba(255,255,255,0.08)", strokeWidth: 1 }}
								/>
								<Legend
									onClick={(e: any) => toggleProvider(e.dataKey as string)}
									wrapperStyle={{ cursor: "pointer", fontSize: "11px", paddingTop: "15px" }}
									{...({
										payload: providers.map((p, i) => ({
											value: p,
											type: "circle",
											id: p,
											color: COLORS[i % COLORS.length],
											dataKey: p,
											inactive: !activeProviders[p],
										})),
									} as any)}
									formatter={(value, entry: any) => (
										<span
											style={{
												color: !entry.inactive ? "inherit" : "#666",
												textDecoration: !entry.inactive ? "none" : "line-through",
												paddingRight: "8px",
											}}
										>
											{value}
										</span>
									)}
								/>
								{[...providers].reverse().map((p) => {
									const i = providers.indexOf(p);
									const color = COLORS[i % COLORS.length];
									return (
										activeProviders[p] && (
											<Area
												key={p}
												dataKey={p}
												type="monotone"
												stroke={color}
												fill={`url(#color${p})`}
												strokeWidth={2}
												activeDot={{ r: 5, strokeWidth: 0, fill: color }}
											/>
										)
									);
								})}
							</AreaChart>
						</ResponsiveContainer>
					</div>
					<div className="flex hidden h-full w-full flex-col items-center justify-center border-base-200/40 border-l pl-4 lg:flex lg:w-1/4">
						<span className="mb-2 font-bold text-base-content/70 text-xs uppercase tracking-wider">
							Usage Breakdown
						</span>
						<ResponsiveContainer width="100%" height="100%">
							<PieChart>
								<Pie
									data={pieData}
									innerRadius={55}
									outerRadius={75}
									paddingAngle={4}
									dataKey="value"
								>
									{pieData.map((entry, index) => (
										<Cell
											key={`cell-${index}`}
											fill={COLORS[providers.indexOf(entry.name) % COLORS.length]}
										/>
									))}
								</Pie>
								<Tooltip
									formatter={(value: number) => formatBytes(value)}
									contentStyle={{
										borderRadius: "12px",
										border: "1px solid hsl(var(--bc) / 0.1)",
										backgroundColor: "hsl(var(--b1) / 0.95)",
										fontSize: "11px",
										backdropFilter: "blur(8px)",
										boxShadow: "0 10px 15px -3px rgba(0, 0, 0, 0.3)",
									}}
								/>
							</PieChart>
						</ResponsiveContainer>
					</div>
				</div>
			</div>
		</div>
	);
}
