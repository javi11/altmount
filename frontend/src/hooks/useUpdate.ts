import { useMutation, useQuery } from "@tanstack/react-query";
import { useToast } from "../contexts/ToastContext";
import type { UpdateStatusResponse } from "../types/update";

export const updateKeys = {
	all: ["update"] as const,
	status: () => [...updateKeys.all, "status"] as const,
};

export function useUpdateStatus() {
	return useQuery({
		queryKey: updateKeys.status(),
		queryFn: async (): Promise<UpdateStatusResponse> => {
			const response = await fetch("/api/update/status");
			if (!response.ok) throw new Error("Failed to fetch update status");
			const data = await response.json();
			return data.data as UpdateStatusResponse;
		},
		staleTime: 1000 * 60 * 5,
		refetchOnWindowFocus: false,
	});
}

export function useApplyUpdate() {
	const { showToast } = useToast();
	return useMutation({
		mutationFn: async () => {
			const response = await fetch("/api/update/apply", { method: "POST" });
			if (!response.ok) throw new Error("Failed to apply update");
			return response.json();
		},
		onSuccess: () => {
			showToast({
				type: "success",
				title: "Update Applied",
				message: "The application is restarting with the new version...",
			});
		},
		onError: (error: Error) => {
			showToast({
				type: "error",
				title: "Update Failed",
				message: error.message,
			});
		},
	});
}
