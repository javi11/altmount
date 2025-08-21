import { Download, Info, MoreHorizontal, Trash2 } from "lucide-react";
import type { WebDAVFile } from "../../types/webdav";

interface FileActionsProps {
	file: WebDAVFile;
	currentPath: string;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	isDownloading?: boolean;
	isDeleting?: boolean;
}

export function FileActions({
	file,
	currentPath,
	onDownload,
	onDelete,
	onInfo,
	isDownloading = false,
	isDeleting = false,
}: FileActionsProps) {
	const filePath = `${currentPath}/${file.basename}`.replace(/\/+/g, "/");

	const handleDownload = () => {
		if (file.type === "file") {
			onDownload(filePath, file.basename);
		}
	};

	const handleDelete = () => {
		if (confirm(`Are you sure you want to delete "${file.basename}"?`)) {
			onDelete(filePath);
		}
	};

	const handleInfo = () => {
		onInfo(filePath);
	};

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
					<hr />
				</li>
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
