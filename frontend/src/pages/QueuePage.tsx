import {
	AlertCircle,
	Download,
	MoreHorizontal,
	Pause,
	Play,
	PlayCircle,
	RefreshCw,
	Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { DragDropUpload } from "../components/queue/DragDropUpload";
import { ManualScanSection } from "../components/queue/ManualScanSection";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingTable } from "../components/ui/LoadingSpinner";
import { Pagination } from "../components/ui/Pagination";
import { PathDisplay } from "../components/ui/PathDisplay";
import { StatusBadge } from "../components/ui/StatusBadge";
import { useConfirm } from "../contexts/ModalContext";
import {
	useClearCompletedQueue,
	useDeleteBulkQueueItems,
	useDeleteQueueItem,
	useQueue,
	useQueueStats,
	useRetryQueueItem,
} from "../hooks/useApi";
import { formatBytes, formatRelativeTime, truncateText } from "../lib/utils";
import { type QueueItem, QueueStatus } from "../types/api";

export function QueuePage() {
	const [page, setPage] = useState(0);
	const [statusFilter, setStatusFilter] = useState<string>("");
	const [searchTerm, setSearchTerm] = useState("");
	const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true);
	const [refreshInterval, setRefreshInterval] = useState(5000); // 5 seconds default
	const [nextRefreshTime, setNextRefreshTime] = useState<Date | null>(null);
	const [userInteracting, setUserInteracting] = useState(false);
	const [countdown, setCountdown] = useState(0);
	const [selectedItems, setSelectedItems] = useState<Set<number>>(new Set());

	const pageSize = 20;
	const {
		data: queueResponse,
		isLoading,
		error,
		refetch,
	} = useQueue({
		limit: pageSize,
		offset: page * pageSize,
		status: statusFilter || undefined,
		search: searchTerm || undefined,
		refetchInterval: autoRefreshEnabled && !userInteracting ? refreshInterval : undefined,
	});

	const queueData = queueResponse?.data;
	const meta = queueResponse?.meta;
	const totalPages = meta?.total ? Math.ceil(meta.total / pageSize) : 0;

	const { data: stats } = useQueueStats();
	const deleteItem = useDeleteQueueItem();
	const deleteBulk = useDeleteBulkQueueItems();
	const retryItem = useRetryQueueItem();
	const clearCompleted = useClearCompletedQueue();
	const { confirmDelete, confirmAction } = useConfirm();

	const handleDelete = async (id: number) => {
		const confirmed = await confirmDelete("queue item");
		if (confirmed) {
			await deleteItem.mutateAsync(id);
		}
	};

	const handleRetry = async (id: number) => {
		await retryItem.mutateAsync(id);
	};

	const handleClearCompleted = async () => {
		const confirmed = await confirmAction(
			"Clear Completed Items",
			"Are you sure you want to clear all completed items? This action cannot be undone.",
			{
				type: "warning",
				confirmText: "Clear All",
				confirmButtonClass: "btn-warning",
			},
		);
		if (confirmed) {
			await clearCompleted.mutateAsync("");
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

	// Multi-select handlers
	const handleSelectItem = (id: number, checked: boolean) => {
		setSelectedItems(prev => {
			const newSet = new Set(prev);
			if (checked) {
				newSet.add(id);
			} else {
				newSet.delete(id);
			}
			return newSet;
		});
	};

	const handleSelectAll = (checked: boolean) => {
		if (checked && queueData) {
			setSelectedItems(new Set(queueData.map(item => item.id)));
		} else {
			setSelectedItems(new Set());
		}
	};

	const handleBulkDelete = async () => {
		if (selectedItems.size === 0) return;
		
		const confirmed = await confirmAction(
			"Delete Selected Items",
			`Are you sure you want to delete ${selectedItems.size} selected queue items? This action cannot be undone.`,
			{
				type: "warning",
				confirmText: "Delete Selected",
				confirmButtonClass: "btn-error",
			}
		);
		
		if (confirmed) {
			try {
				const itemIds = Array.from(selectedItems);
				await deleteBulk.mutateAsync(itemIds);
				setSelectedItems(new Set());
			} catch (error) {
				console.error("Failed to delete selected items:", error);
			}
		}
	};

	// Clear selection when page changes or filters change
	const clearSelection = useCallback(() => {
		setSelectedItems(new Set());
	}, []);

	// Helper functions for select all checkbox state
	const isAllSelected = queueData && queueData.length > 0 && queueData.every(item => selectedItems.has(item.id));
	const isIndeterminate = queueData && selectedItems.size > 0 && !isAllSelected;

	// Update next refresh time when auto-refresh is enabled
	useEffect(() => {
		if (autoRefreshEnabled && !userInteracting) {
			// Set initial next refresh time
			setNextRefreshTime(new Date(Date.now() + refreshInterval));
			
			// Reset the timer every time React Query refetches
			const interval = setInterval(() => {
				setNextRefreshTime(new Date(Date.now() + refreshInterval));
			}, refreshInterval);

			return () => clearInterval(interval);
		}
			setNextRefreshTime(null);
	}, [autoRefreshEnabled, refreshInterval, userInteracting]);

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

	// Update countdown timer every second
	useEffect(() => {
		if (nextRefreshTime && autoRefreshEnabled && !userInteracting) {
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
		}
			setCountdown(0);
	}, [nextRefreshTime, autoRefreshEnabled, userInteracting, refreshInterval]);

	// Reset to page 1 when search or status filter changes
	useEffect(() => {
		setPage(0);
	}, []);

	// Clear selection when page, search, or filters change
	useEffect(() => {
		clearSelection();
	}, [clearSelection]);

	if (error) {
		return (
			<div className="space-y-4">
				<h1 className="font-bold text-3xl">Queue Management</h1>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<h1 className="font-bold text-3xl">Queue Management</h1>
					<p className="text-base-content/70">
						Manage and monitor your download queue
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
					{stats && stats.total_completed > 0 && (
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

			{/* Manual Scan Section */}
			<ManualScanSection />

			{/* Drag & Drop Upload Section */}
			<DragDropUpload />

			{/* Stats Cards */}
			{stats && (
				<div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Total</div>
						<div className="stat-value text-primary">{stats.total_completed}</div>
					</div>
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Pending</div>
						<div className="stat-value text-warning">{stats.total_queued}</div>
					</div>
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Processing</div>
						<div className="stat-value text-info">{stats.total_processing}</div>
					</div>
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Completed</div>
						<div className="stat-value text-success">{stats.total_completed}</div>
					</div>
					<div className="stat rounded-box bg-base-100 shadow">
						<div className="stat-title">Failed</div>
						<div className="stat-value text-error">{stats.total_failed}</div>
					</div>
				</div>
			)}

			{/* Filters and Search */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body">
					<div className="flex flex-col gap-4 sm:flex-row">
						{/* Search */}
						<fieldset className="fieldset flex-1">
							<legend className="fieldset-legend">Search Queue Items</legend>
							<input
								type="text"
								placeholder="Search queue items..."
								className="input"
								value={searchTerm}
								onChange={(e) => setSearchTerm(e.target.value)}
								onFocus={handleUserInteractionStart}
								onBlur={handleUserInteractionEnd}
							/>
						</fieldset>

						{/* Status Filter */}
						<fieldset className="fieldset">
							<legend className="fieldset-legend">Filter by Status</legend>
							<select
								className="select"
								value={statusFilter}
								onChange={(e) => setStatusFilter(e.target.value)}
								onFocus={handleUserInteractionStart}
								onBlur={handleUserInteractionEnd}
							>
								<option value="">All Status</option>
								<option value={QueueStatus.PENDING}>Pending</option>
								<option value={QueueStatus.PROCESSING}>Processing</option>
								<option value={QueueStatus.COMPLETED}>Completed</option>
								<option value={QueueStatus.FAILED}>Failed</option>
								<option value={QueueStatus.RETRYING}>Retrying</option>
							</select>
						</fieldset>
					</div>
				</div>
			</div>

			{/* Bulk Actions Toolbar */}
			{selectedItems.size > 0 && (
				<div className="card bg-base-100 shadow-lg">
					<div className="card-body">
						<div className="flex items-center justify-between">
							<div className="flex items-center gap-4">
								<span className="font-semibold text-sm">
									{selectedItems.size} item{selectedItems.size !== 1 ? 's' : ''} selected
								</span>
								<button
									type="button"
									className="btn btn-ghost btn-sm"
									onClick={() => setSelectedItems(new Set())}
								>
									Clear Selection
								</button>
							</div>
							<div className="flex items-center gap-2">
								<button
									type="button"
									className="btn btn-error btn-sm"
									onClick={handleBulkDelete}
									disabled={deleteBulk.isPending}
								>
									<Trash2 className="h-4 w-4" />
									{deleteBulk.isPending ? "Deleting..." : "Delete Selected"}
								</button>
							</div>
						</div>
					</div>
				</div>
			)}

			{/* Queue Table */}
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body p-0">
					{isLoading ? (
						<LoadingTable columns={9} />
					) : queueData && queueData.length > 0 ? (
						<table className="table-zebra table">
							<thead>
								<tr>
									<th className="w-12">
										<label className="cursor-pointer">
											<input
												type="checkbox"
												className="checkbox"
												checked={isAllSelected}
												ref={(input) => {
													if (input) input.indeterminate = Boolean(isIndeterminate);
												}}
												onChange={(e) => handleSelectAll(e.target.checked)}
											/>
										</label>
									</th>
									<th>NZB File</th>
									<th>Target Path</th>
									<th>Category</th>
									<th>File Size</th>
									<th>Status</th>
									<th>Retry Count</th>
									<th>Updated</th>
									<th>Actions</th>
								</tr>
							</thead>
							<tbody>
								{queueData.map((item: QueueItem) => (
									<tr 
										key={item.id} 
										className={`hover ${selectedItems.has(item.id) ? "bg-base-200" : ""}`}
									>
										<td>
											<label className="cursor-pointer">
												<input
													type="checkbox"
													className="checkbox"
													checked={selectedItems.has(item.id)}
													onChange={(e) => handleSelectItem(item.id, e.target.checked)}
												/>
											</label>
										</td>
										<td>
											<div className="flex items-center space-x-3">
												<Download className="h-4 w-4 text-primary" />
												<div>
													<div className="font-bold">
														<PathDisplay path={item.nzb_path} maxLength={40} showFileName={true} />
													</div>
													<div className="text-base-content/70 text-sm">ID: {item.id}</div>
												</div>
											</div>
										</td>
										<td>
											<PathDisplay path={item.target_path} maxLength={50} className="text-sm" />
										</td>
										<td>
											{item.category ? (
												<span className="badge badge-outline badge-sm">{item.category}</span>
											) : (
												<span className="text-base-content/50 text-sm">—</span>
											)}
										</td>
										<td>
											{item.file_size ? (
												<span className="text-sm">{formatBytes(item.file_size)}</span>
											) : (
												<span className="text-base-content/50 text-sm">—</span>
											)}
										</td>
										<td>
											{(item.status === QueueStatus.FAILED ||
												item.status === QueueStatus.RETRYING) &&
											item.error_message ? (
												<div
													className="tooltip tooltip-top"
													data-tip={truncateText(item.error_message, 200)}
												>
													<div className="flex items-center gap-1">
														<StatusBadge status={item.status} />
														<AlertCircle className="h-3 w-3 text-error" />
													</div>
												</div>
											) : (
												<StatusBadge status={item.status} />
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
											<span className="text-base-content/70 text-sm">
												{formatRelativeTime(item.updated_at)}
											</span>
										</td>
										<td>
											<div className="dropdown dropdown-end">
												<button tabIndex={0} type="button" className="btn btn-ghost btn-sm">
													<MoreHorizontal className="h-4 w-4" />
												</button>
												<ul className="dropdown-content menu w-48 rounded-box bg-base-100 shadow-lg">
													{(item.status === QueueStatus.FAILED ||
														item.status === QueueStatus.COMPLETED) && (
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
					) : (
						<div className="flex flex-col items-center justify-center py-12">
							<Download className="mb-4 h-12 w-12 text-base-content/30" />
							<h3 className="font-semibold text-base-content/70 text-lg">No queue items found</h3>
							<p className="text-base-content/50">
								{searchTerm || statusFilter
									? "No items match your search or filters"
									: "Your queue is empty"}
							</p>
						</div>
					)}
				</div>
			</div>

			{/* Pagination */}
			{totalPages > 1 && (
				<Pagination
					currentPage={page + 1}
					totalPages={totalPages}
					onPageChange={(newPage) => setPage(newPage - 1)}
					totalItems={meta?.total}
					itemsPerPage={pageSize}
					showSummary={true}
				/>
			)}
		</div>
	);
}
