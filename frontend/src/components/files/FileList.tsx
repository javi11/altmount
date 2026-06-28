import { useVirtualizer } from "@tanstack/react-virtual";
import { formatDistanceToNow } from "date-fns";
import { File, FileArchive, FileImage, FileText, FileVideo, Folder, Music } from "lucide-react";
import type React from "react";
import { useEffect, useRef } from "react";
import { formatBytes } from "../../lib/utils";
import type { WebDAVFile } from "../../types/webdav";
import { joinPath } from "../../utils/fileUtils";
import { FileActions } from "./FileActions";

interface FileListProps {
	files: WebDAVFile[];
	currentPath: string;
	editable?: boolean;
	selectedPaths?: Set<string>;
	cutPaths?: Set<string>;
	onToggleSelect?: (path: string) => void;
	onToggleSelectAll?: (paths: string[]) => void;
	onNavigate: (path: string) => void;
	onDownload: (path: string, filename: string) => void;
	onDelete: (path: string) => void;
	onInfo: (path: string) => void;
	onExportNZB?: (path: string, filename: string) => void;
	onPreview?: (file: WebDAVFile, currentPath: string) => void;
	onRegenerateSymlink?: (path: string) => void;
	onRename?: (path: string, currentName: string) => void;
	onCut?: (path: string) => void;
	isDownloading?: boolean;
	isDeleting?: boolean;
	isExportingNZB?: boolean;
	isRegenerateSymlinkPending?: boolean;
}

// Lists longer than this virtualize their rows.
const VIRTUALIZE_THRESHOLD = 80;
const ESTIMATED_ROW_HEIGHT = 56;

function getFileIcon(file: WebDAVFile) {
	if (file.type === "directory") {
		return <Folder className="h-5 w-5 shrink-0 text-primary" />;
	}

	const extension = file.basename.split(".").pop()?.toLowerCase() || "";
	const iconClass = "h-5 w-5 shrink-0 text-base-content/70";

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
}

// Native checkbox that supports the tri-state "indeterminate" visual.
function TriStateCheckbox({
	checked,
	indeterminate,
	onChange,
	ariaLabel,
}: {
	checked: boolean;
	indeterminate: boolean;
	onChange: () => void;
	ariaLabel: string;
}) {
	const ref = useRef<HTMLInputElement>(null);
	useEffect(() => {
		if (ref.current) {
			ref.current.indeterminate = indeterminate;
		}
	}, [indeterminate]);
	return (
		<input
			ref={ref}
			type="checkbox"
			className="checkbox checkbox-sm"
			aria-label={ariaLabel}
			checked={checked}
			onChange={onChange}
			onClick={(e) => e.stopPropagation()}
		/>
	);
}

