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
	useCleanupHealth,
	useCorruptedFiles,
	useDeleteHealthItem,
	useHealthStats,
	useRetryHealthItem,
} from "../hooks/useApi";
import { formatRelativeTime, truncateText } from "../lib/utils";
import { type FileHealth, HealthStatus } from "../types/api";

export function HealthPage() {
	const [page, setPage] = useState(0);
	const [searchTerm, setSearchTerm] = useState("");

	const pageSize = 20;
	const { data, isLoading, refetch, error } = useCorruptedFiles({
		limit: pageSize,
		offset: page * pageSize,
	});

	const { data: stats } = useHealthStats();
	const deleteItem = useDeleteHealthItem();
	const retryItem = useRetryHealthItem();
	const cleanupHealth = useCleanupHealth();

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

	const filteredData = data?.filter(
		(item: FileHealth) =>
			!searchTerm ||
			item.file_path.toLowerCase().includes(searchTerm.toLowerCase()) ||
			item.source_nzb_path.toLowerCase().includes(searchTerm.toLowerCase()),
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
						Monitor file integrity - healthy files are automatically removed
						from tracking
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

			{/* Filters and Search */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body">
					<div className="flex flex-col sm:flex-row gap-4">
						{/* Search */}
						<div className="form-control flex-1">
							<div className="input-group">
								<input
									type="text"
									placeholder="Search files..."
									className="input input-bordered flex-1"
									value={searchTerm}
									onChange={(e) => setSearchTerm(e.target.value)}
								/>
							</div>
						</div>
					</div>
				</div>
			</div>

			{/* Health Table */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body p-0">
					{isLoading ? (
						<LoadingTable columns={6} />
					) : filteredData && filteredData.length > 0 ? (
						<div className="overflow-x-auto">
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
														item.source_nzb_path.split("/").pop() || "",
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
														{(item.status === HealthStatus.CORRUPTED ||
															item.status === HealthStatus.PARTIAL) && (
															<>
																<li>
																	<button
																		type="button"
																		onClick={() => handleRetry(item.file_path)}
																		disabled={retryItem.isPending}
																	>
																		<PlayCircle className="h-4 w-4" />
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
																<li>
																	<hr />
																</li>
															</>
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
							<Shield className="h-12 w-12 text-base-content/30 mb-4" />
							<h3 className="text-lg font-semibold text-base-content/70">
								No corrupted files found
							</h3>
							<p className="text-base-content/50">
								{searchTerm
									? "Try adjusting your filters"
									: "All your files are healthy!"}
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
		</div>
	);
}
