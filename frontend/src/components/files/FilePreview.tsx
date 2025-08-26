import { AlertTriangle, Download, FileText, Image, Music, RefreshCw, Video, X } from "lucide-react";
import { useEffect, useRef } from "react";
import type { WebDAVFile } from "../../types/webdav";
import {
	formatFileSize,
	getCodeLanguage,
	getFileTypeInfo,
	isAudioFile,
	isImageFile,
	isTextFile,
	isVideoFile,
} from "../../utils/fileUtils";

interface FilePreviewProps {
	isOpen: boolean;
	file: WebDAVFile | null;
	content: string | null;
	blobUrl: string | null;
	streamUrl: string | null;
	isLoading: boolean;
	error: Error | null;
	onClose: () => void;
	onRetry: () => void;
	onDownload: (path: string, filename: string) => void;
	currentPath?: string;
}

export function FilePreview({
	isOpen,
	file,
	content,
	blobUrl,
	streamUrl,
	isLoading,
	error,
	onClose,
	onRetry,
	onDownload,
	currentPath,
}: FilePreviewProps) {
	const modalRef = useRef<HTMLDialogElement>(null);

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
		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.key === "Escape" && isOpen) {
				onClose();
			}
		};

		if (isOpen) {
			document.addEventListener("keydown", handleKeyDown);
		}

		return () => {
			document.removeEventListener("keydown", handleKeyDown);
		};
	}, [isOpen, onClose]);

	if (!file) return null;

	const fileInfo = getFileTypeInfo(file.basename, file.mime);

	const handleDownload = () => {
		if (!file) return;

		// Use the same path construction as FileExplorer and other components
		const filePath = currentPath
			? `${currentPath}/${file.basename}`.replace(/\/+/g, "/")
			: file.filename;

		onDownload(filePath, file.basename);
	};

	const renderPreviewContent = () => {
		if (isLoading) {
			return (
				<div className="flex flex-col items-center justify-center py-16">
					<div className="loading loading-spinner loading-lg mb-4" />
					<p className="text-base-content/70">Loading preview...</p>
				</div>
			);
		}

		if (error) {
			return (
				<div className="flex flex-col items-center justify-center space-y-4 py-16">
					<AlertTriangle className="mb-4 h-16 w-16 text-error" />
					<h3 className="font-semibold text-base-content/70 text-xl">Preview Failed</h3>
					<p className="max-w-md text-center text-base-content/50">{error.message}</p>
					<div className="flex gap-2">
						<button type="button" className="btn btn-outline btn-sm" onClick={onRetry}>
							<RefreshCw className="h-4 w-4" />
							Retry
						</button>
						<button type="button" className="btn btn-primary btn-sm" onClick={handleDownload}>
							<Download className="h-4 w-4" />
							Download
						</button>
					</div>
				</div>
			);
		}

		// Image preview
		if (isImageFile(file.basename, file.mime) && blobUrl) {
			return (
				<div className="flex min-h-[400px] items-center justify-center">
					<img
						src={blobUrl}
						alt={file.basename}
						className="max-h-[70vh] max-w-full rounded-lg object-contain"
						onError={() => onRetry()}
					/>
				</div>
			);
		}

		// Video preview
		if (isVideoFile(file.basename, file.mime) && (streamUrl || blobUrl)) {
			const videoSrc = streamUrl || blobUrl || "";
			return (
				<div className="flex min-h-[400px] items-center justify-center">
					<video
						src={videoSrc}
						controls
						className="max-h-[70vh] max-w-full rounded-lg"
						onError={() => onRetry()}
					>
						<track kind="captions" src="" label="No captions available" />
						Your browser does not support video playback.
					</video>
				</div>
			);
		}

		// Audio preview
		if (isAudioFile(file.basename, file.mime) && (streamUrl || blobUrl)) {
			const audioSrc = streamUrl || blobUrl || "";
			return (
				<div className="flex flex-col items-center justify-center space-y-6 py-16">
					<Music className="h-16 w-16 text-primary" />
					<h3 className="font-semibold text-xl">{file.basename}</h3>
					<audio src={audioSrc} controls className="w-full max-w-md" onError={() => onRetry()}>
						<track kind="captions" src="" label="No captions available" />
						Your browser does not support audio playback.
					</audio>
				</div>
			);
		}

		// Text content preview
		if (isTextFile(file.basename, file.mime) && content) {
			const language = getCodeLanguage(file.basename);
			const isCode = language !== "text" && language !== "markdown";

			return (
				<div className="w-full">
					<div className="rounded-lg bg-base-200 p-4">
						<div className="mb-3 flex items-center justify-between">
							<div className="flex items-center space-x-2">
								<FileText className="h-5 w-5 text-base-content/70" />
								<span className="font-medium text-base-content/70 text-sm">
									{language.toUpperCase()} File
								</span>
							</div>
							<span className="text-base-content/50 text-xs">
								{content.split("\n").length} lines
							</span>
						</div>
						<div className="max-h-[60vh] overflow-auto rounded border bg-base-100 p-4">
							<pre className={`text-sm ${isCode ? "font-mono" : "font-sans"} whitespace-pre-wrap`}>
								{content}
							</pre>
						</div>
					</div>
				</div>
			);
		}

		// PDF preview (using browser's built-in PDF viewer)
		if (file.basename.toLowerCase().endsWith(".pdf") && blobUrl) {
			return (
				<div className="h-[70vh] w-full">
					<iframe
						src={blobUrl}
						className="h-full w-full rounded-lg border"
						title={`PDF Preview: ${file.basename}`}
					>
						<div className="flex flex-col items-center justify-center space-y-4 py-16">
							<AlertTriangle className="h-16 w-16 text-warning" />
							<h3 className="font-semibold text-base-content/70 text-xl">
								PDF Preview Not Available
							</h3>
							<p className="text-base-content/50">Your browser doesn't support PDF preview.</p>
							<button type="button" className="btn btn-primary" onClick={handleDownload}>
								<Download className="h-4 w-4" />
								Download PDF
							</button>
						</div>
					</iframe>
				</div>
			);
		}

		// Fallback for unsupported types
		return (
			<div className="flex flex-col items-center justify-center space-y-4 py-16">
				<AlertTriangle className="h-16 w-16 text-warning" />
				<h3 className="font-semibold text-base-content/70 text-xl">Preview Not Available</h3>
				<p className="text-base-content/50">This file type cannot be previewed.</p>
				<button type="button" className="btn btn-primary" onClick={handleDownload}>
					<Download className="h-4 w-4" />
					Download File
				</button>
			</div>
		);
	};

	const getFileIcon = () => {
		switch (fileInfo.category) {
			case "image":
				return <Image className="h-5 w-5" />;
			case "video":
				return <Video className="h-5 w-5" />;
			case "audio":
				return <Music className="h-5 w-5" />;
			case "text":
				return <FileText className="h-5 w-5" />;
			default:
				return <FileText className="h-5 w-5" />;
		}
	};

	return (
		<dialog ref={modalRef} className="modal modal-open" onClose={onClose}>
			<div className="modal-box flex h-5/6 w-11/12 max-w-5xl flex-col">
				{/* Header */}
				<div className="flex items-center justify-between border-base-300 border-b pb-4">
					<div className="flex min-w-0 flex-1 items-center space-x-3">
						{getFileIcon()}
						<div className="min-w-0 flex-1">
							<h3 className="truncate font-semibold text-lg">{file.basename}</h3>
							<p className="text-base-content/70 text-sm">
								{formatFileSize(file.size)} • {fileInfo.category}
							</p>
						</div>
					</div>
					<div className="flex items-center space-x-2">
						<button
							type="button"
							className="btn btn-ghost btn-sm"
							onClick={handleDownload}
							title="Download file"
						>
							<Download className="h-4 w-4" />
						</button>
						<button
							type="button"
							className="btn btn-ghost btn-sm"
							onClick={onClose}
							title="Close preview"
						>
							<X className="h-4 w-4" />
						</button>
					</div>
				</div>

				{/* Content */}
				<div className="flex-1 overflow-auto py-4">{renderPreviewContent()}</div>
			</div>

			{/* Backdrop */}
			<button type="button" className="modal-backdrop" onClick={onClose} aria-label="Close modal" />
		</dialog>
	);
}
