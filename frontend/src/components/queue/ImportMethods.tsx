import {
	AlertCircle,
	CheckCircle2,
	Database,
	Download,
	FileCode,
	FileIcon,
	FileText,
	FolderInput,
	FolderOpen,
	Link,
	Play,
	Square,
	Upload,
	UploadCloud,
	X,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useToast } from "../../contexts/ToastContext";
import {
	useCancelNzbdavImport,
	useCancelScan,
	useNzbdavImportStatus,
	useResetNzbdavImportStatus,
	useScanStatus,
	useStartManualScan,
	useUploadNZBLnks,
	useUploadToQueue,
} from "../../hooks/useApi";
import { useConfig } from "../../hooks/useConfig";
import { ScanStatus } from "../../types/api";
import { FileBrowserModal } from "../files/FileBrowserModal";
import { ErrorAlert } from "../ui/ErrorAlert";
import { LoadingSpinner } from "../ui/LoadingSpinner";

type ImportTab = "nzbdav" | "directory" | "upload";

const IMPORT_SECTIONS = {
	nzbdav: {
		title: "From NZBDav",
		description: "Import your existing NZBDav database to populate the library.",
		icon: Database,
	},
	directory: {
		title: "From Directory",
		description: "Scan a directory on the server to find and import NZB files into the queue.",
		icon: FolderOpen,
	},
	upload: {
		title: "Upload",
		description: "Upload NZB files or NZBLNKs directly from your computer.",
		icon: UploadCloud,
	},
};

