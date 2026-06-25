import {
	Download,
	Eye,
	FileDown,
	Info,
	Link2,
	MoreVertical,
	Pencil,
	Scissors,
	Trash2,
} from "lucide-react";
import { useConfirm } from "../../contexts/ModalContext";
import type { WebDAVFile } from "../../types/webdav";
import { getFileTypeInfo } from "../../utils/fileUtils";

interface FileActionsProps {
	file: WebDAVFile;
	currentPath: string;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	onExportNZB?: (path: string, filename: string) => void;
	onPreview?: (file: WebDAVFile, currentPath: string) => void;
	onRegenerateSymlink?: (path: string) => void;
	onRename?: (path: string, currentName: string) => void;
	onCut?: (path: string) => void;
	isCut?: boolean;
	isDownloading?: boolean;
	isDeleting?: boolean;
	isExportingNZB?: boolean;
	isRegenerateSymlinkPending?: boolean;
}

export function FileActions({
	file,
	currentPath,
	onDownload,
	onDelete,
	onInfo,
	onExportNZB,
	onPreview,
	onRegenerateSymlink,
	onRename,
	onCut,
	isCut = false,
	isDownloading = false,
	isDeleting = false,
	isExportingNZB = false,
	isRegenerateSymlinkPending = false,
}: FileActionsProps) {
	const filePath = currentPath
		? `${currentPath}/${file.basename}`.replace(/\/+/g, "/")
		: file.filename;
	const { confirmDelete } = useConfirm();

	const blurMenu = () => {
		// DaisyUI dropdowns stay open on click; blur the focused element to close it.
		if (document.activeElement instanceof HTMLElement) {
			document.activeElement.blur();
		}
	};

	const handleDownload = () => {
		if (file.type === "file") {
			onDownload(filePath, file.basename);
		}
	};

	const handleDelete = async () => {
		const confirmed = await confirmDelete(file.basename);
		if (confirmed) {
			onDelete(filePath);
		}
	};

	const handleInfo = () => {
		onInfo(filePath);
	};

	const handleExportNZB = () => {
		if (file.type === "file" && onExportNZB) {
			onExportNZB(filePath, file.basename);
		}
	};

	const handlePreview = () => {
		if (file.type === "file" && onPreview) {
			onPreview(file, currentPath);
		}
	};

	const handleRegenerateSymlink = () => {
		if (file.type === "file" && onRegenerateSymlink) {
			onRegenerateSymlink(filePath);
		}
	};

	const handleRename = () => {
		onRename?.(filePath, file.basename);
	};

	const handleCut = () => {
		onCut?.(filePath);
	};

	const fileInfo = getFileTypeInfo(file.basename, file.mime);
	const canPreview = file.type === "file" && fileInfo.isPreviewable && onPreview;

	return (
		<div className="dropdown dropdown-end">
			<button
				tabIndex={0}
				type="button"
				className="btn btn-ghost btn-sm btn-square"
				aria-label="Item actions"
				disabled={isDownloading || isDeleting}
			>
				<MoreVertical className="h-4 w-4" />
			</button>
			<ul className="dropdown-content menu z-10 w-48 rounded-box bg-base-100 shadow-lg">
				{onRename && (
					<li>
						<button
							type="button"
							onClick={() => {
								blurMenu();
								handleRename();
							}}
						>
							<Pencil className="h-4 w-4" />
							Rename
						</button>
					</li>
				)}
				{onCut && (
					<li>
						<button
							type="button"
							onClick={() => {
								blurMenu();
								handleCut();
							}}
						>
							<Scissors className="h-4 w-4" />
							{isCut ? "Cut (selected)" : "Cut"}
						</button>
					</li>
				)}
				<li>
					<button
						type="button"
						onClick={() => {
							blurMenu();
							handleInfo();
						}}
					>
						<Info className="h-4 w-4" />
						File Info
					</button>
				</li>
				{canPreview && (
					<li>
						<button
							type="button"
							onClick={() => {
								blurMenu();
								handlePreview();
							}}
						>
							<Eye className="h-4 w-4" />
							Preview
						</button>
					</li>
				)}
				{file.type === "file" && (
					<li>
						<button
							type="button"
							onClick={() => {
								blurMenu();
								handleDownload();
							}}
							disabled={isDownloading}
						>
							<Download className="h-4 w-4" />
							{isDownloading ? "Downloading..." : "Download"}
						</button>
					</li>
				)}
				{file.type === "file" && onExportNZB && (
					<li>
						<button
							type="button"
							onClick={() => {
								blurMenu();
								handleExportNZB();
							}}
							disabled={isExportingNZB}
						>
							<FileDown className="h-4 w-4" />
							{isExportingNZB ? "Exporting..." : "Export as NZB"}
						</button>
					</li>
				)}
				{file.type === "file" && onRegenerateSymlink && (
					<li>
						<button
							type="button"
							onClick={() => {
								blurMenu();
								handleRegenerateSymlink();
							}}
							disabled={isRegenerateSymlinkPending}
						>
							<Link2 className="h-4 w-4 text-primary" />
							{isRegenerateSymlinkPending ? "Regenerating..." : "Regenerate Symlink"}
						</button>
					</li>
				)}
				<li>
					<button
						type="button"
						onClick={() => {
							blurMenu();
							handleDelete();
						}}
						disabled={isDeleting}
						className="text-error"
					>
						<Trash2 className="h-4 w-4" />
						{isDeleting ? "Deleting..." : "Delete"}
					</button>
				</li>
			</ul>
		</div>
	);
}
