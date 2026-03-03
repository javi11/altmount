import {
	AlertTriangle,
	FolderTree,
	History,
	Info,
	RefreshCw,
	Search,
	Wifi,
	WifiOff,
	X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useImportHistory } from "../../hooks/useApi";
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
	initialPath?: string;
	activeView?: string;
}

export function FileExplorer({
	isConnected,
	hasConnectionFailed,
	isConnecting,
	connectionError,
	onRetryConnection,
	initialPath = "/",
	activeView = "all",
}: FileExplorerProps) {
	const [currentPath, setCurrentPath] = useState(initialPath);
	const [searchTerm, setSearchTerm] = useState("");
	const [showCorrupted, setShowCorrupted] = useState(false);

	const isRecentView = activeView === "recent";

	// Sync currentPath if initialPath changes (from sidebar)
	useEffect(() => {
		setCurrentPath(initialPath);
	}, [initialPath]);

	const {
		data: directory,
		isLoading: isDirectoryLoading,
		error: directoryError,
		refetch: refetchDirectory,
	} = useWebDAVDirectory(currentPath, isConnected, hasConnectionFailed, showCorrupted);

	const {
		data: history,
		isLoading: isHistoryLoading,
		refetch: refetchHistory,
	} = useImportHistory(50);

	const isLoading = isRecentView ? isHistoryLoading : isDirectoryLoading;
	const error = isRecentView ? null : directoryError;
	const refetch = isRecentView ? refetchHistory : refetchDirectory;

	const historyFiles = useMemo(() => {
		if (!history) return [];
		return history.map((item) => {
			const virtualPath = item.virtual_path || "";
			const fileName = item.file_name || "";

			// For folder imports, VirtualPath usually ends with FileName (the release folder)
			// For single file imports, VirtualPath is the parent folder
			let fullVirtualPath = virtualPath;
			if (fileName && !virtualPath.endsWith(fileName)) {
				fullVirtualPath = `${virtualPath}/${fileName}`.replace(/\/+/g, "/");
			}

			if (!fullVirtualPath && item.nzb_name) {
				fullVirtualPath = item.nzb_name;
			}

			return {
				basename: fileName || item.nzb_name || "unknown",
				filename: fullVirtualPath,
				size: item.file_size || 0,
				lastmod: item.completed_at,
				type: "file" as const,
				library_path: item.library_path,
			};
		});
	}, [history]);

	const {
		downloadFile,
		deleteFile,
		getFileMetadata,
		exportNZB,
		isDownloading,
		isDeleting,
		isGettingMetadata,
		isExportingNZB,
		downloadError,
		deleteError,
		metadataError,
		exportNZBError,
		metadataData,
	} = useWebDAVFileOperations();

	const preview = useFilePreview();

	// Filter files based on search term
	const filteredFiles = useMemo(() => {
		const files = isRecentView ? historyFiles : directory?.files || [];
		if (!searchTerm.trim()) {
			return files;
		}

		return files.filter((file) => file.basename.toLowerCase().includes(searchTerm.toLowerCase()));
	}, [isRecentView, historyFiles, directory?.files, searchTerm]);

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

	const handleExportNZB = (path: string, filename: string) => {
		exportNZB({ path, filename });
	};

	const handleFileInfo = (path: string) => {
		const file = filteredFiles.find((f) => {
			const filePath = isRecentView
				? f.filename
				: `${currentPath}/${f.basename}`.replace(/\/+/g, "/");
			return filePath === path;
		});

		if (file) {
			setFileInfoModal({
				isOpen: true,
				file,
			});
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
			const filePath = isRecentView
				? fileInfoModal.file.filename
				: `${currentPath}/${fileInfoModal.file.basename}`.replace(/\/+/g, "/");
			getFileMetadata(filePath);
		}
	};

	if (isConnecting) {
		return (
			<div className="flex flex-col items-center justify-center py-20">
				<div className="rounded-full bg-primary/10 p-6">
					<Wifi className="h-12 w-12 animate-pulse text-primary" />
				</div>
				<h3 className="mt-6 font-bold text-base-content/70 text-xl tracking-tight">
					Connecting...
				</h3>
				<p className="mt-2 text-base-content/50 text-sm">Authenticating with WebDAV server</p>
				<div className="mt-8">
					<LoadingSpinner />
				</div>
			</div>
		);
	}

	if (!isConnected && connectionError) {
		return (
			<div className="flex flex-col items-center justify-center py-20 text-center">
				<div className="rounded-full bg-error/10 p-6">
					<WifiOff className="h-12 w-12 text-error" />
				</div>
				<h3 className="mt-6 font-bold text-base-content/70 text-xl tracking-tight">
					Connection Failed
				</h3>
				<p className="mt-2 max-w-xs text-base-content/50 text-sm leading-relaxed">
					{connectionError.message || "Unable to connect to WebDAV server"}
				</p>
				<button
					type="button"
					className="btn btn-primary btn-md mt-10 px-8 shadow-lg shadow-primary/20"
					onClick={onRetryConnection}
				>
					<RefreshCw className="h-4 w-4" />
					Retry Connection
				</button>
			</div>
		);
	}

	if (error) {
		return (
			<div className="space-y-6 py-4">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-2">
						<AlertTriangle className="h-5 w-5 text-error" />
						<h2 className="font-bold text-xl tracking-tight">Navigation Error</h2>
					</div>
					<button type="button" className="btn btn-outline btn-sm px-4" onClick={() => refetch()}>
						<RefreshCw className="h-3 w-3" />
						Reload
					</button>
				</div>
				<ErrorAlert error={error as Error} onRetry={() => refetch()} />
			</div>
		);
	}

	return (
		<div className="space-y-8">
			{/* Breadcrumb & Global Actions */}
			<section className="space-y-4">
				<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
					<div className="flex-1 overflow-hidden">
						<div className="flex items-center gap-2 font-bold text-base-content/40 text-xs uppercase tracking-widest">
							{isRecentView ? (
								<>
									<History className="h-3 w-3" />
									<span>Recent Imports</span>
								</>
							) : (
								<>
									<FolderTree className="h-3 w-3" />
									<span>Current Location</span>
								</>
							)}
						</div>
						<div className="scrollbar-hide mt-2 overflow-x-auto rounded-lg bg-base-200/50 p-2">
							{isRecentView ? (
								<div className="flex items-center gap-2 px-3 py-1 font-semibold text-sm">
									<span>Recently Added Items</span>
								</div>
							) : (
								<BreadcrumbNav path={currentPath} onNavigate={handleNavigate} />
							)}
						</div>
					</div>

					<div className="flex shrink-0 items-center gap-2">
						<button
							type="button"
							className="btn btn-ghost btn-sm gap-2 text-base-content/80 hover:opacity-100"
							onClick={() => refetch()}
							disabled={isLoading}
						>
							<RefreshCw className={`h-3.5 w-3.5 ${isLoading ? "animate-spin" : ""}`} />
							<span className="text-xs">Refresh</span>
						</button>
					</div>
				</div>
			</section>

			{/* Search & Filters Section */}
			<section className="space-y-4">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
						Search & Filters
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6 md:grid-cols-3">
					<div className="relative md:col-span-2">
						<div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-4">
							<Search className="h-4 w-4 text-base-content/40" />
						</div>
						<input
							type="text"
							placeholder="Search files..."
							className="input input-sm w-full bg-base-200/50 pl-10 font-medium"
							value={searchTerm}
							onChange={(e) => setSearchTerm(e.target.value)}
						/>
						{searchTerm && (
							<button
								type="button"
								className="absolute inset-y-0 right-0 flex items-center pr-3 text-base-content/40 hover:text-error"
								onClick={handleClearSearch}
							>
								<X className="h-4 w-4" />
							</button>
						)}
					</div>

					{!isRecentView && (
						<div className="flex items-center justify-end">
							<label className="label cursor-pointer gap-3 p-0">
								<input
									type="checkbox"
									className="checkbox checkbox-sm checkbox-primary"
									checked={showCorrupted}
									onChange={(e) => setShowCorrupted(e.target.checked)}
								/>
								<div className="flex flex-col">
									<span className="label-text font-semibold text-xs">Corrupted Files</span>
									<span className="label-text-alt text-base-content/80 text-xs">
										Show items with errors
									</span>
								</div>
							</label>
						</div>
					)}
				</div>
			</section>

			{/* Operation Errors */}
			{(downloadError || deleteError || exportNZBError) && (
				<div className="alert alert-error fade-in slide-in-from-top-2 animate-in text-sm shadow-md">
					<AlertTriangle className="h-5 w-5" />
					<div className="flex-1">
						<div className="font-bold">FileSystem Operation Failed</div>
						<div className="text-xs opacity-90">
							{downloadError?.message || deleteError?.message || exportNZBError?.message}
						</div>
					</div>
				</div>
			)}

			{/* File List Section */}
			<section className="space-y-4">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
						Contents
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="min-h-[300px] rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-2 sm:p-6">
					{searchTerm && (directory || isRecentView) && (
						<div className="mb-6 flex items-center gap-2 px-2 text-base-content/60 text-xs">
							<Info className="h-3 w-3" />
							{filteredFiles.length === 0 ? (
								<span>No matches for "{searchTerm}"</span>
							) : (
								<span>
									Showing {filteredFiles.length} items matching "{searchTerm}"
								</span>
							)}
						</div>
					)}

					{isLoading && isConnected ? (
						<div className="flex h-64 items-center justify-center">
							<LoadingSpinner />
						</div>
					) : isRecentView || directory ? (
						searchTerm &&
						filteredFiles.length === 0 &&
						(isRecentView ? historyFiles.length > 0 : (directory?.files?.length ?? 0) > 0) ? (
							<div className="flex flex-col items-center justify-center py-20">
								<Search className="mb-4 h-12 w-12 text-base-content/20" />
								<h3 className="font-bold text-base-content/60 text-lg">No Results Found</h3>
								<p className="mt-1 text-base-content/40 text-sm">Try adjusting your search terms</p>
								<button
									type="button"
									className="btn btn-ghost btn-sm mt-6 text-primary"
									onClick={handleClearSearch}
								>
									Clear Filter
								</button>
							</div>
						) : (
							<FileList
								files={filteredFiles}
								currentPath={currentPath}
								onNavigate={handleNavigate}
								onDownload={handleDownload}
								onDelete={handleDelete}
								onInfo={handleFileInfo}
								onExportNZB={handleExportNZB}
								onPreview={preview.openPreview}
								isDownloading={isDownloading}
								isDeleting={isDeleting}
								isExportingNZB={isExportingNZB}
							/>
						)
					) : null}
				</div>
			</section>

			{/* Modals */}
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
