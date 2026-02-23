import {
	Activity,
	AlertCircle,
	Box,
	CheckCircle2,
	ChevronDown,
	ChevronUp,
	Clock,
	Download,
	FileCode,
	Filter,
	Import,
	List,
	MoreVertical,
	PlayCircle,
	RefreshCw,
	Search,
	Settings,
	Trash2,
	XCircle,
	XOctagon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ImportMethods } from "../components/queue/ImportMethods";
import { QueueItemCard } from "../components/queue/QueueItemCard";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner, LoadingTable } from "../components/ui/LoadingSpinner";
import { Pagination } from "../components/ui/Pagination";
import { PathDisplay } from "../components/ui/PathDisplay";
import { StatusBadge } from "../components/ui/StatusBadge";
import { useConfirm } from "../contexts/ModalContext";
import {
	useAddTestQueueItem,
	useBulkCancelQueueItems,
	useCancelQueueItem,
	useClearCompletedQueue,
	useClearFailedQueue,
	useClearPendingQueue,
	useDeleteBulkQueueItems,
	useDeleteQueueItem,
	useQueue,
	useQueueStats,
	useRestartBulkQueueItems,
	useRetryQueueItem,
} from "../hooks/useApi";
import { useProgressStream } from "../hooks/useProgressStream";
import { formatBytes, formatRelativeTime, truncateText } from "../lib/utils";
import { type QueueItem, QueueStatus } from "../types/api";

type QueueFilter = "" | "pending" | "processing" | "completed" | "failed";
type QueueView = "list" | "import";

const QUEUE_SECTIONS = [
	{ id: "", title: "All Items", icon: List },
	{ id: "pending", title: "Pending", icon: Clock },
	{ id: "processing", title: "Processing", icon: Activity },
	{ id: "completed", title: "Completed", icon: CheckCircle2 },
	{ id: "failed", title: "Failed", icon: XOctagon },
];

