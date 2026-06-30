import { Activity } from "lucide-react";
import { useMemo, useState } from "react";
import { usePoolMetrics, useProviderSpeedHistory } from "../../hooks/useApi";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import {
	buildProviderColorMap,
	type ChartDatum,
	ProviderAreaChart,
	type TimeRangeTab,
} from "./chartShared";

const TABS: TimeRangeTab[] = [
	{ label: "24h", value: 1 },
	{ label: "7d", value: 7 },
	{ label: "30d", value: 30 },
	{ label: "90d", value: 90 },
	{ label: "All Time", value: 365 },
];

const formatSpeed = (value: number) => `${value.toFixed(1)} MB/s`;

export function ProviderSpeedChart() {
	const [days, setDays] = useState(7);
	const { data: historyResponse, isLoading: historyLoading } = useProviderSpeedHistory(days);
	const { data: poolData } = usePoolMetrics();

	const { chartData, providers } = useMemo(() => {
		const grouped: Record<string, ChartDatum> = {};
		const maxes: Record<string, number> = {};

		if (poolData?.providers) {
			poolData.providers.forEach((p) => {
				maxes[p.host] = 0;
			});
		}

		if (historyResponse?.history) {
			historyResponse.history.forEach((stat) => {
				const date = new Date(stat.created_at);

				let timestamp = "";
				if (days <= 1) {
					timestamp = date.toLocaleString(undefined, {
						hour: "2-digit",
						minute: "2-digit",
						hour12: false,
					});
				} else if (days <= 7) {
					timestamp = `${date.toLocaleString(undefined, {
						month: "short",
						day: "numeric",
						hour: "2-digit",
						hour12: false,
					})}:00`;
				} else if (days <= 60) {
					timestamp = date.toLocaleString(undefined, {
						month: "short",
						day: "numeric",
					});
				} else {
					timestamp = `Wk of ${date.toLocaleString(undefined, {
						month: "short",
						day: "numeric",
					})}`;
				}

				if (!grouped[timestamp]) {
					grouped[timestamp] = { name: timestamp };
				}

				const provider = poolData?.providers.find((p) => p.id === stat.provider_id);
				const label = provider ? provider.host : stat.provider_id;

				grouped[timestamp][label] = stat.speed_mbps;
				maxes[label] = Math.max(maxes[label] || 0, stat.speed_mbps);
			});
		}

		const sortedProviders = Object.keys(maxes).sort((a, b) => maxes[b] - maxes[a]);

		return { chartData: Object.values(grouped), providers: sortedProviders };
	}, [historyResponse, poolData, days]);

	const colorMap = useMemo(
		() => buildProviderColorMap([...(poolData?.providers ?? []).map((p) => p.host), ...providers]),
		[poolData, providers],
	);

	if (historyLoading)
		return (
			<div className="flex h-64 items-center justify-center">
				<LoadingSpinner size="lg" />
			</div>
		);

	return (
		<ProviderAreaChart
			icon={Activity}
			iconClassName="text-primary"
			title="Speed Performance History"
			subtitle="Top speed (MB/s) per provider over time"
			tabs={TABS}
			days={days}
			onDaysChange={setDays}
			tabActiveClassName="bg-primary text-primary-content shadow hover:bg-primary"
			chartData={chartData}
			providers={providers}
			colorMap={colorMap}
			gradientPrefix="colorSpeed"
			formatValue={formatSpeed}
			tooltipTotalClassName="text-success"
			yAxisUnit=" MB/s"
			connectNulls
		/>
	);
}
