import { formatDistanceToNow } from "date-fns";
import { File, FileArchive, FileImage, FileText, FileVideo, Folder, Music } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { WebDAVFile } from "../../types/webdav";
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
	isDownloading?: boolean;
	isDeleting?: boolean;
	isExportingNZB?: boolean;
}

// Virtual scrolling constants
const ITEM_HEIGHT = 200; // Fixed height for each card
const ITEMS_PER_ROW = {
	sm: 1,
	md: 2,
	lg: 3,
	xl: 4,
};
const BUFFER_SIZE = 2; // Number of extra rows to render above and below viewport

export function FileList({
	files,
	currentPath,
	onNavigate,
	onDownload,
	onDelete,
	onInfo,
	onExportNZB,
	onPreview,
	isDownloading = false,
	isDeleting = false,
	isExportingNZB = false,
}: FileListProps) {
	const [containerDimensions, setContainerDimensions] = useState({ width: 0, height: 0 });
	const [scrollTop, setScrollTop] = useState(0);
	const containerRef = useRef<HTMLDivElement>(null);
	const scrollElementRef = useRef<HTMLDivElement>(null);

	// Calculate items per row based on container width
	const itemsPerRow = useMemo(() => {
		if (containerDimensions.width < 768) return ITEMS_PER_ROW.sm;
		if (containerDimensions.width < 1024) return ITEMS_PER_ROW.md;
		if (containerDimensions.width < 1280) return ITEMS_PER_ROW.lg;
		return ITEMS_PER_ROW.xl;
	}, [containerDimensions.width]);

	// Calculate virtual scrolling parameters
	const virtualScrolling = useMemo(() => {
		const totalRows = Math.ceil(files.length / itemsPerRow);
		const containerHeight = containerDimensions.height || 600;
		const visibleRows = Math.ceil(containerHeight / ITEM_HEIGHT);
		const startRow = Math.max(0, Math.floor(scrollTop / ITEM_HEIGHT) - BUFFER_SIZE);
		const endRow = Math.min(totalRows, startRow + visibleRows + BUFFER_SIZE * 2);

		const startIndex = startRow * itemsPerRow;
		const endIndex = Math.min(files.length, endRow * itemsPerRow);

		return {
			totalRows,
			totalHeight: totalRows * ITEM_HEIGHT,
			startRow,
			endRow,
			startIndex,
			endIndex,
			offsetY: startRow * ITEM_HEIGHT,
		};
	}, [files.length, itemsPerRow, containerDimensions.height, scrollTop]);

	// Get visible files
	const visibleFiles = useMemo(() => {
		return files.slice(virtualScrolling.startIndex, virtualScrolling.endIndex);
	}, [files, virtualScrolling.startIndex, virtualScrolling.endIndex]);

	// Handle scroll events
	const handleScroll = useCallback((e: React.UIEvent<HTMLDivElement>) => {
		setScrollTop(e.currentTarget.scrollTop);
	}, []);

	// Handle container resize
	const updateDimensions = useCallback(() => {
		if (containerRef.current) {
			const rect = containerRef.current.getBoundingClientRect();
			setContainerDimensions({
				width: rect.width,
				height: rect.height,
			});
		}
	}, []);

	// Set up resize observer
	useEffect(() => {
		updateDimensions();

		const resizeObserver = new ResizeObserver(updateDimensions);
		if (containerRef.current) {
			resizeObserver.observe(containerRef.current);
		}

		return () => {
			resizeObserver.disconnect();
		};
	}, [updateDimensions]);

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

	// For small lists (< 100 items), render normally without virtualization
	if (files.length < 100) {
		return (
			<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				{files.map((file) => (
					<FileCard
						key={file.filename}
						file={file}
						currentPath={currentPath}
						onDownload={onDownload}
						onDelete={onDelete}
						onInfo={onInfo}
						onPreview={onPreview}
						isDownloading={isDownloading}
						isDeleting={isDeleting}
						getFileIcon={getFileIcon}
						formatFileSize={formatFileSize}
						handleItemClick={handleItemClick}
						onExportNZB={onExportNZB}
						isExportingNZB={isExportingNZB}
					/>
				))}
			</div>
		);
	}

	// Virtual scrolling for large lists
	return (
		<div className="relative">
			<div ref={containerRef} className="h-[600px] overflow-auto" onScroll={handleScroll}>
				<div
					ref={scrollElementRef}
					style={{ height: virtualScrolling.totalHeight }}
					className="relative"
				>
					<div
						style={{
							transform: `translateY(${virtualScrolling.offsetY}px)`,
						}}
						className={`grid gap-4 ${
							itemsPerRow === 1
								? "grid-cols-1"
								: itemsPerRow === 2
									? "grid-cols-2"
									: itemsPerRow === 3
										? "grid-cols-3"
										: "grid-cols-4"
						}`}
					>
						{visibleFiles.map((file) => (
							<FileCard
								key={file.filename}
								file={file}
								currentPath={currentPath}
								onDownload={onDownload}
								onDelete={onDelete}
								onInfo={onInfo}
								onPreview={onPreview}
								isDownloading={isDownloading}
								isDeleting={isDeleting}
								getFileIcon={getFileIcon}
								formatFileSize={formatFileSize}
								handleItemClick={handleItemClick}
							/>
						))}
					</div>
				</div>
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
	isExportingNZB?: boolean;
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
	isExportingNZB,
}: FileCardProps) {
	return (
		<div
			className="card cursor-pointer bg-base-100 shadow-md transition-shadow hover:shadow-lg"
			style={{ height: ITEM_HEIGHT - 16 }} // Account for gap
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
						onExportNZB={onExportNZB}
						onPreview={onPreview}
						isDownloading={isDownloading}
						isDeleting={isDeleting}
						isExportingNZB={isExportingNZB}
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
	);
}
