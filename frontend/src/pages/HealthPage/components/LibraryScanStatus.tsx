import { AlertTriangle, CheckCircle, Loader2, Play, RefreshCw, X } from "lucide-react";
import { formatFutureTime, formatRelativeTime } from "../../../lib/utils";

interface LibrarySyncProgress {
	processed_files: number;
	total_files: number;
	start_time?: string;
}

interface LibrarySyncResult {
	files_added: number;
	files_deleted: number;
	duration: number;
	completed_at: string;
}

interface LibrarySyncStatus {
	is_running: boolean;
	progress?: LibrarySyncProgress;
	last_sync_result?: LibrarySyncResult;
}

interface LibraryScanStatusProps {
	status: LibrarySyncStatus | undefined;
	isLoading: boolean;
	error: Error | null;
	isStartPending: boolean;
	isCancelPending: boolean;
	syncIntervalMinutes?: number;
	onStart: () => void;
	onCancel: () => void;
	onRetry: () => void;
}

export function LibraryScanStatus({
	status,
	isLoading,
	error,
	isStartPending,
	isCancelPending,
	syncIntervalMinutes,
	onStart,
	onCancel,
	onRetry,
}: LibraryScanStatusProps) {
	// Calculate next sync time
	const calculateNextSyncTime = (): Date | null => {
		if (
			!status ||
			status.is_running ||
			!status.last_sync_result ||
			!syncIntervalMinutes ||
			syncIntervalMinutes === 0
		) {
			return null;
		}

		const lastSyncTime = new Date(status.last_sync_result.completed_at);
		const nextSyncTime = new Date(lastSyncTime.getTime() + syncIntervalMinutes * 60 * 1000);
		return nextSyncTime;
	};

	const nextSyncTime = calculateNextSyncTime();
	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<h3 className="card-title">Library Scan Status</h3>

				{/* Loading State */}
				{isLoading && (
					<div className="flex items-center gap-2">
						<Loader2 className="h-5 w-5 animate-spin text-info" />
						<span>Loading scan status...</span>
					</div>
				)}

				{/* Error State */}
				{error && !isLoading && (
					<div className="alert alert-error">
						<AlertTriangle className="h-5 w-5" />
						<div>
							<div className="font-bold">Failed to load library sync status</div>
							<div className="text-sm">{error.message}</div>
						</div>
						<button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>
							<RefreshCw className="h-4 w-4" />
							Retry
						</button>
					</div>
				)}

				{/* Success State */}
				{!isLoading && !error && status && (
					<>
						<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
							<div className="flex-1">
								<div className="mt-2 flex items-center gap-2">
									{status.is_running ? (
										<>
											<Loader2 className="h-4 w-4 animate-spin text-info" />
											<span className="badge badge-info">Running</span>
										</>
									) : (
										<>
											<CheckCircle className="h-4 w-4 text-success" />
											<span className="badge badge-success">Idle</span>
										</>
									)}
								</div>
							</div>

							<div className="flex gap-2">
								<button
									type="button"
									className="btn btn-primary btn-sm"
									onClick={onStart}
									disabled={status.is_running || isStartPending}
								>
									{isStartPending ? (
										<Loader2 className="h-4 w-4 animate-spin" />
									) : (
										<Play className="h-4 w-4" />
									)}
									Start Scan
								</button>
								<button
									type="button"
									className="btn btn-error btn-sm"
									onClick={onCancel}
									disabled={!status.is_running || isCancelPending}
								>
									{isCancelPending ? (
										<Loader2 className="h-4 w-4 animate-spin" />
									) : (
										<X className="h-4 w-4" />
									)}
									Cancel
								</button>
							</div>
						</div>

						{/* Progress Bar */}
						{status.is_running && status.progress && (
							<div className="mt-4">
								<div className="mb-2 flex justify-between text-sm">
									<span>
										Scanning: {status.progress.processed_files} / {status.progress.total_files}{" "}
										files
									</span>
									<span>
										{status.progress.total_files > 0
											? Math.round(
													(status.progress.processed_files / status.progress.total_files) * 100,
												)
											: 0}
										%
									</span>
								</div>
								<progress
									className="progress progress-primary w-full"
									value={status.progress.processed_files}
									max={status.progress.total_files}
								/>
								{status.progress.start_time && (
									<div className="mt-1 text-sm">
										Elapsed: {formatRelativeTime(new Date(status.progress.start_time))}
									</div>
								)}
							</div>
						)}

						{/* Last Scan Result */}
						{!status.is_running && status.last_sync_result && (
							<div className="mt-4 rounded bg-base-200 p-3">
								<div className="font-semibold text-sm">Last Scan:</div>
								<div className="mt-1 flex flex-wrap gap-4 text-sm">
									<span>
										<strong>Added:</strong> {status.last_sync_result.files_added}
									</span>
									<span>
										<strong>Deleted:</strong> {status.last_sync_result.files_deleted}
									</span>
									<span>
										<strong>Duration:</strong> {(status.last_sync_result.duration / 1e9).toFixed(2)}
										s
									</span>
									<span>
										<strong>Completed:</strong>{" "}
										{formatRelativeTime(new Date(status.last_sync_result.completed_at))}
									</span>
								</div>
							</div>
						)}

						{/* Next Sync Information */}
						{!status.is_running && (
							<div className="mt-4 rounded bg-base-200 p-3">
								<div className="font-semibold text-sm">Next Scan:</div>
								<div className="mt-1 text-sm">
									{nextSyncTime ? (
										<span>
											<strong>Scheduled:</strong> {formatFutureTime(nextSyncTime)} (
											{nextSyncTime.toLocaleString()})
										</span>
									) : (
										<span className="text-base-content/70">
											{syncIntervalMinutes === 0
												? "Automatic sync disabled (interval set to 0)"
												: "Automatic sync not configured"}
										</span>
									)}
								</div>
							</div>
						)}
					</>
				)}
			</div>
		</div>
	);
}
