import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type { HealthCleanupRequest, HealthPriority } from "../types/api";

// Queue hooks
export const useQueue = (params?: {
	limit?: number;
	offset?: number;
	status?: string;
	since?: string;
	search?: string;
	sort_by?: string;
	sort_order?: "asc" | "desc";
	refetchInterval?: number;
}) => {
	return useQuery({
		queryKey: ["queue", params],
		queryFn: () => apiClient.getQueue(params),
		refetchInterval: params?.refetchInterval,
	});
};

export const useQueueItem = (id: number) => {
	return useQuery({
		queryKey: ["queue", id],
		queryFn: () => apiClient.getQueueItem(id),
		enabled: !!id,
	});
};

export const useQueueStats = () => {
	return useQuery({
		queryKey: ["queue", "stats"],
		queryFn: () => apiClient.getQueueStats(),
		refetchInterval: 5000, // Refetch every 5 seconds
	});
};

export const useDeleteQueueItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: number) => apiClient.deleteQueueItem(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

export const useDeleteBulkQueueItems = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (ids: number[]) => apiClient.deleteBulkQueueItems(ids),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

export const useRestartBulkQueueItems = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (ids: number[]) => apiClient.restartBulkQueueItems(ids),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
			queryClient.invalidateQueries({ queryKey: ["queue", "stats"] });
		},
	});
};

export const useRetryQueueItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: number) => apiClient.retryQueueItem(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

export const useCancelQueueItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: number) => apiClient.cancelQueueItem(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
			queryClient.invalidateQueries({ queryKey: ["queue-stats"] });
		},
	});
};

export const useBulkCancelQueueItems = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (ids: number[]) => apiClient.cancelBulkQueueItems(ids),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
			queryClient.invalidateQueries({ queryKey: ["queue-stats"] });
		},
	});
};

export const useClearCompletedQueue = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (olderThan?: string) => apiClient.clearCompletedQueue(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

export const useClearFailedQueue = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (olderThan?: string) => apiClient.clearFailedQueue(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

export const useClearPendingQueue = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (olderThan?: string) => apiClient.clearPendingQueue(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

// Health hooks
export const useHealth = (params?: {
	limit?: number;
	offset?: number;
	status?: string;
	since?: string;
	search?: string;
	sort_by?: string;
	sort_order?: "asc" | "desc";
	refetchInterval?: number;
}) => {
	return useQuery({
		queryKey: ["health", params],
		queryFn: () => apiClient.getHealth(params),
		refetchInterval: (query) => {
			// Use custom refetch interval if provided
			if (params?.refetchInterval !== undefined) {
				return params.refetchInterval;
			}
			// Otherwise, poll every 5 seconds if any items are in "checking" status
			const data = query.state.data?.data;
			const hasCheckingItems = data?.some((item) => item.status === "checking");
			return hasCheckingItems ? 5000 : false;
		},
	});
};

export const useHealthItem = (id: string) => {
	return useQuery({
		queryKey: ["health", id],
		queryFn: () => apiClient.getHealthItem(id),
		enabled: !!id,
	});
};

export const useCorruptedFiles = (params?: { limit?: number; offset?: number }) => {
	return useQuery({
		queryKey: ["health", "corrupted", params],
		queryFn: () => apiClient.getCorruptedFiles(params),
	});
};

export const useHealthStats = () => {
	return useQuery({
		queryKey: ["health", "stats"],
		queryFn: () => apiClient.getHealthStats(),
		refetchInterval: 5000, // Refetch every 5 seconds
	});
};

export const useResetAllHealthChecks = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.resetAllHealthChecks(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
			queryClient.invalidateQueries({ queryKey: ["health", "stats"] });
		},
	});
};

export const useRegenerateSymlinks = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.regenerateSymlinks(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
			queryClient.invalidateQueries({ queryKey: ["health", "stats"] });
		},
	});
};

export const useDeleteHealthItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: number) => apiClient.deleteHealthItem(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useDeleteBulkHealthItems = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (filePaths: string[]) => apiClient.deleteBulkHealthItems(filePaths),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useRestartBulkHealthItems = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (filePaths: string[]) => apiClient.restartBulkHealthItems(filePaths),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useRepairBulkHealthItems = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (filePaths: string[]) => apiClient.repairBulkHealthItems(filePaths),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useRetryHealthItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ id, resetStatus }: { id: string; resetStatus?: boolean }) =>
			apiClient.retryHealthItem(id, resetStatus),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useRepairHealthItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ id, resetRepairRetryCount }: { id: number; resetRepairRetryCount?: boolean }) =>
			apiClient.repairHealthItem(id, resetRepairRetryCount),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useCleanupHealth = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (params?: HealthCleanupRequest) => apiClient.cleanupHealth(params),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

// ... surrounding code ...
export const useAddHealthCheck = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (data: { file_path: string; source_nzb_path: string; priority?: HealthPriority }) =>
			apiClient.addHealthCheck(data),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useHealthWorkerStatus = () => {
	return useQuery({
		queryKey: ["health", "worker", "status"],
		queryFn: () => apiClient.getHealthWorkerStatus(),
		refetchInterval: 5000,
	});
};

