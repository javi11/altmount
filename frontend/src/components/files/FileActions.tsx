import { Download, Eye, Info, MoreHorizontal, Trash2 } from "lucide-react";
import type { WebDAVFile } from "../../types/webdav";
import { getFileTypeInfo } from "../../utils/fileUtils";
import { useConfirm } from "../../contexts/ModalContext";

interface FileActionsProps {
	file: WebDAVFile;
	currentPath: string;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	onPreview?: (file: WebDAVFile, currentPath: string) => void;
	isDownloading?: boolean;
	isDeleting?: boolean;
}

export function FileActions({
	file,
	currentPath,
	onDownload,
	onDelete,
	onInfo,
	onPreview,
	isDownloading = false,
	isDeleting = false,
}: FileActionsProps) {
	const filePath = `${currentPath}/${file.basename}`.replace(/\/+/g, "/");
	const { confirmDelete } = useConfirm();

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

	const handlePreview = () => {
		if (file.type === "file" && onPreview) {
			onPreview(file, currentPath);
		}
	};

	const fileInfo = getFileTypeInfo(file.basename, file.mime);
	const canPreview =
		file.type === "file" && fileInfo.isPreviewable && onPreview;

	return (
		<div className="dropdown dropdown-end">
			<button
				tabIndex={0}
				type="button"
				className="btn btn-ghost btn-sm"
				disabled={isDownloading || isDeleting}
			>
				<MoreHorizontal className="h-4 w-4" />
			</button>
			<ul className="dropdown-content menu bg-base-100 shadow-lg rounded-box w-48 z-10">
				<li>
					<button type="button" onClick={handleInfo}>
						<Info className="h-4 w-4" />
						File Info
					</button>
				</li>
				{canPreview && (
					<li>
						<button type="button" onClick={handlePreview}>
							<Eye className="h-4 w-4" />
							Preview
						</button>
					</li>
				)}
				{file.type === "file" && (
					<li>
						<button
							type="button"
							onClick={handleDownload}
							disabled={isDownloading}
						>
							<Download className="h-4 w-4" />
							{isDownloading ? "Downloading..." : "Download"}
						</button>
					</li>
				)}
				<li>
					<button
						type="button"
						onClick={handleDelete}
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
