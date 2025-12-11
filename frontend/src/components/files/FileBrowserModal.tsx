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
}

export function FileBrowserModal({
	isOpen,
	onClose,
	onSelect,
	title = "Browse Server Files",
	initialPath = "/",
	filterExtension,
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

	// Filter files based on extension if provided
	const filteredFiles = data?.files.filter((entry) => {
		if (entry.is_dir) return true; // Always show directories
		if (!filterExtension) return true; // No filter, show all files
		return entry.name.endsWith(filterExtension);
	});

	return (
		<dialog ref={modalRef} className="modal" onClose={onClose}>
			<div className="modal-box w-11/12 max-w-5xl">
				{/* Header */}
				<div className="mb-4 flex items-center justify-between">
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
				<div className="mb-4 flex items-center gap-2">
					<button
						type="button"
						className="btn btn-ghost btn-sm"
						onClick={handleGoUp}
						disabled={!data?.parent_path || data.parent_path === currentPath}
					>
						<Upload className="h-4 w-4 rotate-90" />
					</button>
					<span className="flex-1 font-mono text-sm">{currentPath}</span>
					<button type="button" className="btn btn-ghost btn-sm" onClick={handleRefresh}>
						<RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
					</button>
				</div>

				{/* File List */}
				<div className="overflow-x-auto">
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
							<thead>
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
