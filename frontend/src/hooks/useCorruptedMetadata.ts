import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import type { CorruptedMetadataStats } from "../types/api";

const CORRUPTED_METADATA_KEY = ["metadata", "corrupted"];

export function useCorruptedMetadataStats() {
	return useQuery<CorruptedMetadataStats>({
		queryKey: CORRUPTED_METADATA_KEY,
		queryFn: () => apiClient.getCorruptedMetadataStats(),
		retry: 1,
	});
}

export function usePurgeCorruptedMetadata() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: () => apiClient.purgeCorruptedMetadata(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: CORRUPTED_METADATA_KEY });
		},
		onError: (error) => {
			console.error("Failed to purge corrupted metadata:", error);
		},
	});
}
