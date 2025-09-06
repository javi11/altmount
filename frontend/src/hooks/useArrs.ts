import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import React from "react";
import type { ArrsInstance, SyncProgress, SyncResult } from "../types/config";

// API functions for arrs operations
const arrsAPI = {
	// Get sync status for a specific instance by type and name
	getSyncStatus: async (instanceType: string, instanceName: string): Promise<SyncProgress> => {
		const response = await fetch(`/api/arrs/instances/${instanceType}/${instanceName}/status`);
		if (!response.ok) {
			if (response.status === 404) {
				throw new Error("No active sync for this instance");
			}
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Get last sync result for a specific instance by type and name
	getLastSyncResult: async (instanceType: string, instanceName: string): Promise<SyncResult> => {
		const response = await fetch(`/api/arrs/instances/${instanceType}/${instanceName}/result`);
		if (!response.ok) {
			if (response.status === 404) {
				throw new Error("No sync result found for this instance");
			}
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Get all active syncs
	getAllActiveSyncs: async (): Promise<SyncProgress[]> => {
		const response = await fetch("/api/arrs/active");
		if (!response.ok) {
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Trigger manual sync for an instance by type and name
	triggerSync: async (instanceType: string, instanceName: string): Promise<void> => {
		const response = await fetch(`/api/arrs/instances/${instanceType}/${instanceName}/sync`, {
			method: "POST",
		});
		if (!response.ok) {
			const errorData = await response.json().catch(() => ({}));
			throw new Error(errorData.error || `HTTP ${response.status}: ${response.statusText}`);
		}
	},

	// Cancel active sync for an instance by type and name
	cancelSync: async (instanceType: string, instanceName: string): Promise<void> => {
		const response = await fetch(`/api/arrs/instances/${instanceType}/${instanceName}/cancel`, {
			method: "POST",
		});
		if (!response.ok) {
			const errorData = await response.json().catch(() => ({}));
			throw new Error(errorData.error || `HTTP ${response.status}: ${response.statusText}`);
		}
	},

	// Get all configuration-based instances
	getInstances: async (): Promise<ArrsInstance[]> => {
		const response = await fetch("/api/arrs/instances");
		if (!response.ok) {
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Get a specific configuration-based instance by type and name
	getInstance: async (instanceType: string, instanceName: string): Promise<ArrsInstance> => {
		const response = await fetch(`/api/arrs/instances/${instanceType}/${instanceName}`);
		if (!response.ok) {
			if (response.status === 404) {
				throw new Error("Instance not found");
			}
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},
};

// Query keys for React Query
export const arrsKeys = {
	all: ["arrs"] as const,
	instances: () => [...arrsKeys.all, "instances"] as const,
	instance: (instanceType: string, instanceName: string) =>
		[...arrsKeys.all, "instance", instanceType, instanceName] as const,
	status: (instanceType: string, instanceName: string) =>
		[...arrsKeys.all, "status", instanceType, instanceName] as const,
	result: (instanceType: string, instanceName: string) =>
		[...arrsKeys.all, "result", instanceType, instanceName] as const,
	activeSyncs: () => [...arrsKeys.all, "active"] as const,
};

// Hook to get sync status for a specific instance
export function useSyncStatus(instanceType: string, instanceName: string, enabled = true) {
	return useQuery({
		queryKey: arrsKeys.status(instanceType, instanceName),
		queryFn: () => arrsAPI.getSyncStatus(instanceType, instanceName),
		enabled: enabled && !!instanceType && !!instanceName,
		refetchInterval: (query) => {
			// Poll every 2 seconds if sync is active
			// Continue polling for 5 seconds after completion to catch transition
			const data = query.state.data as SyncProgress | undefined;

			if (data?.status === "running" || data?.status === "cancelling") {
				return 2000; // Poll every 2 seconds during active sync
			}

			if (data?.status === "completed" || data?.status === "failed") {
				// Continue polling for 5 seconds after completion to ensure frontend catches it
				const completedTime = data.started_at ? new Date(data.started_at).getTime() : 0;
				const now = Date.now();
				if (now - completedTime < 5000) {
					// 5 seconds
					return 1000; // Poll every second during transition period
				}
			}

			return false; // Stop polling
		},
		retry: (failureCount, error) => {
			// Don't retry if it's a 404 (no active sync)
			if (error?.message?.includes("No active sync")) {
				return false;
			}
			return failureCount < 3;
		},
	});
}

// Hook to get last sync result for a specific instance
export function useLastSyncResult(instanceType: string, instanceName: string, enabled = true) {
	return useQuery({
		queryKey: arrsKeys.result(instanceType, instanceName),
		queryFn: () => arrsAPI.getLastSyncResult(instanceType, instanceName),
		enabled: enabled && !!instanceType && !!instanceName,
		staleTime: 1000 * 60 * 5, // 5 minutes
		retry: (failureCount, error) => {
			// Don't retry if it's a 404 (no result found)
			if (error?.message?.includes("No sync result found")) {
				return false;
			}
			return failureCount < 3;
		},
	});
}

// Hook to get all active syncs
export function useActiveSyncs(enabled = true) {
	return useQuery({
		queryKey: arrsKeys.activeSyncs(),
		queryFn: arrsAPI.getAllActiveSyncs,
		enabled,
		refetchInterval: 5000, // Poll every 5 seconds
		staleTime: 1000 * 30, // 30 seconds
	});
}

// Hook to trigger manual sync
export function useTriggerSync() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ instanceType, instanceName }: { instanceType: string; instanceName: string }) =>
			arrsAPI.triggerSync(instanceType, instanceName),
		onSuccess: (_, { instanceType, instanceName }) => {
			// Invalidate status queries to trigger immediate refetch
			queryClient.invalidateQueries({ queryKey: arrsKeys.status(instanceType, instanceName) });
			queryClient.invalidateQueries({ queryKey: arrsKeys.activeSyncs() });

			// Start polling immediately by enabling the query
			queryClient.refetchQueries({ queryKey: arrsKeys.status(instanceType, instanceName) });
		},
		onError: (error) => {
			console.error("Failed to trigger sync:", error);
		},
	});
}

// Hook to cancel active sync
export function useCancelSync() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ instanceType, instanceName }: { instanceType: string; instanceName: string }) =>
			arrsAPI.cancelSync(instanceType, instanceName),
		onSuccess: (_, { instanceType, instanceName }) => {
			// Invalidate status queries to trigger immediate refetch
			queryClient.invalidateQueries({ queryKey: arrsKeys.status(instanceType, instanceName) });
			queryClient.invalidateQueries({ queryKey: arrsKeys.activeSyncs() });
			// Also invalidate the result query as cancellation creates a result
			queryClient.invalidateQueries({ queryKey: arrsKeys.result(instanceType, instanceName) });

			// Continue polling to catch the cancellation transition
			queryClient.refetchQueries({ queryKey: arrsKeys.status(instanceType, instanceName) });
		},
		onError: (error) => {
			console.error("Failed to cancel sync:", error);
		},
	});
}

