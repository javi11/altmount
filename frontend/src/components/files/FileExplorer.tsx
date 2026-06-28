import {
	AlertTriangle,
	ArrowUpDown,
	Check,
	ChevronDown,
	ClipboardPaste,
	FolderPlus,
	FolderTree,
	History,
	Info,
	RefreshCw,
	Scissors,
	Search,
	Trash2,
	Wifi,
	WifiOff,
	X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useImportHistory, useRegenerateSymlinks } from "../../hooks/useApi";
import { useFilePreview } from "../../hooks/useFilePreview";
import { useWebDAVDirectory, useWebDAVFileOperations } from "../../hooks/useWebDAV";
import type { WebDAVFile } from "../../types/webdav";
import { parentPath } from "../../utils/fileUtils";
import { ErrorAlert } from "../ui/ErrorAlert";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { Pagination } from "../ui/Pagination";
import { BreadcrumbNav } from "./BreadcrumbNav";
import { FileInfoModal } from "./FileInfoModal";
import { FileList } from "./FileList";
import { FilePreview } from "./FilePreview";
import { PromptModal } from "./PromptModal";

interface FileExplorerProps {
	isConnected: boolean;
	hasConnectionFailed: boolean;
	isConnecting: boolean;
	connectionError: Error | null;
	onRetryConnection: () => void;
	initialPath?: string;
	activeView?: string;
}

