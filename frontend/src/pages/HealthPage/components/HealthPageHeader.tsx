import { Link, Pause, Play, RefreshCw, Trash2 } from "lucide-react";

interface HealthPageHeaderProps {
	autoRefreshEnabled: boolean;
	refreshInterval: number;
	countdown: number;
	userInteracting: boolean;
	isLoading: boolean;
	isCleanupPending: boolean;
	isRegenerateSymlinksPending: boolean;
	onToggleAutoRefresh: () => void;
	onRefreshIntervalChange: (interval: number) => void;
	onRefresh: () => void;
	onResetAll: () => void;
	onCleanup: () => void;
	onRegenerateSymlinks: () => void;
	onUserInteractionStart: () => void;
	onUserInteractionEnd: () => void;
}

export function HealthPageHeader({
	autoRefreshEnabled,
	refreshInterval,
	countdown,
	userInteracting,
	isLoading,
	isCleanupPending,
	isRegenerateSymlinksPending,
	onToggleAutoRefresh,
	onRefreshIntervalChange,
	onRefresh,
	onResetAll,
	onCleanup,
	onRegenerateSymlinks,
	onUserInteractionStart,
	onUserInteractionEnd,
}: HealthPageHeaderProps) {
	return (
		<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<h1 className="font-bold text-3xl">Health Monitoring</h1>
				<p className="text-base-content/70">
					Monitor file integrity status - view all files being health checked
					{autoRefreshEnabled && !userInteracting && countdown > 0 && (
						<span className="ml-2 text-info text-sm">• Auto-refresh in {countdown}s</span>
					)}
					{userInteracting && autoRefreshEnabled && (
						<span className="ml-2 text-sm text-warning">• Auto-refresh paused</span>
					)}
				</p>
			</div>
			<div className="flex flex-wrap gap-2">
				{/* Auto-refresh controls */}
				<div className="flex items-center gap-2">
					<button
						type="button"
						className={`btn btn-sm ${autoRefreshEnabled ? "btn-success" : "btn-outline"}`}
						onClick={onToggleAutoRefresh}
						title={autoRefreshEnabled ? "Disable auto-refresh" : "Enable auto-refresh"}
					>
						{autoRefreshEnabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
						Auto
					</button>

					{autoRefreshEnabled && (
						<select
							className="select select-sm"
							value={refreshInterval}
							onChange={(e) => onRefreshIntervalChange(Number(e.target.value))}
							onFocus={onUserInteractionStart}
							onBlur={onUserInteractionEnd}
						>
							<option value={5000}>5s</option>
							<option value={10000}>10s</option>
							<option value={30000}>30s</option>
							<option value={60000}>60s</option>
						</select>
					)}
				</div>

				<button type="button" className="btn btn-outline" onClick={onRefresh} disabled={isLoading}>
					<RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
					Refresh
				</button>
				<button type="button" className="btn btn-outline" onClick={onResetAll}>
					<RefreshCw className="h-4 w-4" />
					Reset All Checks
				</button>
				<button
					type="button"
					className="btn btn-primary"
					onClick={onRegenerateSymlinks}
					disabled={isRegenerateSymlinksPending}
				>
					<Link className="h-4 w-4" />
					Regenerate Symlinks
				</button>
				<button
					type="button"
					className="btn btn-warning"
					onClick={onCleanup}
					disabled={isCleanupPending}
				>
					<Trash2 className="h-4 w-4" />
					Cleanup Old Records
				</button>
			</div>
		</div>
	);
}
