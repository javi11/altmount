import {
	AlertCircle,
	ArrowRight,
	CheckCircle2,
	Database,
	Download,
	FileCode,
	FileIcon,
	FileText,
	FolderOpen,
	Info,
	Link,
	Play,
	Search,
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
	useClearAllNzbdavMigrations,
	useClearPendingNzbdavMigrations,
	useMigrateNzbdavSymlinks,
	useNzbdavImportStatus,
	useQueueStats,
	useResetNzbdavImportStatus,
	useScanStatus,
	useSearchNZBByName,
	useStartManualScan,
	useUploadNZBLnks,
	useUploadToQueue,
} from "../../hooks/useApi";
import { useConfig } from "../../hooks/useConfig";
import type { NzbdavMigrateSymlinksResponse } from "../../types/api";
import { ScanStatus } from "../../types/api";
import { FileBrowserModal } from "../files/FileBrowserModal";
import { ErrorAlert } from "../ui/ErrorAlert";
import { LoadingSpinner } from "../ui/LoadingSpinner";

type ImportTab = "nzbdav" | "directory" | "upload";

const IMPORT_SECTIONS = {
	nzbdav: {
		title: "From NZBDav",
		description:
			"Import your existing NZBDav database, then migrate arr library symlinks to point at AltMount.",
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
	const [nameInput, setNameInput] = useState<string>("");
	const [passwordInput, setPasswordInput] = useState<string>("");
	const [uploadTab, setUploadTab] = useState<"files" | "nzblnk" | "byname">("files");
	const uploadMutation = useUploadToQueue();
	const uploadLinksMutation = useUploadNZBLnks();
	const searchByNameMutation = useSearchNZBByName();
	const { showToast } = useToast();
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

	const handleNameSubmit = useCallback(async () => {
		const name = nameInput.trim();
		if (!name) return;
		try {
			const result = await searchByNameMutation.mutateAsync({
				name,
				password: passwordInput || undefined,
				category: category || undefined,
			});
			showToast({
				title: "Added to Queue",
				message: `"${result.title}" found via ${result.indexer} (ID: ${result.queue_id})`,
				type: "success",
			});
			setNameInput("");
			setPasswordInput("");
		} catch (error) {
			showToast({
				title: "Search Failed",
				message: error instanceof Error ? error.message : "Could not find NZB for the given name",
				type: "error",
			});
		}
	}, [nameInput, passwordInput, category, searchByNameMutation, showToast]);

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
			<div role="tablist" className="tabs tabs-boxed mb-4 max-w-lg">
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
				<button
					type="button"
					role="tab"
					className={`tab ${uploadTab === "byname" ? "tab-active" : ""}`}
					onClick={() => setUploadTab("byname")}
				>
					<Search className="mr-2 h-4 w-4" />
					By Name
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

			{uploadTab === "byname" && (
				<div className="space-y-4">
					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold">Name / Title</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50"
							placeholder="e.g. Some.Show.S01E01.1080p"
							value={nameInput}
							onChange={(e) => setNameInput(e.target.value)}
							onKeyDown={(e) => e.key === "Enter" && handleNameSubmit()}
						/>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold">Password (optional)</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50"
							placeholder="Archive password if required"
							value={passwordInput}
							onChange={(e) => setPasswordInput(e.target.value)}
						/>
					</fieldset>
					<button
						type="button"
						className="btn btn-primary btn-sm"
						onClick={handleNameSubmit}
						disabled={!nameInput.trim() || searchByNameMutation.isPending}
					>
						{searchByNameMutation.isPending ? (
							<LoadingSpinner size="sm" />
						) : (
							<Search className="h-4 w-4" />
						)}
						Search & Queue
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
	const [nzbdavTab, setNzbdavTab] = useState<"import" | "migrate">("import");
	const [inputMethod, setInputMethod] = useState<"server" | "upload">("server");
	const [selectedDbPath, setSelectedDbPath] = useState("");
	const [blobsPath, setBlobsPath] = useState("");
	const [selectedFile, setSelectedFile] = useState<File | null>(null);
	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<Error | null>(null);
	const { showToast } = useToast();
	const [isFileBrowserOpen, setIsFileBrowserOpen] = useState(false);
	const [isBlobsBrowserOpen, setIsBlobsBrowserOpen] = useState(false);

	const { data: importStatus } = useNzbdavImportStatus(2000);
	const { data: queueStats } = useQueueStats(2000);
	const cancelImport = useCancelNzbdavImport();
	const resetImport = useResetNzbdavImportStatus();
	const clearPending = useClearPendingNzbdavMigrations();
	const clearAllMigrations = useClearAllNzbdavMigrations();

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
		if (inputMethod === "server" && !selectedDbPath) return;
		if (inputMethod === "upload" && !selectedFile) return;

		setIsLoading(true);
		setError(null);

		const formData = new FormData();
		if (blobsPath) {
			formData.append("blobsPath", blobsPath);
		}

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

	const handleBlobsSelect = (path: string) => {
		setBlobsPath(path);
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

	const handleClearAllMigrations = async () => {
		const total = importStatus?.migration_stats?.total ?? 0;
		const confirmed = window.confirm(
			`Delete ALL ${total} NZBDav migration row(s)?\n\nThis will force a full re-import of every blob on the next scan. Use this only if you've deleted the imported files from AltMount and want to start over.`,
		);
		if (!confirmed) return;
		try {
			const res = await clearAllMigrations.mutateAsync();
			await resetImport.mutateAsync();
			showToast({
				title: "Cleared",
				message: `Deleted ${res.data?.deleted ?? 0} migration rows and reset scanner.`,
				type: "success",
			});
		} catch (err) {
			showToast({
				title: "Clear Failed",
				message: err instanceof Error ? err.message : "Could not clear migrations",
				type: "error",
			});
		}
	};

	const handleClearPendingAndReset = async () => {
		const pendingCount = importStatus?.migration_stats?.pending ?? 0;
		const confirmed = window.confirm(
			`Delete ${pendingCount} pending migration row(s) and reset the scanner?\n\nImported and migrated rows are preserved. Use this to start fresh after a cancelled or stuck import.`,
		);
		if (!confirmed) return;
		try {
			const res = await clearPending.mutateAsync();
			await resetImport.mutateAsync();
			showToast({
				title: "Cleared",
				message: `Deleted ${res.data?.deleted ?? 0} pending migration rows and reset scanner.`,
				type: "success",
			});
		} catch (err) {
			showToast({
				title: "Clear Failed",
				message: err instanceof Error ? err.message : "Could not clear pending migrations",
				type: "error",
			});
		}
	};

	const migrationStats = importStatus?.migration_stats;
	const queueProcessing = queueStats?.total_processing ?? 0;
	const queueQueued = queueStats?.total_queued ?? 0;
	const queueDrained = queueProcessing === 0 && queueQueued === 0;
	const showPhase2 = isCompleted || (migrationStats?.symlinks_migrated ?? 0) > 0;

	return (
		<div className="space-y-8">
			<div className="rounded-xl border border-info/30 bg-info/5 p-4">
				<div className="mb-3 flex items-center gap-2">
					<Info className="h-4 w-4 text-info" />
					<h4 className="font-bold text-sm">Migration Steps</h4>
				</div>
				<ol className="list-decimal space-y-1.5 pl-5 text-sm">
					<li>Import the files</li>
					<li className="font-semibold text-warning">Backup library symlinks (very important)</li>
					<li>Make sure AltMount mount is there</li>
					<li>Run the symlink migration</li>
				</ol>
			</div>

			<div role="tablist" className="tabs tabs-boxed mb-2 max-w-md">
				<button
					type="button"
					role="tab"
					className={`tab ${nzbdavTab === "import" ? "tab-active" : ""}`}
					onClick={() => setNzbdavTab("import")}
				>
					<Database className="mr-2 h-4 w-4" />
					Import
				</button>
				<button
					type="button"
					role="tab"
					className={`tab ${nzbdavTab === "migrate" ? "tab-active" : ""}`}
					onClick={() => setNzbdavTab("migrate")}
				>
					<ArrowRight className="mr-2 h-4 w-4" />
					Migrate Symlinks
				</button>
			</div>

			{nzbdavTab === "migrate" ? (
				<MigrateSymlinksSection />
			) : (
				<NzbDavImportTabContent
					error={error}
					isRunning={isRunning}
					isCanceling={isCanceling}
					isCompleted={isCompleted}
					hasResults={hasResults}
					importStatus={importStatus}
					progressPercent={progressPercent}
					inputMethod={inputMethod}
					setInputMethod={setInputMethod}
					selectedDbPath={selectedDbPath}
					setSelectedDbPath={setSelectedDbPath}
					blobsPath={blobsPath}
					setBlobsPath={setBlobsPath}
					selectedFile={selectedFile}
					isLoading={isLoading}
					handleSubmit={handleSubmit}
					handleFileUpload={handleFileUpload}
					handleCancel={handleCancel}
					handleReset={handleReset}
					handleClearPendingAndReset={handleClearPendingAndReset}
					isFileBrowserOpen={isFileBrowserOpen}
					setIsFileBrowserOpen={setIsFileBrowserOpen}
					isBlobsBrowserOpen={isBlobsBrowserOpen}
					setIsBlobsBrowserOpen={setIsBlobsBrowserOpen}
					handleFileSelect={handleFileSelect}
					handleBlobsSelect={handleBlobsSelect}
					cancelImport={cancelImport}
					resetImport={resetImport}
					clearPending={clearPending}
					clearAllMigrations={clearAllMigrations}
					handleClearAllMigrations={handleClearAllMigrations}
					showPhase2={showPhase2}
					migrationStats={migrationStats}
					queueDrained={queueDrained}
					queueProcessing={queueProcessing}
					queueQueued={queueQueued}
				/>
			)}
		</div>
	);
}

type NzbdavImportTabContentProps = {
	error: Error | null;
	isRunning: boolean;
	isCanceling: boolean;
	isCompleted: boolean;
	hasResults: boolean;
	importStatus: ReturnType<typeof useNzbdavImportStatus>["data"];
	progressPercent: number;
	inputMethod: "server" | "upload";
	setInputMethod: (v: "server" | "upload") => void;
	selectedDbPath: string;
	setSelectedDbPath: (v: string) => void;
	blobsPath: string;
	setBlobsPath: (v: string) => void;
	selectedFile: File | null;
	isLoading: boolean;
	handleSubmit: (e: React.FormEvent) => Promise<void>;
	handleFileUpload: (e: React.ChangeEvent<HTMLInputElement>) => void;
	handleCancel: () => Promise<void>;
	handleReset: () => Promise<void>;
	handleClearPendingAndReset: () => Promise<void>;
	isFileBrowserOpen: boolean;
	setIsFileBrowserOpen: (v: boolean) => void;
	isBlobsBrowserOpen: boolean;
	setIsBlobsBrowserOpen: (v: boolean) => void;
	handleFileSelect: (path: string) => void;
	handleBlobsSelect: (path: string) => void;
	cancelImport: ReturnType<typeof useCancelNzbdavImport>;
	resetImport: ReturnType<typeof useResetNzbdavImportStatus>;
	clearPending: ReturnType<typeof useClearPendingNzbdavMigrations>;
	clearAllMigrations: ReturnType<typeof useClearAllNzbdavMigrations>;
	handleClearAllMigrations: () => Promise<void>;
	showPhase2: boolean;
	migrationStats?: MigrationStats;
	queueDrained: boolean;
	queueProcessing: number;
	queueQueued: number;
};

function NzbDavImportTabContent({
	error,
	isRunning,
	isCanceling,
	isCompleted,
	hasResults,
	importStatus,
	progressPercent,
	inputMethod,
	setInputMethod,
	selectedDbPath,
	setSelectedDbPath,
	blobsPath,
	setBlobsPath,
	selectedFile,
	isLoading,
	handleSubmit,
	handleFileUpload,
	handleCancel,
	handleReset,
	handleClearPendingAndReset,
	isFileBrowserOpen,
	setIsFileBrowserOpen,
	isBlobsBrowserOpen,
	setIsBlobsBrowserOpen,
	handleFileSelect,
	handleBlobsSelect,
	cancelImport,
	resetImport,
	clearPending,
	clearAllMigrations,
	handleClearAllMigrations,
	showPhase2,
	migrationStats,
	queueDrained,
	queueProcessing,
	queueQueued,
}: NzbdavImportTabContentProps) {
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
								{(isRunning || isCanceling) && (
									<button
										type="button"
										className="btn btn-error btn-sm px-4"
										onClick={handleReset}
										disabled={resetImport.isPending}
										title="Force-clear import state (use if the import is stuck)"
									>
										Force Reset
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
					{(importStatus?.migration_stats?.pending ?? 0) > 0 && (
						<div className="flex items-center justify-between rounded-xl border border-warning/30 bg-warning/5 p-3">
							<div className="flex items-center gap-2 text-sm">
								<AlertCircle className="h-4 w-4 text-warning" />
								<span>
									<strong>{importStatus?.migration_stats?.pending}</strong> pending migration row(s)
									from a previous run. Clear them to start fresh.
								</span>
							</div>
							<button
								type="button"
								className="btn btn-warning btn-sm"
								onClick={handleClearPendingAndReset}
								disabled={clearPending.isPending || resetImport.isPending}
							>
								<X className="h-4 w-4" /> Clear Pending & Reset
							</button>
						</div>
					)}

					{(importStatus?.migration_stats?.total ?? 0) > 0 && (
						<div className="flex flex-col gap-3 rounded-xl border border-error/30 bg-error/5 p-3 sm:flex-row sm:items-center sm:justify-between">
							<div className="flex items-start gap-2 text-sm">
								<AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-error" />
								<span>
									<strong>{importStatus?.migration_stats?.total}</strong> migration row(s) exist
									from previous imports. Clear all to force a full re-import (use after deleting the
									imported files from AltMount).
								</span>
							</div>
							<button
								type="button"
								className="btn btn-error btn-sm"
								onClick={handleClearAllMigrations}
								disabled={clearAllMigrations.isPending || resetImport.isPending}
							>
								<X className="h-4 w-4" /> Clear All Migrations
							</button>
						</div>
					)}

					<section className="space-y-6">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-base-content/40 text-xs text-xs uppercase tracking-widest">
								Parameters
							</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="space-y-6">
							<div className="flex flex-col space-y-3">
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

						<div className="space-y-4 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
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

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold text-xs">
									Blobs Directory (Required for NzbDav alpha)
								</legend>
								<div className="join w-full">
									<input
										type="text"
										placeholder="e.g. /data/nzbdav/blobs"
										className="input join-item w-full bg-base-100 font-mono"
										value={blobsPath}
										onChange={(e) => setBlobsPath(e.target.value)}
									/>
									<button
										type="button"
										className="btn btn-primary join-item px-6"
										onClick={() => setIsBlobsBrowserOpen(true)}
									>
										Browse
									</button>
								</div>
								<p className="label text-base-content/60 text-xs">
									If left empty, it will default to a "blobs" folder in the same directory as the
									database.
								</p>
							</fieldset>
						</div>
					</section>

					<div className="flex justify-end border-base-200 border-t pt-6">
						<button
							type="submit"
							className="btn btn-primary btn-md px-10 shadow-lg shadow-primary/20"
							disabled={isLoading || (inputMethod === "server" ? !selectedDbPath : !selectedFile)}
						>
							{isLoading ? <LoadingSpinner size="sm" /> : <Upload className="h-4 w-4" />}
							Start Import
						</button>
					</div>
				</form>
			)}

			{showPhase2 && (
				<NzbdavPhase2Section
					migrationStats={migrationStats}
					queueDrained={queueDrained}
					queueProcessing={queueProcessing}
					queueQueued={queueQueued}
				/>
			)}

			<FileBrowserModal
				isOpen={isFileBrowserOpen}
				onClose={() => setIsFileBrowserOpen(false)}
				onSelect={handleFileSelect}
				filterExtension=".sqlite"
			/>

			<FileBrowserModal
				isOpen={isBlobsBrowserOpen}
				onClose={() => setIsBlobsBrowserOpen(false)}
				onSelect={handleBlobsSelect}
				title="Select Blobs Directory"
				allowDirectorySelection={true}
			/>
		</div>
	);
}

interface MigrationStats {
	pending: number;
	imported: number;
	failed: number;
	symlinks_migrated: number;
	total: number;
}

interface SymlinkMigrationFormProps {
	migrationStats?: MigrationStats;
	intro?: React.ReactNode;
	disabled?: boolean;
	disabledBanner?: React.ReactNode;
}

function SymlinkMigrationForm({
	migrationStats,
	intro,
	disabled = false,
	disabledBanner,
}: SymlinkMigrationFormProps) {
	const [libraryPath, setLibraryPath] = useState("");
	const [sourceMountPath, setSourceMountPath] = useState("");
	const [dryRun, setDryRun] = useState(true);
	const [lastReport, setLastReport] = useState<NzbdavMigrateSymlinksResponse | null>(null);
	const migrate = useMigrateNzbdavSymlinks();
	const { showToast } = useToast();

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		try {
			const report = await migrate.mutateAsync({ libraryPath, sourceMountPath, dryRun });
			setLastReport(report);
			showToast({
				title: dryRun ? "Dry Run Complete" : "Migration Complete",
				message: dryRun
					? `Would rewrite ${report.matched} of ${report.scanned} symlinks.`
					: `Rewrote ${report.rewritten} of ${report.scanned} symlinks.`,
				type: "success",
			});
		} catch (err) {
			showToast({
				title: "Migration Failed",
				message: err instanceof Error ? err.message : "An error occurred",
				type: "error",
			});
		}
	};

	const handleApply = async () => {
		try {
			const report = await migrate.mutateAsync({ libraryPath, sourceMountPath, dryRun: false });
			setLastReport(report);
			showToast({
				title: "Migration Complete",
				message: `Rewrote ${report.rewritten} of ${report.scanned} symlinks.`,
				type: "success",
			});
		} catch (err) {
			showToast({
				title: "Migration Failed",
				message: err instanceof Error ? err.message : "An error occurred",
				type: "error",
			});
		}
	};

	return (
		<div className="space-y-6 rounded-2xl border-2 border-primary/20 bg-primary/5 p-6">
			{intro && (
				<div className="flex items-start gap-3">
					<ArrowRight className="mt-0.5 h-5 w-5 shrink-0 text-primary" />
					<div className="text-base-content/70 text-sm">{intro}</div>
				</div>
			)}

			{migrationStats && migrationStats.total > 0 && (
				<div className="grid grid-cols-3 gap-3">
					<div className="rounded-xl bg-base-100 p-3 text-center shadow-sm">
						<span className="block font-bold text-base-content/40 text-xs uppercase tracking-wider">
							Imported
						</span>
						<span className="font-bold font-mono text-success text-xl">
							{migrationStats.imported}
						</span>
					</div>
					<div className="rounded-xl bg-base-100 p-3 text-center shadow-sm">
						<span className="block font-bold text-base-content/40 text-xs uppercase tracking-wider">
							Migrated
						</span>
						<span className="font-bold font-mono text-primary text-xl">
							{migrationStats.symlinks_migrated}
						</span>
					</div>
					<div className="rounded-xl bg-base-100 p-3 text-center shadow-sm">
						<span className="block font-bold text-base-content/40 text-xs uppercase tracking-wider">
							Failed
						</span>
						<span className="font-bold font-mono text-error text-xl">{migrationStats.failed}</span>
					</div>
				</div>
			)}

			{disabled ? (
				disabledBanner
			) : (
				<form onSubmit={handleSubmit} className="space-y-4">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold text-xs">Library Path</legend>
						<input
							type="text"
							placeholder="/mnt/media/movies"
							className="input w-full bg-base-100 font-mono"
							value={libraryPath}
							onChange={(e) => setLibraryPath(e.target.value)}
							required
						/>
						<p className="label text-base-content/60 text-xs">
							The directory containing your arr library symlinks.
						</p>
					</fieldset>

					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold text-xs">NZBDav Mount Path</legend>
						<input
							type="text"
							placeholder="/mnt/remote/nzbdav"
							className="input w-full bg-base-100 font-mono"
							value={sourceMountPath}
							onChange={(e) => setSourceMountPath(e.target.value)}
							required
						/>
						<p className="label text-base-content/60 text-xs">
							The root path of your nzbdav mount (symlinks currently point here).
						</p>
					</fieldset>

					<label className="flex cursor-pointer items-center gap-3">
						<input
							type="checkbox"
							className="checkbox checkbox-primary checkbox-sm"
							checked={dryRun}
							onChange={(e) => setDryRun(e.target.checked)}
						/>
						<span className="text-sm">Dry run (preview only, no changes)</span>
					</label>

					<div className="flex items-center gap-3 pt-2">
						<button
							type="submit"
							className="btn btn-primary btn-sm px-6"
							disabled={migrate.isPending || !libraryPath || !sourceMountPath}
						>
							{migrate.isPending ? <LoadingSpinner size="sm" /> : <Play className="h-4 w-4" />}
							{dryRun ? "Preview" : "Migrate"}
						</button>

						{lastReport?.dry_run && lastReport.matched > 0 && (
							<button
								type="button"
								className="btn btn-success btn-sm px-6"
								onClick={handleApply}
								disabled={migrate.isPending}
							>
								<ArrowRight className="h-4 w-4" />
								Apply ({lastReport.matched} symlinks)
							</button>
						)}
					</div>
				</form>
			)}

			{lastReport && (
				<div className="space-y-3 rounded-xl border border-base-300 bg-base-100 p-4">
					<div className="flex items-center gap-2">
						<CheckCircle2 className="h-4 w-4 text-success" />
						<span className="font-semibold text-sm">
							{lastReport.dry_run ? "Dry Run Results" : "Migration Results"}
						</span>
					</div>
					<div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
						<div className="text-center">
							<div className="font-bold font-mono text-lg">{lastReport.scanned}</div>
							<div className="text-base-content/50 text-xs uppercase">Scanned</div>
						</div>
						<div className="text-center">
							<div className="font-bold font-mono text-lg text-primary">{lastReport.matched}</div>
							<div className="text-base-content/50 text-xs uppercase">Matched</div>
						</div>
						<div className="text-center">
							<div className="font-bold font-mono text-lg text-success">{lastReport.rewritten}</div>
							<div className="text-base-content/50 text-xs uppercase">
								{lastReport.dry_run ? "Would Rewrite" : "Rewritten"}
							</div>
						</div>
						<div className="text-center">
							<div className="font-bold font-mono text-lg text-warning">
								{(lastReport.unmatched ?? []).length}
							</div>
							<div className="text-base-content/50 text-xs uppercase">Unmatched</div>
						</div>
					</div>
					{(lastReport.unmatched ?? []).length > 0 && (
						<details className="mt-2">
							<summary className="cursor-pointer text-warning text-xs">
								{(lastReport.unmatched ?? []).length} unmatched GUIDs
							</summary>
							<ul className="mt-2 max-h-32 overflow-y-auto font-mono text-xs">
								{(lastReport.unmatched ?? []).map((guid) => (
									<li key={guid} className="text-base-content/60">
										{guid}
									</li>
								))}
							</ul>
						</details>
					)}
					{(lastReport.skipped_wrong_prefix ?? 0) > 0 && (
						<div className="alert alert-warning mt-2 text-xs">
							<AlertCircle className="h-4 w-4" />
							<span>
								<strong>{lastReport.skipped_wrong_prefix}</strong> symlink(s) skipped — their target
								doesn't point at the configured NZBDav Mount Path. Check that the path is correct.
							</span>
						</div>
					)}
					{(lastReport.errors ?? []).length > 0 && (
						<div className="alert alert-error mt-2 text-xs">
							<AlertCircle className="h-4 w-4" />
							<span>{(lastReport.errors ?? []).length} error(s) — check server logs</span>
						</div>
					)}
				</div>
			)}
		</div>
	);
}

interface NzbdavPhase2SectionProps {
	migrationStats?: MigrationStats;
	queueDrained: boolean;
	queueProcessing: number;
	queueQueued: number;
}

function NzbdavPhase2Section({
	migrationStats,
	queueDrained,
	queueProcessing,
	queueQueued,
}: NzbdavPhase2SectionProps) {
	const disabledBanner = (
		<div className="rounded-xl border border-warning/30 bg-warning/5 p-4">
			<div className="mb-2 flex items-center gap-2">
				<LoadingSpinner size="sm" />
				<span className="font-semibold text-sm">Waiting for queue to finish</span>
			</div>
			<p className="text-base-content/70 text-sm">
				Symlink rewrite runs after all imported NZBs finish processing in the queue.
			</p>
			<p className="mt-2 font-mono text-base-content/60 text-xs">
				<strong>{queueProcessing}</strong> processing · <strong>{queueQueued}</strong> queued
				remaining
			</p>
		</div>
	);

	return (
		<section className="space-y-6">
			<div className="flex items-center gap-2">
				<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
					Phase 2 — Migrate Library Symlinks
				</h4>
				<div className="h-px flex-1 bg-base-300" />
			</div>

			<SymlinkMigrationForm
				migrationStats={migrationStats}
				intro={
					<p>
						Rewrite your arr library symlinks from the nzbdav mount to AltMount. Available once the
						import queue drains. Run a <strong>dry run</strong> first to preview changes, then
						apply.
					</p>
				}
				disabled={!queueDrained}
				disabledBanner={disabledBanner}
			/>
		</section>
	);
}

function MigrateSymlinksSection() {
	return (
		<div className="space-y-6">
			<SymlinkMigrationForm
				intro={
					<p>
						Use this if you've already imported NZBs (either through AltMount or a previous NZBDav
						setup) and just need to rewrite your arr library symlinks to point at AltMount. Only
						rows already marked as imported will be matched.
					</p>
				}
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
