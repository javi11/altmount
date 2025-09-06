import { AlertTriangle, RefreshCw, Search, Wifi, WifiOff, X } from "lucide-react";
import { useMemo, useState } from "react";
import { useFilePreview } from "../../hooks/useFilePreview";
import { useWebDAVDirectory, useWebDAVFileOperations } from "../../hooks/useWebDAV";
import type { WebDAVFile } from "../../types/webdav";
import { ErrorAlert } from "../ui/ErrorAlert";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { BreadcrumbNav } from "./BreadcrumbNav";
import { FileInfoModal } from "./FileInfoModal";
import { FileList } from "./FileList";
import { FilePreview } from "./FilePreview";

interface FileExplorerProps {
	isConnected: boolean;
	hasConnectionFailed: boolean;
	isConnecting: boolean;
	connectionError: Error | null;
	onRetryConnection: () => void;
}

export function FileExplorer({
	isConnected,
	hasConnectionFailed,
	isConnecting,
	connectionError,
	onRetryConnection,
}: FileExplorerProps) {
	const [currentPath, setCurrentPath] = useState("/");
	const [searchTerm, setSearchTerm] = useState("");

	const {
		data: directory,
		isLoading,
		error,
		refetch,
	} = useWebDAVDirectory(currentPath, isConnected, hasConnectionFailed);

	const {
		downloadFile,
		deleteFile,
		getFileMetadata,
		isDownloading,
		isDeleting,
		isGettingMetadata,
		downloadError,
		deleteError,
		metadataError,
		metadataData,
	} = useWebDAVFileOperations();

	const preview = useFilePreview();

	// Filter files based on search term
	const filteredFiles = useMemo(() => {
		if (!directory?.files || !searchTerm.trim()) {
			return directory?.files || [];
		}

		return directory.files.filter((file) =>
			file.basename.toLowerCase().includes(searchTerm.toLowerCase()),
		);
	}, [directory?.files, searchTerm]);

	// File info modal state
	const [fileInfoModal, setFileInfoModal] = useState<{
		isOpen: boolean;
		file: WebDAVFile | null;
	}>({
		isOpen: false,
		file: null,
	});

	const handleNavigate = (path: string) => {
		setCurrentPath(path);
		setSearchTerm(""); // Clear search when navigating
	};

	const handleClearSearch = () => {
		setSearchTerm("");
	};

	const handleDownload = (path: string, filename: string) => {
		downloadFile({ path, filename });
	};

	const handleDelete = (path: string) => {
		deleteFile(path);
	};

	const handleFileInfo = (path: string) => {
		// Find the file object from the filtered files
		const file = filteredFiles.find((f) => {
			const filePath = `${currentPath}/${f.basename}`.replace(/\/+/g, "/");
			return filePath === path;
		});

		if (file) {
			setFileInfoModal({
				isOpen: true,
				file,
			});
			// Fetch metadata for the file
			getFileMetadata(path);
		}
	};

	const handleCloseFileInfo = () => {
		setFileInfoModal({
			isOpen: false,
			file: null,
		});
	};

	const handleRetryFileInfo = () => {
		if (fileInfoModal.file) {
			const filePath = `${currentPath}/${fileInfoModal.file.basename}`.replace(/\/+/g, "/");
			getFileMetadata(filePath);
		}
	};

	// Show connecting state
	if (isConnecting) {
		return (
			<div className="flex flex-col items-center justify-center py-16">
				<Wifi className="mb-4 h-16 w-16 animate-pulse text-primary" />
				<h3 className="mb-2 font-semibold text-base-content/70 text-xl">Connecting...</h3>
				<p className="mb-6 text-base-content/50">Authenticating with WebDAV server</p>
				<LoadingSpinner />
			</div>
		);
	}

	// Show connection error state
	if (!isConnected && connectionError) {
		return (
			<div className="flex flex-col items-center justify-center py-16">
				<WifiOff className="mb-4 h-16 w-16 text-error" />
				<h3 className="mb-2 font-semibold text-base-content/70 text-xl">Connection Failed</h3>
				<p className="mb-4 text-base-content/50">
					{connectionError.message || "Unable to connect to WebDAV server"}
				</p>
				<p className="mb-6 text-base-content/40">Make sure you're logged in to the application</p>
				<button type="button" className="btn btn-primary" onClick={onRetryConnection}>
					<RefreshCw className="h-4 w-4" />
					Retry Connection
				</button>
			</div>
		);
	}

	// Show generic not connected state (shouldn't normally happen with auto-connect)
	if (!isConnected) {
		return (
			<div className="flex flex-col items-center justify-center py-16">
				<WifiOff className="mb-4 h-16 w-16 text-base-content/30" />
				<h3 className="mb-2 font-semibold text-base-content/70 text-xl">Not Connected</h3>
				<p className="mb-6 text-base-content/50">WebDAV connection required to browse files</p>
				<button type="button" className="btn btn-primary" onClick={onRetryConnection}>
					<Wifi className="h-4 w-4" />
					Connect
				</button>
			</div>
		);
	}

	if (error) {
		return (
			<div className="space-y-4">
				<div className="flex items-center justify-between">
					<h2 className="font-bold text-2xl">Files</h2>
					<button type="button" className="btn btn-outline" onClick={() => refetch()}>
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
			<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<h2 className="font-bold text-2xl">Files</h2>
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
						<RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
						Refresh
					</button>
				</div>
			</div>

			{/* Search Bar */}
			<div className="card bg-base-100 shadow-md">
				<div className="card-body p-4">
					<div className="relative">
						<div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
							<Search className="h-4 w-4 text-base-content/50" />
						</div>
						<input
							type="text"
							placeholder="Search in current directory..."
							className="input input-bordered w-full pr-10 pl-10"
							value={searchTerm}
							onChange={(e) => setSearchTerm(e.target.value)}
						/>
						{searchTerm && (
							<button
								type="button"
								className="absolute inset-y-0 right-0 flex items-center pr-3 hover:text-base-content/70"
								onClick={handleClearSearch}
								aria-label="Clear search"
							>
								<X className="h-4 w-4 text-base-content/50" />
							</button>
						)}
					</div>
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
						<div className="text-sm">{downloadError?.message || deleteError?.message}</div>
					</div>
				</div>
			)}

			{/* File List */}
			<div className="card bg-base-100 shadow-md">
				<div className="card-body p-6">
					{/* Search Results Info */}
					{searchTerm && directory && (
						<div className="mb-4">
							{directory.files.length === 0 ? (
								<div className="text-base-content/70 text-sm">
									Cannot search - directory is empty
								</div>
							) : filteredFiles.length === 0 ? (
								<div className="text-base-content/70 text-sm">
									No items match "{searchTerm}" in this directory ({directory.files.length} total
									items)
								</div>
							) : (
								<div className="text-base-content/70 text-sm">
									{filteredFiles.length} of {directory.files.length} items match "{searchTerm}"
								</div>
							)}
						</div>
					)}

					{/* Loading State */}
					{isLoading && isConnected ? (
						<LoadingSpinner />
					) : directory ? (
						/* Directory Content */
						searchTerm && filteredFiles.length === 0 && directory.files.length > 0 ? (
							/* No Search Results State */
							<div className="flex flex-col items-center justify-center py-12">
								<Search className="mb-4 h-12 w-12 text-base-content/30" />
								<h3 className="mb-2 font-semibold text-base-content/70 text-lg">
									No Search Results
								</h3>
								<p className="mb-4 text-center text-base-content/50">
									No files match "{searchTerm}" in this directory
								</p>
								<button
									type="button"
									className="btn btn-outline btn-sm"
									onClick={handleClearSearch}
								>
									<X className="h-4 w-4" />
									Clear Search
								</button>
							</div>
						) : (
							/* File List or Empty Directory */
							<FileList
								files={filteredFiles}
								currentPath={currentPath}
								onNavigate={handleNavigate}
								onDownload={handleDownload}
								onDelete={handleDelete}
								onInfo={handleFileInfo}
								onPreview={preview.openPreview}
								isDownloading={isDownloading}
								isDeleting={isDeleting}
							/>
						)
					) : null}
				</div>
			</div>

			{/* File Preview Modal */}
			<FilePreview
				isOpen={preview.isOpen}
				file={preview.file}
				content={preview.content}
				blobUrl={preview.blobUrl}
				streamUrl={preview.streamUrl}
				isLoading={preview.isLoading}
				error={preview.error}
				onClose={preview.closePreview}
				onRetry={preview.retryPreview}
				onDownload={handleDownload}
				currentPath={preview.currentPath || undefined}
			/>

			{/* File Info Modal */}
			<FileInfoModal
				isOpen={fileInfoModal.isOpen}
				file={fileInfoModal.file}
				currentPath={currentPath}
				metadata={metadataData || null}
				isLoading={isGettingMetadata}
				error={metadataError}
				onClose={handleCloseFileInfo}
				onRetry={handleRetryFileInfo}
			/>
		</div>
	);
}