export function ImportMethods() {
	const [activeTab, setActiveTab] = useState<ImportTab>("nzbdav");

	return (
		<div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
			{/* Sidebar Navigation */}
			<div className="lg:col-span-1">
				<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
					<div className="card-body p-2 sm:p-4">
						<div>
							<h3 className="mb-2 px-4 font-bold text-base-content/40 text-xs uppercase tracking-widest">
								Methods
							</h3>
							<ul className="menu menu-md gap-1 p-0">
								{(
									Object.entries(IMPORT_SECTIONS) as [ImportTab, typeof IMPORT_SECTIONS.nzbdav][]
								).map(([key, section]) => {
									const IconComponent = section.icon;
									const isActive = activeTab === key;
									return (
										<li key={key}>
											<button
												type="button"
												className={`flex items-center gap-3 rounded-lg px-4 py-3 transition-all ${
													isActive
														? "bg-primary font-semibold text-primary-content shadow-md shadow-primary/20"
														: "hover:bg-base-200"
												}`}
												onClick={() => setActiveTab(key)}
											>
												<IconComponent
													className={`h-5 w-5 ${isActive ? "" : "text-base-content/60"}`}
												/>
												<div className="min-w-0 flex-1 text-left">
													<div className="text-sm">{section.title}</div>
												</div>
											</button>
										</li>
									);
								})}
							</ul>
						</div>
					</div>
				</div>
			</div>

			{/* Content Area */}
			<div className="lg:col-span-3">
				<div className="card min-h-[500px] border-2 border-base-300/50 bg-base-100 shadow-md">
					<div className="card-body p-4 sm:p-8">
						{/* Section Header */}
						<div className="mb-8 border-base-200 border-b pb-6">
							<div className="mb-2 flex items-center space-x-4">
								<div className="rounded-xl bg-primary/10 p-3">
									{(() => {
										const IconComponent = IMPORT_SECTIONS[activeTab].icon;
										return <IconComponent className="h-6 w-6 text-primary" />;
									})()}
								</div>
								<div>
									<h2 className="font-bold text-2xl tracking-tight">
										{IMPORT_SECTIONS[activeTab].title}
									</h2>
									<p className="max-w-2xl text-base-content/60 text-sm">
										{IMPORT_SECTIONS[activeTab].description}
									</p>
								</div>
							</div>
						</div>

						<div className="max-w-4xl">
							{activeTab === "nzbdav" && <NzbDavImportSection />}
							{activeTab === "directory" && <DirectoryScanSection />}
							{activeTab === "upload" && <EnhancedUploadSection />}
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}

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

function EnhancedUploadSection() {
	const [isDragOver, setIsDragOver] = useState(false);
	const [uploadedFiles, setUploadedFiles] = useState<UploadedFile[]>([]);
	const [uploadedLinks, setUploadedLinks] = useState<UploadedLink[]>([]);
	const [category, setCategory] = useState<string>("");
	const [linkInput, setLinkInput] = useState<string>("");
	const [uploadTab, setUploadTab] = useState<"files" | "nzblnk">("files");
	const uploadMutation = useUploadToQueue();
	const uploadLinksMutation = useUploadNZBLnks();
	const { data: config } = useConfig();

	const categories = config?.sabnzbd?.categories ?? [];

	const validateFile = useCallback((file: File): string | null => {
		if (!file.name.toLowerCase().endsWith(".nzb")) return "Only .nzb files are allowed";
		if (file.size > 100 * 1024 * 1024) return "File size must be less than 100MB";
		return null;
	}, []);

	const validateNZBLink = useCallback((link: string): string | null => {
		const trimmed = link.trim();
		if (!trimmed) return null;
		if (!trimmed.startsWith("nzblnk:?")) return "Link must start with 'nzblnk:?'";
		if (!trimmed.includes("t=")) return "Missing required parameter 't' (title)";
		if (!trimmed.includes("h=")) return "Missing required parameter 'h' (header)";
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

			const validatedFiles = newFiles.map((uploadFile) => {
				const error = validateFile(uploadFile.file);
				if (error) {
					return { ...uploadFile, status: "error" as const, errorMessage: error };
				}
				return uploadFile;
			});

			setUploadedFiles((prev) => [...prev, ...validatedFiles]);

			validatedFiles.forEach(async (uploadFile) => {
				if (uploadFile.status === "error") return;

				setUploadedFiles((prev) =>
					prev.map((f) => (f.id === uploadFile.id ? { ...f, status: "uploading" as const } : f)),
				);

				try {
					const response = await uploadMutation.mutateAsync({
						file: uploadFile.file,
						category: uploadFile.category,
					});

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

		const validLinks = linkEntries
			.filter((entry) => entry.status === "pending")
			.map((entry) => entry.link);

		if (validLinks.length === 0) return;

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

			if (response.success_count > 0) setLinkInput("");
		} catch (error) {
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
		}
	}, [linkInput, category, uploadLinksMutation, parseLinks, validateNZBLink, extractTitleFromLink]);

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
			if (files.length > 0) handleFiles(files);
		},
		[handleFiles],
	);

	const handleFileInput = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const files = Array.from(e.target.files || []);
			if (files.length > 0) handleFiles(files);
			e.target.value = "";
		},
		[handleFiles],
	);

	const removeFile = (fileId: string) =>
		setUploadedFiles((prev) => prev.filter((f) => f.id !== fileId));
	const removeLink = (linkId: string) =>
		setUploadedLinks((prev) => prev.filter((l) => l.id !== linkId));
	const clearAllFiles = () => setUploadedFiles([]);
	const clearAllLinks = () => {
		setUploadedLinks([]);
		setLinkInput("");
	};

	return (
		<div className="space-y-8">
			{/* Tab Selector */}
			<div role="tablist" className="tabs tabs-boxed mb-4 max-w-sm">
				<button
					type="button"
					role="tab"
					className={`tab ${uploadTab === "files" ? "tab-active" : ""}`}
					onClick={() => setUploadTab("files")}
				>
					<FileIcon className="mr-2 h-4 w-4" />
					Files
				</button>
				<button
					type="button"
					role="tab"
					className={`tab ${uploadTab === "nzblnk" ? "tab-active" : ""}`}
					onClick={() => setUploadTab("nzblnk")}
				>
					<Link className="mr-2 h-4 w-4" />
					NZBLNK
				</button>
			</div>

			{/* Category Input */}
			<fieldset className="fieldset mb-4 max-w-sm">
				<legend className="fieldset-legend font-semibold">Category (optional)</legend>
				<select
					className="select select-sm w-full bg-base-200/50"
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
			</fieldset>

			{uploadTab === "files" && (
				<section
					aria-label="File drop zone"
					className={`rounded-2xl border-2 border-dashed p-12 text-center transition-colors ${
						isDragOver
							? "border-primary bg-primary/5"
							: "border-base-300 bg-base-200/30 hover:border-base-content/20"
					}`}
					onDragOver={handleDragOver}
					onDragLeave={handleDragLeave}
					onDrop={handleDrop}
				>
					<UploadCloud
						className={`mx-auto mb-4 h-12 w-12 ${isDragOver ? "text-primary" : "text-base-content/30"}`}
					/>
					<h3 className="mb-2 font-semibold text-lg">
						{isDragOver ? "Drop files now" : "Drag & Drop NZB Files"}
					</h3>
					<p className="mb-6 text-base-content/50 text-sm">or click to browse from computer</p>
					<label className="btn btn-primary btn-sm px-8">
						Browse Files
						<input
							type="file"
							multiple
							accept=".nzb"
							onChange={handleFileInput}
							className="hidden"
						/>
					</label>
				</section>
			)}

			{uploadTab === "nzblnk" && (
				<div className="space-y-4">
					<textarea
						className="textarea h-40 w-full bg-base-200/50 font-mono text-sm"
						placeholder="Paste nzblnk:// links, one per line..."
						value={linkInput}
						onChange={(e) => setLinkInput(e.target.value)}
					/>
					<button
						type="button"
						className="btn btn-primary btn-sm"
						onClick={handleLinkSubmit}
						disabled={!linkInput.trim() || uploadLinksMutation.isPending}
					>
						{uploadLinksMutation.isPending ? (
							<LoadingSpinner size="sm" />
						) : (
							<Download className="h-4 w-4" />
						)}
						Resolve & Queue
					</button>
				</div>
			)}

			{/* Status Lists */}
			{(uploadedFiles.length > 0 || uploadedLinks.length > 0) && (
				<div className="space-y-4">
					<div className="flex items-center justify-between">
						<h4 className="font-bold text-base-content/60 text-xs uppercase tracking-widest">
							Status
						</h4>
						<button
							type="button"
							className="btn btn-ghost btn-sm"
							onClick={uploadTab === "files" ? clearAllFiles : clearAllLinks}
						>
							Clear All
						</button>
					</div>
					<div className="max-h-60 space-y-2 overflow-y-auto rounded-xl border border-base-300 p-2">
						{uploadTab === "files"
							? uploadedFiles.map((f) => (
									<div key={f.id} className="flex items-center gap-3 rounded-lg bg-base-200/50 p-2">
										<FileCode className="h-4 w-4 text-base-content/60" />
										<span className="flex-1 truncate text-sm">{f.file.name}</span>
										<StatusBadge status={f.status} />
										<button type="button" onClick={() => removeFile(f.id)}>
											<X className="h-4 w-4 text-base-content/60" />
										</button>
									</div>
								))
							: uploadedLinks.map((l) => (
									<div key={l.id} className="flex items-center gap-3 rounded-lg bg-base-200/50 p-2">
										<Link className="h-4 w-4 text-base-content/60" />
										<span className="flex-1 truncate text-sm">{l.title || l.link}</span>
										<StatusBadge status={l.status} />
										<button type="button" onClick={() => removeLink(l.id)}>
											<X className="h-4 w-4 text-base-content/60" />
										</button>
									</div>
								))}
					</div>
				</div>
			)}
		</div>
	);
}

