import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { webdavClient } from "../services/webdavClient";
import type { WebDAVDirectory } from "../types/webdav";

export function useWebDAVConnection() {
	const [isConnected, setIsConnected] = useState(false);
	const [hasConnectionFailed, setHasConnectionFailed] = useState(false);
	const queryClient = useQueryClient();

	const connect = useMutation({
		mutationFn: async () => {
			webdavClient.connect(); // Connect using cookie authentication
			const success = await webdavClient.testConnection();
			if (!success) {
				throw new Error("Failed to connect to WebDAV server - authentication required");
			}
			return success;
		},
		onSuccess: () => {
			setIsConnected(true);
			setHasConnectionFailed(false); // Reset failure flag on success
			// Invalidate all WebDAV queries to refresh with new connection
			queryClient.invalidateQueries({ queryKey: ["webdav"] });
		},
		onError: () => {
			setIsConnected(false);
			setHasConnectionFailed(true); // Mark connection as failed
		},
	});

	return {
		isConnected,
		hasConnectionFailed,
		connect: connect.mutate,
		isConnecting: connect.isPending && !isConnected,
		connectionError: connect.error,
	};
}

export function useWebDAVDirectory(path: string, isConnected = true, hasConnectionFailed = false) {
	return useQuery<WebDAVDirectory>({
		queryKey: ["webdav", "directory", path],
		queryFn: () => webdavClient.listDirectory(path),
		// Only enable based on React state - the mutationFn already verifies connection
		enabled: isConnected && !hasConnectionFailed,
		staleTime: 30000, // 30 seconds
		retry: (failureCount, error) => {
			// Don't retry on client errors (4xx) or server errors (5xx)
			const errorMessage = error.message.toLowerCase();
			
			// Client errors - don't retry
			if (errorMessage.includes("401") || 
				errorMessage.includes("403") || 
				errorMessage.includes("404") || 
				errorMessage.includes("400")) {
				return false;
			}
			
			// Server errors - don't retry to prevent bombardment
			if (errorMessage.includes("500") || 
				errorMessage.includes("502") || 
				errorMessage.includes("503") || 
				errorMessage.includes("504")) {
				return false;
			}
			
			// Connection/network errors - only retry once
			return failureCount < 1;
		},
		// Disable background refetching on error to prevent bombardment
		refetchOnWindowFocus: false,
		refetchOnReconnect: false,
	});
}

export function useWebDAVFileOperations() {
	const queryClient = useQueryClient();

	const downloadFile = useMutation({
		mutationFn: async ({
			path,
			filename,
		}: {
			path: string;
			filename: string;
		}) => {
			const blob = await webdavClient.downloadFile(path);

			// Create download link
			const url = URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);

			return true;
		},
	});

	const deleteFile = useMutation({
		mutationFn: async (path: string) => {
			await webdavClient.deleteFile(path);
		},
		onSuccess: (_, path) => {
			// Invalidate the directory containing this file
			const dirPath = path.substring(0, path.lastIndexOf("/")) || "/";
			queryClient.invalidateQueries({
				queryKey: ["webdav", "directory", dirPath],
			});
		},
	});

	const getFileInfo = useMutation({
		mutationFn: async (path: string) => {
			return webdavClient.getFileInfo(path);
		},
	});

	return {
		downloadFile: downloadFile.mutate,
		deleteFile: deleteFile.mutate,
		getFileInfo: getFileInfo.mutate,
		isDownloading: downloadFile.isPending,
		isDeleting: deleteFile.isPending,
		downloadError: downloadFile.error,
		deleteError: deleteFile.error,
	};
}
