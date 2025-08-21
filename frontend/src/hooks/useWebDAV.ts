import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { webdavClient } from "../services/webdavClient";
import type { WebDAVConnection, WebDAVDirectory } from "../types/webdav";

export function useWebDAVConnection() {
	const [isConnected, setIsConnected] = useState(false);
	const queryClient = useQueryClient();

	const connect = useMutation({
		mutationFn: async (connection: WebDAVConnection) => {
			webdavClient.connect(connection);
			const success = await webdavClient.testConnection();
			if (!success) {
				throw new Error("Failed to connect to WebDAV server");
			}
			return success;
		},
		onSuccess: () => {
			setIsConnected(true);
			// Invalidate all WebDAV queries to refresh with new connection
			queryClient.invalidateQueries({ queryKey: ["webdav"] });
		},
		onError: () => {
			setIsConnected(false);
		},
	});

	return {
		isConnected,
		connect: connect.mutate,
		isConnecting: connect.isPending,
		connectionError: connect.error,
	};
}

export function useWebDAVDirectory(path: string) {
	return useQuery<WebDAVDirectory>({
		queryKey: ["webdav", "directory", path],
		queryFn: () => webdavClient.listDirectory(path),
		enabled: webdavClient.isConnected(),
		staleTime: 30000, // 30 seconds
		retry: (failureCount, error) => {
			// Don't retry on authentication errors
			if (error.message.includes("401") || error.message.includes("403")) {
				return false;
			}
			return failureCount < 2;
		},
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
