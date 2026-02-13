import {
	Clock,
	FileCheck,
	RefreshCw,
	RotateCcw,
	Server,
	Settings,
	ShieldCheck,
	Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { Pagination } from "../components/ui/Pagination";
import { useConfirm } from "../contexts/ModalContext";
import { useToast } from "../contexts/ToastContext";
import {
	useCancelHealthCheck,
	useCleanupHealth,
	useDeleteBulkHealthItems,
	useDeleteHealthItem,
	useDirectHealthCheck,
	useHealth,
	useHealthStats,
	useRegenerateSymlinks,
	useRepairBulkHealthItems,
	useRepairHealthItem,
	useResetAllHealthChecks,
	useRestartBulkHealthItems,
	useSetHealthPriority,
} from "../hooks/useApi";
import { useConfig } from "../hooks/useConfig";
import {
	useCancelLibrarySync,
	useLibrarySyncStatus,
	useStartLibrarySync,
} from "../hooks/useLibrarySync";
import { HealthPriority } from "../types/api";
import { BulkActionsToolbar } from "./HealthPage/components/BulkActionsToolbar";
import { CleanupModal } from "./HealthPage/components/CleanupModal";
import { HealthFilters } from "./HealthPage/components/HealthFilters";
import { HealthStatsCards } from "./HealthPage/components/HealthStatsCards";
import { HealthStatusAlert } from "./HealthPage/components/HealthStatusAlert";
import { HealthTable } from "./HealthPage/components/HealthTable/HealthTable";
import { LibraryScanStatus } from "./HealthPage/components/LibraryScanStatus";
import { ProviderHealth } from "./HealthPage/components/ProviderHealth/ProviderHealth";
import type { CleanupConfig, SortBy, SortOrder } from "./HealthPage/types";

type HealthTab = "files" | "providers";

const HEALTH_SECTIONS = {
	files: {
		title: "File Health",
		description: "Monitor and repair corrupted media files",
		icon: FileCheck,
	},
	providers: {
		title: "Provider Health",
		description: "Check Usenet provider connectivity and speed",
		icon: Server,
	},
};

export function HealthPage() {
	const [activeTab, setActiveTab] = useState<HealthTab>("files");
	const [page, setPage] = useState(0);
	const [searchTerm, setSearchTerm] = useState("");
	const [statusFilter, setStatusFilter] = useState("");
	const [showCleanupModal, setShowCleanupModal] = useState(false);
	const [cleanupConfig, setCleanupConfig] = useState<CleanupConfig>({
		older_than: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().slice(0, 16),
		delete_files: false,
	});
	const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true);
	const [refreshInterval] = useState(5000);
	const [nextRefreshTime, setNextRefreshTime] = useState<Date | null>(null);
	const [userInteracting, setUserInteracting] = useState(false);
	const [countdown, setCountdown] = useState(0);
	const [selectedItems, setSelectedItems] = useState<Set<string>>(new Set());
	const [sortBy, setSortBy] = useState<SortBy>("created_at");
	const [sortOrder, setSortOrder] = useState<SortOrder>("desc");

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
		status: statusFilter || undefined,
		sort_by: sortBy,
		sort_order: sortOrder,
		refetchInterval: autoRefreshEnabled && !userInteracting ? refreshInterval : undefined,
	});

	const { data: stats } = useHealthStats();
	const deleteItem = useDeleteHealthItem();
	const deleteBulkItems = useDeleteBulkHealthItems();
	const restartBulkItems = useRestartBulkHealthItems();
	const repairBulkItems = useRepairBulkHealthItems();
	const cleanupHealth = useCleanupHealth();
	const resetAllHealth = useResetAllHealthChecks();
	const regenerateSymlinks = useRegenerateSymlinks();
	const directHealthCheck = useDirectHealthCheck();
	const cancelHealthCheck = useCancelHealthCheck();
	const repairHealthItem = useRepairHealthItem();
	const setHealthPriority = useSetHealthPriority();
	const { confirmAction } = useConfirm();
	const { showToast } = useToast();

	// Config hook
	const { data: config } = useConfig();

	// Library sync hooks
	const {
		data: librarySyncStatus,
		error: librarySyncError,
		isLoading: librarySyncLoading,
		refetch: refetchLibrarySync,
	} = useLibrarySyncStatus();
	const startLibrarySync = useStartLibrarySync();
	const cancelLibrarySync = useCancelLibrarySync();

	const handleDelete = async (id: number) => {
		const confirmed = await confirmAction(
			"Delete Health Record",
			"Are you sure you want to delete this health record? The actual file won´t be deleted.",
			{
				type: "warning",
				confirmText: "Delete",
				confirmButtonClass: "btn-error",
			},
		);
		if (confirmed) {
			await deleteItem.mutateAsync(id);
		}
	};

	const handleSetPriority = async (id: number, priority: HealthPriority) => {
		try {
			await setHealthPriority.mutateAsync({ id, priority });
			const priorityLabel =
				priority === HealthPriority.Next
					? "Next"
					: priority === HealthPriority.High
						? "High"
						: "Normal";

			showToast({
				title: "Priority Updated",
				message: `File priority set to ${priorityLabel}`,
				type: "success",
			});
		} catch (err) {
			console.error("Failed to update priority:", err);
			showToast({
				title: "Update Failed",
				message: "Failed to update file priority",
				type: "error",
			});
		}
	};

	const handleCleanup = () => {
		setCleanupConfig({
			older_than: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split("T")[0],
			delete_files: false,
		});
		setShowCleanupModal(true);
	};

	const handleResetAll = async () => {
		const confirmed = await confirmAction(
			"Reset All Health Checks",
			"Are you sure you want to reset all health checks? All files will be set to 'Pending' status and scheduled for immediate check.",
			{
				type: "warning",
				confirmText: "Reset All",
				confirmButtonClass: "btn-warning",
			},
		);

		if (confirmed) {
			try {
				const result = await resetAllHealth.mutateAsync();
				showToast({
					title: "Reset Successful",
					message: `Successfully reset ${result.restarted_count} health checks`,
					type: "success",
				});
			} catch (error) {
				console.error("Failed to reset all health checks:", error);
				showToast({
					title: "Reset Failed",
					message: "Failed to reset all health checks",
					type: "error",
				});
			}
		}
	};

	const handleRegenerateSymlinks = async () => {
		const confirmed = await confirmAction(
			"Regenerate Symlinks",
			"This will regenerate symlinks for all files without library path. This operation is only available when import strategy is set to SYMLINK. Are you sure you want to continue?",
			{
				type: "info",
				confirmText: "Regenerate",
				confirmButtonClass: "btn-primary",
			},
		);

		if (confirmed) {
			try {
				const result = await regenerateSymlinks.mutateAsync();
				showToast({
					title: "Symlinks Regenerated",
					message: result.message,
					type: result.error_count > 0 ? "warning" : "success",
				});
			} catch (error) {
				console.error("Failed to regenerate symlinks:", error);
				showToast({
					title: "Regeneration Failed",
					message: error instanceof Error ? error.message : "Failed to regenerate symlinks",
					type: "error",
				});
			}
		}
	};

	const handleCleanupConfirm = async () => {
		try {
			const data = await cleanupHealth.mutateAsync({
				older_than: new Date(cleanupConfig.older_than).toISOString(),
				delete_files: cleanupConfig.delete_files,
			});

			setShowCleanupModal(false);

			let message = `Successfully deleted ${data.records_deleted} health record${data.records_deleted !== 1 ? "s" : ""}`;
			if (cleanupConfig.delete_files && data.files_deleted !== undefined) {
				message += ` and ${data.files_deleted} file${data.files_deleted !== 1 ? "s" : ""}`;
			}

			showToast({
				title: "Cleanup Successful",
				message,
				type: "success",
			});

			if (data.warning && data.file_deletion_errors) {
				showToast({
					title: "Warning",
					message: data.warning,
					type: "warning",
				});
			}
		} catch (error) {
			console.error("Failed to cleanup health records:", error);
			showToast({
				title: "Cleanup Failed",
				message: "Failed to cleanup health records",
				type: "error",
			});
		}
	};

	const handleManualCheck = async (id: number) => {
		try {
			await directHealthCheck.mutateAsync(id);
		} catch (err) {
			console.error("Failed to perform direct health check:", err);
		}
	};

	const handleCancelCheck = async (id: number) => {
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
				await cancelHealthCheck.mutateAsync(id);
			} catch (err) {
				console.error("Failed to cancel health check:", err);
			}
		}
	};

	const handleRepair = async (id: number) => {
		const confirmed = await confirmAction(
			"Trigger Repair",
			"This will attempt to ask the ARR to redownload the corrupted file from your media library. THIS FILE WILL BE DELETED IF THE REPAIR IS SUCCESSFUL. Are you sure you want to proceed?",
			{
				type: "info",
				confirmText: "Trigger Repair",
				confirmButtonClass: "btn-info",
			},
		);
		if (confirmed) {
			try {
				await repairHealthItem.mutateAsync({
					id,
					resetRepairRetryCount: false,
				});
				showToast({
					title: "Repair Triggered",
					message: "Repair triggered successfully",
					type: "success",
				});
			} catch (err: unknown) {
				const error = err as {
					message?: string;
					code?: string;
				};
				console.error("Failed to trigger repair:", err);

				if (error.code === "NOT_FOUND") {
					showToast({
						title: "File Not Found in ARR",
						message:
							"This file is not managed by any configured Radarr or Sonarr instance. Please check your ARR configuration and ensure the file is in your media library.",
						type: "warning",
					});
					return;
				}

				const errorMessage = error.message || "Unknown error";

				showToast({
					title: "Failed to trigger repair",
					message: errorMessage,
					type: "error",
				});
			}
		}
	};

	const handleStartLibrarySync = async () => {
		try {
			await startLibrarySync.mutateAsync();
			showToast({
				title: "Library Scan Started",
				message: "Library scan has been triggered successfully",
				type: "success",
			});
		} catch (err) {
			console.error("Failed to start library sync:", err);
			showToast({
				title: "Failed to Start Scan",
				message: "Could not start library scan. Please try again.",
				type: "error",
			});
		}
	};

	const handleCancelLibrarySync = async () => {
		try {
			await cancelLibrarySync.mutateAsync();
			showToast({
				title: "Library Scan Cancelled",
				message: "Library scan has been cancelled",
				type: "info",
			});
		} catch (err) {
			console.error("Failed to cancel library sync:", err);
			showToast({
				title: "Failed to Cancel Scan",
				message: "Could not cancel library scan. Please try again.",
				type: "error",
			});
		}
	};

	const toggleAutoRefresh = () => {
		setAutoRefreshEnabled(!autoRefreshEnabled);
		setNextRefreshTime(null);
	};

	const handleSelectItem = (filePath: string, checked: boolean) => {
		setSelectedItems((prev) => {
			const newSet = new Set(prev);
			if (checked) {
				newSet.add(filePath);
			} else {
				newSet.delete(filePath);
			}
			return newSet;
		});
	};

	const handleSelectAll = (checked: boolean) => {
		if (checked && data) {
			setSelectedItems(new Set(data.map((item) => item.file_path)));
		} else {
			setSelectedItems(new Set());
		}
	};

	const handleBulkDelete = async () => {
		if (selectedItems.size === 0) return;

		const confirmed = await confirmAction(
			"Delete Selected Health Records",
			`Are you sure you want to delete ${selectedItems.size} selected health records? The actual file won´t be deleted.`,
			{
				type: "warning",
				confirmText: "Delete Selected",
				confirmButtonClass: "btn-error",
			},
		);

		if (confirmed) {
			try {
				const filePaths = Array.from(selectedItems);
				await deleteBulkItems.mutateAsync(filePaths);
				setSelectedItems(new Set());
				showToast({
					title: "Success",
					message: `Successfully deleted ${filePaths.length} health records`,
					type: "success",
				});
			} catch (error) {
				console.error("Failed to delete selected health records:", error);
				showToast({
					title: "Error",
					message: "Failed to delete selected health records",
					type: "error",
				});
			}
		}
	};

	const handleBulkRestart = async () => {
		if (selectedItems.size === 0) return;

		const confirmed = await confirmAction(
			"Restart Selected Health Checks",
			`Are you sure you want to restart ${selectedItems.size} selected health records? They will be reset to pending status and rechecked.`,
			{
				type: "info",
				confirmText: "Restart Checks",
				confirmButtonClass: "btn-info",
			},
		);

		if (confirmed) {
			try {
				const filePaths = Array.from(selectedItems);
				await restartBulkItems.mutateAsync(filePaths);
				setSelectedItems(new Set());
				showToast({
					title: "Success",
					message: `Successfully restarted ${filePaths.length} health checks`,
					type: "success",
				});
			} catch (error) {
				console.error("Failed to restart selected health checks:", error);
				showToast({
					title: "Error",
					message: "Failed to restart selected health checks",
					type: "error",
				});
			}
		}
	};

	const handleBulkRepair = async () => {
		if (selectedItems.size === 0) return;

		const confirmed = await confirmAction(
			"Repair Selected Files",
			`Are you sure you want to trigger repair for ${selectedItems.size} selected files? This will ask the ARR applications to redownload them.`,
			{
				type: "warning",
				confirmText: "Repair Selected",
				confirmButtonClass: "btn-warning",
			},
		);

		if (confirmed) {
			try {
				const filePaths = Array.from(selectedItems);
				await repairBulkItems.mutateAsync(filePaths);
				setSelectedItems(new Set());
				showToast({
					title: "Success",
					message: `Repair triggered for ${filePaths.length} files`,
					type: "success",
				});
			} catch (error) {
				console.error("Failed to trigger bulk repair:", error);
				showToast({
					title: "Error",
					message: "Failed to trigger bulk repair",
					type: "error",
				});
			}
		}
	};

	const clearSelection = useCallback(() => {
		setSelectedItems(new Set());
	}, []);

	const handleSort = (column: SortBy) => {
		if (sortBy === column) {
			setSortOrder(sortOrder === "asc" ? "desc" : "asc");
		} else {
			setSortBy(column);
			setSortOrder(column === "created_at" ? "desc" : "asc");
		}
		setPage(0);
		clearSelection();
	};

	const handleUserInteractionStart = () => {
		setUserInteracting(true);
	};

	const handleUserInteractionEnd = () => {
		const timer = setTimeout(() => {
			setUserInteracting(false);
		}, 2000);

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

		setNextRefreshTime(new Date(Date.now() + refreshInterval));

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

			if (remaining === 0) {
				setNextRefreshTime(new Date(Date.now() + refreshInterval));
			}
		};

		updateCountdown();
		const timer = setInterval(updateCountdown, 1000);

		return () => clearInterval(timer);
	}, [nextRefreshTime, autoRefreshEnabled, userInteracting, refreshInterval]);

	// Reset page when search term or status filter changes
	useEffect(() => {
		if (searchTerm !== "" || statusFilter !== "") {
			setPage(0);
		}
	}, [searchTerm, statusFilter]);

	// Clear selection when page, search, or filter changes
	useEffect(() => {
		clearSelection();
	}, [clearSelection]);

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
			<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
				<div className="flex items-center space-x-3">
					<div className="rounded-xl bg-primary/10 p-2">
						<ShieldCheck className="h-8 w-8 text-primary" />
					</div>
					<div>
						<h1 className="font-bold text-3xl tracking-tight">Health Monitoring</h1>
						<p className="text-base-content/60 text-sm">
							Monitor library integrity and provider status
						</p>
					</div>
				</div>

				<div className="flex items-center gap-2">
					<div className="dropdown">
						<div tabIndex={0} role="button" className="btn btn-outline btn-sm gap-2">
							<Settings className="h-3.5 w-3.5" />
							Maintenance
						</div>
						<ul className="dropdown-content menu z-[1] mt-2 w-52 rounded-box border border-base-200 bg-base-100 p-2 shadow-lg">
							<li>
								<button type="button" onClick={handleResetAll} className="gap-2 text-warning">
									<RotateCcw className="h-4 w-4" /> Reset All Checks
								</button>
							</li>
							<li>
								<button type="button" onClick={handleCleanup} className="gap-2 text-error">
									<Trash2 className="h-4 w-4" /> Cleanup Records
								</button>
							</li>
							<li>
								<button type="button" onClick={handleRegenerateSymlinks} className="gap-2">
									<RefreshCw className="h-4 w-4" /> Regenerate Symlinks
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
							{autoRefreshEnabled ? (
								<Clock className="h-3.5 w-3.5" />
							) : (
								<Clock className="h-3.5 w-3.5 opacity-50" />
							)}
							{autoRefreshEnabled ? `${countdown}s` : "Off"}
						</button>

						<button
							type="button"
							className="btn btn-outline btn-sm join-item"
							onClick={() => refetch()}
							disabled={isLoading}
						>
							{isLoading ? (
								<span className="loading loading-spinner loading-xs" />
							) : (
								<RefreshCw className="h-3.5 w-3.5" />
							)}
							Refresh
						</button>
					</div>
				</div>
			</div>

			<div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
				{/* Sidebar Navigation */}
				<div className="lg:col-span-1">
					<div className="space-y-6">
						<div className="card border border-base-200 bg-base-100 shadow-sm">
							<div className="card-body p-2 sm:p-4">
								<div>
									<h3 className="mb-2 px-4 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
										Monitoring
									</h3>
									<ul className="menu menu-md gap-1 p-0">
										{(
											Object.entries(HEALTH_SECTIONS) as [HealthTab, typeof HEALTH_SECTIONS.files][]
										).map(([key, section]) => {
											const IconComponent = section.icon;
											const isActive = activeTab === key;
											return (
												<li key={key}>
													<button
														type="button"
														className={`flex items-center gap-3 rounded-lg px-4 py-3 transition-all ${
															isActive
																? "bg-primary font-semibold text-primary-content shadow-md shadow-primary/20"
																: "hover:bg-base-200"
														}`}
														onClick={() => setActiveTab(key)}
													>
														<IconComponent
															className={`h-5 w-5 ${isActive ? "" : "text-base-content/60"}`}
														/>
														<div className="min-w-0 flex-1 text-left">
															<div className="text-sm">{section.title}</div>
														</div>
													</button>
												</li>
											);
										})}
									</ul>
								</div>
							</div>
						</div>

						{/* Library Sync Mini Card */}
						<LibraryScanStatus
							status={librarySyncStatus}
							isLoading={librarySyncLoading}
							error={librarySyncError}
							isStartPending={startLibrarySync.isPending}
							isCancelPending={cancelLibrarySync.isPending}
							syncIntervalMinutes={config?.health.library_sync_interval_minutes}
							onStart={handleStartLibrarySync}
							onCancel={handleCancelLibrarySync}
							onRetry={refetchLibrarySync}
							variant="sidebar"
						/>
					</div>
				</div>

				{/* Content Area */}
				<div className="lg:col-span-3">
					<div className="space-y-6">
						{/* Section Description Card */}
						<div className="card border border-base-200 bg-base-100 shadow-sm">
							<div className="card-body p-4 sm:p-6">
								<div className="flex items-center space-x-4">
									<div className="rounded-xl bg-primary/10 p-3">
										{(() => {
											const IconComponent = HEALTH_SECTIONS[activeTab].icon;
											return <IconComponent className="h-6 w-6 text-primary" />;
										})()}
									</div>
									<div>
										<h2 className="font-bold text-2xl tracking-tight">
											{HEALTH_SECTIONS[activeTab].title}
										</h2>
										<p className="max-w-2xl text-base-content/60 text-sm">
											{HEALTH_SECTIONS[activeTab].description}
										</p>
									</div>
								</div>
							</div>
						</div>

						{activeTab === "files" ? (
							<div className="space-y-6">
								<HealthStatsCards stats={stats} />

								<div className="card border border-base-200 bg-base-100 shadow-sm">
									<div className="card-body p-4 sm:p-8">
										<HealthFilters
											searchTerm={searchTerm}
											statusFilter={statusFilter}
											onSearchChange={setSearchTerm}
											onStatusFilterChange={setStatusFilter}
											onUserInteractionStart={handleUserInteractionStart}
											onUserInteractionEnd={handleUserInteractionEnd}
										/>

										<BulkActionsToolbar
											selectedCount={selectedItems.size}
											isRestartPending={restartBulkItems.isPending}
											isDeletePending={deleteBulkItems.isPending}
											isRepairPending={repairBulkItems.isPending}
											onClearSelection={() => setSelectedItems(new Set())}
											onBulkRestart={handleBulkRestart}
											onBulkDelete={handleBulkDelete}
											onBulkRepair={handleBulkRepair}
										/>

										<div className="mt-6">
											<HealthTable
												data={data}
												isLoading={isLoading}
												selectedItems={selectedItems}
												sortBy={sortBy}
												sortOrder={sortOrder}
												searchTerm={searchTerm}
												statusFilter={statusFilter}
												isCancelPending={cancelHealthCheck.isPending}
												isDirectCheckPending={directHealthCheck.isPending}
												isRepairPending={repairHealthItem.isPending}
												isDeletePending={deleteItem.isPending}
												onSelectItem={handleSelectItem}
												onSelectAll={handleSelectAll}
												onSort={handleSort}
												onCancelCheck={handleCancelCheck}
												onManualCheck={handleManualCheck}
												onRepair={handleRepair}
												onDelete={handleDelete}
												onSetPriority={handleSetPriority}
											/>
										</div>

										{meta?.total && meta.total > pageSize && (
											<div className="mt-6">
												<Pagination
													currentPage={page + 1}
													totalPages={Math.ceil(meta.total / pageSize)}
													onPageChange={(newPage) => setPage(newPage - 1)}
													totalItems={meta.total}
													itemsPerPage={pageSize}
													showSummary={true}
												/>
											</div>
										)}
									</div>
								</div>

								<HealthStatusAlert stats={stats} />

								<CleanupModal
									show={showCleanupModal}
									config={cleanupConfig}
									isPending={cleanupHealth.isPending}
									onClose={() => setShowCleanupModal(false)}
									onConfigChange={setCleanupConfig}
									onConfirm={handleCleanupConfirm}
								/>
							</div>
						) : (
							<div className="card border border-base-200 bg-base-100 shadow-sm">
								<div className="card-body p-4 sm:p-8">
									<ProviderHealth />
								</div>
							</div>
						)}
					</div>
				</div>
			</div>
		</div>
	);
}
