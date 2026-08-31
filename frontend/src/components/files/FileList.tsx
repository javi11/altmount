import { useVirtualizer } from "@tanstack/react-virtual";
import { formatDistanceToNow } from "date-fns";
import { File, FileArchive, FileImage, FileText, FileVideo, Folder, Music } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { formatBytes } from "../../lib/utils";
import type { WebDAVFile } from "../../types/webdav";
import { getFormatLabel } from "../../utils/fileUtils";
import { FileActions } from "./FileActions";

interface FileListProps {
	files: WebDAVFile[];
	currentPath: string;
	onNavigate: (path: string) => void;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	onExportNZB?: (path: string, filename: string) => void;
	onPreview?: (file: WebDAVFile, currentPath: string) => void;
	onRegenerateSymlink?: (path: string) => void;
	isDownloading?: boolean;
	isDeleting?: boolean;
	isExportingNZB?: boolean;
	isRegenerateSymlinkPending?: boolean;
}

// Small lists use a plain grid, large lists virtualize; cards are content-sized so folders stay compact.
const VIRTUALIZE_THRESHOLD = 100;
const ESTIMATED_ROW_HEIGHT = 132;

function columnsForWidth(width: number): number {
	if (width === 0) return 4; // sensible default before the first measure (desktop)
	if (width < 640) return 1;
	if (width < 1024) return 2;
	if (width < 1400) return 3;
	return 4;
}

export function FileList({
	files,
	currentPath,
	onNavigate,
	onDownload,
	onDelete,
	onInfo,
	onExportNZB,
	onPreview,
	onRegenerateSymlink,
	isDownloading = false,
	isDeleting = false,
	isExportingNZB = false,
	isRegenerateSymlinkPending = false,
}: FileListProps) {
	const scrollRef = useRef<HTMLDivElement | null>(null);
	const resizeObserverRef = useRef<ResizeObserver | null>(null);
	const [scrollWidth, setScrollWidth] = useState(0);

	// Track the scroll container width to pick a column count.
	const attachScrollContainer = useCallback((node: HTMLDivElement | null) => {
		scrollRef.current = node;
		resizeObserverRef.current?.disconnect();
		if (node) {
			const observer = new ResizeObserver(() => setScrollWidth(node.clientWidth));
			observer.observe(node);
			setScrollWidth(node.clientWidth);
			resizeObserverRef.current = observer;
		}
	}, []);

	useEffect(() => () => resizeObserverRef.current?.disconnect(), []);

	const itemsPerRow = useMemo(() => columnsForWidth(scrollWidth), [scrollWidth]);
	const rowCount = Math.ceil(files.length / itemsPerRow);

	const rowVirtualizer = useVirtualizer({
		count: rowCount,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => ESTIMATED_ROW_HEIGHT,
		overscan: 5,
	});

	// Row heights change when the column count changes, so force a re-measure.
	useEffect(() => {
		rowVirtualizer.measure();
	}, [rowVirtualizer]);

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
			case ["zip", "rar", "7z", "tar", "gz", "iso"].includes(extension):
				return <FileArchive className={iconClass} />;
			case ["txt", "md", "log", "json", "xml", "csv"].includes(extension):
				return <FileText className={iconClass} />;
			default:
				return <File className={iconClass} />;
		}
	};

	const formatFileSize = (bytes: number): string => formatBytes(bytes, 1, true);

	const handleItemClick = (file: WebDAVFile) => {
		if (file.type === "directory") {
			const newPath = currentPath
				? `${currentPath}/${file.basename}`.replace(/\/+/g, "/")
				: file.filename;
			onNavigate(newPath);
		}
	};

	const renderCard = (file: WebDAVFile) => (
		<FileCard
			key={file.filename}
			file={file}
			currentPath={currentPath}
			onDownload={onDownload}
			onDelete={onDelete}
			onInfo={onInfo}
			onPreview={onPreview}
			onExportNZB={onExportNZB}
			onRegenerateSymlink={onRegenerateSymlink}
			isDownloading={isDownloading}
			isDeleting={isDeleting}
			isExportingNZB={isExportingNZB}
			isRegenerateSymlinkPending={isRegenerateSymlinkPending}
			getFileIcon={getFileIcon}
			formatFileSize={formatFileSize}
			handleItemClick={handleItemClick}
		/>
	);

	if (files.length === 0) {
		return (
			<div className="flex flex-col items-center justify-center py-12">
				<Folder className="mb-4 h-12 w-12 text-base-content/30" />
				<h3 className="font-semibold text-base-content/70 text-lg">Empty Directory</h3>
				<p className="text-base-content/50">This directory contains no files</p>
			</div>
		);
	}

	// Small lists: plain responsive grid; cards stretch so every box in a row is the same height.
	if (files.length < VIRTUALIZE_THRESHOLD) {
		return (
			<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				{files.map(renderCard)}
			</div>
		);
	}

	// Large lists: virtualize rows with dynamic measurement.
	return (
		<div ref={attachScrollContainer} className="h-[60vh] overflow-auto lg:h-[640px]">
			<div style={{ height: rowVirtualizer.getTotalSize(), width: "100%", position: "relative" }}>
				{rowVirtualizer.getVirtualItems().map((virtualRow) => {
					const start = virtualRow.index * itemsPerRow;
					const rowFiles = files.slice(start, start + itemsPerRow);
					return (
						<div
							key={virtualRow.key}
							data-index={virtualRow.index}
							ref={rowVirtualizer.measureElement}
							style={{
								position: "absolute",
								top: 0,
								left: 0,
								width: "100%",
								transform: `translateY(${virtualRow.start}px)`,
							}}
						>
							<div
								className="grid gap-4 pb-4"
								style={{ gridTemplateColumns: `repeat(${itemsPerRow}, minmax(0, 1fr))` }}
							>
								{rowFiles.map(renderCard)}
							</div>
						</div>
					);
				})}
			</div>
		</div>
	);
}

