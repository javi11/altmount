import { AlertCircle, CheckCircle2, FolderOpen, Play, Square } from "lucide-react";
import { useEffect, useState } from "react";
import { useCancelScan, useScanStatus, useStartManualScan } from "../../hooks/useApi";
import { ScanStatus } from "../../types/api";
import { ErrorAlert } from "../ui/ErrorAlert";

export function ManualScanSection() {
	const [scanPath, setScanPath] = useState("");
	const [validationError, setValidationError] = useState("");

	// Auto-refresh scan status every 2 seconds when scanning
	const { data: scanStatus } = useScanStatus(2000);
	const startScan = useStartManualScan();
	const cancelScan = useCancelScan();

	const isScanning = scanStatus?.status === ScanStatus.SCANNING;
	const isCanceling = scanStatus?.status === ScanStatus.CANCELING;
	const isIdle = scanStatus?.status === ScanStatus.IDLE || !scanStatus?.status;

	// Clear validation error when path changes
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
		// Simple progress calculation based on files found vs files added
		// This is approximate since we don't know the total beforehand
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
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<div className="mb-4 flex items-center gap-2">
					<FolderOpen className="h-5 w-5 text-primary" />
					<h2 className="card-title">Manual Directory Scan</h2>
				</div>

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
