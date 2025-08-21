import { formatDistanceToNow } from "date-fns";
import {
	File,
	FileArchive,
	FileImage,
	FileText,
	FileVideo,
	Folder,
	Music,
} from "lucide-react";
import type { WebDAVFile } from "../../types/webdav";
import { FileActions } from "./FileActions";

interface FileListProps {
	files: WebDAVFile[];
	currentPath: string;
	onNavigate: (path: string) => void;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	isDownloading?: boolean;
	isDeleting?: boolean;
}

export function FileList({
	files,
	currentPath,
	onNavigate,
	onDownload,
	onDelete,
	onInfo,
	isDownloading = false,
	isDeleting = false,
}: FileListProps) {
	const getFileIcon = (file: WebDAVFile) => {
		if (file.type === "directory") {
			return <Folder className="h-8 w-8 text-primary" />;
		}

		const extension = file.basename.split(".").pop()?.toLowerCase() || "";
		const iconClass = "h-8 w-8 text-base-content/70";

		switch (true) {
			case ["jpg", "jpeg", "png", "gif", "svg", "webp"].includes(extension):
				return <FileImage className={iconClass} />;
			case ["mp4", "avi", "mkv", "mov", "webm"].includes(extension):
				return <FileVideo className={iconClass} />;
			case ["mp3", "wav", "flac", "aac", "ogg"].includes(extension):
				return <Music className={iconClass} />;
			case ["zip", "rar", "7z", "tar", "gz"].includes(extension):
				return <FileArchive className={iconClass} />;
			case ["txt", "md", "log", "json", "xml", "csv"].includes(extension):
				return <FileText className={iconClass} />;
			default:
				return <File className={iconClass} />;
		}
	};

	const formatFileSize = (bytes: number): string => {
		if (bytes === 0) return "0 B";
		const k = 1024;
		const sizes = ["B", "KB", "MB", "GB", "TB"];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return `${parseFloat((bytes / k ** i).toFixed(1))} ${sizes[i]}`;
	};

	const handleItemClick = (file: WebDAVFile) => {
		if (file.type === "directory") {
			const newPath = `${currentPath}/${file.basename}`.replace(/\/+/g, "/");
			onNavigate(newPath);
		}
	};

	if (files.length === 0) {
		return (
			<div className="flex flex-col items-center justify-center py-12">
				<Folder className="h-12 w-12 text-base-content/30 mb-4" />
				<h3 className="text-lg font-semibold text-base-content/70">
					Empty Directory
				</h3>
				<p className="text-base-content/50">This directory contains no files</p>
			</div>
		);
	}

	return (
		<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
			{files.map((file) => (
				<div
					key={file.filename}
					className="card bg-base-100 shadow-md hover:shadow-lg transition-shadow cursor-pointer"
				>
					<div className="card-body p-4">
						<div className="flex items-start justify-between mb-2">
							<button
								className="flex items-center space-x-3 flex-1 min-w-0 bg-transparent border-none cursor-pointer"
								onClick={() => handleItemClick(file)}
								type="button"
							>
								{getFileIcon(file)}
								<div className="min-w-0 flex-1">
									<h3
										className={`font-medium truncate ${
											file.type === "directory"
												? "text-primary hover:text-primary-focus"
												: "text-base-content"
										}`}
									>
										{file.basename}
									</h3>
								</div>
							</button>
							<FileActions
								file={file}
								currentPath={currentPath}
								onDownload={onDownload}
								onDelete={onDelete}
								onInfo={onInfo}
								isDownloading={isDownloading}
								isDeleting={isDeleting}
							/>
						</div>

						<div className="text-sm text-base-content/70 space-y-1">
							{file.type === "file" && (
								<div className="flex justify-between">
									<span>Size:</span>
									<span>{formatFileSize(file.size)}</span>
								</div>
							)}
							<div className="flex justify-between">
								<span>Modified:</span>
								<span>
									{formatDistanceToNow(new Date(file.lastmod), {
										addSuffix: true,
									})}
								</span>
							</div>
							<div className="flex justify-between">
								<span>Type:</span>
								<span className="capitalize">{file.type}</span>
							</div>
						</div>
					</div>
				</div>
			))}
		</div>
	);
}
