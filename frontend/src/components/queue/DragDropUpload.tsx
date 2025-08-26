import { AlertCircle, CheckCircle2, FileIcon, Upload, X } from "lucide-react";
import { useCallback, useState } from "react";
import { useAuth } from "../../contexts/AuthContext";
import { useUploadNzb } from "../../hooks/useApi";
import { ErrorAlert } from "../ui/ErrorAlert";

interface UploadedFile {
	file: File;
	id: string;
	status: "pending" | "uploading" | "success" | "error";
	errorMessage?: string;
	queueId?: string;
}

export function DragDropUpload() {
	const { user } = useAuth();
	const [isDragOver, setIsDragOver] = useState(false);
	const [uploadedFiles, setUploadedFiles] = useState<UploadedFile[]>([]);
	const uploadMutation = useUploadNzb();

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

	const handleFiles = useCallback(
		(files: File[]) => {
			if (!user?.api_key) {
				console.error("No API key available");
				return;
			}

			const newFiles: UploadedFile[] = files.map((file) => ({
				file,
				id: `${file.name}-${Date.now()}-${Math.random()}`,
				status: "pending" as const,
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
						apiKey: user.api_key || "",
					});

					// Update status to success
					setUploadedFiles((prev) =>
						prev.map((f) =>
							f.id === uploadFile.id
								? {
										...f,
										status: "success" as const,
										queueId: response.nzo_ids[0],
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
		[user?.api_key, uploadMutation, validateFile],
	);

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

	const clearAllFiles = () => {
		setUploadedFiles([]);
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

	if (!user?.api_key) {
		return (
			<div className="card bg-base-100 shadow-lg">
				<div className="card-body">
					<div className="alert alert-warning">
						<AlertCircle className="h-5 w-5" />
						<div>
							<div className="font-bold">API Key Required</div>
							<div>
								You need an API key to upload NZB files. Generate one in the System settings.
							</div>
						</div>
					</div>
				</div>
			</div>
		);
	}

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				<div className="mb-4 flex items-center gap-2">
					<Upload className="h-5 w-5 text-primary" />
					<h2 className="card-title">Upload NZB Files</h2>
				</div>

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
							// Trigger file input click
							const input = e.currentTarget.querySelector("input[type=file]") as HTMLInputElement;
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

				{/* Global upload error */}
				{uploadMutation.error && (
					<div className="mt-4">
						<ErrorAlert
							error={uploadMutation.error as Error}
							onRetry={() => uploadMutation.reset()}
						/>
					</div>
				)}
			</div>
		</div>
	);
}