export function QueuePage() {
	const [view, setView] = useState<QueueView>("list");
	const [page, setPage] = useState(0);
	const [statusFilter, setStatusFilter] = useState<QueueFilter>("");
	const [searchTerm, setSearchTerm] = useState("");
	const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true);
	const [refreshInterval] = useState(5000);
	const [nextRefreshTime, setNextRefreshTime] = useState<Date | null>(null);
	const [userInteracting, setUserInteracting] = useState(false);
	const [countdown, setCountdown] = useState(0);
	const [selectedItems, setSelectedItems] = useState<Set<number>>(new Set());
	const [sortBy, setSortBy] = useState<"created_at" | "updated_at" | "status" | "nzb_path">(
		"updated_at",
	);
	const [sortOrder, setSortOrder] = useState<"asc" | "desc">("desc");

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
		sort_by: sortBy,
		sort_order: sortOrder,
		refetchInterval:
			autoRefreshEnabled && !userInteracting && view === "list" ? refreshInterval : undefined,
	});

	const queueData = queueResponse?.data;
	const meta = queueResponse?.meta;
	const totalPages = meta?.total ? Math.ceil(meta.total / pageSize) : 0;

	const hasProcessingItems = useMemo(() => {
		return queueData?.some((item) => item.status === QueueStatus.PROCESSING) ?? false;
	}, [queueData]);

	const { progress: liveProgress } = useProgressStream({
		enabled: hasProcessingItems && view === "list",
	});

	const enrichedQueueData = useMemo(() => {
		if (!queueData) return undefined;
		return queueData.map((item) => ({
			...item,
			percentage: liveProgress[item.id] ?? item.percentage,
		}));
	}, [queueData, liveProgress]);

	const { data: stats } = useQueueStats();
	const deleteItem = useDeleteQueueItem();
	const deleteBulk = useDeleteBulkQueueItems();
	const restartBulk = useRestartBulkQueueItems();
	const retryItem = useRetryQueueItem();
	const cancelItem = useCancelQueueItem();
	const cancelBulk = useBulkCancelQueueItems();
	const clearCompleted = useClearCompletedQueue();
	const clearFailed = useClearFailedQueue();
	const clearPending = useClearPendingQueue();
	const addTestQueueItem = useAddTestQueueItem();
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

	const handleCancel = async (id: number) => {
		const confirmed = await confirmAction(
			"Cancel Processing",
			"Are you sure you want to cancel this processing item? The item will be marked as failed and can be retried later.",
			{
				type: "warning",
				confirmText: "Cancel Item",
				confirmButtonClass: "btn-warning",
			},
		);
		if (confirmed) {
			await cancelItem.mutateAsync(id);
		}
	};

	const handleDownload = async (id: number) => {
		try {
			const response = await fetch(`/api/queue/${id}/download`);
			if (!response.ok) throw new Error("Failed to download NZB file");
			const contentDisposition = response.headers.get("Content-Disposition");
			const filenameMatch = contentDisposition?.match(/filename[^;=\n]*=["']?([^"'\n]*)["']?/);
			const filename = filenameMatch?.[1] || `queue-${id}.nzb`;
			const blob = await response.blob();
			const url = window.URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			window.URL.revokeObjectURL(url);
			document.body.removeChild(a);
		} catch (error) {
			console.error("Failed to download NZB:", error);
		}
	};

	const handleClearCompleted = async () => {
		const confirmed = await confirmAction(
			"Clear Completed Items",
			"Are you sure you want to clear all completed items? This action cannot be undone.",
			{
				type: "warning",
				confirmText: "Clear All",
				confirmButtonClass: "btn-success",
			},
		);
		if (confirmed) await clearCompleted.mutateAsync("");
	};

	const handleClearFailed = async () => {
		const confirmed = await confirmAction(
			"Clear Failed Items",
			"Are you sure you want to clear all failed items? This action cannot be undone.",
			{
				type: "warning",
				confirmText: "Clear All",
				confirmButtonClass: "btn-error",
			},
		);
		if (confirmed) await clearFailed.mutateAsync("");
	};

	const handleClearPending = async () => {
		const confirmed = await confirmAction(
			"Clear Pending Items",
			"Are you sure you want to clear all pending items? This action cannot be undone.",
			{
				type: "info",
				confirmText: "Clear All",
				confirmButtonClass: "btn-warning",
			},
		);
		if (confirmed) await clearPending.mutateAsync("");
	};

	const handleAddTestFile = async (size: "100MB" | "1GB" | "10GB") => {
		try {
			await addTestQueueItem.mutateAsync(size);
		} catch (error) {
			console.error(`Failed to add ${size} test file:`, error);
		}
	};

	const toggleAutoRefresh = () => {
		setAutoRefreshEnabled(!autoRefreshEnabled);
		setNextRefreshTime(null);
	};

	const handleSelectItem = (id: number, checked: boolean) => {
		setSelectedItems((prev) => {
			const newSet = new Set(prev);
			if (checked) newSet.add(id);
			else newSet.delete(id);
			return newSet;
		});
	};

	const handleSelectAll = (checked: boolean) => {
		if (checked && enrichedQueueData) {
			setSelectedItems(new Set(enrichedQueueData.map((item) => item.id)));
		} else {
			setSelectedItems(new Set());
		}
	};

	const handleBulkDelete = async () => {
		if (selectedItems.size === 0) return;
		const confirmed = await confirmAction(
			"Delete Selected Items",
			`Are you sure you want to delete ${selectedItems.size} selected queue items? This action cannot be undone.`,
			{ type: "warning", confirmText: "Delete Selected", confirmButtonClass: "btn-error" },
		);
		if (confirmed) {
			try {
				await deleteBulk.mutateAsync(Array.from(selectedItems));
				setSelectedItems(new Set());
			} catch (error) {
				console.error("Failed to delete selected items:", error);
			}
		}
	};

	const handleBulkRestart = async () => {
		if (selectedItems.size === 0) return;
		const confirmed = await confirmAction(
			"Restart Selected Items",
			`Are you sure you want to restart ${selectedItems.size} selected queue items? This will reset their retry counts and set them back to pending status.`,
			{ type: "info", confirmText: "Restart Selected", confirmButtonClass: "btn-primary" },
		);
		if (confirmed) {
			try {
				await restartBulk.mutateAsync(Array.from(selectedItems));
				setSelectedItems(new Set());
			} catch (error) {
				console.error("Failed to restart selected items:", error);
			}
		}
	};

	const handleBulkCancel = async () => {
		if (selectedItems.size === 0) return;
		const confirmed = await confirmAction(
			"Cancel Selected Items",
			`Are you sure you want to cancel ${selectedItems.size} selected items? They will be marked as failed and can be retried later.`,
			{ type: "warning", confirmText: "Cancel Selected", confirmButtonClass: "btn-warning" },
		);
		if (confirmed) {
			try {
				await cancelBulk.mutateAsync(Array.from(selectedItems));
				setSelectedItems(new Set());
			} catch (error) {
				console.error("Failed to cancel selected items:", error);
			}
		}
	};

	const clearSelection = useCallback(() => {
		setSelectedItems(new Set());
	}, []);

	const handleSort = (column: "created_at" | "updated_at" | "status" | "nzb_path") => {
		if (sortBy === column) setSortOrder(sortOrder === "asc" ? "desc" : "asc");
		else {
			setSortBy(column);
			setSortOrder(column === "updated_at" || column === "created_at" ? "desc" : "asc");
		}
		setPage(0);
		clearSelection();
	};

	const isAllSelected =
		queueData && queueData.length > 0 && queueData.every((item) => selectedItems.has(item.id));
	const isIndeterminate = queueData && selectedItems.size > 0 && !isAllSelected;

	useEffect(() => {
		if (autoRefreshEnabled && !userInteracting && view === "list") {
			setNextRefreshTime(new Date(Date.now() + refreshInterval));
			const interval = setInterval(() => {
				setNextRefreshTime(new Date(Date.now() + refreshInterval));
			}, refreshInterval);
			return () => clearInterval(interval);
		}
		setNextRefreshTime(null);
	}, [autoRefreshEnabled, refreshInterval, userInteracting, view]);

	const handleUserInteractionStart = () => setUserInteracting(true);
	const handleUserInteractionEnd = () => {
		const timer = setTimeout(() => setUserInteracting(false), 2000);
		return () => clearTimeout(timer);
	};

	useEffect(() => {
		if (nextRefreshTime && autoRefreshEnabled && !userInteracting && view === "list") {
			const updateCountdown = () => {
				const remaining = Math.max(0, Math.ceil((nextRefreshTime.getTime() - Date.now()) / 1000));
				setCountdown(remaining);
				if (remaining === 0) setNextRefreshTime(new Date(Date.now() + refreshInterval));
			};
			updateCountdown();
			const timer = setInterval(updateCountdown, 1000);
			return () => clearInterval(timer);
		}
		setCountdown(0);
	}, [nextRefreshTime, autoRefreshEnabled, userInteracting, refreshInterval, view]);

	useEffect(() => {
		setPage(0);
	}, []);
	useEffect(() => {
		clearSelection();
	}, [clearSelection]);

	if (error) {
		return (
			<div className="space-y-4">
				<h1 className="font-bold text-3xl tracking-tight">Queue Management</h1>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
				<div className="flex items-center space-x-3">
					<div className="rounded-xl bg-primary/10 p-2">
						<List className="h-8 w-8 text-primary" />
					</div>
					<div>
						<h1 className="font-bold text-3xl tracking-tight">Queue</h1>
						<p className="text-base-content/60 text-sm">Monitor and manage NZB processing tasks</p>
					</div>
				</div>

				<div className="flex items-center gap-2">
					<div role="tablist" className="tabs tabs-boxed mr-4">
						<button
							type="button"
							role="tab"
							className={`tab tab-sm gap-2 ${view === "list" ? "tab-active" : ""}`}
							onClick={() => setView("list")}
						>
							<List className="h-3.5 w-3.5" /> List
						</button>
						<button
							type="button"
							role="tab"
							className={`tab tab-sm gap-2 ${view === "import" ? "tab-active" : ""}`}
							onClick={() => setView("import")}
						>
							<Import className="h-3.5 w-3.5" /> Import
						</button>
					</div>

					{view === "list" && (
						<>
							<div className="dropdown">
								<button type="button" tabIndex={0} className="btn btn-outline btn-sm gap-2">
									<Settings className="h-3.5 w-3.5" />
									Cleanup
								</button>
								<ul className="dropdown-content menu z-[1] mt-2 w-52 rounded-box border border-base-200 bg-base-100 p-2 shadow-lg">
									<li>
										<button
											type="button"
											onClick={handleClearCompleted}
											className="text-success"
											disabled={!stats || stats.total_completed === 0 || clearCompleted.isPending}
										>
											<Trash2 className="h-4 w-4" /> Clear Completed
										</button>
									</li>
									<li>
										<button
											type="button"
											onClick={handleClearPending}
											className="text-warning"
											disabled={!stats || stats.total_queued === 0 || clearPending.isPending}
										>
											<Trash2 className="h-4 w-4" /> Clear Pending
										</button>
									</li>
									<li>
										<button
											type="button"
											onClick={handleClearFailed}
											className="text-error"
											disabled={!stats || stats.total_failed === 0 || clearFailed.isPending}
										>
											<Trash2 className="h-4 w-4" /> Clear Failed
										</button>
									</li>
									<div className="divider my-1 text-base-content/70" />
									<li className="menu-title px-4 py-2 font-bold text-base-content/40 text-xs uppercase tracking-widest">
										Testing
									</li>
									<li>
										<button
											type="button"
											onClick={() => handleAddTestFile("100MB")}
											disabled={addTestQueueItem.isPending}
										>
											Add 100MB Test
										</button>
									</li>
									<li>
										<button
											type="button"
											onClick={() => handleAddTestFile("1GB")}
											disabled={addTestQueueItem.isPending}
										>
											Add 1GB Test
										</button>
									</li>
								</ul>
							</div>

							<div className="join">
								<button
									type="button"
									className={`btn btn-outline btn-sm join-item ${autoRefreshEnabled ? "btn-primary" : ""}`}
									onClick={toggleAutoRefresh}
								>
									<Clock
										className={`h-3.5 w-3.5 ${autoRefreshEnabled ? "" : "text-base-content/70"}`}
									/>
									{autoRefreshEnabled ? `${countdown}s` : "Off"}
								</button>

								<button
									type="button"
									className="btn btn-outline btn-sm join-item"
									onClick={() => refetch()}
									disabled={isLoading}
								>
									{isLoading ? <LoadingSpinner size="sm" /> : <RefreshCw className="h-3.5 w-3.5" />}
									Refresh
								</button>
							</div>
						</>
					)}
				</div>
			</div>

			{view === "import" ? (
				<ImportMethods />
			) : (
				<div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
					{/* Sidebar Navigation */}
					<div className="lg:col-span-1">
						<div className="space-y-6">
							<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
								<div className="card-body p-2 sm:p-4">
									<div>
										<h3 className="mb-2 px-4 font-bold text-base-content/40 text-xs uppercase tracking-widest">
											Filters
										</h3>
										<ul className="menu menu-md gap-1 p-0">
											{QUEUE_SECTIONS.map((section) => {
												const IconComponent = section.icon;
												const isActive = statusFilter === section.id;
												const count =
													section.id === ""
														? stats
															? stats.total_queued +
																stats.total_processing +
																stats.total_completed +
																stats.total_failed
															: 0
														: section.id === "pending"
															? stats?.total_queued
															: section.id === "processing"
																? stats?.total_processing
																: section.id === "completed"
																	? stats?.total_completed
																	: stats?.total_failed;

												return (
													<li key={section.id}>
														<button
															type="button"
															className={`flex items-center gap-3 rounded-lg px-4 py-3 transition-all ${
																isActive
																	? "bg-primary font-semibold text-primary-content shadow-md shadow-primary/20"
																	: "hover:bg-base-200"
															}`}
															onClick={() => setStatusFilter(section.id as QueueFilter)}
														>
															<IconComponent
																className={`h-5 w-5 ${isActive ? "" : "text-base-content/60"}`}
															/>
															<div className="min-w-0 flex-1 text-left">
																<div className="text-sm">{section.title}</div>
															</div>
															{count !== undefined && (
																<span
																	className={`badge badge-xs px-2 py-2 font-bold font-mono ${isActive ? "badge-secondary" : "badge-ghost text-base-content/80"}`}
																>
																	{count}
																</span>
															)}
														</button>
													</li>
												);
											})}
										</ul>
									</div>
								</div>
							</div>

							{/* Search Mini-Card */}
							<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
								<div className="card-body p-4">
									<h3 className="mb-3 font-bold text-base-content/40 text-xs uppercase tracking-widest">
										Search
									</h3>
									<div className="relative">
										<Search className="-translate-y-1/2 absolute top-1/2 left-3 h-3.5 w-3.5 text-base-content/60" />
										<input
											type="text"
											placeholder="Find item..."
											className="input input-sm w-full bg-base-200/50 pl-9 text-xs"
											value={searchTerm}
											onChange={(e) => setSearchTerm(e.target.value)}
											onFocus={handleUserInteractionStart}
											onBlur={handleUserInteractionEnd}
										/>
									</div>
								</div>
							</div>
						</div>
					</div>

					{/* Content Area */}
					<div className="lg:col-span-3">
						<div className="space-y-6">
							{/* Bulk Actions Toolbar */}
							{selectedItems.size > 0 && (
								<div className="card border border-primary/20 bg-primary/5 shadow-sm">
									<div className="card-body p-4 sm:p-6">
										<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
											<div className="flex items-center gap-4">
												<div className="rounded-lg bg-primary/20 p-2">
													<Filter className="h-5 w-5 text-primary" />
												</div>
												<div>
													<span className="font-bold text-sm">
														{selectedItems.size} item{selectedItems.size !== 1 ? "s" : ""} selected
													</span>
													<button
														type="button"
														className="btn btn-link btn-sm ml-2 text-base-content/80 no-underline hover:opacity-100"
														onClick={() => setSelectedItems(new Set())}
													>
														Clear Selection
													</button>
												</div>
											</div>
											<div className="flex flex-wrap items-center gap-2">
												<button
													type="button"
													className="btn btn-primary btn-sm px-4"
													onClick={handleBulkRestart}
													disabled={restartBulk.isPending}
												>
													{restartBulk.isPending ? (
														<LoadingSpinner size="sm" />
													) : (
														<RefreshCw className="h-3 w-3" />
													)}
													Restart
												</button>
												<button
													type="button"
													className="btn btn-outline btn-warning btn-sm px-4"
													onClick={handleBulkCancel}
													disabled={cancelBulk.isPending}
												>
													<XCircle className="h-3 w-3" />
													Cancel
												</button>
												<button
													type="button"
													className="btn btn-outline btn-error btn-sm px-4"
													onClick={handleBulkDelete}
													disabled={deleteBulk.isPending}
												>
													<Trash2 className="h-3 w-3" />
													Delete
												</button>
											</div>
										</div>
									</div>
								</div>
							)}

							{/* Queue Table Card */}
							<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
								<div className="card-body p-0">
									{isLoading ? (
										<div className="p-12">
											<LoadingTable columns={9} />
										</div>
									) : queueData && queueData.length > 0 ? (
										<>
											{/* Mobile View (< 640px) */}
											<div className="space-y-3 p-4 sm:hidden">
												{enrichedQueueData?.map((item: QueueItem) => (
													<QueueItemCard
														key={item.id}
														item={item}
														isSelected={selectedItems.has(item.id)}
														onSelectChange={handleSelectItem}
														onRetry={handleRetry}
														onCancel={handleCancel}
														onDelete={handleDelete}
														onDownload={handleDownload}
														isRetryPending={retryItem.isPending}
														isCancelPending={cancelItem.isPending}
														isDeletePending={deleteItem.isPending}
													/>
												))}
											</div>

											{/* Desktop View (≥640px) - Keep Existing */}
											<div className="hidden min-h-[450px] overflow-x-auto pb-24 sm:block">
												{" "}
												<table className="table-zebra table-sm sm:table-md table">
													<thead className="bg-base-200/50">
														<tr>
															<th className="w-12 text-center">
																<input
																	type="checkbox"
																	className="checkbox checkbox-sm"
																	checked={isAllSelected}
																	ref={(input) => {
																		if (input) input.indeterminate = Boolean(isIndeterminate);
																	}}
																	onChange={(e) => handleSelectAll(e.target.checked)}
																/>
															</th>
															<th>
																<button
																	type="button"
																	className="flex items-center gap-1 font-bold text-base-content/80 text-xs uppercase tracking-widest hover:text-primary"
																	onClick={() => handleSort("nzb_path")}
																>
																	NZB File
																	{sortBy === "nzb_path" &&
																		(sortOrder === "asc" ? (
																			<ChevronUp className="h-3 w-3" />
																		) : (
																			<ChevronDown className="h-3 w-3" />
																		))}
																</button>
															</th>
															<th className="font-bold text-base-content/80 text-xs uppercase tracking-widest">
																Category
															</th>
															<th className="font-bold text-base-content/80 text-xs uppercase tracking-widest">
																Size
															</th>
															<th className="font-bold text-base-content/80 text-xs uppercase tracking-widest">
																Status
															</th>
															<th>
																<button
																	type="button"
																	className="flex items-center gap-1 font-bold text-base-content/80 text-xs uppercase tracking-widest hover:text-primary"
																	onClick={() => handleSort("updated_at")}
																>
																	Updated
																	{sortBy === "updated_at" &&
																		(sortOrder === "asc" ? (
																			<ChevronUp className="h-3 w-3" />
																		) : (
																			<ChevronDown className="h-3 w-3" />
																		))}
																</button>
															</th>
															<th className="w-16" />
														</tr>
													</thead>
													<tbody>
														{enrichedQueueData?.map((item: QueueItem) => (
															<tr
																key={item.id}
																className={`hover transition-colors ${selectedItems.has(item.id) ? "bg-primary/5" : ""}`}
															>
																<td className="text-center">
																	<input
																		type="checkbox"
																		className="checkbox checkbox-sm"
																		checked={selectedItems.has(item.id)}
																		onChange={(e) => handleSelectItem(item.id, e.target.checked)}
																	/>
																</td>
																<td>
																	<div className="flex min-w-0 flex-col">
																		<div className="flex items-center gap-2">
																			<FileCode className="h-3.5 w-3.5 shrink-0 text-base-content/60" />
																			<div className="truncate font-bold text-sm">
																				<PathDisplay
																					path={item.nzb_path}
																					maxLength={80}
																					showFileName={true}
																				/>
																			</div>
																		</div>
																		<div className="mt-1 truncate pl-5.5 text-base-content/40 text-xs">
																			{item.target_path ? (
																				<span className="flex items-center gap-1">
																					<Box className="h-2.5 w-2.5" />
																					<PathDisplay path={item.target_path} maxLength={60} />
																				</span>
																			) : (
																				`ID: ${item.id}`
																			)}
																		</div>
																	</div>
																</td>
																<td>
																	{item.category ? (
																		<span className="badge badge-outline badge-xs py-2 font-semibold uppercase tracking-wide">
																			{item.category}
																		</span>
																	) : (
																		<span className="text-base-content/50">—</span>
																	)}
																</td>
																<td>
																	{item.file_size ? (
																		<span className="font-mono text-xs opacity-70">
																			{formatBytes(item.file_size)}
																		</span>
																	) : (
																		<span className="text-base-content/50">—</span>
																	)}
																</td>
																<td>
																	<div className="flex flex-col gap-1">
																		{item.status === QueueStatus.FAILED && item.error_message ? (
																			<div
																				className="tooltip tooltip-top"
																				data-tip={truncateText(item.error_message, 200)}
																			>
																				<div className="flex items-center gap-1">
																					<StatusBadge status={item.status} />
																					<AlertCircle className="h-3 w-3 text-error" />
																				</div>
																			</div>
																		) : item.status === QueueStatus.PROCESSING &&
																			item.percentage != null ? (
																			<div className="flex w-24 flex-col gap-1">
																				<div className="flex justify-between font-bold font-mono text-base-content/80 text-xs">
																					<span>PROGRESS</span>
																					<span>{item.percentage}%</span>
																				</div>
																				<progress
																					className="progress progress-primary h-1.5 w-full"
																					value={item.percentage}
																					max={100}
																				/>
																			</div>
																		) : (
																			<StatusBadge status={item.status} />
																		)}
																	</div>
																</td>
																<td>
																	<div className="flex flex-col">
																		<span className="text-xs opacity-70">
																			{formatRelativeTime(item.updated_at)}
																		</span>
																		{item.retry_count > 0 && (
																			<span className="mt-0.5 font-bold text-warning text-xs uppercase tracking-tighter">
																				{item.retry_count} Retries
																			</span>
																		)}
																	</div>
																</td>
																<td className="text-right">
																	<div className="dropdown dropdown-end">
																		<button
																			tabIndex={0}
																			type="button"
																			className="btn btn-ghost btn-sm btn-square"
																		>
																			<MoreVertical className="h-4 w-4" />
																		</button>
																		<ul className="dropdown-content menu z-[50] w-48 rounded-box border border-base-300 bg-base-100 p-2 shadow-xl">
																			{(item.status === QueueStatus.PENDING ||
																				item.status === QueueStatus.FAILED ||
																				item.status === QueueStatus.COMPLETED) && (
																				<li>
																					<button
																						type="button"
																						onClick={() => handleRetry(item.id)}
																						disabled={retryItem.isPending}
																					>
																						<PlayCircle className="h-4 w-4 text-primary" />
																						{item.status === QueueStatus.PENDING
																							? "Start Now"
																							: "Retry Task"}
																					</button>
																				</li>
																			)}
																			{item.status === QueueStatus.PROCESSING && (
																				<li>
																					<button
																						type="button"
																						onClick={() => handleCancel(item.id)}
																						disabled={cancelItem.isPending}
																						className="text-warning"
																					>
																						<XCircle className="h-4 w-4" />
																						Cancel Process
																					</button>
																				</li>
																			)}
																			<li>
																				<button
																					type="button"
																					onClick={() => handleDownload(item.id)}
																				>
																					<Download className="h-4 w-4" />
																					Download NZB
																				</button>
																			</li>
																			<div className="divider my-1 text-base-content/70" />
																			{item.status !== QueueStatus.PROCESSING && (
																				<li>
																					<button
																						type="button"
																						onClick={() => handleDelete(item.id)}
																						disabled={deleteItem.isPending}
																						className="text-error"
																					>
																						<Trash2 className="h-4 w-4" />
																						Delete Record
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
										</>
									) : (
										<div className="flex flex-col items-center justify-center py-24">
											<div className="rounded-full bg-base-200 p-6">
												<List className="h-12 w-12 opacity-20" />
											</div>
											<h3 className="mt-6 font-bold text-base-content/60 text-lg">Empty Queue</h3>
											<p className="mt-1 text-base-content/40 text-sm">
												{searchTerm || statusFilter
													? "No items match your active filters"
													: "There are currently no items in the processing queue"}
											</p>
											{(searchTerm || statusFilter) && (
												<button
													type="button"
													className="btn btn-ghost btn-sm mt-6 text-primary"
													onClick={() => {
														setSearchTerm("");
														setStatusFilter("");
													}}
												>
													Reset Filters
												</button>
											)}
										</div>
									)}
								</div>
							</div>

							{/* Pagination */}
							{totalPages > 1 && (
								<div className="mt-2">
									<Pagination
										currentPage={page + 1}
										totalPages={totalPages}
										onPageChange={(newPage) => setPage(newPage - 1)}
										totalItems={meta?.total}
										itemsPerPage={pageSize}
										showSummary={true}
									/>
								</div>
							)}
						</div>
					</div>
				</div>
			)}
		</div>
	);
}
