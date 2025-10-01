import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { apiClient } from "../api/client";
import { useToast } from "../contexts/ToastContext";
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

export function useWebDAVDirectory(
	path: string,
	isConnected = true,
	hasConnectionFailed = false,
	showCorrupted = false,
) {
	return useQuery<WebDAVDirectory>({
		queryKey: ["webdav", "directory", path, showCorrupted],
		queryFn: async () => {
			const result = await webdavClient.listDirectory(path, showCorrupted);

			// Log successful empty directory access for debugging
			if (result.files.length === 0) {
				console.debug(`Successfully accessed empty directory: ${path}`);
			}

			return result;
		},
		// Only enable based on React state - the mutationFn already verifies connection
		enabled: isConnected && !hasConnectionFailed,
		staleTime: 30000, // 30 seconds
		retry: (failureCount, error) => {
			// Don't retry on client errors (4xx) or server errors (5xx)
			const errorMessage = error.message.toLowerCase();

			// Client errors - don't retry
			if (
				errorMessage.includes("401") ||
				errorMessage.includes("403") ||
				errorMessage.includes("404") ||
				errorMessage.includes("400")
			) {
				return false;
			}

			// Server errors - don't retry to prevent bombardment
			if (
				errorMessage.includes("500") ||
				errorMessage.includes("502") ||
				errorMessage.includes("503") ||
				errorMessage.includes("504")
			) {
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
	const { showToast } = useToast();

	const downloadFile = useMutation({
		mutationFn: async ({ path, filename }: { path: string; filename: string }) => {
			// Use direct WebDAV URL for download
			const downloadUrl = `/webdav${path}`;
			let downloadMethod = "window";

			try {
				// Try to open in new window/tab to trigger download dialog
				const downloadWindow = window.open(downloadUrl, "_blank");

				// Check if popup was blocked
				if (
					!downloadWindow ||
					downloadWindow.closed ||
					typeof downloadWindow.closed === "undefined"
				) {
					// Popup blocked, fall back to creating a download link
					downloadMethod = "link";
					const a = document.createElement("a");
					a.href = downloadUrl;
					a.download = filename;
					a.target = "_blank";
					a.rel = "noopener noreferrer";
					document.body.appendChild(a);
					a.click();
					document.body.removeChild(a);
				}

				// Give the download a moment to start
				await new Promise((resolve) => setTimeout(resolve, 100));
			} catch (_) {
				// If window.open fails entirely, use link method
				downloadMethod = "fallback";
				const a = document.createElement("a");
				a.href = downloadUrl;
				a.download = filename;
				document.body.appendChild(a);
				a.click();
				document.body.removeChild(a);
			}

			return { filename, downloadUrl, downloadMethod };
		},
		onSuccess: ({ filename, downloadMethod }) => {
			const messages = {
				window: `Download window opened for "${filename}"`,
				link: `Download started for "${filename}" (popup was blocked)`,
				fallback: `Download initiated for "${filename}"`,
			};

			showToast({
				type: "success",
				title: "Download Started",
				message: messages[downloadMethod as keyof typeof messages] || messages.fallback,
			});
		},
		onError: (error, { filename }) => {
			showToast({
				type: "error",
				title: "Download Failed",
				message: `Failed to start download for "${filename}": ${error.message}`,
			});
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

	const getFileMetadata = useMutation({
		mutationFn: async (path: string) => {
			try {
				// Try to get detailed metadata first
				return await apiClient.getFileMetadata(path);
			} catch (error) {
				// If metadata fails, fall back to basic WebDAV info
				console.warn("Failed to get file metadata, falling back to basic info:", error);
				throw error;
			}
		},
	});

	const exportNZB = useMutation({
		mutationFn: async ({ path, filename }: { path: string; filename: string }) => {
			const blob = await apiClient.exportMetadataToNZB(path);

			// Create blob URL and trigger download
			const url = URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			// Remove extension and add .nzb
			const nameWithoutExt =
				filename.lastIndexOf(".") > 0 ? filename.substring(0, filename.lastIndexOf(".")) : filename;
			a.download = `${nameWithoutExt}.nzb`;
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);

			return { filename: `${nameWithoutExt}.nzb` };
		},
		onSuccess: ({ filename }) => {
			showToast({
				type: "success",
				title: "NZB Exported",
				message: `Successfully exported "${filename}"`,
			});
		},
		onError: (error, { filename }) => {
			showToast({
				type: "error",
				title: "Export Failed",
				message: `Failed to export NZB for "${filename}": ${error.message}`,
			});
		},
	});

	return {
		downloadFile: downloadFile.mutate,
		deleteFile: deleteFile.mutate,
		getFileMetadata: getFileMetadata.mutate,
		exportNZB: exportNZB.mutate,
		isDownloading: downloadFile.isPending,
		isDeleting: deleteFile.isPending,
		isGettingMetadata: getFileMetadata.isPending,
		isExportingNZB: exportNZB.isPending,
		downloadError: downloadFile.error,
		deleteError: deleteFile.error,
		metadataError: getFileMetadata.error,
		exportNZBError: exportNZB.error,
		metadataData: getFileMetadata.data,
	};
}
