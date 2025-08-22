import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type {
	ConfigResponse,
	ConfigUpdateRequest,
	ConfigValidateRequest,
	ConfigSection,
} from "../types/config";

// Query keys for React Query
export const configKeys = {
	all: ["config"] as const,
	current: () => [...configKeys.all, "current"] as const,
};

// Hook to fetch current configuration
export function useConfig() {
	return useQuery({
		queryKey: configKeys.current(),
		queryFn: () => apiClient.getConfig(),
		staleTime: 1000 * 60 * 5, // 5 minutes
		refetchOnWindowFocus: false,
	});
}

// Hook to update entire configuration
export function useUpdateConfig() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (config: ConfigUpdateRequest) => apiClient.updateConfig(config),
		onSuccess: (data) => {
			// Update the cache with new configuration
			queryClient.setQueryData(configKeys.current(), data);
		},
		onError: (error) => {
			console.error("Failed to update configuration:", error);
		},
	});
}

// Hook to update specific configuration section
export function useUpdateConfigSection() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({
			section,
			config,
		}: {
			section: ConfigSection;
			config: ConfigUpdateRequest;
		}) => apiClient.updateConfigSection(section, config),
		onSuccess: (data) => {
			// Update the cache with new configuration
			queryClient.setQueryData(configKeys.current(), data);
		},
		onError: (error) => {
			console.error("Failed to update configuration section:", error);
		},
	});
}

// Hook to validate configuration
export function useValidateConfig() {
	return useMutation({
		mutationFn: (config: ConfigValidateRequest) =>
			apiClient.validateConfig(config),
		onError: (error) => {
			console.error("Failed to validate configuration:", error);
		},
	});
}

// Hook to reload configuration from file
export function useReloadConfig() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.reloadConfig(),
		onSuccess: (data) => {
			// Update the cache with reloaded configuration
			queryClient.setQueryData(configKeys.current(), data);
		},
		onError: (error) => {
			console.error("Failed to reload configuration:", error);
		},
	});
}

// Custom hook for optimistic updates with error handling
export function useOptimisticConfigUpdate() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: (config: ConfigUpdateRequest) => apiClient.updateConfig(config),
		onMutate: async (newConfig) => {
			// Cancel any outgoing refetches (so they don't overwrite our optimistic update)
			await queryClient.cancelQueries({ queryKey: configKeys.current() });

			// Snapshot the previous value
			const previousConfig = queryClient.getQueryData<ConfigResponse>(
				configKeys.current(),
			);

			// Optimistically update to the new value
			if (previousConfig) {
				const optimisticConfig = { ...previousConfig, ...newConfig };
				queryClient.setQueryData(configKeys.current(), optimisticConfig);
			}

			// Return a context object with the snapshotted value
			return { previousConfig };
		},
		onError: (_err, _newConfig, context) => {
			// If the mutation fails, use the context returned from onMutate to roll back
			if (context?.previousConfig) {
				queryClient.setQueryData(configKeys.current(), context.previousConfig);
			}
		},
		onSettled: () => {
			// Always refetch after error or success to ensure we have the latest data
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
	});
}

// Helper hook to check if user can edit specific configuration sections
export function useConfigPermissions() {
	// This could be enhanced with actual user permissions
	// For now, just check if user is admin (when auth is implemented)
	return {
		canEditWebDAV: true,
		canEditAPI: true,
		canEditDatabase: false, // Database path cannot be changed via API
		canEditMetadata: true,
		canEditRClone: true,
		canEditWorkers: true,
		canEditProviders: true,
		canEditSystem: true,
	};
}
