export interface FileTypeInfo {
	category: "image" | "video" | "audio" | "text" | "document" | "archive" | "unknown";
	isPreviewable: boolean;
	iconType: "image" | "video" | "audio" | "text" | "archive" | "file";
}

export function getFileTypeInfo(filename: string, mimeType?: string): FileTypeInfo {
	const extension = filename.split(".").pop()?.toLowerCase() || "";

	// Image files
	if (
		["jpg", "jpeg", "png", "gif", "svg", "webp", "bmp", "ico"].includes(extension) ||
		mimeType?.startsWith("image/")
	) {
		return {
			category: "image",
			isPreviewable: true,
			iconType: "image",
		};
	}

	// Video files
	if (
		["mp4", "webm", "avi", "mov", "mkv", "wmv", "flv", "m4v"].includes(extension) ||
		mimeType?.startsWith("video/")
	) {
		return {
			category: "video",
			isPreviewable: true,
			iconType: "video",
		};
	}

	// Audio files
	if (
		["mp3", "wav", "ogg", "aac", "flac", "wma", "m4a"].includes(extension) ||
		mimeType?.startsWith("audio/")
	) {
		return {
			category: "audio",
			isPreviewable: true,
			iconType: "audio",
		};
	}

	// Text and code files
	if (
		["txt", "md", "json", "xml", "csv", "log", "yml", "yaml", "ini", "conf", "cfg"].includes(
			extension,
		) ||
		mimeType?.startsWith("text/")
	) {
		return {
			category: "text",
			isPreviewable: true,
			iconType: "text",
		};
	}

	// Code files
	if (
		[
			"js",
			"ts",
			"jsx",
			"tsx",
			"py",
			"java",
			"c",
			"cpp",
			"h",
			"css",
			"scss",
			"html",
			"php",
			"rb",
			"go",
			"rs",
			"sh",
		].includes(extension)
	) {
		return {
			category: "text",
			isPreviewable: true,
			iconType: "text",
		};
	}

	// Document files
	if (["pdf"].includes(extension) || mimeType === "application/pdf") {
		return {
			category: "document",
			isPreviewable: true,
			iconType: "file",
		};
	}

	// Archive files
	if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz", "iso"].includes(extension)) {
		return {
			category: "archive",
			isPreviewable: false,
			iconType: "archive",
		};
	}

	// Unknown/other files
	return {
		category: "unknown",
		isPreviewable: false,
		iconType: "file",
	};
}

export function formatFileSize(bytes: number): string {
	if (bytes === 0) return "0 B";
	const k = 1024;
	const sizes = ["B", "KB", "MB", "GB", "TB"];
	const i = Math.floor(Math.log(bytes) / Math.log(k));
	return `${Number.parseFloat((bytes / k ** i).toFixed(1))} ${sizes[i]}`;
}

export function createBlobUrl(blob: Blob): string {
	return URL.createObjectURL(blob);
}

export function revokeBlobUrl(url: string): void {
	URL.revokeObjectURL(url);
}

export function isTextFile(filename: string, mimeType?: string): boolean {
	const fileInfo = getFileTypeInfo(filename, mimeType);
	return fileInfo.category === "text";
}

export function isImageFile(filename: string, mimeType?: string): boolean {
	const fileInfo = getFileTypeInfo(filename, mimeType);
	return fileInfo.category === "image";
}

export function isVideoFile(filename: string, mimeType?: string): boolean {
	const fileInfo = getFileTypeInfo(filename, mimeType);
	return fileInfo.category === "video";
}

export function isAudioFile(filename: string, mimeType?: string): boolean {
	const fileInfo = getFileTypeInfo(filename, mimeType);
	return fileInfo.category === "audio";
}

export function getCodeLanguage(filename: string): string {
	const extension = filename.split(".").pop()?.toLowerCase() || "";

	const languageMap: Record<string, string> = {
		js: "javascript",
		jsx: "javascript",
		ts: "typescript",
		tsx: "typescript",
		py: "python",
		java: "java",
		c: "c",
		cpp: "cpp",
		h: "c",
		css: "css",
		scss: "scss",
		html: "html",
		php: "php",
		rb: "ruby",
		go: "go",
		rs: "rust",
		sh: "bash",
		yml: "yaml",
		yaml: "yaml",
		json: "json",
		xml: "xml",
		md: "markdown",
	};

	return languageMap[extension] || "text";
}

export function getFormatLabel(filename: string): string | null {
	const extension = filename.split(".").pop()?.toLowerCase() || "";

	const formatMap: Record<string, string> = {
		rar: "RAR Archive",
		"7z": "7-Zip Archive",
		iso: "ISO Image",
		zip: "ZIP Archive",
		tar: "TAR Archive",
		gz: "GZ Archive",
		bz2: "BZ2 Archive",
		xz: "XZ Archive",
		mkv: "MKV Video",
		mp4: "MP4 Video",
		avi: "AVI Video",
		mov: "MOV Video",
		webm: "WebM Video",
		wmv: "WMV Video",
		flv: "FLV Video",
		m4v: "M4V Video",
		mp3: "MP3 Audio",
		flac: "FLAC Audio",
		wav: "WAV Audio",
		aac: "AAC Audio",
		ogg: "OGG Audio",
		m4a: "M4A Audio",
		wma: "WMA Audio",
		jpg: "JPEG Image",
		jpeg: "JPEG Image",
		png: "PNG Image",
		gif: "GIF Image",
		webp: "WebP Image",
		svg: "SVG Image",
		pdf: "PDF Document",
	};

	return formatMap[extension] ?? null;
}

/**
 * Encodes a WebDAV path for use in URLs by encoding each path segment separately.
 * This preserves the path structure while properly encoding special characters.
 *
 * @param path - The file path to encode (e.g., "/folder/file name.txt")
 * @returns The encoded path (e.g., "/folder/file%20name.txt")
 *
 * @example
 * encodeWebDAVPath("/#1 HAPPY FAMILY USA (2025)/Season 01/file.mkv")
 * // Returns: "/%231%20HAPPY%20FAMILY%20USA%20(2025)/Season%2001/file.mkv"
 */
export function encodeWebDAVPath(path: string): string {
	// Handle empty or root path
	if (!path || path === "/") {
		return path;
	}

	// Split by /, encode each segment, then rejoin
	// Filter out empty segments to handle multiple slashes
	const segments = path.split("/").filter((segment) => segment.length > 0);
	const encodedSegments = segments.map((segment) => encodeURIComponent(segment));

	// Preserve leading slash if present
	const hasLeadingSlash = path.startsWith("/");
	const encodedPath = encodedSegments.join("/");

	return hasLeadingSlash ? `/${encodedPath}` : encodedPath;
}
