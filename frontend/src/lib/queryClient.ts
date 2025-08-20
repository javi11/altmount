import { QueryClient } from "@tanstack/react-query";
import type { ApiError } from "../types/error";

export const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			retry: (failureCount, error: unknown) => {
				// Don't retry on 4xx errors
				if ((error as ApiError)?.status >= 400 && (error as ApiError)?.status < 500) {
					return false;
				}
				return failureCount < 3;
			},
			staleTime: 1000 * 60 * 5, // 5 minutes
			refetchOnWindowFocus: false,
		},
		mutations: {
			retry: false,
		},
	},
});
