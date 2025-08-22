import { useMutation } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { webdavClient } from "../services/webdavClient";
import type { WebDAVFile } from "../types/webdav";
import {
	createBlobUrl,
	getFileTypeInfo,
	isAudioFile,
	isTextFile,
	isVideoFile,
	revokeBlobUrl,
} from "../utils/fileUtils";

interface PreviewState {
	isOpen: boolean;
	file: WebDAVFile | null;
	content: string | null;
	blobUrl: string | null;
	streamUrl: string | null;
	isLoading: boolean;
	error: Error | null;
}

export function useFilePreview() {
	const [state, setState] = useState<PreviewState>({
		isOpen: false,
		file: null,
		content: null,
		blobUrl: null,
		streamUrl: null,
		isLoading: false,
		error: null,
	});

	const currentBlobUrl = useRef<string | null>(null);

	// Clean up blob URL when component unmounts or URL changes
	useEffect(() => {
		return () => {
			if (currentBlobUrl.current) {
				revokeBlobUrl(currentBlobUrl.current);
			}
		};
	}, []);

	const fetchContent = useMutation({
		mutationFn: async ({ path, file }: { path: string; file: WebDAVFile }) => {
			const fileInfo = getFileTypeInfo(file.basename, file.mime);

			if (!fileInfo.isPreviewable) {
				throw new Error("File type not supported for preview");
			}

			// For video and audio files, use direct streaming URL
			if (
				isVideoFile(file.basename, file.mime) ||
				isAudioFile(file.basename, file.mime)
			) {
				const streamUrl = `/webdav${path}`;
				return { content: null, shouldGetAsText: false, fileInfo, streamUrl };
			}

			const shouldGetAsText = isTextFile(file.basename, file.mime);
			const content = await webdavClient.getFileContents(path, shouldGetAsText);

			return { content, shouldGetAsText, fileInfo, streamUrl: null };
		},
		onMutate: ({ file }) => {
			// Clean up previous blob URL
			if (currentBlobUrl.current) {
				revokeBlobUrl(currentBlobUrl.current);
				currentBlobUrl.current = null;
			}

			setState((prev) => ({
				...prev,
				isLoading: true,
				error: null,
				file,
				content: null,
				blobUrl: null,
				streamUrl: null,
			}));
		},
		onSuccess: ({ content, shouldGetAsText, streamUrl }) => {
			if (streamUrl) {
				// For streaming content (video/audio)
				setState((prev) => ({
					...prev,
					isLoading: false,
					content: null,
					blobUrl: null,
					streamUrl,
				}));
			} else if (shouldGetAsText) {
				setState((prev) => ({
					...prev,
					isLoading: false,
					content: content as string,
					blobUrl: null,
					streamUrl: null,
				}));
			} else {
				const blobUrl = createBlobUrl(content as Blob);
				currentBlobUrl.current = blobUrl;

				setState((prev) => ({
					...prev,
					isLoading: false,
					content: null,
					blobUrl,
					streamUrl: null,
				}));
			}
		},
		onError: (error: Error) => {
			setState((prev) => ({
				...prev,
				isLoading: false,
				error,
			}));
		},
	});

	const openPreview = useCallback(
		(file: WebDAVFile, currentPath: string) => {
			const filePath = `${currentPath}/${file.basename}`.replace(/\/+/g, "/");

			setState((prev) => ({
				...prev,
				isOpen: true,
			}));

			fetchContent.mutate({ path: filePath, file });
		},
		[fetchContent],
	);

	const closePreview = useCallback(() => {
		// Clean up blob URL
		if (currentBlobUrl.current) {
			revokeBlobUrl(currentBlobUrl.current);
			currentBlobUrl.current = null;
		}

		setState({
			isOpen: false,
			file: null,
			content: null,
			blobUrl: null,
			streamUrl: null,
			isLoading: false,
			error: null,
		});
	}, []);

	const retryPreview = useCallback(() => {
		if (state.file) {
			fetchContent.mutate({ path: state.file.filename, file: state.file });
		}
	}, [state.file, fetchContent]);

	return {
		...state,
		openPreview,
		closePreview,
		retryPreview,
		canPreview: (file: WebDAVFile) => {
			const fileInfo = getFileTypeInfo(file.basename, file.mime);
			return fileInfo.isPreviewable;
		},
	};
}