// Extracted FileCard component for reuse
interface FileCardProps {
	file: WebDAVFile;
	currentPath: string;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	onPreview?: (file: WebDAVFile, currentPath: string) => void;
	isDownloading: boolean;
	isDeleting: boolean;
	getFileIcon: (file: WebDAVFile) => React.JSX.Element;
	formatFileSize: (bytes: number) => string;
	handleItemClick: (file: WebDAVFile) => void;
	onExportNZB?: (path: string, filename: string) => void;
	onRegenerateSymlink?: (path: string) => void;
	isExportingNZB?: boolean;
	isRegenerateSymlinkPending?: boolean;
}

function FileCard({
	file,
	currentPath,
	onDownload,
	onDelete,
	onInfo,
	onPreview,
	isDownloading,
	isDeleting,
	getFileIcon,
	formatFileSize,
	handleItemClick,
	onExportNZB,
	onRegenerateSymlink,
	isExportingNZB,
	isRegenerateSymlinkPending,
}: FileCardProps) {
	return (
		<div className="card h-full cursor-pointer border border-base-200/40 bg-base-100 shadow-md transition-shadow hover:shadow-lg">
			<div className="card-body p-4">
				<div className="mb-2 flex items-start justify-between">
					<button
						className="flex min-w-0 flex-1 cursor-pointer items-center space-x-3 border-none bg-transparent"
						onClick={() => handleItemClick(file)}
						type="button"
						aria-label={`${file.type === "directory" ? "Open folder" : "Open file"} ${file.basename}`}
					>
						{getFileIcon(file)}
						<div className="min-w-0 flex-1 text-left">
							<h3
								className={`break-all font-medium ${
									file.type === "directory"
										? "text-primary hover:text-primary-focus"
										: "text-base-content"
								}`}
							>
								{file.basename}
							</h3>
							{file.type === "file" && (
								<div className="mt-1 flex flex-col text-base-content/50 text-xs">
									<span className="break-all" title={`Virtual Path: ${file.filename}`}>
										{file.filename}
									</span>
									{file.library_path && (
										<span
											className="mt-0.5 break-all text-base-content/70"
											title={`Library Path: ${file.library_path}`}
										>
											↳ {file.library_path}
										</span>
									)}
								</div>
							)}
						</div>
					</button>
					<FileActions
						file={file}
						currentPath={currentPath}
						onDownload={onDownload}
						onDelete={onDelete}
						onInfo={onInfo}
						onExportNZB={onExportNZB}
						onPreview={onPreview}
						onRegenerateSymlink={onRegenerateSymlink}
						isDownloading={isDownloading}
						isDeleting={isDeleting}
						isExportingNZB={isExportingNZB}
						isRegenerateSymlinkPending={isRegenerateSymlinkPending}
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
						<span className="capitalize">
							{file.type === "file"
								? (getFormatLabel(file.basename) ?? file.mime ?? "File")
								: file.type}
						</span>
					</div>
				</div>
			</div>
		</div>
	);
}
