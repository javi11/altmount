import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type { LibrarySyncStatus } from "../types/api";

// Hook to get library sync status with dynamic polling
export function useLibrarySyncStatus() {
	// Fetch status with dynamic refetch interval
	return useQuery<LibrarySyncStatus>({
		queryKey: ["health", "library-sync", "status"],
		queryFn: () => apiClient.getLibrarySyncStatus(),
		// Retry failed requests up to 3 times for transient errors
		retry: 3,
		// Dynamic refetch interval based on running status
		refetchInterval: (query) => {
			// Stop polling on persistent errors to reduce unnecessary requests
			if (query.state.error) return false;

			if (!query.state.data) return 10000; // Poll every 10s if no data yet

			// Poll every 2 seconds when running for real-time progress
			if (query.state.data.is_running) {
				return 2000;
			}

			// Poll every 10 seconds when idle
			return 10000;
		},
		// Keep polling even when window is not focused if scan is running
		refetchIntervalInBackground: true,
	});
}

// Hook to start library sync
export function useStartLibrarySync() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.startLibrarySync(),
		onSuccess: () => {
			// Invalidate and refetch library sync status immediately
			queryClient.invalidateQueries({ queryKey: ["health", "library-sync"] });
		},
		onError: (error) => {
			console.error("Failed to start library sync:", error);
		},
	});
}

// Hook to cancel library sync
export function useCancelLibrarySync() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.cancelLibrarySync(),
		onSuccess: () => {
			// Invalidate and refetch library sync status immediately
			queryClient.invalidateQueries({ queryKey: ["health", "library-sync"] });
		},
		onError: (error) => {
			console.error("Failed to cancel library sync:", error);
		},
	});
}
