import {
	Download,
	Filter,
	MoreHorizontal,
	PlayCircle,
	RefreshCw,
	Search,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingTable } from "../components/ui/LoadingSpinner";
import { StatusBadge } from "../components/ui/StatusBadge";
import {
	useClearCompletedQueue,
	useDeleteQueueItem,
	useQueue,
	useQueueStats,
	useRetryQueueItem,
} from "../hooks/useApi";
import { formatRelativeTime, truncateText } from "../lib/utils";
import { type QueueItem, QueueStatus } from "../types/api";

export function QueuePage() {
	const [page, setPage] = useState(0);
	const [statusFilter, setStatusFilter] = useState<string>("");
	const [searchTerm, setSearchTerm] = useState("");

	const pageSize = 20;
	const {
		data: queueData,
		isLoading,
		error,
		refetch,
	} = useQueue({
		limit: pageSize,
		offset: page * pageSize,
		status: statusFilter || undefined,
	});

	const { data: stats } = useQueueStats();
	const deleteItem = useDeleteQueueItem();
	const retryItem = useRetryQueueItem();
	const clearCompleted = useClearCompletedQueue();

	const handleDelete = async (id: number) => {
		if (confirm("Are you sure you want to delete this queue item?")) {
			await deleteItem.mutateAsync(id);
		}
	};

	const handleRetry = async (id: number, resetRetryCount = false) => {
		await retryItem.mutateAsync({ id, resetRetryCount });
	};

	const handleClearCompleted = async () => {
		if (confirm("Are you sure you want to clear all completed items?")) {
			await clearCompleted.mutateAsync("");
		}
	};

	const filteredData = queueData?.filter(
		(item: QueueItem) =>
			!searchTerm ||
			item.nzb_path.toLowerCase().includes(searchTerm.toLowerCase()) ||
			item.target_path.toLowerCase().includes(searchTerm.toLowerCase()),
	);

	if (error) {
		return (
			<div className="space-y-4">
				<h1 className="text-3xl font-bold">Queue Management</h1>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
				<div>
					<h1 className="text-3xl font-bold">Queue Management</h1>
					<p className="text-base-content/70">
						Manage and monitor your download queue
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
					{stats && stats.completed > 0 && (
						<button
							type="button"
							className="btn btn-warning"
							onClick={handleClearCompleted}
							disabled={clearCompleted.isPending}
						>
							<Trash2 className="h-4 w-4" />
							Clear Completed
						</button>
					)}
				</div>
			</div>

			{/* Stats Cards */}
			{stats && (
				<div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Total</div>
						<div className="stat-value text-primary">{stats.total}</div>
					</div>
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Pending</div>
						<div className="stat-value text-warning">{stats.pending}</div>
					</div>
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Processing</div>
						<div className="stat-value text-info">{stats.processing}</div>
					</div>
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Completed</div>
						<div className="stat-value text-success">{stats.completed}</div>
					</div>
					<div className="stat bg-base-100 rounded-box shadow">
						<div className="stat-title">Failed</div>
						<div className="stat-value text-error">{stats.failed}</div>
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
								<span>
									<Search className="h-4 w-4" />
								</span>
								<input
									type="text"
									placeholder="Search queue items..."
									className="input input-bordered flex-1"
									value={searchTerm}
									onChange={(e) => setSearchTerm(e.target.value)}
								/>
							</div>
						</div>

						{/* Status Filter */}
						<div className="form-control">
							<div className="input-group">
								<span>
									<Filter className="h-4 w-4" />
								</span>
								<select
									className="select select-bordered"
									value={statusFilter}
									onChange={(e) => setStatusFilter(e.target.value)}
								>
									<option value="">All Status</option>
									<option value={QueueStatus.PENDING}>Pending</option>
									<option value={QueueStatus.PROCESSING}>Processing</option>
									<option value={QueueStatus.COMPLETED}>Completed</option>
									<option value={QueueStatus.FAILED}>Failed</option>
									<option value={QueueStatus.RETRYING}>Retrying</option>
								</select>
							</div>
						</div>
					</div>
				</div>
			</div>

			{/* Queue Table */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body p-0">
					{isLoading ? (
						<LoadingTable columns={6} />
					) : filteredData && filteredData.length > 0 ? (
						<div className="overflow-x-auto">
							<table className="table table-zebra">
								<thead>
									<tr>
										<th>NZB File</th>
										<th>Target Path</th>
										<th>Status</th>
										<th>Retry Count</th>
										<th>Updated</th>
										<th>Actions</th>
									</tr>
								</thead>
								<tbody>
									{filteredData.map((item: QueueItem) => (
										<tr key={item.id} className="hover">
											<td>
												<div className="flex items-center space-x-3">
													<Download className="h-4 w-4 text-primary" />
													<div>
														<div className="font-bold">
															{truncateText(
																item.nzb_path.split("/").pop() || "",
																40,
															)}
														</div>
														<div className="text-sm text-base-content/70">
															ID: {item.id}
														</div>
													</div>
												</div>
											</td>
											<td>
												<div className="text-sm">
													{truncateText(item.target_path, 50)}
												</div>
											</td>
											<td>
												<StatusBadge status={item.status} />
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
													{formatRelativeTime(item.updated_at)}
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
														{(item.status === QueueStatus.FAILED ||
															item.status === QueueStatus.COMPLETED) && (
															<>
																<li>
																	<button
																		type="button"
																		onClick={() => handleRetry(item.id)}
																		disabled={retryItem.isPending}
																	>
																		<PlayCircle className="h-4 w-4" />
																		Retry
																	</button>
																</li>
																<li>
																	<button
																		type="button"
																		onClick={() => handleRetry(item.id, true)}
																		disabled={retryItem.isPending}
																	>
																		<RefreshCw className="h-4 w-4" />
																		Reset & Retry
																	</button>
																</li>
															</>
														)}
														{item.status !== QueueStatus.PROCESSING && (
															<li>
																<button
																	type="button"
																	onClick={() => handleDelete(item.id)}
																	disabled={deleteItem.isPending}
																	className="text-error"
																>
																	<Trash2 className="h-4 w-4" />
																	Delete
																</button>
															</li>
														)}
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
							<Download className="h-12 w-12 text-base-content/30 mb-4" />
							<h3 className="text-lg font-semibold text-base-content/70">
								No queue items found
							</h3>
							<p className="text-base-content/50">
								{searchTerm || statusFilter
									? "Try adjusting your filters"
									: "Your queue is empty"}
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
		</div>
	);
}
