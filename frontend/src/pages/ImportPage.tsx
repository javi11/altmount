import {
	AlertCircle,
	Box,
	CheckCircle2,
	Database,
	FileText,
	FolderInput,
	FolderOpen,
	Play,
	Square,
	Upload,
	UploadCloud,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { FileBrowserModal } from "../components/files/FileBrowserModal";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { LoadingSpinner } from "../components/ui/LoadingSpinner";
import { useToast } from "../contexts/ToastContext";
import {
	useCancelNzbdavImport,
	useCancelScan,
	useNzbdavImportStatus,
	useResetNzbdavImportStatus,
	useScanStatus,
	useStartManualScan,
	useUploadToQueue,
} from "../hooks/useApi";
import { ScanStatus } from "../types/api";

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
		title: "Upload NZBs",
		description:
			"Upload NZB files directly from your computer. You can select multiple files or a folder.",
		icon: UploadCloud,
	},
};

export function ImportPage() {
	const [activeTab, setActiveTab] = useState<ImportTab>("nzbdav");

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
				<div className="flex items-center space-x-3">
					<div className="rounded-xl bg-primary/10 p-2">
						<Box className="h-8 w-8 text-primary" />
					</div>
					<div>
						<h1 className="font-bold text-3xl tracking-tight">Import</h1>
						<p className="text-base-content/60 text-sm">
							Import existing data from NZBDav database, scan a directory, or upload NZB files.
						</p>
					</div>
				</div>
			</div>

			<div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
				{/* Sidebar Navigation */}
				<div className="lg:col-span-1">
					<div className="card border border-base-200 bg-base-100 shadow-sm">
						<div className="card-body p-2 sm:p-4">
							<div>
								<h3 className="mb-2 px-4 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
									Import Methods
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
					<div className="card min-h-[500px] border border-base-200 bg-base-100 shadow-sm">
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
								{activeTab === "upload" && <UploadSection />}
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}

