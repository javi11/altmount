import { formatDistanceToNow } from "date-fns";
import { File, FileArchive, FileImage, FileText, FileVideo, Folder, Music } from "lucide-react";
import type { WebDAVFile } from "../../types/webdav";
import { FileActions } from "./FileActions";

interface FileListProps {
	files: WebDAVFile[];
	currentPath: string;
	onNavigate: (path: string) => void;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	onPreview?: (file: WebDAVFile, currentPath: string) => void;
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
	onPreview,
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
		return `${Number.parseFloat((bytes / k ** i).toFixed(1))} ${sizes[i]}`;
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
				<Folder className="mb-4 h-12 w-12 text-base-content/30" />
				<h3 className="font-semibold text-base-content/70 text-lg">Empty Directory</h3>
				<p className="text-base-content/50">This directory contains no files</p>
			</div>
		);
	}

	return (
		<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			{files.map((file) => (
				<div
					key={file.filename}
					className="card cursor-pointer bg-base-100 shadow-md transition-shadow hover:shadow-lg"
				>
					<div className="card-body p-4">
						<div className="mb-2 flex items-start justify-between">
							<button
								className="flex min-w-0 flex-1 cursor-pointer items-center space-x-3 border-none bg-transparent"
								onClick={() => handleItemClick(file)}
								type="button"
							>
								{getFileIcon(file)}
								<div className="min-w-0 flex-1">
									<h3
										className={`truncate font-medium ${
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
								onPreview={onPreview}
								isDownloading={isDownloading}
								isDeleting={isDeleting}
							/>
						</div>

						<div className="space-y-1 text-base-content/70 text-sm">
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
