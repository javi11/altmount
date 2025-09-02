import {
	AlertTriangle,
	Heart,
	MoreHorizontal,
	Pause,
	Play,
	PlayCircle,
	RefreshCw,
	Shield,
	Trash2,
	Wrench,
	X,
} from "lucide-react";
import { useEffect, useState } from "react";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingTable } from "../components/ui/LoadingSpinner";
import { Pagination } from "../components/ui/Pagination";
import { HealthBadge } from "../components/ui/StatusBadge";
import { useConfirm } from "../contexts/ModalContext";
import { useToast } from "../contexts/ToastContext";
import {
	useAddHealthCheck,
	useCancelHealthCheck,
	useCleanupHealth,
	useDeleteHealthItem,
	useDirectHealthCheck,
	useHealth,
	useHealthStats,
	useHealthWorkerStatus,
	useRepairHealthItem,
} from "../hooks/useApi";
import { formatRelativeTime, truncateText } from "../lib/utils";
import type { FileHealth } from "../types/api";

export function HealthPage() {
	const [page, setPage] = useState(0);
	const [searchTerm, setSearchTerm] = useState("");
	const [showAddHealthModal, setShowAddHealthModal] = useState(false);
	const [healthCheckForm, setHealthCheckForm] = useState({
		file_path: "",
		source_nzb_path: "",
		priority: false,
	});
	const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true);
	const [refreshInterval, setRefreshInterval] = useState(5000); // 5 seconds default
	const [nextRefreshTime, setNextRefreshTime] = useState<Date | null>(null);
	const [userInteracting, setUserInteracting] = useState(false);
	const [countdown, setCountdown] = useState(0);

	const pageSize = 20;
	const {
		data: healthResponse,
		isLoading,
		refetch,
		error,
	} = useHealth({
		limit: pageSize,
		offset: page * pageSize,
		search: searchTerm,
		refetchInterval: autoRefreshEnabled && !userInteracting ? refreshInterval : undefined,
	});

	const { data: stats } = useHealthStats();
	const { data: workerStatus } = useHealthWorkerStatus();
	const deleteItem = useDeleteHealthItem();
	const cleanupHealth = useCleanupHealth();
	const addHealthCheck = useAddHealthCheck();
	const directHealthCheck = useDirectHealthCheck();
	const cancelHealthCheck = useCancelHealthCheck();
	const repairHealthItem = useRepairHealthItem();
	const { confirmDelete, confirmAction } = useConfirm();
	const { showToast } = useToast();

	const handleDelete = async (filePath: string) => {
		const confirmed = await confirmDelete("health record");
		if (confirmed) {
			await deleteItem.mutateAsync(filePath);
		}
	};

	const handleCleanup = async () => {
		const confirmed = await confirmAction(
			"Cleanup Old Health Records",
			"Are you sure you want to cleanup old health records? This will remove records older than 7 days.",
			{
				type: "warning",
				confirmText: "Cleanup",
				confirmButtonClass: "btn-warning",
			},
		);
		if (confirmed) {
			await cleanupHealth.mutateAsync({
				older_than: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(),
			});
		}
	};

	const handleAddHealthCheck = async () => {
		if (!healthCheckForm.file_path.trim() || !healthCheckForm.source_nzb_path?.trim()) {
			showToast({
				type: "warning",
				title: "Missing Required Fields",
				message: "Please fill in both file path and source NZB path",
			});
			return;
		}

		try {
			await addHealthCheck.mutateAsync(healthCheckForm);
			setShowAddHealthModal(false);
			setHealthCheckForm({
				file_path: "",
				source_nzb_path: "",
				priority: false,
			});
		} catch (err) {
			console.error("Failed to add health check:", err);
		}
	};

	const handleManualCheck = async (filePath: string) => {
		try {
			await directHealthCheck.mutateAsync(filePath);
		} catch (err) {
			console.error("Failed to perform direct health check:", err);
		}
	};

	const handleCancelCheck = async (filePath: string) => {
		const confirmed = await confirmAction(
			"Cancel Health Check",
			"Are you sure you want to cancel this health check?",
			{
				type: "warning",
				confirmText: "Cancel Check",
				confirmButtonClass: "btn-warning",
			},
		);
		if (confirmed) {
			try {
				await cancelHealthCheck.mutateAsync(filePath);
			} catch (err) {
				console.error("Failed to cancel health check:", err);
			}
		}
	};

	const handleRepair = async (filePath: string) => {
		const confirmed = await confirmAction(
			"Trigger Repair",
			"This will attempt to redownload the corrupted file from your media library. Are you sure you want to proceed?",
			{
				type: "info",
				confirmText: "Trigger Repair",
				confirmButtonClass: "btn-info",
			},
		);
		if (confirmed) {
			try {
				await repairHealthItem.mutateAsync({ 
					id: filePath, 
					resetRepairRetryCount: false 
				});
				showToast({
					title: "Repair Triggered",
					message: "Repair triggered successfully",
					type: "success",
				});
			} catch (err: unknown) {
				const error = err as { 
					message?: string; 
					response?: { 
						data?: { 
							error?: { 
								message?: string;
								details?: string;
							} 
						} 
					} 
				};
				console.error("Failed to trigger repair:", err);
				
				// Get error message from response or direct error
				const apiErrorMessage = error.response?.data?.error?.message;
				const apiErrorDetails = error.response?.data?.error?.details;
				const errorMessage = apiErrorMessage || error.message || "Unknown error";
				
				// Handle specific error cases
				if (errorMessage.includes("Repair not available")) {
					showToast({
						title: "Repair not available",
						message: apiErrorDetails || "File not found in media library",
						type: "error",
					});
				} else if (errorMessage.includes("Media library not configured")) {
					showToast({
						title: "Configuration Required",
						message: "Media library must be configured to use repair functionality",
						type: "error",
					});
				} else if (errorMessage.includes("Media library error")) {
					showToast({
						title: "Media Library Error",
						message: "Unable to access media library to verify file availability",
						type: "error",
					});
				} else {
					showToast({
						title: "Failed to trigger repair",
						message: errorMessage,
						type: "error",
					});
				}
			}
		}
	};

	const toggleAutoRefresh = () => {
		setAutoRefreshEnabled(!autoRefreshEnabled);
		setNextRefreshTime(null);
	};

	const handleRefreshIntervalChange = (interval: number) => {
		setRefreshInterval(interval);
		setNextRefreshTime(null);
	};

	// Pause auto-refresh during user interactions
	const handleUserInteractionStart = () => {
		setUserInteracting(true);
	};

	const handleUserInteractionEnd = () => {
		// Resume auto-refresh after a short delay
		const timer = setTimeout(() => {
			setUserInteracting(false);
		}, 2000); // 2 second delay before resuming auto-refresh

		return () => clearTimeout(timer);
	};

	const data = healthResponse?.data;
	const meta = healthResponse?.meta;

	// Update next refresh time when auto-refresh is enabled
	useEffect(() => {
		if (!autoRefreshEnabled || userInteracting) {
			setNextRefreshTime(null);
			return;
		}
		
		// Set initial next refresh time
		setNextRefreshTime(new Date(Date.now() + refreshInterval));
		
		// Reset the timer every time React Query refetches
		const interval = setInterval(() => {
			setNextRefreshTime(new Date(Date.now() + refreshInterval));
		}, refreshInterval);

		return () => clearInterval(interval);
	}, [autoRefreshEnabled, refreshInterval, userInteracting]);

	// Update countdown timer every second
	useEffect(() => {
		if (!nextRefreshTime || !autoRefreshEnabled || userInteracting) {
			setCountdown(0);
			return;
		}
		
		const updateCountdown = () => {
			const remaining = Math.max(0, Math.ceil((nextRefreshTime.getTime() - Date.now()) / 1000));
			setCountdown(remaining);
			
			// If countdown reaches 0, reset to the full interval (handles any sync issues)
			if (remaining === 0) {
				setNextRefreshTime(new Date(Date.now() + refreshInterval));
			}
		};

		// Initial countdown update
		updateCountdown();
		const timer = setInterval(updateCountdown, 1000);

		return () => clearInterval(timer);
	}, [nextRefreshTime, autoRefreshEnabled, userInteracting, refreshInterval]);

	// Reset page when search term changes
	useEffect(() => {
		if (searchTerm !== "") {
			setPage(0);
		}
	}, [searchTerm]);

	if (error) {
		return (
			<div className="space-y-4">
				<h1 className="font-bold text-3xl">Health Monitoring</h1>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
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
							onClick={toggleAutoRefresh}
							title={autoRefreshEnabled ? "Disable auto-refresh" : "Enable auto-refresh"}
						>
							{autoRefreshEnabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
							Auto
						</button>

						{autoRefreshEnabled && (
							<select
								className="select select-sm"
								value={refreshInterval}
								onChange={(e) => handleRefreshIntervalChange(Number(e.target.value))}
								onFocus={handleUserInteractionStart}
								onBlur={handleUserInteractionEnd}
							>
								<option value={5000}>5s</option>
								<option value={10000}>10s</option>
								<option value={30000}>30s</option>
								<option value={60000}>60s</option>
							</select>
						)}
					</div>

					<button
						type="button"
						className="btn btn-outline"
						onClick={() => refetch()}
						disabled={isLoading}
					>
						<RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
						Refresh
					</button>
					<button
						type="button"
						className="btn btn-warning"
						onClick={handleCleanup}
						disabled={cleanupHealth.isPending}
					>
						<Trash2 className="h-4 w-4" />
						Cleanup Old Records
					</button>
				</div>
			</div>

			{/* Stats Cards */}
			{stats && (
				<div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Files Tracked</div>
						<div className="stat-value text-primary">{stats.total}</div>
						<div className="stat-desc">Issues being monitored</div>
					</div>
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Pending</div>
						<div className="stat-value text-info">{stats.pending || 0}</div>
						<div className="stat-desc">Awaiting check</div>
					</div>
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Partial</div>
						<div className="stat-value text-warning">{stats.partial}</div>
						<div className="stat-desc">Need attention</div>
					</div>
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Corrupted</div>
						<div className="stat-value text-error">{stats.corrupted}</div>
						<div className="stat-desc">Require action</div>
					</div>
				</div>
			)}

			{/* Health Worker Status */}
			{workerStatus && (
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<div className="flex items-center justify-between">
							<h2 className="card-title">Health Worker Status</h2>
							<div
								className={`badge ${workerStatus.status === "running" ? "badge-success" : "badge-error"}`}
							>
								{workerStatus.status === "running" ? "Running" : "Stopped"}
							</div>
						</div>

						<div className="mt-4 grid grid-cols-2 gap-4 lg:grid-cols-4">
							<div className="stat">
								<div className="stat-title">Manual Checks</div>
								<div className="stat-value text-info">{workerStatus.pending_manual_checks}</div>
								<div className="stat-desc">Pending checks</div>
							</div>
							<div className="stat">
								<div className="stat-title">Files Checked</div>
								<div className="stat-value text-success">{workerStatus.total_files_checked}</div>
								<div className="stat-desc">Total checked</div>
							</div>
							<div className="stat">
								<div className="stat-title">Corrupted</div>
								<div className="stat-value text-error">{workerStatus.total_files_corrupted}</div>
								<div className="stat-desc">Files corrupted</div>
							</div>
							<div className="stat">
								<div className="stat-title">Runs</div>
								<div className="stat-value text-sm">{workerStatus.total_runs_completed}</div>
								<div className="stat-desc">Cycles completed</div>
							</div>
						</div>

						{workerStatus.last_run_time && (
							<div className="mt-2 text-base-content/70 text-sm">
								Last run: {formatRelativeTime(workerStatus.last_run_time)}
							</div>
						)}
					</div>
				</div>
			)}

			{/* Filters and Search */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body">
					<div className="flex flex-col gap-4 sm:flex-row">
						{/* Search */}
						<fieldset className="fieldset flex-1">
							<legend className="fieldset-legend">Search Files</legend>
							<input
								type="text"
								placeholder="Search files..."
								className="input"
								value={searchTerm}
								onChange={(e) => setSearchTerm(e.target.value)}
								onFocus={handleUserInteractionStart}
								onBlur={handleUserInteractionEnd}
							/>
						</fieldset>
					</div>
				</div>
			</div>

			{/* Health Table */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body p-0">
					{isLoading ? (
						<LoadingTable columns={6} />
					) : data && data.length > 0 ? (
						<div>
							<table className="table-zebra table">
								<thead>
									<tr>
										<th>File Path</th>
										<th>Source NZB</th>
										<th>Status</th>
										<th>Retries (H/R)</th>
										<th>Last Check</th>
										<th>Actions</th>
									</tr>
								</thead>
								<tbody>
									{data.map((item: FileHealth) => (
										<tr key={item.id} className="hover">
											<td>
												<div className="flex items-center space-x-3">
													<Heart className="h-4 w-4 text-primary" />
													<div>
														<div className="font-bold">
															{truncateText(item.file_path.split("/").pop() || "", 40)}
														</div>
														<div
															className="tooltip text-base-content/70 text-sm"
															data-tip={item.file_path}
														>
															{truncateText(item.file_path, 60)}
														</div>
													</div>
												</div>
											</td>
											<td>
												<div className="tooltip text-sm" data-tip={item.source_nzb_path}>
													{truncateText(item.source_nzb_path?.split("/").pop() || "", 40)}
												</div>
											</td>
											<td>
												<HealthBadge status={item.status} />
												{/* Show last_error for repair failures and general errors */}
												{item.last_error && (
													<div className="mt-1">
														<div className="tooltip tooltip-bottom text-left" data-tip={item.last_error}>
															<div className="cursor-help text-error text-xs">
																{truncateText(item.last_error, 50)}
															</div>
														</div>
													</div>
												)}
												{/* Show error_details for additional technical details */}
												{item.error_details && item.error_details !== item.last_error && (
													<div className="mt-1">
														<div className="tooltip tooltip-bottom text-left" data-tip={item.error_details}>
															<div className="cursor-help text-warning text-xs">
																Technical: {truncateText(item.error_details, 40)}
															</div>
														</div>
													</div>
												)}
											</td>
											<td>
												<div className="flex flex-col gap-1">
													<span
														className={`badge badge-sm ${item.retry_count > 0 ? "badge-warning" : "badge-ghost"}`}
														title="Health check retries"
													>
														H: {item.retry_count}/{item.max_retries}
													</span>
													{(item.status === "repair_triggered" || item.repair_retry_count > 0) && (
														<span
															className={`badge badge-sm ${item.repair_retry_count > 0 ? "badge-info" : "badge-ghost"}`}
															title="Repair retries"
														>
															R: {item.repair_retry_count}/{item.max_repair_retries}
														</span>
													)}
												</div>
											</td>
											<td>
												<span className="text-base-content/70 text-sm">
													{item.last_checked ? formatRelativeTime(item.last_checked) : "Never"}
												</span>
											</td>
											<td>
												<div className="dropdown dropdown-end">
													<button tabIndex={0} type="button" className="btn btn-ghost btn-sm">
														<MoreHorizontal className="h-4 w-4" />
													</button>
													<ul className="dropdown-content menu w-48 rounded-box bg-base-100 shadow-lg">
														{item.status === "checking" ? (
															<li>
																<button
																	type="button"
																	onClick={() => handleCancelCheck(item.file_path)}
																	disabled={cancelHealthCheck.isPending}
																	className="text-warning"
																>
																	<X className="h-4 w-4" />
																	Cancel Check
																</button>
															</li>
														) : (
															<li>
																<button
																	type="button"
																	onClick={() => handleManualCheck(item.file_path)}
																	disabled={directHealthCheck.isPending}
																>
																	<PlayCircle className="h-4 w-4" />
																	Retry Check
																</button>
															</li>
														)}
														{item.status === "corrupted" && (
															<li>
																{/* Check if repair is available based on error message */}
																{item.last_error && 
																 (item.last_error.includes("Cannot repair: File not found in media library") ||
																  item.last_error.includes("Cannot repair: Media library not configured") ||
																  item.last_error.includes("Failed to check media library")) ? (
																	<div className="tooltip tooltip-left" data-tip={item.last_error}>
																		<button
																			type="button"
																			disabled={true}
																			className="w-full cursor-not-allowed text-left text-base-content/50"
																		>
																			<Wrench className="h-4 w-4" />
																			Repair Not Available
																		</button>
																	</div>
																) : (
																	<button
																		type="button"
																		onClick={() => handleRepair(item.file_path)}
																		disabled={repairHealthItem.isPending}
																		className="text-info"
																	>
																		<Wrench className="h-4 w-4" />
																		Trigger Repair
																	</button>
																)}
															</li>
														)}
														<li>
															<button
																type="button"
																onClick={() => handleDelete(item.file_path)}
																disabled={deleteItem.isPending}
																className="text-error"
															>
																<Trash2 className="h-4 w-4" />
																Delete Record
															</button>
														</li>
													</ul>
												</div>
											</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					) : (
						<div className="flex flex-col items-center justify-center py-12">
							<Shield className="mb-4 h-12 w-12 text-base-content/30" />
							<h3 className="font-semibold text-base-content/70 text-lg">
								No health records found
							</h3>
							<p className="text-base-content/50">
								{searchTerm
									? "Try adjusting your filters"
									: "No files are currently being health checked"}
							</p>
						</div>
					)}
				</div>
			</div>

			{/* Pagination */}
			{meta?.total && meta.total > pageSize && (
				<Pagination
					currentPage={page + 1}
					totalPages={Math.ceil(meta.total / pageSize)}
					onPageChange={(newPage) => setPage(newPage - 1)}
					totalItems={meta.total}
					itemsPerPage={pageSize}
					showSummary={true}
				/>
			)}

			{/* Health Status Alert */}
			{stats && stats.corrupted > 0 && (
				<div className="alert alert-error">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">File Integrity Issues Detected</div>
						<div className="text-sm">
							{stats.corrupted} corrupted files require immediate attention.
							{stats.partial > 0 && ` ${stats.partial} files have partial issues.`}
						</div>
					</div>
				</div>
			)}

			{/* Add Health Check Modal */}
			{showAddHealthModal && (
				<div className="modal modal-open">
					<div className="modal-box">
						<div className="mb-4 flex items-center justify-between">
							<h3 className="font-bold text-lg">Add Manual Health Check</h3>
							<button
								type="button"
								className="btn btn-sm btn-circle btn-ghost"
								onClick={() => setShowAddHealthModal(false)}
							>
								✕
							</button>
						</div>

						<div className="space-y-4">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">File Path</legend>
								<input
									type="text"
									className="input"
									placeholder="/path/to/file.mkv"
									value={healthCheckForm.file_path}
									onChange={(e) =>
										setHealthCheckForm((prev) => ({
											...prev,
											file_path: e.target.value,
										}))
									}
								/>
								<p className="label text-base-content/70 text-sm">
									Full path to the file that needs health checking
								</p>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Source NZB Path</legend>
								<input
									type="text"
									className="input"
									placeholder="/path/to/source.nzb"
									value={healthCheckForm.source_nzb_path}
									onChange={(e) =>
										setHealthCheckForm((prev) => ({
											...prev,
											source_nzb_path: e.target.value,
										}))
									}
								/>
								<p className="label text-base-content/70 text-sm">
									Path to the original NZB file used for this download
								</p>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Priority</legend>
								<label className="label cursor-pointer">
									<span className="label-text">Process with high priority</span>
									<input
										type="checkbox"
										className="checkbox"
										checked={healthCheckForm.priority}
										onChange={(e) =>
											setHealthCheckForm((prev) => ({
												...prev,
												priority: e.target.checked,
											}))
										}
									/>
								</label>
								<p className="label text-base-content/70 text-sm">
									Priority checks are processed before normal queue items
								</p>
							</fieldset>
						</div>

						<div className="modal-action">
							<button
								type="button"
								className="btn btn-ghost"
								onClick={() => setShowAddHealthModal(false)}
							>
								Cancel
							</button>
							<button
								type="button"
								className="btn btn-primary"
								onClick={handleAddHealthCheck}
								disabled={addHealthCheck.isPending}
							>
								{addHealthCheck.isPending ? (
									<>
										<span className="loading loading-spinner loading-sm" />
										Adding...
									</>
								) : (
									"Add Health Check"
								)}
							</button>
						</div>
					</div>
				</div>
			)}
		</div>
	);
}
