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
	useRepairBulkHealthItems,
	useRepairHealthItem,
	useRestartBulkHealthItems,
} from "../hooks/useApi";
import { useConfig } from "../hooks/useConfig";
import {
	useCancelLibrarySync,
	useLibrarySyncStatus,
	useStartLibrarySync,
} from "../hooks/useLibrarySync";
import { BulkActionsToolbar } from "./HealthPage/components/BulkActionsToolbar";
import { CleanupModal } from "./HealthPage/components/CleanupModal";
import { HealthFilters } from "./HealthPage/components/HealthFilters";
import { HealthPageHeader } from "./HealthPage/components/HealthPageHeader";
import { HealthStatsCards } from "./HealthPage/components/HealthStatsCards";
import { HealthStatusAlert } from "./HealthPage/components/HealthStatusAlert";
import { HealthTable } from "./HealthPage/components/HealthTable/HealthTable";
import { LibraryScanStatus } from "./HealthPage/components/LibraryScanStatus";
import { ProviderHealth } from "./HealthPage/components/ProviderHealth/ProviderHealth";
import type { CleanupConfig, SortBy, SortOrder } from "./HealthPage/types";

export function HealthPage() {
	const [activeTab, setActiveTab] = useState<"files" | "providers">("files");
	const [page, setPage] = useState(0);
	const [searchTerm, setSearchTerm] = useState("");
	const [statusFilter, setStatusFilter] = useState("");
	const [showCleanupModal, setShowCleanupModal] = useState(false);
	const [cleanupConfig, setCleanupConfig] = useState<CleanupConfig>({
		older_than: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().slice(0, 16),
		delete_files: false,
	});
	const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true);
	const [refreshInterval, setRefreshInterval] = useState(5000);
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
	const directHealthCheck = useDirectHealthCheck();
	const cancelHealthCheck = useCancelHealthCheck();
	const repairHealthItem = useRepairHealthItem();
	const { confirmDelete, confirmAction } = useConfirm();
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
		const confirmed = await confirmDelete("health record");
		if (confirmed) {
			await deleteItem.mutateAsync(id);
		}
	};

	const handleCleanup = () => {
		setCleanupConfig({
			older_than: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split("T")[0],
			delete_files: false,
		});
		setShowCleanupModal(true);
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

	const handleRefreshIntervalChange = (interval: number) => {
		setRefreshInterval(interval);
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
			<HealthPageHeader
				autoRefreshEnabled={autoRefreshEnabled}
				refreshInterval={refreshInterval}
				countdown={countdown}
				userInteracting={userInteracting}
				isLoading={isLoading}
				isCleanupPending={cleanupHealth.isPending}
				onToggleAutoRefresh={toggleAutoRefresh}
				onRefreshIntervalChange={handleRefreshIntervalChange}
				onRefresh={() => refetch()}
				onCleanup={handleCleanup}
				onUserInteractionStart={handleUserInteractionStart}
				onUserInteractionEnd={handleUserInteractionEnd}
			/>

			<HealthStatsCards stats={stats} />

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
			/>

			<div className="flex gap-2 mb-6">
				<button
					type="button"
					className={`btn ${activeTab === "files" ? "btn-primary" : "btn-ghost"}`}
					onClick={() => setActiveTab("files")}
				>
					File Health
				</button>
				<button
					type="button"
					className={`btn ${activeTab === "providers" ? "btn-primary" : "btn-ghost"}`}
					onClick={() => setActiveTab("providers")}
				>
					Provider Health
				</button>
			</div>

			{activeTab === "files" ? (
				<>
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
					/>

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

					<HealthStatusAlert stats={stats} />

					<CleanupModal
						show={showCleanupModal}
						config={cleanupConfig}
						isPending={cleanupHealth.isPending}
						onClose={() => setShowCleanupModal(false)}
						onConfigChange={setCleanupConfig}
						onConfirm={handleCleanupConfirm}
					/>
				</>
			) : (
				<ProviderHealth />
			)}
		</div>
	);
}
