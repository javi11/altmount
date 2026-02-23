import {
	Activity,
	AlertTriangle,
	CheckCircle,
	Clock,
	Loader2,
	Play,
	RefreshCw,
	Search,
	X,
} from "lucide-react";
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
	variant?: "default" | "sidebar";
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
	variant = "default",
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

	if (variant === "sidebar") {
		return (
			<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
				<div className="card-body p-4">
					<div className="mb-3 flex items-center justify-between">
						<h3 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Library Sync
						</h3>
						{status?.is_running && (
							<span className="loading loading-ring loading-xs text-primary" />
						)}
					</div>

					{isLoading ? (
						<div className="flex items-center gap-2 text-base-content/80 text-xs">
							<Loader2 className="h-3 w-3 animate-spin" />
							<span>Loading...</span>
						</div>
					) : error ? (
						<div className="flex flex-col gap-2">
							<div className="flex items-center gap-2 text-error text-xs">
								<AlertTriangle className="h-3 w-3" />
								<span className="font-medium">Failed to load</span>
							</div>
							<button type="button" className="btn btn-ghost btn-sm w-full" onClick={onRetry}>
								Retry
							</button>
						</div>
					) : (
						<div className="space-y-4">
							<div className="flex items-center justify-between">
								<div className="flex items-center gap-2">
									{status?.is_running ? (
										<div className="badge badge-info badge-xs gap-1 py-2">
											<Activity className="h-2 w-2" /> RUNNING
										</div>
									) : (
										<div className="badge badge-success badge-outline badge-xs gap-1 py-2">
											<CheckCircle className="h-2 w-2" /> IDLE
										</div>
									)}
								</div>
								<div className="flex gap-1">
									{status?.is_running ? (
										<button
											type="button"
											className="btn btn-ghost btn-sm text-error"
											onClick={onCancel}
											disabled={isCancelPending}
										>
											<X className="h-3 w-3" />
										</button>
									) : (
										<button
											type="button"
											className="btn btn-ghost btn-sm text-primary"
											onClick={onStart}
											disabled={isStartPending}
										>
											<Play className="h-3 w-3" />
										</button>
									)}
								</div>
							</div>

							{status?.is_running && status.progress && (
								<div className="space-y-1.5">
									<div className="flex justify-between font-bold font-mono text-base-content/80 text-xs">
										<span>PROGRESS</span>
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
										className="progress progress-primary h-1 w-full"
										value={status.progress.processed_files}
										max={status.progress.total_files}
									/>
									<div className="text-base-content/70 text-xs">
										{status.progress.processed_files} / {status.progress.total_files} items
									</div>
								</div>
							)}

							{!status?.is_running && (
								<div className="space-y-3">
									{status?.last_sync_result && (
										<div className="rounded-lg bg-base-200/50 p-2 text-xs">
											<div className="mb-1 font-bold text-base-content/60 uppercase">LAST SCAN</div>
											<div className="flex flex-wrap gap-x-3 gap-y-1">
												<span>
													Added: <strong>{status.last_sync_result.files_added}</strong>
												</span>
												<span>
													Gone: <strong>{status.last_sync_result.files_deleted}</strong>
												</span>
												<span>
													Took:{" "}
													<strong>{(status.last_sync_result.duration / 1e9).toFixed(1)}s</strong>
												</span>
											</div>
											<div className="mt-1 text-base-content/80">
												{formatRelativeTime(new Date(status.last_sync_result.completed_at))}
											</div>
										</div>
									)}

									<div className="flex items-center gap-2 px-1 text-base-content/70 text-xs">
										<Clock className="h-3 w-3" />
										{nextSyncTime ? (
											<span>Next: {formatFutureTime(nextSyncTime)}</span>
										) : (
											<span>Auto-sync disabled</span>
										)}
									</div>
								</div>
							)}
						</div>
					)}
				</div>
			</div>
		);
	}

	return (
		<div className="card border border-base-200 bg-base-100 shadow-lg">
			<div className="card-body">
				<h3 className="card-title font-bold text-base-content/80 text-sm uppercase tracking-widest">
					Library Scan Status
				</h3>

				{/* Loading State */}
				{isLoading && (
					<div className="flex items-center gap-2 py-4">
						<Loader2 className="h-5 w-5 animate-spin text-primary" />
						<span className="text-sm">Loading scan status...</span>
					</div>
				)}

				{/* Error State */}
				{error && !isLoading && (
					<div className="alert alert-error mt-2">
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
					<div className="mt-2">
						<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
							<div className="flex-1">
								<div className="flex items-center gap-2">
									{status.is_running ? (
										<div className="badge badge-info gap-2 px-4 py-3 font-bold text-xs">
											<Loader2 className="h-3.5 w-3.5 animate-spin" /> RUNNING
										</div>
									) : (
										<div className="badge badge-success badge-outline gap-2 px-4 py-3 font-bold text-xs">
											<CheckCircle className="h-3.5 w-3.5" /> IDLE
										</div>
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
									className="btn btn-outline btn-error btn-sm"
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
							<div className="mt-6 space-y-3">
								<div className="flex justify-between font-bold font-mono text-base-content/80 text-xs">
									<span>
										SCANNING: {status.progress.processed_files} / {status.progress.total_files}{" "}
										ITEMS
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
									className="progress progress-primary h-2 w-full"
									value={status.progress.processed_files}
									max={status.progress.total_files}
								/>
								{status.progress.start_time && (
									<div className="flex items-center gap-1.5 text-base-content/70 text-xs">
										<Clock className="h-3 w-3" />
										Elapsed: {formatRelativeTime(new Date(status.progress.start_time))}
									</div>
								)}
							</div>
						)}

						{/* Last Scan Result */}
						{!status.is_running && (
							<div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2">
								{status.last_sync_result && (
									<div className="rounded-xl border-2 border-base-300/80 bg-base-200/60 p-4">
										<div className="mb-3 flex items-center gap-2 font-bold text-base-content/60 text-xs uppercase tracking-widest">
											<Search className="h-3 w-3" />
											Last Scan Results
										</div>
										<div className="grid grid-cols-2 gap-x-2 gap-y-3 text-xs">
											<div className="flex flex-col">
												<span className="font-bold text-base-content/70 text-xs uppercase">Added</span>
												<span className="font-mono text-lg">
													{status.last_sync_result.files_added}
												</span>
											</div>
											<div className="flex flex-col">
												<span className="font-bold text-base-content/70 text-xs uppercase">Deleted</span>
												<span className="font-mono text-lg">
													{status.last_sync_result.files_deleted}
												</span>
											</div>
											<div className="flex flex-col">
												<span className="font-bold text-base-content/70 text-xs uppercase">Duration</span>
												<span className="font-mono">
													{(status.last_sync_result.duration / 1e9).toFixed(2)}s
												</span>
											</div>
											<div className="flex flex-col">
												<span className="font-bold text-base-content/70 text-xs uppercase">Completed</span>
												<span className="font-mono">
													{formatRelativeTime(new Date(status.last_sync_result.completed_at))}
												</span>
											</div>
										</div>
									</div>
								)}

								<div className="rounded-xl border-2 border-base-300/80 bg-base-200/60 p-4">
									<div className="mb-3 flex items-center gap-2 font-bold text-base-content/60 text-xs uppercase tracking-widest">
										<Clock className="h-3 w-3" />
										Next Scheduled Scan
									</div>
									{nextSyncTime ? (
										<div className="space-y-1">
											<div className="font-semibold text-primary text-sm">
												{formatFutureTime(nextSyncTime)}
											</div>
											<div className="font-mono text-base-content/70 text-xs">
												{nextSyncTime.toLocaleString()}
											</div>
										</div>
									) : (
										<div className="py-2 text-base-content/70 text-xs italic">
											{syncIntervalMinutes === 0
												? "Automatic sync disabled (interval set to 0)"
												: "Automatic sync not configured"}
										</div>
									)}
								</div>
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
