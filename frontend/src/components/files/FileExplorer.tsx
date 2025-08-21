import { AlertTriangle, RefreshCw, Wifi, WifiOff } from "lucide-react";
import { useState } from "react";
import {
	useWebDAVDirectory,
	useWebDAVFileOperations,
} from "../../hooks/useWebDAV";
import { ErrorAlert } from "../ui/ErrorAlert";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { BreadcrumbNav } from "./BreadcrumbNav";
import { FileList } from "./FileList";

interface FileExplorerProps {
	isConnected: boolean;
	onConnect: () => void;
}

export function FileExplorer({ isConnected, onConnect }: FileExplorerProps) {
	const [currentPath, setCurrentPath] = useState("/");

	const {
		data: directory,
		isLoading,
		error,
		refetch,
	} = useWebDAVDirectory(currentPath);

	const {
		downloadFile,
		deleteFile,
		getFileInfo,
		isDownloading,
		isDeleting,
		downloadError,
		deleteError,
	} = useWebDAVFileOperations();

	const handleNavigate = (path: string) => {
		setCurrentPath(path);
	};

	const handleDownload = (path: string, filename: string) => {
		downloadFile({ path, filename });
	};

	const handleDelete = (path: string) => {
		deleteFile(path);
	};

	const handleFileInfo = (path: string) => {
		getFileInfo(path);
		// TODO: Show file info in a modal
		console.log("File info for:", path);
	};

	if (!isConnected) {
		return (
			<div className="flex flex-col items-center justify-center py-16">
				<WifiOff className="h-16 w-16 text-base-content/30 mb-4" />
				<h3 className="text-xl font-semibold text-base-content/70 mb-2">
					Not Connected
				</h3>
				<p className="text-base-content/50 mb-6">
					Connect to WebDAV server to browse files
				</p>
				<button type="button" className="btn btn-primary" onClick={onConnect}>
					<Wifi className="h-4 w-4" />
					Connect to WebDAV
				</button>
			</div>
		);
	}

	if (error) {
		return (
			<div className="space-y-4">
				<div className="flex items-center justify-between">
					<h2 className="text-2xl font-bold">Files</h2>
					<button
						type="button"
						className="btn btn-outline"
						onClick={() => refetch()}
					>
						<RefreshCw className="h-4 w-4" />
						Retry
					</button>
				</div>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
				<div>
					<h2 className="text-2xl font-bold">Files</h2>
					<p className="text-base-content/70">Browse WebDAV filesystem</p>
				</div>
				<div className="flex items-center gap-2">
					<div className="flex items-center space-x-2">
						<Wifi className="h-4 w-4 text-success" />
						<span className="text-sm text-success">Connected</span>
					</div>
					<button
						type="button"
						className="btn btn-outline btn-sm"
						onClick={() => refetch()}
						disabled={isLoading}
					>
						<RefreshCw
							className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`}
						/>
						Refresh
					</button>
				</div>
			</div>

			{/* Breadcrumb Navigation */}
			<div className="card bg-base-100 shadow-md">
				<div className="card-body p-4">
					<BreadcrumbNav path={currentPath} onNavigate={handleNavigate} />
				</div>
			</div>

			{/* Error Messages */}
			{(downloadError || deleteError) && (
				<div className="alert alert-error">
					<AlertTriangle className="h-6 w-6" />
					<div>
						<div className="font-bold">Operation Failed</div>
						<div className="text-sm">
							{downloadError?.message || deleteError?.message}
						</div>
					</div>
				</div>
			)}

			{/* File List */}
			<div className="card bg-base-100 shadow-md">
				<div className="card-body p-6">
					{isLoading ? (
						<LoadingSpinner />
					) : directory ? (
						<FileList
							files={directory.files}
							currentPath={currentPath}
							onNavigate={handleNavigate}
							onDownload={handleDownload}
							onDelete={handleDelete}
							onInfo={handleFileInfo}
							isDownloading={isDownloading}
							isDeleting={isDeleting}
						/>
					) : null}
				</div>
			</div>
		</div>
	);
}