function StatusBadge({ status }: { status: string }) {
	switch (status) {
		case "uploading":
		case "resolving":
			return <span className="loading loading-spinner loading-xs text-primary" />;
		case "success":
			return <CheckCircle2 className="h-4 w-4 text-success" />;
		case "error":
			return <AlertCircle className="h-4 w-4 text-error" />;
		default:
			return <div className="h-2 w-2 rounded-full bg-base-content/20" />;
	}
}

function NzbDavImportSection() {
	const [inputMethod, setInputMethod] = useState<"server" | "upload">("server");
	const [selectedDbPath, setSelectedDbPath] = useState("");
	const [selectedFile, setSelectedFile] = useState<File | null>(null);
	const [rootFolder, setRootFolder] = useState("");
	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<Error | null>(null);
	const { showToast } = useToast();
	const [isFileBrowserOpen, setIsFileBrowserOpen] = useState(false);

	const { data: importStatus } = useNzbdavImportStatus(2000);
	const cancelImport = useCancelNzbdavImport();
	const resetImport = useResetNzbdavImportStatus();

	const isRunning = importStatus?.status === "running";
	const isCanceling = importStatus?.status === "canceling";
	const isCompleted = importStatus?.status === "completed";
	const hasResults = (importStatus?.total || 0) > 0 || !!importStatus?.last_error;

	const total = importStatus?.total || 0;
	const processed =
		(importStatus?.added || 0) + (importStatus?.failed || 0) + (importStatus?.skipped || 0);
	const progressPercent = total > 0 ? Math.min((processed / total) * 100, 100) : 0;

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		if (!rootFolder) return;
		if (inputMethod === "server" && !selectedDbPath) return;
		if (inputMethod === "upload" && !selectedFile) return;

		setIsLoading(true);
		setError(null);

		const formData = new FormData();
		formData.append("rootFolder", rootFolder);

		if (inputMethod === "server") {
			formData.append("dbPath", selectedDbPath);
		} else if (selectedFile) {
			formData.append("file", selectedFile);
		}

		try {
			const response = await fetch("/api/import/nzbdav", {
				method: "POST",
				credentials: "include",
				body: formData,
			});

			if (!response.ok) {
				const data = await response.json().catch(() => ({}));
				throw new Error(data.message || "Failed to start import");
			}

			showToast({
				title: "Import Started",
				message: "The import process has started in the background.",
				type: "success",
			});
		} catch (err: unknown) {
			const error = err instanceof Error ? err : new Error("An error occurred");
			setError(error);
			showToast({
				title: "Import Failed",
				message: error.message,
				type: "error",
			});
		} finally {
			setIsLoading(false);
		}
	};

	const handleFileSelect = (path: string) => {
		setSelectedDbPath(path);
	};

	const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
		if (e.target.files && e.target.files.length > 0) {
			setSelectedFile(e.target.files[0]);
		}
	};

	const handleCancel = async () => {
		try {
			await cancelImport.mutateAsync();
			showToast({
				title: "Cancellation Requested",
				message: "Stopping the import process...",
				type: "info",
			});
		} catch (error) {
			console.error("Failed to cancel import:", error);
		}
	};

	const handleReset = async () => {
		try {
			await resetImport.mutateAsync();
		} catch (error) {
			console.error("Failed to reset import status:", error);
		}
	};

	return (
		<div className="space-y-8">
			{error && <ErrorAlert error={error} />}

			{isRunning || isCanceling || isCompleted || hasResults ? (
				<section className="space-y-6">
					<div className="flex items-center gap-2">
						<h4 className="font-bold text-base-content/40 text-xs text-xs uppercase tracking-widest">
							Status
						</h4>
						<div className="h-px flex-1 bg-base-300" />
					</div>

					<div
						className={`rounded-2xl border ${isRunning ? "border-primary/20 bg-primary/5" : "border-base-300 bg-base-200/30"} p-6 shadow-sm`}
					>
						<div className="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
							<div className="flex items-center gap-4">
								<div
									className={`rounded-xl p-3 ${isRunning ? "bg-primary/20" : isCanceling ? "bg-warning/20" : "bg-success/20"}`}
								>
									{isRunning ? (
										<LoadingSpinner size="sm" />
									) : isCanceling ? (
										<Square className="h-6 w-6 text-warning" />
									) : (
										<CheckCircle2 className="h-6 w-6 text-success" />
									)}
								</div>
								<div>
									<h3 className="font-bold text-lg">
										{isRunning
											? "Importing Database..."
											: isCanceling
												? "Canceling Import..."
												: "Import Complete"}
									</h3>
									<p className="text-base-content/60 text-xs">
										{isRunning ? "Processing records in background" : "Process finished"}
									</p>
								</div>
							</div>

							<div className="flex gap-2">
								{isRunning && !isCanceling && (
									<button
										type="button"
										className="btn btn-outline btn-error btn-sm px-4"
										onClick={handleCancel}
										disabled={cancelImport.isPending}
									>
										Stop Import
									</button>
								)}
								{!isRunning && !isCanceling && (
									<button
										type="button"
										className="btn btn-primary btn-sm px-6"
										onClick={handleReset}
										disabled={resetImport.isPending}
									>
										Done
									</button>
								)}
							</div>
						</div>

						{/* Progress */}
						<div className="mb-8 space-y-2">
							<div className="flex justify-between font-bold font-mono text-base-content/80 text-xs">
								<span>PROGRESS</span>
								<span>{Math.round(progressPercent)}%</span>
							</div>
							<div className="h-2.5 w-full overflow-hidden rounded-full bg-base-300">
								<div
									className={`h-full transition-all duration-300 ${isCanceling ? "bg-warning" : "bg-primary"}`}
									style={{ width: `${progressPercent}%` }}
								/>
							</div>
						</div>

						{/* Stats Grid */}
						<div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
							<div className="rounded-xl bg-base-100 p-4 text-center shadow-sm">
								<span className="block font-bold text-base-content/40 text-xs uppercase tracking-wider">
									Total
								</span>
								<span className="font-bold font-mono text-2xl">{importStatus?.total || 0}</span>
							</div>
							<div className="rounded-xl border-success/20 border-b-2 bg-base-100 p-4 text-center shadow-sm">
								<span className="block font-bold text-success/60 text-xs uppercase tracking-wider">
									Added
								</span>
								<span className="font-bold font-mono text-2xl text-success">
									{importStatus?.added || 0}
								</span>
							</div>
							<div className="rounded-xl border-warning/20 border-b-2 bg-base-100 p-4 text-center shadow-sm">
								<span className="block font-bold text-warning/60 text-xs uppercase tracking-wider">
									Skipped
								</span>
								<span className="font-bold font-mono text-2xl text-warning">
									{importStatus?.skipped || 0}
								</span>
							</div>
							<div className="rounded-xl border-error/20 border-b-2 bg-base-100 p-4 text-center shadow-sm">
								<span className="block font-bold text-error/60 text-xs uppercase tracking-wider">
									Failed
								</span>
								<span className="font-bold font-mono text-2xl text-error">
									{importStatus?.failed || 0}
								</span>
							</div>
						</div>

						{importStatus?.last_error && (
							<div className="alert alert-error mt-6 text-xs sm:text-sm">
								<AlertCircle className="h-4 w-4" />
								<span>{importStatus.last_error}</span>
							</div>
						)}
					</div>
				</section>
			) : (
				<form onSubmit={handleSubmit} className="space-y-8">
					<section className="space-y-6">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-base-content/40 text-xs text-xs uppercase tracking-widest">
								Parameters
							</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Target Directory Name</legend>
								<div className="flex items-center gap-3">
									<div className="rounded-lg bg-base-200 p-2.5">
										<FolderInput className="h-5 w-5 text-base-content/60" />
									</div>
									<input
										type="text"
										placeholder="e.g. MyLibrary"
										className="input w-full bg-base-200/50 font-mono"
										value={rootFolder}
										onChange={(e) => setRootFolder(e.target.value)}
										required
									/>
								</div>
								<p className="label text-base-content/80 text-xs">
									This will create /movies and /tv subdirectories under this name.
								</p>
							</fieldset>

							<div className="flex flex-col justify-center space-y-3">
								<div className="label mb-1 font-semibold text-base-content/80 text-xs">
									Input Method
								</div>
								<div className="flex gap-4">
									<label className="label cursor-pointer gap-2">
										<input
											type="radio"
											name="inputMethod"
											className="radio radio-primary radio-sm"
											checked={inputMethod === "server"}
											onChange={() => setInputMethod("server")}
										/>
										<span className="label-text">File on Server</span>
									</label>
									<label className="label cursor-pointer gap-2">
										<input
											type="radio"
											name="inputMethod"
											className="radio radio-primary radio-sm"
											checked={inputMethod === "upload"}
											onChange={() => setInputMethod("upload")}
										/>
										<span className="label-text">Upload File</span>
									</label>
								</div>
							</div>
						</div>
					</section>

					<section className="space-y-6">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-base-content/40 text-xs text-xs uppercase tracking-widest">
								Source Selection
							</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
							{inputMethod === "server" ? (
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-semibold text-xs">
										Select Database File from Server
									</legend>
									<div className="join w-full">
										<input
											type="text"
											placeholder="e.g. /data/nzbdav/db.sqlite"
											className="input join-item w-full bg-base-100 font-mono"
											value={selectedDbPath}
											onChange={(e) => setSelectedDbPath(e.target.value)}
											required={inputMethod === "server"}
										/>
										<button
											type="button"
											className="btn btn-primary join-item px-6"
											onClick={() => setIsFileBrowserOpen(true)}
										>
											Browse
										</button>
									</div>
								</fieldset>
							) : (
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-semibold text-xs">
										Upload Database File
									</legend>
									<input
										type="file"
										accept=".sqlite,.db"
										className="file-input file-input-bordered file-input-primary file-input-sm w-full bg-base-100"
										onChange={handleFileUpload}
										required={inputMethod === "upload"}
									/>
								</fieldset>
							)}
						</div>
					</section>

					<div className="flex justify-end border-base-200 border-t pt-6">
						<button
							type="submit"
							className="btn btn-primary btn-md px-10 shadow-lg shadow-primary/20"
							disabled={
								isLoading ||
								!rootFolder ||
								(inputMethod === "server" ? !selectedDbPath : !selectedFile)
							}
						>
							{isLoading ? <LoadingSpinner size="sm" /> : <Upload className="h-4 w-4" />}
							Start Import
						</button>
					</div>
				</form>
			)}

			<FileBrowserModal
				isOpen={isFileBrowserOpen}
				onClose={() => setIsFileBrowserOpen(false)}
				onSelect={handleFileSelect}
				filterExtension=".sqlite"
			/>
		</div>
	);
}

