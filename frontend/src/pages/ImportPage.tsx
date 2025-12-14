import {
	AlertCircle,
	CheckCircle2,
	Database,
	FolderInput,
	FolderOpen,
	HardDrive,
	Play,
	Square,
	Upload,
} from "lucide-react";
import { useEffect, useState } from "react";
import { FileBrowserModal } from "../components/files/FileBrowserModal";
import { ErrorAlert } from "../components/ui/ErrorAlert";
import { useToast } from "../contexts/ToastContext";
import {
	useCancelNzbdavImport,
	useCancelScan,
	useNzbdavImportStatus,
	useScanStatus,
	useStartManualScan,
} from "../hooks/useApi";
import { ScanStatus } from "../types/api";

type ImportTab = "nzbdav" | "directory";

export function ImportPage() {
	const [activeTab, setActiveTab] = useState<ImportTab>("nzbdav");

	return (
		<div className="space-y-6">
			<div>
				<h1 className="font-bold text-3xl">Import</h1>
				<p className="text-base-content/70">
					Import existing data from NZBDav database or scan a directory for NZB files.
				</p>
			</div>

			{/* Tabs */}
			<div role="tablist" className="tabs tabs-border">
				<button
					type="button"
					role="tab"
					className={`tab gap-2 ${activeTab === "nzbdav" ? "tab-active" : ""}`}
					onClick={() => setActiveTab("nzbdav")}
				>
					<Database className="h-4 w-4" />
					From NZBDav
				</button>
				<button
					type="button"
					role="tab"
					className={`tab gap-2 ${activeTab === "directory" ? "tab-active" : ""}`}
					onClick={() => setActiveTab("directory")}
				>
					<FolderOpen className="h-4 w-4" />
					From Directory
				</button>
			</div>

			{/* Tab Content */}
			{activeTab === "nzbdav" && <NzbDavImportSection />}
			{activeTab === "directory" && <DirectoryScanSection />}
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

	const isRunning = importStatus?.status === "running";
	const isCanceling = importStatus?.status === "canceling";

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

	return (
		<>
			{error && <ErrorAlert error={error} />}

			<div className="card max-w-2xl bg-base-100 shadow-lg">
				<div className="card-body">
					<div className="mb-4 flex items-center gap-2">
						<Database className="h-5 w-5 text-primary" />
						<h2 className="card-title">Import from NZBDav Database</h2>
					</div>
					<p className="mb-4 text-base-content/70 text-sm">
						Import your existing NZBDav database to populate the library.
					</p>

					{(isRunning || isCanceling) ? (
						<div className="space-y-4 rounded-lg bg-base-200 p-6">
							<div className="flex items-center justify-between">
								<div className="flex items-center gap-3">
									{isCanceling ? (
										<Square className="h-5 w-5 text-warning" />
									) : (
										<span className="loading loading-spinner text-primary" />
									)}
									<div>
										<h3 className="font-bold">
											{isCanceling ? "Canceling Import..." : "Importing Database..."}
										</h3>
										<p className="text-xs text-base-content/70">
											This process runs in the background.
										</p>
									</div>
								</div>
								{!isCanceling && (
									<button 
										className="btn btn-sm btn-warning"
										onClick={handleCancel}
										disabled={cancelImport.isPending}
									>
										Cancel
									</button>
								)}
							</div>

							<div className="stats w-full bg-base-100 shadow">
								<div className="stat place-items-center">
									<div className="stat-title">Total Found</div>
									<div className="stat-value text-base">{importStatus?.total || 0}</div>
								</div>
								<div className="stat place-items-center">
									<div className="stat-title">Added</div>
									<div className="stat-value text-success text-base">{importStatus?.added || 0}</div>
								</div>
								<div className="stat place-items-center">
									<div className="stat-title">Failed</div>
									<div className="stat-value text-error text-base">{importStatus?.failed || 0}</div>
								</div>
							</div>

							{importStatus?.last_error && (
								<div className="alert alert-error text-sm">
									<AlertCircle className="h-4 w-4" />
									<span>{importStatus.last_error}</span>
								</div>
							)}
						</div>
					) : (
						<form onSubmit={handleSubmit} className="space-y-4">
							<fieldset className="fieldset">
								<legend className="fieldset-legend flex items-center gap-2">
									<FolderInput className="h-4 w-4" />
									Target Directory Name
								</legend>
								<input
									type="text"
									placeholder="e.g. MyLibrary"
									className="input"
									value={rootFolder}
									onChange={(e) => setRootFolder(e.target.value)}
									required
								/>
								<p className="label text-base-content/60">
									This will create /movies and /tv subdirectories under this name.
								</p>
							</fieldset>

							<div className="flex gap-4 mb-2">
								<label className="label cursor-pointer gap-2">
									<input
										type="radio"
										name="inputMethod"
										className="radio radio-primary"
										checked={inputMethod === "server"}
										onChange={() => setInputMethod("server")}
									/>
									<span className="label-text">File on Server</span>
								</label>
								<label className="label cursor-pointer gap-2">
									<input
										type="radio"
										name="inputMethod"
										className="radio radio-primary"
										checked={inputMethod === "upload"}
										onChange={() => setInputMethod("upload")}
									/>
									<span className="label-text">Upload File</span>
								</label>
							</div>

							{inputMethod === "server" ? (
								<fieldset className="fieldset">
									<legend className="fieldset-legend flex items-center gap-2">
										<HardDrive className="h-4 w-4" />
										Select Database File from Server
									</legend>
									<div className="join w-full">
										<input
											type="text"
											placeholder="e.g. /data/nzbdav/db.sqlite"
											className="input join-item w-full"
											value={selectedDbPath}
											onChange={(e) => setSelectedDbPath(e.target.value)}
											required={inputMethod === "server"}
										/>
										<button
											type="button"
											className="btn btn-primary join-item"
											onClick={() => setIsFileBrowserOpen(true)}
										>
											Browse
										</button>
									</div>
								</fieldset>
							) : (
								<fieldset className="fieldset">
									<legend className="fieldset-legend flex items-center gap-2">
										<Upload className="h-4 w-4" />
										Upload Database File
									</legend>
									<input
										type="file"
										accept=".sqlite,.db"
										className="file-input file-input-bordered w-full"
										onChange={handleFileUpload}
										required={inputMethod === "upload"}
									/>
								</fieldset>
							)}

							<div className="card-actions mt-4 justify-end">
								<button
									type="submit"
									className="btn btn-primary"
									disabled={isLoading || !rootFolder || (inputMethod === "server" ? !selectedDbPath : !selectedFile)}
								>
									{isLoading ? (
										<span className="loading loading-spinner" />
									) : (
										<Upload className="h-4 w-4" />
									)}
									Start Import
								</button>
							</div>
						</form>
					)}
				</div>
			</div>

			<FileBrowserModal
				isOpen={isFileBrowserOpen}
				onClose={() => setIsFileBrowserOpen(false)}
				onSelect={handleFileSelect}
				filterExtension=".sqlite"
			/>
		</>
	);
}

function DirectoryScanSection() {
	const [scanPath, setScanPath] = useState("");
	const [validationError, setValidationError] = useState("");

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

		setValidationError("");
		return true;
	};

	const handleStartScan = async () => {
		if (!validatePath(scanPath)) {
			return;
		}

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

	const getStatusIcon = () => {
		if (isScanning) return <Play className="h-4 w-4 animate-pulse text-info" />;
		if (isCanceling) return <Square className="h-4 w-4 text-warning" />;
		if (scanStatus?.last_error) return <AlertCircle className="h-4 w-4 text-error" />;
		return <CheckCircle2 className="h-4 w-4 text-success" />;
	};

	const getStatusText = () => {
		if (isCanceling) return "Canceling...";
		if (isScanning) return "Scanning";
		if (scanStatus?.last_error) return "Error";
		return "Idle";
	};

	return (
		<div className="card max-w-2xl bg-base-100 shadow-lg">
			<div className="card-body">
				<div className="mb-4 flex items-center gap-2">
					<FolderOpen className="h-5 w-5 text-primary" />
					<h2 className="card-title">Scan Directory for NZB Files</h2>
				</div>
				<p className="mb-4 text-base-content/70 text-sm">
					Scan a directory on the server to find and import NZB files into the queue.
				</p>

				{/* Path Input and Controls */}
				<div className="mb-4 flex flex-col gap-4 sm:flex-row">
					<fieldset className="fieldset flex-1">
						<legend className="fieldset-legend">Directory Path</legend>
						<input
							type="text"
							placeholder="/path/to/directory"
							className={`input ${validationError ? "input-error" : ""}`}
							value={scanPath}
							onChange={(e) => setScanPath(e.target.value)}
							disabled={isScanning || isCanceling}
						/>
						{validationError && <p className="label text-error">{validationError}</p>}
					</fieldset>

					<div className="flex items-end gap-2">
						{isIdle && (
							<button
								type="button"
								className="btn btn-primary"
								onClick={handleStartScan}
								disabled={startScan.isPending || !scanPath.trim()}
							>
								<Play className="h-4 w-4" />
								Start Scan
							</button>
						)}

						{(isScanning || isCanceling) && (
							<button
								type="button"
								className="btn btn-warning"
								onClick={handleCancelScan}
								disabled={cancelScan.isPending || isCanceling}
							>
								<Square className="h-4 w-4" />
								{isCanceling ? "Canceling..." : "Cancel"}
							</button>
						)}
					</div>
				</div>

				{/* Status Display */}
				<div className="rounded-lg bg-base-200 p-4">
					<div className="mb-2 flex items-center justify-between">
						<div className="flex items-center gap-2">
							{getStatusIcon()}
							<span className="font-medium">Status: {getStatusText()}</span>
						</div>

						<div className="flex gap-4 text-base-content/70 text-sm">
							<span>Files Found: {scanStatus?.files_found || 0}</span>
							<span>Files Added: {scanStatus?.files_added || 0}</span>
						</div>
					</div>

					{/* Progress Bar */}
					{isScanning && (
						<div className="mb-2">
							<div className="mb-1 flex justify-between text-base-content/70 text-xs">
								<span>Progress</span>
								<span>{Math.round(getProgressPercentage())}%</span>
							</div>
							<div className="h-2 w-full rounded-full bg-base-300">
								<div
									className="h-2 rounded-full bg-primary transition-all duration-300"
									style={{ width: `${getProgressPercentage()}%` }}
								/>
							</div>
						</div>
					)}

					{/* Current File */}
					{isScanning && scanStatus?.current_file && (
						<div className="text-base-content/70 text-xs">
							<span>Current: </span>
							<span className="font-mono">
								{scanStatus.current_file.length > 60
									? `...${scanStatus.current_file.slice(-60)}`
									: scanStatus.current_file}
							</span>
						</div>
					)}

					{/* Scan Path */}
					{scanStatus?.path && scanStatus.path !== scanPath && (
						<div className="mt-1 text-base-content/70 text-xs">
							<span>Scanning: </span>
							<span className="font-mono">{scanStatus.path}</span>
						</div>
					)}

					{/* Error Display */}
					{scanStatus?.last_error && (
						<div className="mt-2">
							<ErrorAlert
								error={new Error(scanStatus.last_error)}
								onRetry={() => scanStatus?.path && handleStartScan()}
							/>
						</div>
					)}

					{/* API Error Display */}
					{(startScan.error || cancelScan.error) && (
						<div className="mt-2">
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
			</div>
		</div>
	);
}
