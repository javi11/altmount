import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type {
	ProviderConfig,
	ProviderCreateRequest,
	ProviderReorderRequest,
	ProviderTestRequest,
	ProviderTestResponse,
	ProviderUpdateRequest,
} from "../types/config";
import { configKeys } from "./useConfig";

// Test provider connectivity
function useTestProvider() {
	const queryClient = useQueryClient();
	return useMutation<ProviderTestResponse, Error, ProviderTestRequest>({
		mutationFn: (data) => apiClient.testProvider(data),
		onSuccess: () => {
			// Invalidate config cache to refetch providers (including RTT)
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
	});
}

// Test provider speed
function useTestProviderSpeed() {
	const queryClient = useQueryClient();
	return useMutation<{ speed_mbps: number; duration_seconds: number }, Error, string>({
		mutationFn: (id) => apiClient.testProviderSpeed(id),
		onSuccess: () => {
			// Invalidate config cache to refetch providers
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
	});
}

// Create new provider
function useCreateProvider() {
	const queryClient = useQueryClient();

	return useMutation<ProviderConfig, Error, ProviderCreateRequest>({
		mutationFn: (data) => apiClient.createProvider(data),
		onSuccess: () => {
			// Invalidate config cache to refetch providers
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to create provider:", error);
		},
	});
}

// Update existing provider
function useUpdateProvider() {
	const queryClient = useQueryClient();

	return useMutation<ProviderConfig, Error, { id: string; data: Partial<ProviderUpdateRequest> }>({
		mutationFn: ({ id, data }) => apiClient.updateProvider(id, data),
		onSuccess: () => {
			// Invalidate config cache to refetch providers
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to update provider:", error);
		},
	});
}

// Delete provider
function useDeleteProvider() {
	const queryClient = useQueryClient();

	return useMutation<{ message: string }, Error, string>({
		mutationFn: (id) => apiClient.deleteProvider(id),
		onSuccess: () => {
			// Invalidate config cache to refetch providers
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to delete provider:", error);
		},
	});
}

// Reset provider quota
function useResetProviderQuota() {
	const queryClient = useQueryClient();

	return useMutation<{ message: string }, Error, string>({
		mutationFn: (id) => apiClient.resetProviderQuota(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
			queryClient.invalidateQueries({ queryKey: ["system", "pool", "metrics"] });
		},
	});
}

// Reorder providers
function useReorderProviders() {
	const queryClient = useQueryClient();

	return useMutation<ProviderConfig[], Error, ProviderReorderRequest>({
		mutationFn: (data) => apiClient.reorderProviders(data),
		onSuccess: () => {
			// Invalidate config cache to refetch providers
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to reorder providers:", error);
		},
	});
}

// Combined hook for easier usage
export function useProviders() {
	const testProvider = useTestProvider();
	const testProviderSpeed = useTestProviderSpeed();
	const createProvider = useCreateProvider();
	const updateProvider = useUpdateProvider();
	const deleteProvider = useDeleteProvider();
	const resetProviderQuota = useResetProviderQuota();
	const reorderProviders = useReorderProviders();

	return {
		testProvider,
		testProviderSpeed,
		createProvider,
		updateProvider,
		deleteProvider,
		resetProviderQuota,
		reorderProviders,
	};
}