const FILE_LIST_PAGE_SIZE = 50;

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
		createFolder,
		renameItem,
		moveItems,
		bulkDelete,
		isDownloading,
		isDeleting,
		isGettingMetadata,
		isExportingNZB,
		isCreatingFolder,
		isRenaming,
		isMoving,
		isBulkDeleting,
		downloadError,
		deleteError,
		metadataError,
		exportNZBError,
		metadataData,
	} = useWebDAVFileOperations();

	const preview = useFilePreview();
	const regenerateSymlinks = useRegenerateSymlinks();
	const { confirmAction } = useConfirm();

	// --- Editable file-manager state (directory view only) ---
	const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
	const [cutPaths, setCutPaths] = useState<Set<string>>(new Set());
	const [sortKey, setSortKey] = useState<"name" | "size" | "modified">("name");
	const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
	const [currentPage, setCurrentPage] = useState(1);
	const [createFolderOpen, setCreateFolderOpen] = useState(false);
	const [renameState, setRenameState] = useState<{ path: string; name: string } | null>(null);

	const editable = !isRecentView;

	// Clear the selection on directory change; the cut buffer persists so items can be pasted elsewhere.
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset whenever the directory changes
	useEffect(() => {
		setSelectedPaths(new Set());
	}, [currentPath]);

	const toggleSelect = useCallback((path: string) => {
		setSelectedPaths((prev) => {
			const next = new Set(prev);
			if (next.has(path)) {
				next.delete(path);
			} else {
				next.add(path);
			}
			return next;
		});
	}, []);

	const toggleSelectAll = useCallback((paths: string[]) => {
		setSelectedPaths((prev) => {
			const allSelected = paths.length > 0 && paths.every((p) => prev.has(p));
			const next = new Set(prev);
			for (const p of paths) {
				if (allSelected) {
					next.delete(p);
				} else {
					next.add(p);
				}
			}
			return next;
		});
	}, []);

	const clearSelection = useCallback(() => setSelectedPaths(new Set()), []);

	const handleCut = useCallback((path: string) => {
		setCutPaths(new Set([path]));
	}, []);

	const handleCutSelected = useCallback(() => {
		setCutPaths(new Set(selectedPaths));
		clearSelection();
	}, [selectedPaths, clearSelection]);

	const cancelCut = useCallback(() => setCutPaths(new Set()), []);

	// A cut item can be pasted here unless it already lives here or we'd move a folder into itself.
	const pasteablePaths = useMemo(
		() =>
			Array.from(cutPaths).filter(
				(p) =>
					parentPath(p) !== currentPath && currentPath !== p && !currentPath.startsWith(`${p}/`),
			),
		[cutPaths, currentPath],
	);

	const handlePaste = useCallback(() => {
		if (pasteablePaths.length === 0) {
			return;
		}
		moveItems({ paths: pasteablePaths, destDir: currentPath });
		setCutPaths(new Set());
	}, [pasteablePaths, currentPath, moveItems]);

	const handleRename = useCallback((path: string, currentName: string) => {
		setRenameState({ path, name: currentName });
	}, []);

	const submitRename = useCallback(
		(newName: string) => {
			if (renameState) {
				renameItem({ path: renameState.path, newName });
			}
			setRenameState(null);
		},
		[renameState, renameItem],
	);

	const submitCreateFolder = useCallback(
		(name: string) => {
			createFolder({ parentDir: currentPath, name });
			setCreateFolderOpen(false);
		},
		[currentPath, createFolder],
	);

	const handleDeleteSelected = useCallback(async () => {
		const paths = Array.from(selectedPaths);
		if (paths.length === 0) {
			return;
		}
		const confirmed = await confirmAction(
			"Delete items",
			`Are you sure you want to delete ${paths.length} selected item${
				paths.length === 1 ? "" : "s"
			}? This action cannot be undone.`,
			{ type: "error", confirmText: "Delete", confirmButtonClass: "btn-error" },
		);
		if (confirmed) {
			bulkDelete({ paths });
			clearSelection();
		}
	}, [selectedPaths, confirmAction, bulkDelete, clearSelection]);

	// Filter files based on search term
	const filteredFiles = useMemo(() => {
		const files = isRecentView ? historyFiles : directory?.files || [];
		if (!searchTerm.trim()) {
			return files;
		}

		return files.filter((file) => file.basename.toLowerCase().includes(searchTerm.toLowerCase()));
	}, [isRecentView, historyFiles, directory?.files, searchTerm]);

	// Apply the chosen sort with directories always first.
	const sortedFiles = useMemo(() => {
		const arr = [...filteredFiles];
		arr.sort((a, b) => {
			if (a.type !== b.type) {
				return a.type === "directory" ? -1 : 1;
			}
			let cmp = 0;
			if (sortKey === "name") {
				cmp = a.basename.localeCompare(b.basename);
			} else if (sortKey === "size") {
				cmp = (a.size || 0) - (b.size || 0);
			} else {
				cmp = new Date(a.lastmod).getTime() - new Date(b.lastmod).getTime();
			}
			return sortDir === "asc" ? cmp : -cmp;
		});
		return arr;
	}, [filteredFiles, sortKey, sortDir]);

	const totalPages = Math.max(1, Math.ceil(sortedFiles.length / FILE_LIST_PAGE_SIZE));
	const effectivePage = Math.min(currentPage, totalPages);
	const paginatedFiles = useMemo(() => {
		const start = (effectivePage - 1) * FILE_LIST_PAGE_SIZE;
		return sortedFiles.slice(start, start + FILE_LIST_PAGE_SIZE);
	}, [effectivePage, sortedFiles]);

	useEffect(() => {
		setCurrentPage(1);
	}, [activeView, currentPath, searchTerm, showCorrupted, sortKey, sortDir]);

	useEffect(() => {
		setCurrentPage((page) => Math.min(page, totalPages));
	}, [totalPages]);

	const handlePageChange = useCallback(
		(page: number) => {
			setCurrentPage(Math.min(Math.max(page, 1), totalPages));
		},
		[totalPages],
	);

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

	const handleRegenerateSymlink = (path: string) => {
		regenerateSymlinks.mutate({ filePaths: [path], useImportPath: true });
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
				{/* Toolbar */}
				<div className="flex flex-wrap items-center justify-between gap-2">
					<div className="flex flex-wrap items-center gap-2">
						{editable && (
							<button
								type="button"
								className="btn btn-primary btn-sm gap-2"
								onClick={() => setCreateFolderOpen(true)}
								disabled={isCreatingFolder}
							>
								<FolderPlus className="h-4 w-4" />
								Create Folder
							</button>
						)}

						{editable && cutPaths.size > 0 && (
							<>
								<button
									type="button"
									className="btn btn-success btn-sm gap-2"
									onClick={handlePaste}
									disabled={pasteablePaths.length === 0 || isMoving}
								>
									<ClipboardPaste className="h-4 w-4" />
									{isMoving ? "Pasting..." : `Paste ${pasteablePaths.length || cutPaths.size} here`}
								</button>
								<button type="button" className="btn btn-ghost btn-sm" onClick={cancelCut}>
									Cancel
								</button>
							</>
						)}

						{editable && selectedPaths.size > 0 && (
							<div className="flex items-center gap-1 rounded-lg bg-base-200 px-2 py-1">
								<span className="px-1 font-medium text-sm">{selectedPaths.size} selected</span>
								<button
									type="button"
									className="btn btn-ghost btn-xs gap-1"
									onClick={handleCutSelected}
								>
									<Scissors className="h-3.5 w-3.5" />
									Cut
								</button>
								<button
									type="button"
									className="btn btn-ghost btn-xs gap-1 text-error"
									onClick={handleDeleteSelected}
									disabled={isBulkDeleting}
								>
									<Trash2 className="h-3.5 w-3.5" />
									{isBulkDeleting ? "Deleting..." : "Delete"}
								</button>
								<button
									type="button"
									className="btn btn-ghost btn-xs"
									aria-label="Clear selection"
									onClick={clearSelection}
								>
									<X className="h-3.5 w-3.5" />
								</button>
							</div>
						)}
					</div>

					<div className="flex items-center gap-2">
						<button
							type="button"
							className="btn btn-ghost btn-sm gap-2"
							onClick={() => refetch()}
							disabled={isLoading}
						>
							<RefreshCw className={`h-3.5 w-3.5 ${isLoading ? "animate-spin" : ""}`} />
							Refresh
						</button>

						<div className="dropdown dropdown-end">
							<button tabIndex={0} type="button" className="btn btn-sm gap-2">
								<ArrowUpDown className="h-3.5 w-3.5" />
								Sort
								<ChevronDown className="h-3.5 w-3.5" />
							</button>
							<ul className="dropdown-content menu z-10 w-44 rounded-box bg-base-100 shadow-lg">
								{(["name", "size", "modified"] as const).map((key) => (
									<li key={key}>
										<button
											type="button"
											className="justify-between capitalize"
											onClick={() => setSortKey(key)}
										>
											{key}
											{sortKey === key && <Check className="h-4 w-4 text-primary" />}
										</button>
									</li>
								))}
								<div className="divider my-1" />
								<li>
									<button
										type="button"
										onClick={() => setSortDir((d) => (d === "asc" ? "desc" : "asc"))}
									>
										{sortDir === "asc" ? "Ascending" : "Descending"}
									</button>
								</li>
							</ul>
						</div>
					</div>
				</div>

				<div className="min-h-[300px]">
					{searchTerm && (directory || isRecentView) && (
						<div className="mb-4 flex items-center gap-2 px-2 text-base-content/60 text-xs">
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
							<>
								<FileList
									files={paginatedFiles}
									currentPath={currentPath}
									editable={editable}
									selectedPaths={selectedPaths}
									cutPaths={cutPaths}
									onToggleSelect={toggleSelect}
									onToggleSelectAll={toggleSelectAll}
									onNavigate={handleNavigate}
									onDownload={handleDownload}
									onDelete={handleDelete}
									onInfo={handleFileInfo}
									onExportNZB={handleExportNZB}
									onPreview={preview.openPreview}
									onRegenerateSymlink={handleRegenerateSymlink}
									onRename={handleRename}
									onCut={handleCut}
									isDownloading={isDownloading}
									isDeleting={isDeleting}
									isExportingNZB={isExportingNZB}
									isRegenerateSymlinkPending={regenerateSymlinks.isPending}
								/>
								<div className="mt-4">
									<Pagination
										currentPage={effectivePage}
										totalPages={totalPages}
										onPageChange={handlePageChange}
										totalItems={sortedFiles.length}
										itemsPerPage={FILE_LIST_PAGE_SIZE}
										showAllPages
									/>
								</div>
							</>
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

			<PromptModal
				isOpen={createFolderOpen}
				title="Create Folder"
				label="Folder name"
				placeholder="New folder"
				confirmText="Create"
				isPending={isCreatingFolder}
				onConfirm={submitCreateFolder}
				onCancel={() => setCreateFolderOpen(false)}
			/>

			<PromptModal
				isOpen={renameState !== null}
				title="Rename"
				label="New name"
				initialValue={renameState?.name ?? ""}
				confirmText="Rename"
				selectOnOpen
				isPending={isRenaming}
				onConfirm={submitRename}
				onCancel={() => setRenameState(null)}
			/>
		</div>
	);
}
