import {
	AlertTriangle,
	Download,
	FileText,
	Image,
	Music,
	RefreshCw,
	Video,
	X,
} from "lucide-react";
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
		onDownload(file.filename, file.basename);
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
				<div className="flex flex-col items-center justify-center py-16 space-y-4">
					<AlertTriangle className="h-16 w-16 text-error mb-4" />
					<h3 className="text-xl font-semibold text-base-content/70">
						Preview Failed
					</h3>
					<p className="text-base-content/50 text-center max-w-md">
						{error.message}
					</p>
					<div className="flex gap-2">
						<button
							type="button"
							className="btn btn-outline btn-sm"
							onClick={onRetry}
						>
							<RefreshCw className="h-4 w-4" />
							Retry
						</button>
						<button
							type="button"
							className="btn btn-primary btn-sm"
							onClick={handleDownload}
						>
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
				<div className="flex justify-center items-center min-h-[400px]">
					<img
						src={blobUrl}
						alt={file.basename}
						className="max-w-full max-h-[70vh] object-contain rounded-lg"
						onError={() => onRetry()}
					/>
				</div>
			);
		}

		// Video preview
		if (isVideoFile(file.basename, file.mime) && (streamUrl || blobUrl)) {
			const videoSrc = streamUrl || blobUrl || "";
			return (
				<div className="flex justify-center items-center min-h-[400px]">
					<video
						src={videoSrc}
						controls
						className="max-w-full max-h-[70vh] rounded-lg"
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
				<div className="flex flex-col items-center justify-center py-16 space-y-6">
					<Music className="h-16 w-16 text-primary" />
					<h3 className="text-xl font-semibold">{file.basename}</h3>
					<audio
						src={audioSrc}
						controls
						className="w-full max-w-md"
						onError={() => onRetry()}
					>
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
					<div className="bg-base-200 rounded-lg p-4">
						<div className="flex items-center justify-between mb-3">
							<div className="flex items-center space-x-2">
								<FileText className="h-5 w-5 text-base-content/70" />
								<span className="text-sm font-medium text-base-content/70">
									{language.toUpperCase()} File
								</span>
							</div>
							<span className="text-xs text-base-content/50">
								{content.split("\n").length} lines
							</span>
						</div>
						<div className="bg-base-100 rounded border p-4 max-h-[60vh] overflow-auto">
							<pre
								className={`text-sm ${isCode ? "font-mono" : "font-sans"} whitespace-pre-wrap`}
							>
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
				<div className="w-full h-[70vh]">
					<iframe
						src={blobUrl}
						className="w-full h-full rounded-lg border"
						title={`PDF Preview: ${file.basename}`}
					>
						<div className="flex flex-col items-center justify-center py-16 space-y-4">
							<AlertTriangle className="h-16 w-16 text-warning" />
							<h3 className="text-xl font-semibold text-base-content/70">
								PDF Preview Not Available
							</h3>
							<p className="text-base-content/50">
								Your browser doesn't support PDF preview.
							</p>
							<button
								type="button"
								className="btn btn-primary"
								onClick={handleDownload}
							>
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
			<div className="flex flex-col items-center justify-center py-16 space-y-4">
				<AlertTriangle className="h-16 w-16 text-warning" />
				<h3 className="text-xl font-semibold text-base-content/70">
					Preview Not Available
				</h3>
				<p className="text-base-content/50">
					This file type cannot be previewed.
				</p>
				<button
					type="button"
					className="btn btn-primary"
					onClick={handleDownload}
				>
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
			<div className="modal-box w-11/12 max-w-5xl h-5/6 flex flex-col">
				{/* Header */}
				<div className="flex items-center justify-between pb-4 border-b border-base-300">
					<div className="flex items-center space-x-3 min-w-0 flex-1">
						{getFileIcon()}
						<div className="min-w-0 flex-1">
							<h3 className="font-semibold text-lg truncate">
								{file.basename}
							</h3>
							<p className="text-sm text-base-content/70">
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
				<div className="flex-1 py-4 overflow-auto">
					{renderPreviewContent()}
				</div>
			</div>

			{/* Backdrop */}
			<button
				type="button"
				className="modal-backdrop"
				onClick={onClose}
				aria-label="Close modal"
			></button>
		</dialog>
	);
}
