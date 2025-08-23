import {
	AlertTriangle,
	Heart,
	MoreHorizontal,
	PlayCircle,
	RefreshCw,
	Shield,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingTable } from "../components/ui/LoadingSpinner";
import { HealthBadge } from "../components/ui/StatusBadge";
import {
	useAddHealthCheck,
	useCleanupHealth,
	useHealth,
	useDeleteHealthItem,
	useHealthStats,
	useHealthWorkerStatus,
	useRetryHealthItem,
	useTriggerManualHealthCheck,
} from "../hooks/useApi";
import { formatRelativeTime, truncateText } from "../lib/utils";
import { type FileHealth, HealthStatus } from "../types/api";

export function HealthPage() {
	const [page, setPage] = useState(0);
	const [searchTerm, setSearchTerm] = useState("");
	const [showAddHealthModal, setShowAddHealthModal] = useState(false);
	const [healthCheckForm, setHealthCheckForm] = useState({
		file_path: "",
		source_nzb_path: "",
		priority: false,
	});

	const pageSize = 20;
	const { data, isLoading, refetch, error } = useHealth({
		limit: pageSize,
		offset: page * pageSize,
	});

	const { data: stats } = useHealthStats();
	const { data: workerStatus } = useHealthWorkerStatus();
	const deleteItem = useDeleteHealthItem();
	const retryItem = useRetryHealthItem();
	const cleanupHealth = useCleanupHealth();
	const addHealthCheck = useAddHealthCheck();
	const triggerManualCheck = useTriggerManualHealthCheck();

	const handleDelete = async (filePath: string) => {
		if (confirm("Are you sure you want to delete this health record?")) {
			await deleteItem.mutateAsync(filePath);
		}
	};

	const handleRetry = async (filePath: string, resetStatus = false) => {
		await retryItem.mutateAsync({ id: filePath, resetStatus });
	};

	const handleCleanup = async () => {
		if (confirm("Are you sure you want to cleanup old health records?")) {
			await cleanupHealth.mutateAsync({
				older_than: new Date(
					Date.now() - 7 * 24 * 60 * 60 * 1000,
				).toISOString(),
			});
		}
	};

	const handleAddHealthCheck = async () => {
		if (!healthCheckForm.file_path.trim() || !healthCheckForm.source_nzb_path?.trim()) {
			alert("Please fill in both file path and source NZB path");
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

	const handleManualCheck = async (filePath: string, priority = false) => {
		try {
			await triggerManualCheck.mutateAsync({ filePath, priority });
		} catch (err) {
			console.error("Failed to trigger manual health check:", err);
		}
	};

	const filteredData = data?.filter(
		(item: FileHealth) =>
			!searchTerm ||
			item.file_path.toLowerCase().includes(searchTerm.toLowerCase()) ||
			item.source_nzb_path?.toLowerCase().includes(searchTerm.toLowerCase()),
	);

	if (error) {
		return (
			<div className="space-y-4">
				<h1 className="text-3xl font-bold">Health Monitoring</h1>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
				<div>
					<h1 className="text-3xl font-bold">Health Monitoring</h1>
					<p className="text-base-content/70">
						Monitor file integrity status - view all files being health checked
					</p>
				</div>
				<div className="flex gap-2">
					<button
						type="button"
						className="btn btn-outline"
						onClick={() => refetch()}
						disabled={isLoading}
					>
						<RefreshCw
							className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`}
						/>
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
				<div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Files Tracked</div>
						<div className="stat-value text-primary">{stats.total}</div>
						<div className="stat-desc">Issues being monitored</div>
					</div>
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Pending</div>
						<div className="stat-value text-info">{stats.pending || 0}</div>
						<div className="stat-desc">Awaiting check</div>
					</div>
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Partial</div>
						<div className="stat-value text-warning">{stats.partial}</div>
						<div className="stat-desc">Need attention</div>
					</div>
					<div className="stat bg-base-100 rounded-box shadow">
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
							<div className={`badge ${workerStatus.is_running ? 'badge-success' : 'badge-error'}`}>
								{workerStatus.is_running ? 'Running' : 'Stopped'}
							</div>
						</div>
						
						<div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mt-4">
							<div className="stat">
								<div className="stat-title">Queue Size</div>
								<div className="stat-value text-info">{workerStatus.queue_size}</div>
								<div className="stat-desc">
									{workerStatus.priority_queue_size > 0 && 
										`${workerStatus.priority_queue_size} priority`
									}
								</div>
							</div>
							<div className="stat">
								<div className="stat-title">Processed</div>
								<div className="stat-value text-success">{workerStatus.total_processed}</div>
								<div className="stat-desc">Total completed</div>
							</div>
							<div className="stat">
								<div className="stat-title">Failed</div>
								<div className="stat-value text-error">{workerStatus.total_failed}</div>
								<div className="stat-desc">Total failed</div>
							</div>
							<div className="stat">
								<div className="stat-title">Status</div>
								<div className="stat-value text-sm">
									{workerStatus.is_processing ? 
										<span className="loading loading-spinner loading-sm text-primary" /> : 
										'Idle'
									}
								</div>
								<div className="stat-desc">
									{workerStatus.current_file && 
										`Checking: ${truncateText(workerStatus.current_file.split('/').pop() || '', 20)}`
									}
								</div>
							</div>
						</div>
						
						{workerStatus.last_activity && (
							<div className="text-sm text-base-content/70 mt-2">
								Last activity: {formatRelativeTime(workerStatus.last_activity)}
							</div>
						)}
					</div>
				</div>
			)}

			{/* Filters and Search */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body">
					<div className="flex flex-col sm:flex-row gap-4">
						{/* Search */}
						<fieldset className="fieldset flex-1">
							<legend className="fieldset-legend">Search Files</legend>
							<input
								type="text"
								placeholder="Search files..."
								className="input"
								value={searchTerm}
								onChange={(e) => setSearchTerm(e.target.value)}
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
					) : filteredData && filteredData.length > 0 ? (
						<div>
							<table className="table table-zebra">
								<thead>
									<tr>
										<th>File Path</th>
										<th>Source NZB</th>
										<th>Status</th>
										<th>Retry Count</th>
										<th>Last Check</th>
										<th>Actions</th>
									</tr>
								</thead>
								<tbody>
									{filteredData.map((item: FileHealth) => (
										<tr key={item.id} className="hover">
											<td>
												<div className="flex items-center space-x-3">
													<Heart className="h-4 w-4 text-primary" />
													<div>
														<div className="font-bold">
															{truncateText(
																item.file_path.split("/").pop() || "",
																40,
															)}
														</div>
														<div className="text-sm text-base-content/70">
															{truncateText(item.file_path, 60)}
														</div>
													</div>
												</div>
											</td>
											<td>
												<div className="text-sm">
													{truncateText(
														item.source_nzb_path?.split("/").pop() || "",
														40,
													)}
												</div>
											</td>
											<td>
												<HealthBadge status={item.status} />
												{item.error_message && (
													<div className="text-xs text-error mt-1">
														{truncateText(item.error_message, 50)}
													</div>
												)}
											</td>
											<td>
												<span
													className={`badge ${item.retry_count > 0 ? "badge-warning" : "badge-ghost"}`}
												>
													{item.retry_count}
												</span>
											</td>
											<td>
												<span className="text-sm text-base-content/70">
													{item.last_check
														? formatRelativeTime(item.last_check)
														: "Never"}
												</span>
											</td>
											<td>
												<div className="dropdown dropdown-end">
													<button
														tabIndex={0}
														type="button"
														className="btn btn-ghost btn-sm"
													>
														<MoreHorizontal className="h-4 w-4" />
													</button>
													<ul className="dropdown-content menu bg-base-100 shadow-lg rounded-box w-48">
														<li>
															<button
																type="button"
																onClick={() => handleManualCheck(item.file_path)}
																disabled={triggerManualCheck.isPending}
															>
																<PlayCircle className="h-4 w-4" />
																Manual Check
															</button>
														</li>
														{(item.status === HealthStatus.CORRUPTED ||
															item.status === HealthStatus.PARTIAL) && (
															<>
																<li>
																	<hr />
																</li>
																<li>
																	<button
																		type="button"
																		onClick={() => handleRetry(item.file_path)}
																		disabled={retryItem.isPending}
																	>
																		<RefreshCw className="h-4 w-4" />
																		Retry Check
																	</button>
																</li>
																<li>
																	<button
																		type="button"
																		onClick={() =>
																			handleRetry(item.file_path, true)
																		}
																		disabled={retryItem.isPending}
																	>
																		<RefreshCw className="h-4 w-4" />
																		Reset & Check
																	</button>
																</li>
															</>
														)}
														<li>
															<hr />
														</li>
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
							<Shield className="h-12 w-12 text-base-content/30 mb-4" />
							<h3 className="text-lg font-semibold text-base-content/70">
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
			{filteredData && filteredData.length === pageSize && (
				<div className="flex justify-center">
					<div className="join">
						<button
							type="button"
							className="join-item btn"
							disabled={page === 0}
							onClick={() => setPage(page - 1)}
						>
							Previous
						</button>
						<button type="button" className="join-item btn btn-active">
							Page {page + 1}
						</button>
						<button
							type="button"
							className="join-item btn"
							onClick={() => setPage(page + 1)}
						>
							Next
						</button>
					</div>
				</div>
			)}

			{/* Health Status Alert */}
			{stats && stats.corrupted > 0 && (
				<div className="alert alert-error">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">File Integrity Issues Detected</div>
						<div className="text-sm">
							{stats.corrupted} corrupted files require immediate attention.
							{stats.partial > 0 &&
								` ${stats.partial} files have partial issues.`}
						</div>
					</div>
				</div>
			)}

			{/* Add Health Check Modal */}
			{showAddHealthModal && (
				<div className="modal modal-open">
					<div className="modal-box">
						<div className="flex items-center justify-between mb-4">
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
										setHealthCheckForm(prev => ({
											...prev,
											file_path: e.target.value
										}))
									}
								/>
								<p className="label text-sm text-base-content/70">
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
										setHealthCheckForm(prev => ({
											...prev,
											source_nzb_path: e.target.value
										}))
									}
								/>
								<p className="label text-sm text-base-content/70">
									Path to the original NZB file used for this download
								</p>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Priority</legend>
								<label className="cursor-pointer label">
									<span className="label-text">Process with high priority</span>
									<input
										type="checkbox"
										className="checkbox"
										checked={healthCheckForm.priority}
										onChange={(e) =>
											setHealthCheckForm(prev => ({
												...prev,
												priority: e.target.checked
											}))
										}
									/>
								</label>
								<p className="label text-sm text-base-content/70">
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
