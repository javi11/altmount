import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";

// Queue hooks
export const useQueue = (params?: {
	limit?: number;
	offset?: number;
	status?: string;
	since?: string;
}) => {
	return useQuery({
		queryKey: ["queue", params],
		queryFn: () => apiClient.getQueue(params),
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
		mutationFn: ({
			id,
			resetRetryCount,
		}: {
			id: number;
			resetRetryCount?: boolean;
		}) => apiClient.retryQueueItem(id, resetRetryCount),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["queue"] });
		},
	});
};

export const useClearCompletedQueue = () => {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (olderThan?: string) =>
			apiClient.clearCompletedQueue(olderThan),
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
}) => {
	return useQuery({
		queryKey: ["health", params],
		queryFn: () => apiClient.getHealth(params),
	});
};

export const useHealthItem = (id: string) => {
	return useQuery({
		queryKey: ["health", id],
		queryFn: () => apiClient.getHealthItem(id),
		enabled: !!id,
	});
};

export const useCorruptedFiles = (params?: {
	limit?: number;
	offset?: number;
}) => {
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