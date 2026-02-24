import { useMutation, useQuery } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type { UpdateChannel } from "../types/update";

export function useUpdateStatus(channel: UpdateChannel, enabled: boolean) {
	return useQuery({
		queryKey: ["system", "update", "status", channel],
		queryFn: () => apiClient.checkUpdateStatus(channel),
		enabled,
		staleTime: 60 * 1000, // Cache for 1 minute
	});
}

export function useApplyUpdate() {
	return useMutation({
		mutationFn: (channel: UpdateChannel) => apiClient.applyUpdate(channel),
	});
}
