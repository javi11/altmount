import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type { MetadataMigrationResult, MetadataMigrationStatus } from "../types/api";

const MIGRATION_KEY = ["metadata", "migration"];

// Hook to poll migration status: fast while running, slow when idle.
export function useMetadataMigrationStatus() {
	return useQuery<MetadataMigrationStatus>({
		queryKey: [...MIGRATION_KEY, "status"],
		queryFn: () => apiClient.getMetadataMigrationStatus(),
		retry: 3,
		refetchInterval: (query) => {
			if (query.state.error) return false;
			if (!query.state.data) return 10000;
			return query.state.data.is_running ? 2000 : 10000;
		},
		refetchIntervalInBackground: true,
	});
}

// Hook to run a dry run. Resolves with the measured result.
export function useDryRunMetadataMigration() {
	const queryClient = useQueryClient();

	return useMutation<MetadataMigrationResult>({
		mutationFn: () => apiClient.dryRunMetadataMigration(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: MIGRATION_KEY });
		},
		onError: (error) => {
			console.error("Metadata migration dry run failed:", error);
		},
	});
}

// Hook to start the migration.
export function useStartMetadataMigration() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.startMetadataMigration(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: MIGRATION_KEY });
		},
		onError: (error) => {
			console.error("Failed to start metadata migration:", error);
		},
	});
}

// Hook to cancel an in-flight migration.
export function useCancelMetadataMigration() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.cancelMetadataMigration(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: MIGRATION_KEY });
		},
		onError: (error) => {
			console.error("Failed to cancel metadata migration:", error);
		},
	});
}
