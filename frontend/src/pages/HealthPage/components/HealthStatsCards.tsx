interface HealthStats {
	total: number;
	pending: number;
	corrupted: number;
}

interface HealthStatsCardsProps {
	stats: HealthStats | undefined;
}

export function HealthStatsCards({ stats }: HealthStatsCardsProps) {
	if (!stats) {
		return null;
	}

	return (
		<div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Files Tracked</div>
				<div className="stat-value text-primary">{stats.total}</div>
				<div className="stat-desc">Total in database</div>
			</div>
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Pending</div>
				<div className="stat-value text-info">{stats.pending || 0}</div>
				<div className="stat-desc">Awaiting check</div>
			</div>
			<div className="stat rounded-box bg-base-100 shadow">
				<div className="stat-title">Corrupted</div>
				<div className="stat-value text-error">{stats.corrupted}</div>
				<div className="stat-desc">Require action</div>
			</div>
		</div>
	);
}
