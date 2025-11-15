import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type APIError, apiClient } from "../api/client";
import { useToast } from "../contexts/ToastContext";
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
	const { showToast } = useToast();

	return useMutation({
		mutationFn: ({ section, config }: { section: ConfigSection; config: ConfigUpdateRequest }) =>
			apiClient.updateConfigSection(section, config),
		onSuccess: (data) => {
			// Update the cache with new configuration
			queryClient.setQueryData(configKeys.current(), data);
		},
		onError: (error) => {
			const err = error as APIError;
			console.error("Failed to update configuration section:", error);

			showToast({
				type: "error",
				title: "Update Failed",
				message: err.details,
			});
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
		mutationFn: (force?: boolean) => apiClient.restartServer(force),
		onError: (error) => {
			console.error("Failed to restart server:", error);
		},
	});
}

// Hook to check if library sync is needed
export function useLibrarySyncNeeded() {
	return useQuery({
		queryKey: ["library-sync", "needed"],
		queryFn: async () => {
			const response = await fetch("/api/health/library-sync/needed");
			if (!response.ok) {
				throw new Error("Failed to check library sync status");
			}
			const data = await response.json();
			return data.data as { needs_sync: boolean; reason: string };
		},
		refetchInterval: 10000, // Poll every 10 seconds
		staleTime: 5000,
	});
}

// Hook to trigger library sync
export function useTriggerLibrarySync() {
	const queryClient = useQueryClient();
	const { showToast } = useToast();

	return useMutation({
		mutationFn: async () => {
			const response = await fetch("/api/health/library-sync/start", {
				method: "POST",
			});
			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || "Failed to trigger library sync");
			}
			return response.json();
		},
		onSuccess: () => {
			// Invalidate the sync needed query
			queryClient.invalidateQueries({ queryKey: ["library-sync", "needed"] });

			showToast({
				type: "success",
				title: "Library Sync Started",
				message: "Library sync has been triggered successfully",
			});
		},
		onError: (error: Error) => {
			console.error("Failed to trigger library sync:", error);

			showToast({
				type: "error",
				title: "Sync Failed",
				message: error.message,
			});
		},
	});
}