export const usePoolMetrics = () => {
	return useQuery({
		queryKey: ["system", "pool", "metrics"],
		queryFn: () => apiClient.getPoolMetrics(),
		refetchInterval: 5000, // Refetch every 5 seconds
	});
};

export const useActiveStreams = () => {
	return useQuery({
		queryKey: ["files", "active-streams"],
		queryFn: () => apiClient.getActiveStreams(),
		refetchInterval: 3000, // Refetch every 3 seconds for liveliness
	});
};

export const useDirectHealthCheck = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: number) => apiClient.directHealthCheck(id),
		onSuccess: () => {
			// Immediately refresh health data to show "checking" status
			queryClient.invalidateQueries({ queryKey: ["health"] });
			queryClient.invalidateQueries({ queryKey: ["health", "worker", "status"] });
		},
	});
};

export const useSetHealthPriority = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ id, priority }: { id: number; priority: HealthPriority }) =>
			apiClient.setHealthPriority(id, priority),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useCancelHealthCheck = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: number) => apiClient.cancelHealthCheck(id),
		onSuccess: () => {
			// Immediately refresh health data to show cancelled status
			queryClient.invalidateQueries({ queryKey: ["health"] });
			queryClient.invalidateQueries({ queryKey: ["health", "worker", "status"] });
		},
	});
};

// Manual Scan hooks
export const useScanStatus = (refetchInterval?: number) => {
	return useQuery({
		queryKey: ["scan", "status"],
		queryFn: () => apiClient.getScanStatus(),
		refetchInterval: refetchInterval,
	});
};

export const useStartManualScan = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (path: string) => apiClient.startManualScan({ path }),
		onSuccess: () => {
			// Invalidate scan status to update immediately
			queryClient.invalidateQueries({ queryKey: ["scan", "status"] });
			// Invalidate queue to refresh when scan completes
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

export const useCancelScan = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.cancelScan(),
		onSuccess: () => {
			// Invalidate scan status to update immediately
			queryClient.invalidateQueries({ queryKey: ["scan", "status"] });
		},
	});
};

// NZBDav Import hooks
export const useNzbdavImportStatus = (refetchInterval?: number) => {
	return useQuery({
		queryKey: ["import", "nzbdav", "status"],
		queryFn: () => apiClient.getNzbdavImportStatus(),
		refetchInterval: refetchInterval,
	});
};

export const useResetNzbdavImportStatus = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.resetNzbdavImportStatus(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["import", "nzbdav", "status"] });
		},
	});
};

export const useCancelNzbdavImport = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.cancelNzbdavImport(),
		onSuccess: () => {
			// Invalidate scan status to update immediately
			queryClient.invalidateQueries({ queryKey: ["import", "nzbdav", "status"] });
		},
	});
};

// NZB file upload hook (SABnzbd API)
export const useUploadNzb = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ file, apiKey }: { file: File; apiKey: string }) =>
			apiClient.uploadNzbFile(file, apiKey),
		onSuccess: () => {
			// Invalidate queue data to show newly uploaded files
			queryClient.invalidateQueries({ queryKey: ["queue"] });
			queryClient.invalidateQueries({ queryKey: ["queue", "stats"] });
		},
	});
};

// Native upload hook (using JWT authentication)
export const useUploadToQueue = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({
			file,
			category,
			priority,
			relativePath,
		}: {
			file: File;
			category?: string;
			priority?: number;
			relativePath?: string;
		}) => apiClient.uploadToQueue(file, category, priority, relativePath),
		onSuccess: () => {
			// Invalidate queue data to show newly uploaded files
			queryClient.invalidateQueries({ queryKey: ["queue"] });
			queryClient.invalidateQueries({ queryKey: ["queue", "stats"] });
		},
	});
};

export const useUploadNZBLnks = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({
			links,
			category,
			priority,
			relativePath,
		}: {
			links: string[];
			category?: string;
			priority?: number;
			relativePath?: string;
		}) => apiClient.uploadNZBLnks(links, category, priority, relativePath),
		onSuccess: () => {
			// Invalidate queue data to show newly added items
			queryClient.invalidateQueries({ queryKey: ["queue"] });
			queryClient.invalidateQueries({ queryKey: ["queue", "stats"] });
		},
	});
};

export const useAddTestQueueItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (size: "100MB" | "1GB" | "10GB") => apiClient.addTestQueueItem(size),
		onSuccess: () => {
			// Invalidate queue data to show newly added test file
			queryClient.invalidateQueries({ queryKey: ["queue"] });
			queryClient.invalidateQueries({ queryKey: ["queue", "stats"] });
		},
	});
};

export const useSystemBrowse = (path?: string) => {
	return useQuery({
		queryKey: ["system", "browse", path],
		queryFn: () => apiClient.getSystemBrowse(path),
	});
};

// ARR Webhook Registration hook
export const useRegisterArrsWebhooks = () => {
	return useMutation({
		mutationFn: () => apiClient.registerArrsWebhooks(),
	});
};

// ARR Download Client Registration hook
export const useRegisterArrsDownloadClients = () => {
	return useMutation({
		mutationFn: () => apiClient.registerArrsDownloadClients(),
	});
};

// ARR Download Client Test hook
export const useTestArrsDownloadClients = () => {
	return useMutation({
		mutationFn: () => apiClient.testArrsDownloadClients(),
	});
};