export function FileList({
	files,
	currentPath,
	editable = false,
	selectedPaths,
	cutPaths,
	onToggleSelect,
	onToggleSelectAll,
	onNavigate,
	onDownload,
	onDelete,
	onInfo,
	onExportNZB,
	onPreview,
	onRegenerateSymlink,
	onRename,
	onCut,
	isDownloading = false,
	isDeleting = false,
	isExportingNZB = false,
	isRegenerateSymlinkPending = false,
}: FileListProps) {
	const scrollRef = useRef<HTMLDivElement | null>(null);

	const itemPathOf = (file: WebDAVFile): string =>
		editable ? joinPath(currentPath, file.basename) : file.filename;

	const selectablePaths = files.map(itemPathOf);
	const selectedCount = selectablePaths.filter((p) => selectedPaths?.has(p)).length;
	const allSelected = files.length > 0 && selectedCount === files.length;
	const someSelected = selectedCount > 0 && !allSelected;

	const handleItemClick = (file: WebDAVFile) => {
		if (file.type === "directory") {
			onNavigate(joinPath(currentPath, file.basename));
		} else {
			onInfo(itemPathOf(file));
		}
	};

	const renderRow = (file: WebDAVFile, index: number) => {
		const itemPath = itemPathOf(file);
		const isSelected = selectedPaths?.has(itemPath) ?? false;
		const isCut = cutPaths?.has(itemPath) ?? false;
		const rowTone = isSelected
			? "bg-primary/15 hover:bg-primary/20"
			: index % 2 === 0
				? "bg-base-100 hover:bg-base-200/70"
				: "bg-base-200/50 hover:bg-base-200/80";

		return (
			<div
				key={itemPath}
				className={`group flex min-h-16 items-center gap-3 px-2 py-3 transition-colors sm:px-3 ${
					index > 0 ? "border-base-300/70 border-t" : ""
				} ${rowTone} ${isCut ? "opacity-50" : ""}`}
			>
				{editable && (
					<input
						type="checkbox"
						className="checkbox checkbox-sm shrink-0"
						aria-label={`Select ${file.basename}`}
						checked={isSelected}
						onChange={() => onToggleSelect?.(itemPath)}
					/>
				)}

				<button
					type="button"
					className="flex min-w-0 flex-1 items-center gap-3 border-none bg-transparent text-left"
					onClick={() => handleItemClick(file)}
					aria-label={`${file.type === "directory" ? "Open folder" : "View"} ${file.basename}`}
				>
					{getFileIcon(file)}
					<div className="min-w-0 flex-1">
						<div
							className={`truncate font-medium ${
								file.type === "directory" ? "text-primary" : "text-base-content"
							} ${isCut ? "italic" : ""}`}
							title={file.basename}
						>
							{file.basename}
						</div>
						{file.type === "file" && file.library_path && (
							<div
								className="truncate text-base-content/50 text-xs"
								title={`Library Path: ${file.library_path}`}
							>
								↳ {file.library_path}
							</div>
						)}
					</div>
				</button>

				<div className="hidden w-24 shrink-0 text-right text-base-content/60 text-sm sm:block">
					{file.type === "file" ? formatBytes(file.size, 1, true) : "—"}
				</div>
				<div className="hidden w-32 shrink-0 text-right text-base-content/60 text-sm md:block">
					{file.lastmod ? formatDistanceToNow(new Date(file.lastmod), { addSuffix: true }) : "—"}
				</div>

				<div className="shrink-0">
					<FileActions
						file={file}
						currentPath={currentPath}
						onDownload={onDownload}
						onDelete={onDelete}
						onInfo={onInfo}
						onExportNZB={onExportNZB}
						onPreview={onPreview}
						onRegenerateSymlink={onRegenerateSymlink}
						onRename={editable ? onRename : undefined}
						onCut={editable ? onCut : undefined}
						isCut={isCut}
						isDownloading={isDownloading}
						isDeleting={isDeleting}
						isExportingNZB={isExportingNZB}
						isRegenerateSymlinkPending={isRegenerateSymlinkPending}
					/>
				</div>
			</div>
		);
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

	const header = (
		<div className="flex items-center gap-3 border-base-300/80 border-b bg-base-200/40 px-2 py-3 font-semibold text-base-content/60 text-xs uppercase tracking-widest sm:px-3">
			{editable && (
				<TriStateCheckbox
					checked={allSelected}
					indeterminate={someSelected}
					ariaLabel="Select all items"
					onChange={() => onToggleSelectAll?.(selectablePaths)}
				/>
			)}
			<span className="flex-1">Name</span>
			<span className="hidden w-24 text-right sm:block">Size</span>
			<span className="hidden w-32 text-right md:block">Modified</span>
			<span className="w-8" />
		</div>
	);

	return (
		<div className="overflow-hidden rounded-2xl border border-base-300/80 bg-base-100 shadow-lg">
			{header}
			{files.length < VIRTUALIZE_THRESHOLD ? (
				<div>{files.map((file, index) => renderRow(file, index))}</div>
			) : (
				<VirtualRows
					files={files}
					scrollRef={scrollRef}
					renderRow={renderRow}
					estimatedRowHeight={ESTIMATED_ROW_HEIGHT}
				/>
			)}
		</div>
	);
}

function VirtualRows({
	files,
	scrollRef,
	renderRow,
	estimatedRowHeight,
}: {
	files: WebDAVFile[];
	scrollRef: React.MutableRefObject<HTMLDivElement | null>;
	renderRow: (file: WebDAVFile, index: number) => React.JSX.Element;
	estimatedRowHeight: number;
}) {
	const rowVirtualizer = useVirtualizer({
		count: files.length,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => estimatedRowHeight,
		overscan: 8,
	});

	return (
		<div ref={scrollRef} className="h-[60vh] overflow-auto lg:h-[640px]">
			<div style={{ height: rowVirtualizer.getTotalSize(), width: "100%", position: "relative" }}>
				{rowVirtualizer.getVirtualItems().map((virtualRow) => (
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
						{renderRow(files[virtualRow.index], virtualRow.index)}
					</div>
				))}
			</div>
		</div>
	);
}
