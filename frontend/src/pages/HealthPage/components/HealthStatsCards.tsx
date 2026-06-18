import type { HealthStats } from "../../../types/api";

interface HealthStatsCardsProps {
	stats: HealthStats | undefined;
}

export function HealthStatsCards({ stats }: HealthStatsCardsProps) {
	if (!stats) {
		return null;
	}

	const healthyPercentage =
		stats.total > 0 ? ((stats.healthy / stats.total) * 100).toFixed(1) : "0.0";
	const corruptedPercentage =
		stats.total > 0 ? ((stats.corrupted / stats.total) * 100).toFixed(1) : "0.0";

	const cards: {
		label: string;
		value: number;
		valueClass: string;
		caption: string;
		captionClass?: string;
	}[] = [
		{
			label: "Files Tracked",
			value: stats.total,
			valueClass: "text-primary",
			caption: "Total in database",
		},
		{
			label: "Healthy",
			value: stats.healthy || 0,
			valueClass: "text-success",
			caption: `${healthyPercentage}% of total`,
		},
		{
			label: "Pending",
			value: stats.pending || 0,
			valueClass: "text-info",
			caption: "Awaiting check",
		},
		{
			label: "Checking",
			value: stats.checking || 0,
			valueClass: "text-warning",
			caption: "In progress",
		},
		{
			label: "Repairing",
			value: stats.repair_triggered || 0,
			valueClass: "text-secondary",
			caption: "Triggered",
		},
		{
			label: "Corrupted",
			value: stats.corrupted,
			valueClass: "text-error",
			caption: `${corruptedPercentage}% - Require action`,
			captionClass: "font-bold text-error text-xs",
		},
	];

	return (
		<div className="grid grid-cols-2 gap-px overflow-hidden rounded-box border border-base-content/20 bg-base-content/20 shadow-md lg:grid-cols-3 xl:grid-cols-6">
			{cards.map((card, index) => (
				<div
					key={card.label}
					className={`space-y-1 px-5 py-4 ${index % 2 === 0 ? "bg-base-100" : "bg-base-200"}`}
				>
					<div className="text-base-content/60 text-xs">{card.label}</div>
					<div className={`font-extrabold text-3xl ${card.valueClass}`}>{card.value}</div>
					<div className={card.captionClass ?? "text-base-content/60 text-xs"}>{card.caption}</div>
				</div>
			))}
		</div>
	);
}