// Hook to get all configuration-based instances
export function useArrsInstances(enabled = true) {
	return useQuery({
		queryKey: arrsKeys.instances(),
		queryFn: arrsAPI.getInstances,
		enabled,
		staleTime: 1000 * 60 * 5, // 5 minutes
	});
}

// Hook to get a specific configuration-based instance
export function useArrsInstance(instanceType: string, instanceName: string, enabled = true) {
	return useQuery({
		queryKey: arrsKeys.instance(instanceType, instanceName),
		queryFn: () => arrsAPI.getInstance(instanceType, instanceName),
		enabled: enabled && !!instanceType && !!instanceName,
		staleTime: 1000 * 60 * 5, // 5 minutes
		retry: (failureCount, error) => {
			// Don't retry if it's a 404 (instance not found)
			if (error?.message?.includes("Instance not found")) {
				return false;
			}
			return failureCount < 3;
		},
	});
}

// Hook to get combined arrs information for an instance
export function useArrsInstanceInfo(instanceType: string, instanceName: string, enabled = true) {
	const queryClient = useQueryClient();
	const statusQuery = useSyncStatus(instanceType, instanceName, enabled);
	const resultQuery = useLastSyncResult(instanceType, instanceName, enabled);

	// Track previous status to detect completion transitions
	const prevStatusRef = React.useRef<string | undefined>(undefined);

	// When status changes from running to completed/failed, invalidate result query
	React.useEffect(() => {
		const currentStatus = statusQuery.data?.status;
		const prevStatus = prevStatusRef.current;

		if (prevStatus && currentStatus && prevStatus !== currentStatus) {
			// If status changed from running to completed/failed, refetch result immediately
			if (
				prevStatus === "running" &&
				(currentStatus === "completed" || currentStatus === "failed")
			) {
				// Invalidate and refetch the result query to get the fresh result
				queryClient.invalidateQueries({ queryKey: arrsKeys.result(instanceType, instanceName) });
			}
		}

		prevStatusRef.current = currentStatus;
	}, [statusQuery.data?.status, instanceType, instanceName, queryClient]);

	return {
		status: statusQuery.data,
		result: resultQuery.data,
		isStatusLoading: statusQuery.isLoading,
		isResultLoading: resultQuery.isLoading,
		hasActiveStatus:
			statusQuery.data?.status === "running" || statusQuery.data?.status === "cancelling",
		hasResult: !resultQuery.isError,
		refetchStatus: statusQuery.refetch,
		refetchResult: resultQuery.refetch,
	};
}
