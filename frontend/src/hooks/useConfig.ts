import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type { ConfigSection, ConfigUpdateRequest, ConfigValidateRequest } from "../types/config";

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
		mutationFn: ({ section, config }: { section: ConfigSection; config: ConfigUpdateRequest }) =>
			apiClient.updateConfigSection(section, config),
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
		mutationFn: (config: ConfigValidateRequest) => apiClient.validateConfig(config),
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

// Hook to restart server
export function useRestartServer() {
	return useMutation({
		mutationFn: (force = false) => apiClient.restartServer(force),
		onError: (error) => {
			console.error("Failed to restart server:", error);
		},
	});
}