function DirectoryScanSection() {
	const [scanPath, setScanPath] = useState("");
	const [validationError, setValidationError] = useState("");
	const [isFileBrowserOpen, setIsFileBrowserOpen] = useState(false);

	const { data: scanStatus } = useScanStatus(2000);
	const startScan = useStartManualScan();
	const cancelScan = useCancelScan();

	const isScanning = scanStatus?.status === ScanStatus.SCANNING;
	const isCanceling = scanStatus?.status === ScanStatus.CANCELING;
	const isIdle = scanStatus?.status === ScanStatus.IDLE || !scanStatus?.status;

	useEffect(() => {
		if (validationError && scanPath) {
			setValidationError("");
		}
	}, [scanPath, validationError]);

	const validatePath = (path: string): boolean => {
		if (!path.trim()) {
			setValidationError("Path is required");
			return false;
		}
		if (!path.startsWith("/")) {
			setValidationError("Path must be absolute (start with /)");
			return false;
		}
		return true;
	};

	const handleStartScan = async () => {
		if (!validatePath(scanPath)) return;
		try {
			await startScan.mutateAsync(scanPath);
		} catch (error) {
			console.error("Failed to start scan:", error);
		}
	};

	const handleCancelScan = async () => {
		try {
			await cancelScan.mutateAsync();
		} catch (error) {
			console.error("Failed to cancel scan:", error);
		}
	};

	const getProgressPercentage = (): number => {
		if (!scanStatus || scanStatus.files_found === 0) return 0;
		return Math.min((scanStatus.files_added / scanStatus.files_found) * 100, 100);
	};

	return (
		<div className="space-y-8">
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-base-content/40 text-xs text-xs uppercase tracking-widest">
						Configuration
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="flex flex-col gap-4 sm:flex-row">
					<fieldset className="fieldset min-w-0 flex-1">
						<legend className="fieldset-legend font-semibold">Directory Path</legend>
						<div className="join w-full">
							<input
								type="text"
								placeholder="/path/to/directory"
								className={`input join-item w-full bg-base-200/50 font-mono ${validationError ? "input-error" : ""}`}
								value={scanPath}
								onChange={(e) => setScanPath(e.target.value)}
								disabled={isScanning || isCanceling}
							/>
							<button
								type="button"
								className="btn btn-primary join-item px-6"
								onClick={() => setIsFileBrowserOpen(true)}
								disabled={isScanning || isCanceling}
							>
								Browse
							</button>
						</div>
						{validationError && <p className="label text-error text-xs">{validationError}</p>}
					</fieldset>

					<div className="flex items-end gap-2">
						{isIdle && (
							<button
								type="button"
								className="btn btn-primary btn-md px-8 shadow-lg shadow-primary/20"
								onClick={handleStartScan}
								disabled={startScan.isPending || !scanPath.trim()}
							>
								<Play className="h-4 w-4" /> Start Scan
							</button>
						)}
						{(isScanning || isCanceling) && (
							<button
								type="button"
								className="btn btn-warning btn-md px-8"
								onClick={handleCancelScan}
								disabled={cancelScan.isPending || isCanceling}
							>
								<Square className="h-4 w-4" /> {isCanceling ? "Canceling..." : "Cancel"}
							</button>
						)}
					</div>
				</div>
			</section>

			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-base-content/40 text-xs text-xs uppercase tracking-widest">
						Status
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div
					className={`rounded-2xl border ${isScanning ? "border-primary/20 bg-primary/5" : "border-base-300 bg-base-200/30"} p-6 shadow-sm`}
				>
					<div className="mb-6 flex items-center justify-between">
						<div className="flex items-center gap-2">
							{isScanning ? (
								<Play className="h-4 w-4 animate-pulse text-info" />
							) : isCanceling ? (
								<Square className="h-4 w-4 text-warning" />
							) : scanStatus?.last_error ? (
								<AlertCircle className="h-4 w-4 text-error" />
							) : (
								<CheckCircle2 className="h-4 w-4 text-success" />
							)}
							<span className="font-medium">
								Status:{" "}
								{isCanceling
									? "Canceling..."
									: isScanning
										? "Scanning"
										: scanStatus?.last_error
											? "Error"
											: "Idle"}
							</span>
						</div>

						<div className="flex gap-4 text-base-content/70 text-sm">
							<span>Files Found: {scanStatus?.files_found || 0}</span>
							<span>Files Added: {scanStatus?.files_added || 0}</span>
						</div>
					</div>

					{/* Progress and Details */}
					{(isScanning || isCanceling || (scanStatus?.files_found || 0) > 0) && (
						<div className="space-y-6">
							<div className="space-y-2">
								<div className="flex justify-between font-bold font-mono text-base-content/80 text-xs">
									<span>PROGRESS</span>
									<span>{Math.round(getProgressPercentage())}%</span>
								</div>
								<div className="h-2 w-full rounded-full bg-base-300">
									<div
										className="h-2 rounded-full bg-primary transition-all duration-300"
										style={{ width: `${getProgressPercentage()}%` }}
									/>
								</div>
							</div>

							{isScanning && scanStatus?.current_file && (
								<div className="rounded-lg bg-base-100 p-3">
									<div className="flex items-center gap-2 font-bold text-base-content/40 text-xs uppercase tracking-widest">
										<FileText className="h-3 w-3" />
										<span>Current</span>
									</div>
									<p className="mt-1 truncate font-mono text-xs opacity-80">
										{scanStatus.current_file.length > 60
											? `...${scanStatus.current_file.slice(-60)}`
											: scanStatus.current_file}
									</p>
								</div>
							)}

							{scanStatus?.path && scanStatus.path !== scanPath && (
								<div className="mt-1 text-base-content/70 text-xs">
									<span>Scanning: </span>
									<span className="font-mono">{scanStatus.path}</span>
								</div>
							)}
						</div>
					)}

					{scanStatus?.last_error && (
						<div className="mt-4">
							<ErrorAlert
								error={new Error(scanStatus.last_error)}
								onRetry={() => scanStatus?.path && handleStartScan()}
							/>
						</div>
					)}

					{/* API Error Display */}
					{(startScan.error || cancelScan.error) && (
						<div className="mt-4">
							<ErrorAlert
								error={(startScan.error || cancelScan.error) as Error}
								onRetry={() => {
									startScan.reset();
									cancelScan.reset();
								}}
							/>
						</div>
					)}
				</div>
			</section>

			<FileBrowserModal
				isOpen={isFileBrowserOpen}
				onClose={() => setIsFileBrowserOpen(false)}
				onSelect={(path) => setScanPath(path)}
				title="Select Directory to Scan"
				allowDirectorySelection={true}
			/>
		</div>
	);
}
