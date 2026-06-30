import { BarChart3 } from "lucide-react";
import { useMemo, useState } from "react";
import { usePoolMetrics, useProviderHistoricalStats } from "../../hooks/useApi";
import { formatBytes } from "../../lib/utils";
import type { ProviderStatus } from "../../types/api";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import {
	buildProviderColorMap,
	type ChartDatum,
	ProviderAreaChart,
	type TimeRangeTab,
} from "./chartShared";

const TABS: TimeRangeTab[] = [
	{ label: "7d", value: 7 },
	{ label: "30d", value: 30 },
	{ label: "90d", value: 90 },
	{ label: "All Time", value: 365 },
];

export function ProviderChart() {
	const [days, setDays] = useState(30);
	const { data: poolData } = usePoolMetrics();

	// Dynamically match aggregation interval to timeframe for premium snappiness
	const interval = useMemo(() => {
		if (days <= 7) return "daily";
		if (days <= 60) return "daily";
		if (days <= 180) return "weekly";
		return "monthly";
	}, [days]);

	const { data: response, isLoading } = useProviderHistoricalStats(days, interval);

	const { chartData, providers, totalUsage } = useMemo(() => {
		const groupedByTime: Record<string, ChartDatum> = {};
		const pTotals: Record<string, number> = {};
		let total = 0;

		if (poolData?.providers) {
			poolData.providers.forEach((p: ProviderStatus) => {
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

				const provider = poolData?.providers?.find(
					(p: ProviderStatus) => p.id === stat.provider_id || stat.provider_id.startsWith(p.host),
				);
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
		};
	}, [response, interval, poolData]);

	const colorMap = useMemo(
		() =>
			buildProviderColorMap([
				...(poolData?.providers ?? []).map((p: ProviderStatus) => p.host),
				...providers,
			]),
		[poolData, providers],
	);

	if (isLoading)
		return (
			<div className="flex h-64 items-center justify-center">
				<LoadingSpinner size="lg" />
			</div>
		);

	return (
		<ProviderAreaChart
			icon={BarChart3}
			iconClassName="text-info"
			title="Data Usage Trends"
			subtitle={`Total volume: ${formatBytes(totalUsage)} in the last ${days} days`}
			tabs={TABS}
			days={days}
			onDaysChange={setDays}
			tabActiveClassName="bg-info text-info-content shadow hover:bg-info"
			chartData={chartData}
			providers={providers}
			colorMap={colorMap}
			gradientPrefix="color"
			formatValue={formatBytes}
			tooltipTotalClassName="text-info"
			yAxisTickFormatter={formatBytes}
		/>
	);
}
