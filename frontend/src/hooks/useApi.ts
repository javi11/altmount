import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";

// Queue hooks
export const useQueue = (params?: {
	limit?: number;
	offset?: number;
	status?: string;
	since?: string;
	search?: string;
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

export const useRetryQueueItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ id, resetRetryCount }: { id: number; resetRetryCount?: boolean }) =>
			apiClient.retryQueueItem(id, resetRetryCount),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
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

// Health hooks
export const useHealth = (params?: {
	limit?: number;
	offset?: number;
	status?: string;
	since?: string;
	search?: string;
}) => {
	return useQuery({
		queryKey: ["health", params],
		queryFn: () => apiClient.getHealth(params),
		refetchInterval: (query) => {
			// Poll every 5 seconds if any items are in "checking" status
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

export const useDeleteHealthItem = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (id: string) => apiClient.deleteHealthItem(id),
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

export const useCleanupHealth = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (params?: { older_than?: string; status?: string }) =>
			apiClient.cleanupHealth(params),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["health"] });
		},
	});
};

export const useAddHealthCheck = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (data: { file_path: string; source_nzb_path: string; priority?: boolean }) =>
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

export const useDirectHealthCheck = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (filePath: string) => apiClient.directHealthCheck(filePath),
		onSuccess: () => {
			// Immediately refresh health data to show "checking" status
			queryClient.invalidateQueries({ queryKey: ["health"] });
			queryClient.invalidateQueries({ queryKey: ["health", "worker", "status"] });
		},
	});
};

export const useCancelHealthCheck = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (filePath: string) => apiClient.cancelHealthCheck(filePath),
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

// NZB file upload hook
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
