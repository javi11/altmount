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

	return (
		<div className="grid grid-cols-2 gap-4 lg:grid-cols-3 xl:grid-cols-6">
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Files Tracked</div>
				<div className="stat-value text-primary">{stats.total}</div>
				<div className="stat-desc">Total in database</div>
			</div>
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Healthy</div>
				<div className="stat-value text-success">{stats.healthy || 0}</div>
				<div className="stat-desc">{healthyPercentage}% of total</div>
			</div>
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Pending</div>
				<div className="stat-value text-info">{stats.pending || 0}</div>
				<div className="stat-desc">Awaiting check</div>
			</div>
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Checking</div>
				<div className="stat-value text-warning">{stats.checking || 0}</div>
				<div className="stat-desc">In progress</div>
			</div>
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Repairing</div>
				<div className="stat-value text-secondary">{stats.repair_triggered || 0}</div>
				<div className="stat-desc">Triggered</div>
			</div>
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Corrupted</div>
				<div className="stat-value text-error">{stats.corrupted}</div>
				<div className="stat-desc font-bold text-error">
					{corruptedPercentage}% - Require action
				</div>
			</div>
		</div>
	);
}
