import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type {
	ProviderConfig,
	ProviderCreateRequest,
	ProviderReorderRequest,
	ProviderUpdateRequest,
} from "../types/config";
import { configKeys } from "./useConfig";

function useTestImportProviderSpeed() {
	const queryClient = useQueryClient();
	return useMutation<{ speed_mbps: number; duration_seconds: number }, Error, string>({
		mutationFn: (id) => apiClient.testImportProviderSpeed(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
	});
}

function useCreateImportProvider() {
	const queryClient = useQueryClient();
	return useMutation<ProviderConfig, Error, ProviderCreateRequest>({
		mutationFn: (data) => apiClient.createImportProvider(data),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to create import provider:", error);
		},
	});
}

function useUpdateImportProvider() {
	const queryClient = useQueryClient();
	return useMutation<ProviderConfig, Error, { id: string; data: Partial<ProviderUpdateRequest> }>({
		mutationFn: ({ id, data }) => apiClient.updateImportProvider(id, data),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to update import provider:", error);
		},
	});
}

function useDeleteImportProvider() {
	const queryClient = useQueryClient();
	return useMutation<{ message: string }, Error, string>({
		mutationFn: (id) => apiClient.deleteImportProvider(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to delete import provider:", error);
		},
	});
}

function useReorderImportProviders() {
	const queryClient = useQueryClient();
	return useMutation<ProviderConfig[], Error, ProviderReorderRequest>({
		mutationFn: (data) => apiClient.reorderImportProviders(data),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: configKeys.current() });
		},
		onError: (error) => {
			console.error("Failed to reorder import providers:", error);
		},
	});
}

export function useImportProviders() {
	const testProviderSpeed = useTestImportProviderSpeed();
	const createProvider = useCreateImportProvider();
	const updateProvider = useUpdateImportProvider();
	const deleteProvider = useDeleteImportProvider();
	const reorderProviders = useReorderImportProviders();

	return {
		testProviderSpeed,
		createProvider,
		updateProvider,
		deleteProvider,
		reorderProviders,
	};
}
