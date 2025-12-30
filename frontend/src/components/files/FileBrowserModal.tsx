import { AlertTriangle, File, Folder, RefreshCw, Upload, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useSystemBrowse } from "../../hooks/useApi";
import { formatBytes } from "../../lib/utils";
import type { FileEntry } from "../../types/api";

export interface FileBrowserModalProps {
	isOpen: boolean;
	onClose: () => void;
	onSelect: (filePath: string) => void;
	title?: string;
	initialPath?: string;
	filterExtension?: string; // e.g., ".sqlite", ".db", ".sqlite3"
	allowDirectorySelection?: boolean;
}

export function FileBrowserModal({
	isOpen,
	onClose,
	onSelect,
	title = "Browse Server Files",
	initialPath = "/",
	filterExtension,
	allowDirectorySelection = false,
}: FileBrowserModalProps) {
	const modalRef = useRef<HTMLDialogElement>(null);
	const [currentPath, setCurrentPath] = useState(initialPath);

	const { data, isLoading, error, refetch } = useSystemBrowse(currentPath);

	useEffect(() => {
		const modal = modalRef.current;
		if (modal) {
			if (isOpen) {
				modal.showModal();
			} else {
				modal.close();
			}
		}
	}, [isOpen]);

	useEffect(() => {
		if (isOpen) {
			setCurrentPath(initialPath);
			refetch();
		}
	}, [isOpen, initialPath, refetch]);

	const handleEntryClick = (entry: FileEntry) => {
		if (entry.is_dir) {
			setCurrentPath(entry.path);
		} else {
			// Select file if it matches filter or if no filter
			if (!filterExtension || entry.name.endsWith(filterExtension)) {
				onSelect(entry.path);
				onClose();
			} else {
				// Optionally show a toast or message that file type is not allowed
				// For now, just do nothing
			}
		}
	};

	const handleGoUp = () => {
		if (data?.parent_path) {
			setCurrentPath(data.parent_path);
		}
	};

	const handleRefresh = () => {
		refetch();
	};

	const handleSelectCurrentDir = () => {
		onSelect(currentPath);
		onClose();
	};

	// Filter files based on extension if provided
	const filteredFiles = data?.files.filter((entry) => {
		if (entry.is_dir) return true; // Always show directories
		if (allowDirectorySelection) return false; // If selecting directories, hide files? No, maybe show them for context
		if (!filterExtension) return true; // No filter, show all files
		return entry.name.endsWith(filterExtension);
	});

	return (
		<dialog ref={modalRef} className="modal" onClose={onClose}>
			<div className="modal-box flex max-h-[90vh] w-11/12 max-w-5xl flex-col">
				{/* Header */}
				<div className="mb-4 flex shrink-0 items-center justify-between">
					<h3 className="font-bold text-lg">{title}</h3>
					<button
						type="button"
						className="btn btn-ghost btn-sm"
						onClick={onClose}
						aria-label="Close modal"
					>
						<X className="h-4 w-4" />
					</button>
				</div>

				{/* Current Path & Navigation */}
				<div className="mb-4 flex shrink-0 items-center gap-2">
					<button
						type="button"
						className="btn btn-ghost btn-sm"
						onClick={handleGoUp}
						disabled={!data?.parent_path || data.parent_path === currentPath}
					>
						<Upload className="h-4 w-4 rotate-90" />
					</button>
					<span className="flex-1 truncate font-mono text-sm" title={currentPath}>
						{currentPath}
					</span>
					<button type="button" className="btn btn-ghost btn-sm" onClick={handleRefresh}>
						<RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
					</button>
				</div>

				{/* File List */}
				<div className="flex-1 overflow-x-auto overflow-y-auto">
					{isLoading ? (
						<div className="flex justify-center p-4">
							<span className="loading loading-spinner" />
						</div>
					) : error ? (
						<div className="alert alert-error">
							<AlertTriangle className="h-5 w-5" />
							<span>Error: {error.message}</span>
						</div>
					) : (
						<table className="table-zebra table-sm table">
							<thead className="sticky top-0 z-10 bg-base-100">
								<tr>
									<th /> {/* Icon */}
									<th>Name</th>
									<th>Size</th>
									<th>Modified</th>
								</tr>
							</thead>
							<tbody>
								{filteredFiles?.map((entry) => (
									<tr
										key={entry.path}
										className={`cursor-pointer hover:bg-base-200 ${
											!entry.is_dir && filterExtension && !entry.name.endsWith(filterExtension)
												? "text-base-content/50" // Grey out non-matching files
												: ""
										}`}
										onClick={() => handleEntryClick(entry)}
									>
										<td>{entry.is_dir ? <Folder /> : <File />}</td>
										<td>{entry.name}</td>
										<td>{entry.is_dir ? "-" : formatBytes(entry.size)}</td>
										<td>{new Date(entry.mod_time).toLocaleString()}</td>
									</tr>
								))}
								{filteredFiles?.length === 0 && (
									<tr>
										<td colSpan={4} className="text-center text-base-content/50">
											No files or folders
										</td>
									</tr>
								)}
							</tbody>
						</table>
					)}
				</div>

				{allowDirectorySelection && (
					<div className="modal-action mt-4 shrink-0">
						<button type="button" className="btn" onClick={onClose}>
							Cancel
						</button>
						<button type="button" className="btn btn-primary" onClick={handleSelectCurrentDir}>
							Select This Directory
						</button>
					</div>
				)}
			</div>

			{/* Backdrop */}
			<form method="dialog" className="modal-backdrop">
				<button type="submit" onClick={onClose}>
					close
				</button>
			</form>
		</dialog>
	);
}
