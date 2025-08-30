import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ScrapeProgress, ScrapeResult, ScraperInstance } from "../types/config";

// API functions for scraper operations
const scraperAPI = {
	// Get scrape status for a specific instance by type and name
	getScrapeStatus: async (instanceType: string, instanceName: string): Promise<ScrapeProgress> => {
		const response = await fetch(`/api/scraper/instances/${instanceType}/${instanceName}/status`);
		if (!response.ok) {
			if (response.status === 404) {
				throw new Error("No active scraping for this instance");
			}
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Get last scrape result for a specific instance by type and name
	getLastScrapeResult: async (instanceType: string, instanceName: string): Promise<ScrapeResult> => {
		const response = await fetch(`/api/scraper/instances/${instanceType}/${instanceName}/result`);
		if (!response.ok) {
			if (response.status === 404) {
				throw new Error("No scrape result found for this instance");
			}
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Get all active scrapes
	getAllActiveScrapes: async (): Promise<ScrapeProgress[]> => {
		const response = await fetch("/api/scraper/active");
		if (!response.ok) {
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Trigger manual scrape for an instance by type and name
	triggerScrape: async (instanceType: string, instanceName: string): Promise<void> => {
		const response = await fetch(`/api/scraper/instances/${instanceType}/${instanceName}/scrape`, {
			method: "POST",
		});
		if (!response.ok) {
			const errorData = await response.json().catch(() => ({}));
			throw new Error(errorData.error || `HTTP ${response.status}: ${response.statusText}`);
		}
	},

	// Cancel active scraping for an instance by type and name
	cancelScrape: async (instanceType: string, instanceName: string): Promise<void> => {
		const response = await fetch(`/api/scraper/instances/${instanceType}/${instanceName}/cancel`, {
			method: "POST",
		});
		if (!response.ok) {
			const errorData = await response.json().catch(() => ({}));
			throw new Error(errorData.error || `HTTP ${response.status}: ${response.statusText}`);
		}
	},

	// Get all configuration-based instances
	getInstances: async (): Promise<ScraperInstance[]> => {
		const response = await fetch("/api/scraper/instances");
		if (!response.ok) {
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},

	// Get a specific configuration-based instance by type and name
	getInstance: async (instanceType: string, instanceName: string): Promise<ScraperInstance> => {
		const response = await fetch(`/api/scraper/instances/${instanceType}/${instanceName}`);
		if (!response.ok) {
			if (response.status === 404) {
				throw new Error("Instance not found");
			}
			throw new Error(`HTTP ${response.status}: ${response.statusText}`);
		}
		return response.json();
	},
};

// Query keys for React Query
export const scraperKeys = {
	all: ["scraper"] as const,
	instances: () => [...scraperKeys.all, "instances"] as const,
	instance: (instanceType: string, instanceName: string) => [...scraperKeys.all, "instance", instanceType, instanceName] as const,
	status: (instanceType: string, instanceName: string) => [...scraperKeys.all, "status", instanceType, instanceName] as const,
	result: (instanceType: string, instanceName: string) => [...scraperKeys.all, "result", instanceType, instanceName] as const,
	activeScrapes: () => [...scraperKeys.all, "active"] as const,
};

// Hook to get scrape status for a specific instance
export function useScrapeStatus(instanceType: string, instanceName: string, enabled = true) {
	return useQuery({
		queryKey: scraperKeys.status(instanceType, instanceName),
		queryFn: () => scraperAPI.getScrapeStatus(instanceType, instanceName),
		enabled: enabled && !!instanceType && !!instanceName,
		refetchInterval: (query) => {
			// Poll every 2 seconds if scraping is active
			// Continue polling for 5 seconds after completion to catch transition
			const data = query.state.data as ScrapeProgress | undefined;
			
			if (data?.status === "running" || data?.status === "cancelling") {
				return 2000; // Poll every 2 seconds during active scraping
			}
			
			if (data?.status === "completed" || data?.status === "failed") {
				// Continue polling for 5 seconds after completion to ensure frontend catches it
				const completedTime = data.started_at ? new Date(data.started_at).getTime() : 0;
				const now = Date.now();
				if (now - completedTime < 5000) { // 5 seconds
					return 1000; // Poll every second during transition period
				}
			}
			
			return false; // Stop polling
		},
		retry: (failureCount, error) => {
			// Don't retry if it's a 404 (no active scraping)
			if (error?.message?.includes("No active scraping")) {
				return false;
			}
			return failureCount < 3;
		},
	});
}

// Hook to get last scrape result for a specific instance
export function useLastScrapeResult(instanceType: string, instanceName: string, enabled = true) {
	return useQuery({
		queryKey: scraperKeys.result(instanceType, instanceName),
		queryFn: () => scraperAPI.getLastScrapeResult(instanceType, instanceName),
		enabled: enabled && !!instanceType && !!instanceName,
		staleTime: 1000 * 60 * 5, // 5 minutes
		retry: (failureCount, error) => {
			// Don't retry if it's a 404 (no result found)
			if (error?.message?.includes("No scrape result found")) {
				return false;
			}
			return failureCount < 3;
		},
	});
}

// Hook to get all active scrapes
export function useActiveScrapes(enabled = true) {
	return useQuery({
		queryKey: scraperKeys.activeScrapes(),
		queryFn: scraperAPI.getAllActiveScrapes,
		enabled,
		refetchInterval: 5000, // Poll every 5 seconds
		staleTime: 1000 * 30, // 30 seconds
	});
}

// Hook to trigger manual scraping
export function useTriggerScrape() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ instanceType, instanceName }: { instanceType: string; instanceName: string }) => 
			scraperAPI.triggerScrape(instanceType, instanceName),
		onSuccess: (_, { instanceType, instanceName }) => {
			// Invalidate status queries to trigger immediate refetch
			queryClient.invalidateQueries({ queryKey: scraperKeys.status(instanceType, instanceName) });
			queryClient.invalidateQueries({ queryKey: scraperKeys.activeScrapes() });
			
			// Start polling immediately by enabling the query
			queryClient.refetchQueries({ queryKey: scraperKeys.status(instanceType, instanceName) });
		},
		onError: (error) => {
			console.error("Failed to trigger scrape:", error);
		},
	});
}

// Hook to cancel active scraping
export function useCancelScrape() {
	const queryClient = useQueryClient();

	return useMutation({
		mutationFn: ({ instanceType, instanceName }: { instanceType: string; instanceName: string }) => 
			scraperAPI.cancelScrape(instanceType, instanceName),
		onSuccess: (_, { instanceType, instanceName }) => {
			// Invalidate status queries to trigger immediate refetch
			queryClient.invalidateQueries({ queryKey: scraperKeys.status(instanceType, instanceName) });
			queryClient.invalidateQueries({ queryKey: scraperKeys.activeScrapes() });
			// Also invalidate the result query as cancellation creates a result
			queryClient.invalidateQueries({ queryKey: scraperKeys.result(instanceType, instanceName) });
			
			// Continue polling to catch the cancellation transition
			queryClient.refetchQueries({ queryKey: scraperKeys.status(instanceType, instanceName) });
		},
		onError: (error) => {
			console.error("Failed to cancel scrape:", error);
		},
	});
}

// Hook to get all configuration-based instances
export function useScraperInstances(enabled = true) {
	return useQuery({
		queryKey: scraperKeys.instances(),
		queryFn: scraperAPI.getInstances,
		enabled,
		staleTime: 1000 * 60 * 5, // 5 minutes
	});
}

// Hook to get a specific configuration-based instance
export function useScraperInstance(instanceType: string, instanceName: string, enabled = true) {
	return useQuery({
		queryKey: scraperKeys.instance(instanceType, instanceName),
		queryFn: () => scraperAPI.getInstance(instanceType, instanceName),
		enabled: enabled && !!instanceType && !!instanceName,
		staleTime: 1000 * 60 * 5, // 5 minutes
		retry: (failureCount, error) => {
			// Don't retry if it's a 404 (instance not found)
			if (error?.message?.includes("Instance not found")) {
				return false;
			}
			return failureCount < 3;
		},
	});
}

// Hook to get combined scraper information for an instance
export function useScraperInstanceInfo(instanceType: string, instanceName: string, enabled = true) {
	const queryClient = useQueryClient();
	const statusQuery = useScrapeStatus(instanceType, instanceName, enabled);
	const resultQuery = useLastScrapeResult(instanceType, instanceName, enabled);

	// Track previous status to detect completion transitions
	const prevStatusRef = React.useRef<string | undefined>(undefined);
	
	// When status changes from running to completed/failed, invalidate result query
	React.useEffect(() => {
		const currentStatus = statusQuery.data?.status;
		const prevStatus = prevStatusRef.current;
		
		if (prevStatus && currentStatus && prevStatus !== currentStatus) {
			// If status changed from running to completed/failed, refetch result immediately
			if (prevStatus === "running" && (currentStatus === "completed" || currentStatus === "failed")) {
				// Invalidate and refetch the result query to get the fresh result
				queryClient.invalidateQueries({ queryKey: scraperKeys.result(instanceType, instanceName) });
			}
		}
		
		prevStatusRef.current = currentStatus;
	}, [statusQuery.data?.status, instanceType, instanceName, queryClient]);

	return {
		status: statusQuery.data,
		result: resultQuery.data,
		isStatusLoading: statusQuery.isLoading,
		isResultLoading: resultQuery.isLoading,
		hasActiveStatus: !statusQuery.isError,
		hasResult: !resultQuery.isError,
		refetchStatus: statusQuery.refetch,
		refetchResult: resultQuery.refetch,
	};
}