function UploadSection() {
	const [isUploading, setIsUploading] = useState(false);
	const [progress, setProgress] = useState({ current: 0, total: 0, successes: 0, failures: 0 });
	const fileInputRef = useRef<HTMLInputElement>(null);
	const dirInputRef = useRef<HTMLInputElement>(null);
	const { showToast } = useToast();
	const uploadMutation = useUploadToQueue();

	const handleUpload = async (files: FileList | null) => {
		if (!files || files.length === 0) return;

		const nzbFiles = Array.from(files).filter((f) => f.name.toLowerCase().endsWith(".nzb"));

		if (nzbFiles.length === 0) {
			showToast({
				title: "No NZB Files Found",
				message: "Please select files with .nzb extension.",
				type: "warning",
			});
			return;
		}

		setIsUploading(true);
		setProgress({ current: 0, total: nzbFiles.length, successes: 0, failures: 0 });

		let successes = 0;
		let failures = 0;

		for (let i = 0; i < nzbFiles.length; i++) {
			const file = nzbFiles[i];
			try {
				await uploadMutation.mutateAsync({
					file: file,
					priority: 0,
				});
				successes++;
			} catch (error) {
				console.error(`Failed to upload ${file.name}:`, error);
				failures++;
			}
			setProgress((prev) => ({ ...prev, current: i + 1, successes, failures }));
		}

		setIsUploading(false);
		showToast({
			title: "Upload Complete",
			message: `Successfully uploaded ${successes} files. ${failures} failed.`,
			type: failures > 0 ? "warning" : "success",
		});

		if (fileInputRef.current) fileInputRef.current.value = "";
		if (dirInputRef.current) dirInputRef.current.value = "";
	};

	return (
		<div className="space-y-8">
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
						Selection
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-8 md:grid-cols-2">
					<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Select Files</legend>
							<div className="flex flex-col gap-4">
								<input
									type="file"
									multiple
									accept=".nzb"
									className="file-input file-input-primary file-input-sm w-full"
									onChange={(e) => handleUpload(e.target.files)}
									disabled={isUploading}
									ref={fileInputRef}
								/>
								<p className="text-[10px] opacity-60">Select individual .nzb files</p>
							</div>
						</fieldset>
					</div>

					<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Select Folder</legend>
							<div className="flex flex-col gap-4">
								<input
									type="file"
									// @ts-expect-error - webkitdirectory is supported
									webkitdirectory=""
									directory=""
									className="file-input file-input-primary file-input-sm w-full"
									onChange={(e) => handleUpload(e.target.files)}
									disabled={isUploading}
									ref={dirInputRef}
								/>
								<p className="text-[10px] opacity-60">Upload all .nzb files in a folder</p>
							</div>
						</fieldset>
					</div>
				</div>
			</section>

			{isUploading && (
				<section className="space-y-4">
					<div className="flex items-center gap-2">
						<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
							Upload Progress
						</h4>
						<div className="h-px flex-1 bg-base-300" />
					</div>

					<div className="rounded-2xl border border-primary/20 bg-primary/5 p-6 shadow-sm">
						<div className="space-y-4">
							<div className="flex items-center justify-between">
								<div className="flex items-center gap-2">
									<span className="loading loading-spinner loading-xs text-primary" />
									<span className="font-bold text-sm">Uploading...</span>
								</div>
								<span className="font-mono text-xs">
									{progress.current} / {progress.total}
								</span>
							</div>
							<progress
								className="progress progress-primary w-full"
								value={progress.current}
								max={progress.total}
							/>
							<div className="flex gap-4 font-mono text-[10px]">
								<span className="text-success">Success: {progress.successes}</span>
								<span className="text-error">Failed: {progress.failures}</span>
							</div>
						</div>
					</div>
				</section>
			)}
		</div>
	);
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
			const token = localStorage.getItem("token");
			const headers: HeadersInit = {};
			if (token) {
				headers.Authorization = `Bearer ${token}`;
			}

			const response = await fetch("/api/import/nzbdav", {
				method: "POST",
				headers: headers,
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
						<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
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
										className="btn btn-outline btn-error btn-xs px-4"
										onClick={handleCancel}
										disabled={cancelImport.isPending}
									>
										Stop Import
									</button>
								)}
								{!isRunning && !isCanceling && (
									<button
										type="button"
										className="btn btn-primary btn-xs px-6"
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
							<div className="flex justify-between font-bold font-mono text-[10px] opacity-60">
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
								<span className="block font-bold text-[9px] text-base-content/40 uppercase tracking-wider">
									Total
								</span>
								<span className="font-bold font-mono text-2xl">{importStatus?.total || 0}</span>
							</div>
							<div className="rounded-xl border-success/20 border-b-2 bg-base-100 p-4 text-center shadow-sm">
								<span className="block font-bold text-[9px] text-success/60 uppercase tracking-wider">
									Added
								</span>
								<span className="font-bold font-mono text-2xl text-success">
									{importStatus?.added || 0}
								</span>
							</div>
							<div className="rounded-xl border-warning/20 border-b-2 bg-base-100 p-4 text-center shadow-sm">
								<span className="block font-bold text-[9px] text-warning/60 uppercase tracking-wider">
									Skipped
								</span>
								<span className="font-bold font-mono text-2xl text-warning">
									{importStatus?.skipped || 0}
								</span>
							</div>
							<div className="rounded-xl border-error/20 border-b-2 bg-base-100 p-4 text-center shadow-sm">
								<span className="block font-bold text-[9px] text-error/60 uppercase tracking-wider">
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
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
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
								<p className="label text-[10px] opacity-60">
									This will create /movies and /tv subdirectories under this name.
								</p>
							</fieldset>

							<div className="flex flex-col justify-center space-y-3">
								<label className="label mb-1 font-semibold text-xs opacity-60">Input Method</label>
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
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
								Source Selection
							</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6">
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
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
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
						{validationError && <p className="label text-[10px] text-error">{validationError}</p>}
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
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
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
								<div className="flex justify-between font-bold font-mono text-[10px] opacity-60">
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
									<div className="flex items-center gap-2 font-bold text-[9px] text-base-content/40 uppercase tracking-widest">
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
