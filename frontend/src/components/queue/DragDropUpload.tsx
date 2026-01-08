import { AlertCircle, CheckCircle2, Download, FileIcon, Link, Upload, X } from "lucide-react";
import { useCallback, useState } from "react";
import { useToast } from "../../contexts/ToastContext";
import { useUploadNZBLnks, useUploadToQueue } from "../../hooks/useApi";
import { useConfig } from "../../hooks/useConfig";
import { ErrorAlert } from "../ui/ErrorAlert";

interface UploadedFile {
	file: File;
	id: string;
	status: "pending" | "uploading" | "success" | "error";
	errorMessage?: string;
	queueId?: string;
	category?: string;
}

interface UploadedLink {
	link: string;
	id: string;
	status: "pending" | "resolving" | "success" | "error";
	errorMessage?: string;
	queueId?: string;
	title?: string;
}

export function DragDropUpload() {
	const [isDragOver, setIsDragOver] = useState(false);
	const [uploadedFiles, setUploadedFiles] = useState<UploadedFile[]>([]);
	const [uploadedLinks, setUploadedLinks] = useState<UploadedLink[]>([]);
	const [category, setCategory] = useState<string>("");
	const [linkInput, setLinkInput] = useState<string>("");
	const [activeTab, setActiveTab] = useState<"files" | "nzblnk">("files");
	const uploadMutation = useUploadToQueue();
	const uploadLinksMutation = useUploadNZBLnks();
	const { showToast } = useToast();
	const { data: config } = useConfig();

	// Get available categories from config
	const categories = config?.sabnzbd?.categories ?? [];

	const validateFile = useCallback((file: File): string | null => {
		// Check file extension
		if (!file.name.toLowerCase().endsWith(".nzb")) {
			return "Only .nzb files are allowed";
		}

		// Check file size (100MB limit)
		if (file.size > 100 * 1024 * 1024) {
			return "File size must be less than 100MB";
		}

		return null;
	}, []);

	const validateNZBLink = useCallback((link: string): string | null => {
		const trimmed = link.trim();
		if (!trimmed) return null; // Empty lines are skipped

		if (!trimmed.startsWith("nzblnk:?")) {
			return "Link must start with 'nzblnk:?'";
		}

		// Check for required parameters
		if (!trimmed.includes("t=")) {
			return "Missing required parameter 't' (title)";
		}

		if (!trimmed.includes("h=")) {
			return "Missing required parameter 'h' (header)";
		}

		return null;
	}, []);

	const parseLinks = useCallback((input: string): string[] => {
		return input
			.split("\n")
			.map((line) => line.trim())
			.filter((line) => line.length > 0);
	}, []);

	const extractTitleFromLink = useCallback((link: string): string => {
		try {
			const queryPart = link.replace("nzblnk:?", "");
			const params = new URLSearchParams(queryPart);
			return params.get("t") || "Unknown";
		} catch {
			return "Unknown";
		}
	}, []);

	const handleFiles = useCallback(
		(files: File[]) => {
			const newFiles: UploadedFile[] = files.map((file) => ({
				file,
				id: `${file.name}-${Date.now()}-${Math.random()}`,
				status: "pending" as const,
				category: category || undefined,
			}));

			// Validate files first
			const validatedFiles = newFiles.map((uploadFile) => {
				const error = validateFile(uploadFile.file);
				if (error) {
					return {
						...uploadFile,
						status: "error" as const,
						errorMessage: error,
					};
				}
				return uploadFile;
			});

			setUploadedFiles((prev) => [...prev, ...validatedFiles]);

			// Upload valid files
			validatedFiles.forEach(async (uploadFile) => {
				if (uploadFile.status === "error") return;

				// Update status to uploading
				setUploadedFiles((prev) =>
					prev.map((f) => (f.id === uploadFile.id ? { ...f, status: "uploading" as const } : f)),
				);

				try {
					const response = await uploadMutation.mutateAsync({
						file: uploadFile.file,
						category: uploadFile.category,
					});

					// Update status to success
					setUploadedFiles((prev) =>
						prev.map((f) =>
							f.id === uploadFile.id
								? {
										...f,
										status: "success" as const,
										queueId: response.data?.id.toString(),
									}
								: f,
						),
					);
				} catch (error) {
					// Update status to error
					setUploadedFiles((prev) =>
						prev.map((f) =>
							f.id === uploadFile.id
								? {
										...f,
										status: "error" as const,
										errorMessage: error instanceof Error ? error.message : "Upload failed",
									}
								: f,
						),
					);
				}
			});
		},
		[uploadMutation, validateFile, category],
	);

	const handleLinkSubmit = useCallback(async () => {
		const links = parseLinks(linkInput);
		if (links.length === 0) return;

		// Validate links and create tracking entries
		const linkEntries: UploadedLink[] = links.map((link) => {
			const error = validateNZBLink(link);
			return {
				link,
				id: `${link.slice(0, 50)}-${Date.now()}-${Math.random()}`,
				status: error ? ("error" as const) : ("pending" as const),
				errorMessage: error || undefined,
				title: extractTitleFromLink(link),
			};
		});

		setUploadedLinks((prev) => [...prev, ...linkEntries]);

		// Filter valid links
		const validLinks = linkEntries
			.filter((entry) => entry.status === "pending")
			.map((entry) => entry.link);

		if (validLinks.length === 0) {
			return;
		}

		// Mark valid links as resolving
		setUploadedLinks((prev) =>
			prev.map((l) =>
				validLinks.includes(l.link) && l.status === "pending"
					? { ...l, status: "resolving" as const }
					: l,
			),
		);

		try {
			const response = await uploadLinksMutation.mutateAsync({
				links: validLinks,
				category: category || undefined,
			});

			// Update status based on results
			setUploadedLinks((prev) =>
				prev.map((l) => {
					const result = response.results.find((r) => r.link === l.link);
					if (!result) return l;

					return {
						...l,
						status: result.success ? ("success" as const) : ("error" as const),
						errorMessage: result.error_message,
						queueId: result.queue_id?.toString(),
						title: result.title || l.title,
					};
				}),
			);

			// Show toast feedback based on results
			const successCount = response.success_count ?? 0;
			const failedCount = response.failed_count ?? 0;

			if (failedCount > 0 && successCount === 0) {
				showToast({
					type: "error",
					title: `Failed to resolve ${failedCount} link${failedCount > 1 ? "s" : ""}`,
				});
			} else if (failedCount > 0) {
				showToast({
					type: "warning",
					title: `${successCount} link${successCount > 1 ? "s" : ""} queued, ${failedCount} failed to resolve`,
				});
			} else if (successCount > 0) {
				showToast({
					type: "success",
					title: `${successCount} link${successCount > 1 ? "s" : ""} added to queue`,
				});
			}

			// Clear input on success
			if (response.success_count > 0) {
				setLinkInput("");
			}
		} catch (error) {
			// Mark all as error
			setUploadedLinks((prev) =>
				prev.map((l) =>
					validLinks.includes(l.link) && l.status === "resolving"
						? {
								...l,
								status: "error" as const,
								errorMessage: error instanceof Error ? error.message : "Resolution failed",
							}
						: l,
				),
			);

			showToast({
				type: "error",
				title: "Failed to process links",
				message: "Please try again.",
			});
		}
	}, [
		linkInput,
		category,
		uploadLinksMutation,
		parseLinks,
		validateNZBLink,
		extractTitleFromLink,
		showToast,
	]);

	const handleDragOver = useCallback((e: React.DragEvent) => {
		e.preventDefault();
		e.stopPropagation();
		setIsDragOver(true);
	}, []);

	const handleDragLeave = useCallback((e: React.DragEvent) => {
		e.preventDefault();
		e.stopPropagation();
		setIsDragOver(false);
	}, []);

	const handleDrop = useCallback(
		(e: React.DragEvent) => {
			e.preventDefault();
			e.stopPropagation();
			setIsDragOver(false);

			const files = Array.from(e.dataTransfer.files);
			if (files.length > 0) {
				handleFiles(files);
			}
		},
		[handleFiles],
	);

	const handleFileInput = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const files = Array.from(e.target.files || []);
			if (files.length > 0) {
				handleFiles(files);
			}
			// Clear input so same file can be selected again
			e.target.value = "";
		},
		[handleFiles],
	);

	const removeFile = (fileId: string) => {
		setUploadedFiles((prev) => prev.filter((f) => f.id !== fileId));
	};

	const removeLink = (linkId: string) => {
		setUploadedLinks((prev) => prev.filter((l) => l.id !== linkId));
	};

	const clearAllFiles = () => {
		setUploadedFiles([]);
	};

	const clearAllLinks = () => {
		setUploadedLinks([]);
		setLinkInput("");
	};

	const getFileStatusIcon = (status: UploadedFile["status"]) => {
		switch (status) {
			case "pending":
				return <FileIcon className="h-4 w-4 text-base-content/70" />;
			case "uploading":
				return <Upload className="h-4 w-4 animate-pulse text-info" />;
			case "success":
				return <CheckCircle2 className="h-4 w-4 text-success" />;
			case "error":
				return <AlertCircle className="h-4 w-4 text-error" />;
		}
	};

	const getLinkStatusIcon = (status: UploadedLink["status"]) => {
		switch (status) {
			case "pending":
				return <Link className="h-4 w-4 text-base-content/70" />;
			case "resolving":
				return <Download className="h-4 w-4 animate-pulse text-info" />;
			case "success":
				return <CheckCircle2 className="h-4 w-4 text-success" />;
			case "error":
				return <AlertCircle className="h-4 w-4 text-error" />;
		}
	};

	const getFileStatusText = (status: UploadedFile["status"]) => {
		switch (status) {
			case "pending":
				return "Pending";
			case "uploading":
				return "Uploading...";
			case "success":
				return "Uploaded";
			case "error":
				return "Failed";
		}
	};

	const getLinkStatusText = (status: UploadedLink["status"]) => {
		switch (status) {
			case "pending":
				return "Pending";
			case "resolving":
				return "Resolving...";
			case "success":
				return "Queued";
			case "error":
				return "Failed";
		}
	};

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<div className="mb-4 flex items-center gap-2">
					<Upload className="h-5 w-5 text-primary" />
					<h2 className="card-title">Upload NZB Files</h2>
				</div>

				{/* Tab Selector */}
				<div role="tablist" className="tabs tabs-boxed mb-4">
					<button
						type="button"
						role="tab"
						className={`tab ${activeTab === "files" ? "tab-active" : ""}`}
						onClick={() => setActiveTab("files")}
					>
						<FileIcon className="mr-2 h-4 w-4" />
						Files
					</button>
					<button
						type="button"
						role="tab"
						className={`tab ${activeTab === "nzblnk" ? "tab-active" : ""}`}
						onClick={() => setActiveTab("nzblnk")}
					>
						<Link className="mr-2 h-4 w-4" />
						NZBLNK
					</button>
				</div>

				{/* Category Input (shared) */}
				<fieldset className="fieldset mb-4">
					<legend className="fieldset-legend">Category (optional)</legend>
					<select
						className="select"
						value={category}
						onChange={(e) => setCategory(e.target.value)}
					>
						<option value="">None</option>
						{categories.map((cat) => (
							<option key={cat.name} value={cat.name}>
								{cat.name}
							</option>
						))}
					</select>
					<p className="label">Category will be applied to all uploaded items</p>
				</fieldset>

				{/* Files Tab */}
				{activeTab === "files" && (
					<>
						{/* Drag and Drop Zone */}
						{/* biome-ignore lint/a11y/useSemanticElements: drag-drop zone requires div for proper drag events */}
						<div
							className={`rounded-lg border-2 border-dashed p-8 text-center transition-colors ${
								isDragOver
									? "border-primary bg-primary/10"
									: "border-base-300 hover:border-base-content/30"
							}`}
							onDragOver={handleDragOver}
							onDragLeave={handleDragLeave}
							onDrop={handleDrop}
							role="button"
							tabIndex={0}
							onKeyDown={(e) => {
								if (e.key === "Enter" || e.key === " ") {
									const input = e.currentTarget.querySelector(
										"input[type=file]",
									) as HTMLInputElement;
									input?.click();
								}
							}}
						>
							<Upload
								className={`mx-auto mb-4 h-12 w-12 ${isDragOver ? "text-primary" : "text-base-content/50"}`}
							/>
							<h3 className="mb-2 font-semibold text-lg">
								{isDragOver ? "Drop your NZB files here" : "Drag and drop NZB files"}
							</h3>
							<p className="mb-4 text-base-content/70">or</p>
							<label className="btn btn-outline">
								<Upload className="h-4 w-4" />
								Browse Files
								<input
									type="file"
									multiple
									accept=".nzb"
									onChange={handleFileInput}
									className="hidden"
								/>
							</label>
							<p className="mt-4 text-base-content/50 text-sm">
								Supports multiple .nzb files up to 100MB each
							</p>
						</div>

						{/* File List */}
						{uploadedFiles.length > 0 && (
							<div className="mt-6">
								<div className="mb-4 flex items-center justify-between">
									<h3 className="font-semibold">Upload Queue ({uploadedFiles.length} files)</h3>
									<button type="button" className="btn btn-ghost btn-sm" onClick={clearAllFiles}>
										Clear All
									</button>
								</div>

								<div className="max-h-60 space-y-2 overflow-y-auto">
									{uploadedFiles.map((uploadFile) => (
										<div
											key={uploadFile.id}
											className="flex items-center gap-3 rounded-lg bg-base-200 p-3"
										>
											{getFileStatusIcon(uploadFile.status)}

											<div className="min-w-0 flex-1">
												<div className="truncate font-medium">{uploadFile.file.name}</div>
												<div className="flex items-center gap-2 text-base-content/70 text-sm">
													<span>{getFileStatusText(uploadFile.status)}</span>
													<span>•</span>
													<span>{(uploadFile.file.size / 1024 / 1024).toFixed(1)} MB</span>
													{uploadFile.category && (
														<>
															<span>•</span>
															<span className="badge badge-outline badge-sm">
																{uploadFile.category}
															</span>
														</>
													)}
													{uploadFile.queueId && (
														<>
															<span>•</span>
															<span>Queue ID: {uploadFile.queueId}</span>
														</>
													)}
												</div>
												{uploadFile.errorMessage && (
													<div className="mt-1 text-error text-sm">{uploadFile.errorMessage}</div>
												)}
											</div>

											<button
												type="button"
												className="btn btn-ghost btn-sm"
												onClick={() => removeFile(uploadFile.id)}
												disabled={uploadFile.status === "uploading"}
											>
												<X className="h-4 w-4" />
											</button>
										</div>
									))}
								</div>
							</div>
						)}
					</>
				)}

				{/* NZBLNK Tab */}
				{activeTab === "nzblnk" && (
					<div className="space-y-4">
						<fieldset className="fieldset">
							<legend className="fieldset-legend">NZBLNK Links</legend>
							<textarea
								className="textarea h-32 w-full font-mono text-sm"
								placeholder={
									"Paste nzblnk:// links, one per line\nnzblnk:?t=Movie+Title&h=header123&g=alt.binaries.movies\nnzblnk:?t=Another+File&h=header456"
								}
								value={linkInput}
								onChange={(e) => setLinkInput(e.target.value)}
							/>
							<p className="label">Enter NZBLNK links to resolve and queue (max 20 per request)</p>
						</fieldset>

						<button
							type="button"
							className="btn btn-primary"
							onClick={handleLinkSubmit}
							disabled={!linkInput.trim() || uploadLinksMutation.isPending}
						>
							{uploadLinksMutation.isPending ? (
								<>
									<span className="loading loading-spinner loading-sm" />
									Resolving...
								</>
							) : (
								<>
									<Download className="h-4 w-4" />
									Resolve and Queue
								</>
							)}
						</button>

						{/* Link Results List */}
						{uploadedLinks.length > 0 && (
							<div className="mt-6">
								<div className="mb-4 flex items-center justify-between">
									<h3 className="font-semibold">NZBLNK Queue ({uploadedLinks.length} links)</h3>
									<button type="button" className="btn btn-ghost btn-sm" onClick={clearAllLinks}>
										Clear All
									</button>
								</div>

								<div className="max-h-60 space-y-2 overflow-y-auto">
									{uploadedLinks.map((uploadLink) => (
										<div
											key={uploadLink.id}
											className="flex items-center gap-3 rounded-lg bg-base-200 p-3"
										>
											{getLinkStatusIcon(uploadLink.status)}

											<div className="min-w-0 flex-1">
												<div className="truncate font-medium">{uploadLink.title || "Unknown"}</div>
												<div className="flex items-center gap-2 text-base-content/70 text-sm">
													<span>{getLinkStatusText(uploadLink.status)}</span>
													{uploadLink.queueId && (
														<>
															<span>•</span>
															<span>Queue ID: {uploadLink.queueId}</span>
														</>
													)}
												</div>
												{uploadLink.errorMessage && (
													<div className="mt-1 text-error text-sm">{uploadLink.errorMessage}</div>
												)}
												<div className="mt-1 truncate text-base-content/50 text-xs">
													{uploadLink.link}
												</div>
											</div>

											<button
												type="button"
												className="btn btn-ghost btn-sm"
												onClick={() => removeLink(uploadLink.id)}
												disabled={uploadLink.status === "resolving"}
											>
												<X className="h-4 w-4" />
											</button>
										</div>
									))}
								</div>
							</div>
						)}
					</div>
				)}

				{/* Global upload errors */}
				{uploadMutation.error && activeTab === "files" && (
					<div className="mt-4">
						<ErrorAlert
							error={uploadMutation.error as Error}
							onRetry={() => uploadMutation.reset()}
						/>
					</div>
				)}
				{uploadLinksMutation.error && activeTab === "nzblnk" && (
					<div className="mt-4">
						<ErrorAlert
							error={uploadLinksMutation.error as Error}
							onRetry={() => uploadLinksMutation.reset()}
						/>
					</div>
				)}
			</div>
		</div>
	);
}
