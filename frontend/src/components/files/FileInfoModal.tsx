import { formatDistanceToNow } from "date-fns";
import { Archive, Clock, Database, FileText, HardDrive, Info, Lock, Shield, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useToast } from "../../contexts/ToastContext";
import { useAddHealthCheck } from "../../hooks/useApi";
import { isNil } from "../../lib/utils";
import type {
	FileMetadata,
	NestedSegmentInfo,
	NestedSourceInfo,
	SegmentInfo,
} from "../../types/api";
import type { WebDAVFile } from "../../types/webdav";
import { formatFileSize, getFormatLabel } from "../../utils/fileUtils";
import { HealthBadge } from "../ui/StatusBadge";

interface FileInfoModalProps {
	isOpen: boolean;
	file: WebDAVFile | null;
	currentPath: string;
	metadata: FileMetadata | null;
	isLoading: boolean;
	error: Error | null;
	onClose: () => void;
	onRetry: () => void;
}

type TabType = "overview" | "segments" | "nested-sources" | "source";

export function FileInfoModal({
	isOpen,
	file,
	currentPath,
	metadata,
	isLoading,
	error,
	onClose,
	onRetry,
}: FileInfoModalProps) {
	const modalRef = useRef<HTMLDialogElement>(null);
	const [activeTab, setActiveTab] = useState<TabType>("overview");
	const addHealthCheck = useAddHealthCheck();
	const { showToast } = useToast();

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

	const getHealthIcon = (status: string) => {
		switch (status) {
			case "corrupted":
				return "✗";
			default:
				return "?";
		}
	};

	const getHealthColor = (status: string) => {
		switch (status) {
			case "corrupted":
				return "text-error";
			default:
				return "text-base-content/50";
		}
	};

	const renderOverviewTab = () => {
		if (isLoading) {
			return (
				<div className="py-8 text-center">
					<div className="loading loading-spinner loading-lg" />
					<p className="mt-4 text-base-content/70">Loading file metadata...</p>
				</div>
			);
		}

		if (error) {
			return (
				<div className="py-8 text-center">
					<div className="alert alert-warning">
						<Info className="h-5 w-5" />
						<div>
							<div className="font-semibold">Metadata Not Available</div>
							<div className="text-sm">{error.message || "Unable to load file metadata"}</div>
						</div>
					</div>
					<button type="button" className="btn btn-outline mt-4" onClick={onRetry}>
						Retry
					</button>
					<div className="mt-4 space-y-4">
						<h4 className="font-semibold">Basic File Information</h4>
						<div className="grid grid-cols-2 gap-4 text-left">
							<div>
								<span className="text-base-content/70">Size:</span>
								<span className="ml-2 font-mono">{formatFileSize(file.size)}</span>
							</div>
							<div>
								<span className="text-base-content/70">Modified:</span>
								<span className="ml-2">
									{formatDistanceToNow(new Date(file.lastmod), {
										addSuffix: true,
									})}
								</span>
							</div>
							<div>
								<span className="text-base-content/70">Type:</span>
								<span className="ml-2">{file.mime || "Unknown"}</span>
							</div>
							<div>
								<span className="text-base-content/70">Path:</span>
								<span className="ml-2 break-all font-mono">{file.filename}</span>
							</div>
						</div>
					</div>
				</div>
			);
		}

		if (!metadata) {
			return (
				<div className="py-8 text-center">
					<Info className="mx-auto mb-4 h-16 w-16 text-base-content/30" />
					<h3 className="font-semibold text-base-content/70 text-lg">No Metadata Available</h3>
					<p className="text-base-content/50">Detailed file information is not available.</p>
				</div>
			);
		}

		const segmentPercentage = !isNil(metadata.available_segments)
			? Math.round((metadata.available_segments / metadata.segment_count) * 100)
			: 0;

		return (
			<div className="space-y-6">
				{/* Health Status */}
				<div className="card bg-base-200">
					<div className="card-body p-4">
						<div className="flex items-center justify-between">
							<h4 className="flex items-center gap-2 font-semibold">
								<Shield className="h-5 w-5" />
								Health Status
							</h4>
							<HealthBadge status={metadata.status} />
						</div>
						<div className="mt-2">
							<div className="flex items-center gap-2">
								<span className={`text-2xl ${getHealthColor(metadata.status)}`}>
									{getHealthIcon(metadata.status)}
								</span>
								<div>
									<div className="font-medium capitalize">{metadata.status}</div>
									{!isNil(metadata.available_segments) && (
										<div className="text-base-content/70 text-sm">
											{metadata.available_segments} of {metadata.segment_count} segments available (
											{segmentPercentage}%)
										</div>
									)}
								</div>
							</div>
							{metadata.segment_count > 0 && !isNil(metadata.available_segments) && (
								<div className="mt-3">
									<div className="mb-1 flex items-center justify-between text-sm">
										<span>Segment Availability</span>
										<span>{segmentPercentage}%</span>
									</div>
									<progress
										className="progress progress-primary w-full"
										value={metadata.available_segments}
										max={metadata.segment_count}
									/>
								</div>
							)}
						</div>
						<div className="flex gap-2">
							<button
								type="button"
								className="btn btn-sm btn-primary"
								onClick={async () => {
									if (file?.basename && metadata.source_nzb_path) {
										const filePath = `${currentPath}/${file.basename}`.replace(/\/+/g, "/");
										try {
											await addHealthCheck.mutateAsync({
												file_path: filePath,
												source_nzb_path: metadata.source_nzb_path,
											});
											showToast({
												type: "success",
												title: "File Added to Health Check Queue",
												message: `${file.basename} has been successfully added to the health check queue.`,
											});
										} catch (err) {
											console.error("Failed to add health check:", err);
											showToast({
												type: "error",
												title: "Failed to Add File",
												message:
													err instanceof Error
														? err.message
														: "An error occurred while adding the file to the health check queue.",
											});
										}
									}
								}}
								disabled={addHealthCheck.isPending}
							>
								{addHealthCheck.isPending ? (
									<>
										<span className="loading loading-spinner loading-xs" />
										Adding...
									</>
								) : (
									"Check Now"
								)}
							</button>
						</div>
					</div>
				</div>

				{/* File Information */}
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<div className="space-y-3">
						<h4 className="flex items-center gap-2 font-semibold">
							<HardDrive className="h-5 w-5" />
							File Details
						</h4>
						<div className="space-y-2 text-sm">
							<div className="flex justify-between">
								<span className="text-base-content/70">Size:</span>
								<span className="font-mono">{formatFileSize(metadata.file_size)}</span>
							</div>
							<div className="flex justify-between">
								<span className="text-base-content/70">Segments:</span>
								<span>{metadata.segment_count}</span>
							</div>
							<div className="flex justify-between">
								<span className="text-base-content/70">Available:</span>
								<span
									className={
										metadata.available_segments === metadata.segment_count
											? "text-success"
											: "text-warning"
									}
								>
									{metadata.available_segments}
								</span>
							</div>
							{getFormatLabel(file.basename) && (
								<div className="flex justify-between">
									<span className="text-base-content/70">Format:</span>
									<span>{getFormatLabel(file.basename)}</span>
								</div>
							)}
							<div className="flex justify-between">
								<span className="text-base-content/70">Encryption:</span>
								<div className="flex items-center gap-1">
									{metadata.encryption !== "none" && <Lock className="h-3 w-3" />}
									<span className="capitalize">{metadata.encryption}</span>
								</div>
							</div>
							{metadata.password_protected && (
								<div className="flex justify-between">
									<span className="text-base-content/70">Protected:</span>
									<span className="text-warning">Password Required</span>
								</div>
							)}
							{metadata.nested_sources && metadata.nested_sources.length > 0 && (
								<div className="flex justify-between">
									<span className="text-base-content/70">Nested Archive:</span>
									<span className="flex items-center gap-1">
										<Archive className="h-3 w-3" />
										{metadata.nested_sources.length} volume
										{metadata.nested_sources.length !== 1 ? "s" : ""}
									</span>
								</div>
							)}
						</div>
					</div>

					<div className="space-y-3">
						<h4 className="flex items-center gap-2 font-semibold">
							<Clock className="h-5 w-5" />
							Timestamps
						</h4>
						<div className="space-y-2 text-sm">
							<div>
								<div className="text-base-content/70">Created:</div>
								<div>
									{formatDistanceToNow(new Date(metadata.created_at), {
										addSuffix: true,
									})}
								</div>
								<div className="text-base-content/50 text-xs">
									{new Date(metadata.created_at).toLocaleString()}
								</div>
							</div>
							<div>
								<div className="text-base-content/70">Modified:</div>
								<div>
									{formatDistanceToNow(new Date(metadata.modified_at), {
										addSuffix: true,
									})}
								</div>
								<div className="text-base-content/50 text-xs">
									{new Date(metadata.modified_at).toLocaleString()}
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		);
	};

	const renderSegmentsTab = () => {
		if (isLoading) {
			return (
				<div className="py-8 text-center">
					<div className="loading loading-spinner loading-lg" />
					<p className="mt-4 text-base-content/70">Loading segment information...</p>
				</div>
			);
		}

		if (!metadata) {
			return (
				<div className="py-8 text-center">
					<Database className="mx-auto mb-4 h-16 w-16 text-base-content/30" />
					<h3 className="font-semibold text-base-content/70 text-lg">No Metadata Available</h3>
					<p className="text-base-content/50">File metadata could not be loaded.</p>
				</div>
			);
		}

		if (metadata.segments.length === 0) {
			return (
				<div className="py-8 text-center">
					<Database className="mx-auto mb-4 h-16 w-16 text-base-content/30" />
					<h3 className="font-semibold text-base-content/70 text-lg">No Segment Data</h3>
					<p className="text-base-content/50">
						Detailed segment information is not available for this file.
					</p>
				</div>
			);
		}

		return (
			<div className="space-y-4">
				{/* Segments Summary */}
				<div className="stats w-full shadow">
					<div className="stat">
						<div className="stat-title">Total Segments</div>
						<div className="stat-value text-primary">{metadata.segment_count}</div>
					</div>
					<div className="stat">
						<div className="stat-title">Available</div>
						<div className="stat-value text-success">{metadata.available_segments}</div>
					</div>
					{!isNil(metadata.available_segments) && (
						<div className="stat">
							<div className="stat-title">Missing</div>
							<div className="stat-value text-success">
								{metadata.segment_count - metadata.available_segments}
							</div>
						</div>
					)}
				</div>

				{/* Segments List */}
				<div className="overflow-x-auto">
					<table className="table-sm table w-full">
						<thead>
							<tr>
								<th>Status</th>
								<th>Segment ID</th>
								<th>Size</th>
								<th>Offset Range</th>
							</tr>
						</thead>
						<tbody>
							{metadata.segments.map((segment: SegmentInfo, index: number) => (
								<tr key={segment.message_id || index}>
									<td>
										<div
											className={`badge badge-sm ${
												segment.available ? "badge-success" : "badge-error"
											}`}
										>
											{segment.available ? "✓" : "✗"}
										</div>
									</td>
									<td>
										<code className="text-xs">{segment.message_id}</code>
									</td>
									<td>{formatFileSize(segment.segment_size)}</td>
									<td className="font-mono text-xs">
										{segment.start_offset.toLocaleString()} - {segment.end_offset.toLocaleString()}
									</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
			</div>
		);
	};

	const renderNestedSourcesTab = () => {
		if (isLoading) {
			return (
				<div className="py-8 text-center">
					<div className="loading loading-spinner loading-lg" />
					<p className="mt-4 text-base-content/70">Loading nested source information...</p>
				</div>
			);
		}

		if (!metadata || !metadata.nested_sources || metadata.nested_sources.length === 0) {
			return (
				<div className="py-8 text-center">
					<Archive className="mx-auto mb-4 h-16 w-16 text-base-content/30" />
					<h3 className="font-semibold text-base-content/70 text-lg">No Nested Sources</h3>
					<p className="text-base-content/50">This file has no nested archive sources.</p>
				</div>
			);
		}

		return (
			<div className="space-y-4">
				{/* Summary stats */}
				<div className="stats w-full shadow">
					<div className="stat">
						<div className="stat-title">Inner Volumes</div>
						<div className="stat-value text-primary">{metadata.nested_sources.length}</div>
					</div>
					<div className="stat">
						<div className="stat-title">Total Segments</div>
						<div className="stat-value text-secondary">
							{metadata.nested_sources.reduce((acc, ns) => acc + ns.segment_count, 0)}
						</div>
					</div>
				</div>

				{/* Per-volume cards */}
				{metadata.nested_sources.map((ns: NestedSourceInfo) => (
					<div key={ns.volume_index} className="card bg-base-200">
						<div className="card-body p-4">
							<div className="mb-3 flex items-center justify-between">
								<h4 className="flex items-center gap-2 font-semibold">
									<Archive className="h-4 w-4" />
									Volume {ns.volume_index + 1}
								</h4>
								<div className="flex items-center gap-2">
									{ns.encrypted ? (
										<span className="badge badge-warning badge-sm gap-1">
											<Lock className="h-3 w-3" />
											Encrypted
										</span>
									) : (
										<span className="badge badge-success badge-sm">Unencrypted</span>
									)}
								</div>
							</div>
							<div className="mb-3 grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
								<div>
									<div className="text-base-content/70">Inner Length</div>
									<div className="font-mono">{formatFileSize(ns.inner_length)}</div>
								</div>
								<div>
									<div className="text-base-content/70">Volume Size</div>
									<div className="font-mono">{formatFileSize(ns.inner_volume_size)}</div>
								</div>
								<div>
									<div className="text-base-content/70">Segments</div>
									<div>{ns.segment_count}</div>
								</div>
							</div>
							{ns.segments.length > 0 && (
								<div className="overflow-x-auto">
									<table className="table-xs table w-full">
										<thead>
											<tr>
												<th>#</th>
												<th>Message ID</th>
												<th>Size</th>
												<th>Offset Range</th>
											</tr>
										</thead>
										<tbody>
											{ns.segments.map((seg: NestedSegmentInfo, idx: number) => (
												<tr key={seg.message_id || idx}>
													<td>{idx + 1}</td>
													<td>
														<code className="text-xs">{seg.message_id}</code>
													</td>
													<td>{formatFileSize(seg.segment_size)}</td>
													<td className="font-mono text-xs">
														{seg.start_offset.toLocaleString()} - {seg.end_offset.toLocaleString()}
													</td>
												</tr>
											))}
										</tbody>
									</table>
								</div>
							)}
						</div>
					</div>
				))}
			</div>
		);
	};

	const renderSourceTab = () => {
		if (isLoading) {
			return (
				<div className="py-8 text-center">
					<div className="loading loading-spinner loading-lg" />
					<p className="mt-4 text-base-content/70">Loading source information...</p>
				</div>
			);
		}

		if (!metadata) {
			return (
				<div className="py-8 text-center">
					<FileText className="mx-auto mb-4 h-16 w-16 text-base-content/30" />
					<h3 className="font-semibold text-base-content/70 text-lg">No Source Information</h3>
					<p className="text-base-content/50">Source metadata could not be loaded.</p>
				</div>
			);
		}

		return (
			<div className="space-y-6">
				<div className="card bg-base-200">
					<div className="card-body p-4">
						<h4 className="mb-4 flex items-center gap-2 font-semibold">
							<FileText className="h-5 w-5" />
							Source Information
						</h4>
						<div className="space-y-3">
							<div>
								<div className="mb-1 text-base-content/70 text-sm">NZB Source File:</div>
								<div className="break-all rounded bg-base-100 p-2 font-mono text-sm">
									{metadata.source_nzb_path || "Unknown"}
								</div>
							</div>
						</div>
					</div>
				</div>

				{/* Import Details */}
				<div className="grid grid-cols-1 gap-4 text-sm md:grid-cols-2">
					<div>
						<h5 className="mb-2 font-semibold">Import Status</h5>
						<div className="space-y-1">
							<div className="flex justify-between">
								<span className="text-base-content/70">Status:</span>
								<HealthBadge status={metadata.status} />
							</div>
							<div className="flex justify-between">
								<span className="text-base-content/70">Encryption:</span>
								<span className="capitalize">{metadata.encryption}</span>
							</div>
							<div className="flex justify-between">
								<span className="text-base-content/70">Protected:</span>
								<span>{metadata.password_protected ? "Yes" : "No"}</span>
							</div>
						</div>
					</div>
					{!isNil(metadata.available_segments) && (
						<div>
							<h5 className="mb-2 font-semibold">File Integrity</h5>
							<div className="space-y-1">
								<div className="flex justify-between">
									<span className="text-base-content/70">Completeness:</span>
									<span>
										{Math.round((metadata.available_segments / metadata.segment_count) * 100)}%
									</span>
								</div>
								<div className="flex justify-between">
									<span className="text-base-content/70">Segments:</span>
									<span>
										{metadata.available_segments}/{metadata.segment_count}
									</span>
								</div>
							</div>
						</div>
					)}
				</div>
			</div>
		);
	};

	const renderContent = () => {
		switch (activeTab) {
			case "overview":
				return renderOverviewTab();
			case "segments":
				return renderSegmentsTab();
			case "nested-sources":
				return renderNestedSourcesTab();
			case "source":
				return renderSourceTab();
			default:
				return renderOverviewTab();
		}
	};

	return (
		<dialog ref={modalRef} className="modal modal-open" onClose={onClose}>
			<div className="modal-box flex h-5/6 w-11/12 max-w-4xl flex-col">
				{/* Header */}
				<div className="flex items-center justify-between border-base-300 border-b pb-4">
					<div className="flex min-w-0 flex-1 items-center space-x-3">
						<FileText className="h-6 w-6 text-primary" />
						<div className="min-w-0 flex-1">
							<h3 className="font-semibold text-lg">{file.basename}</h3>
							<p className="text-base-content/70 text-sm">
								{formatFileSize(file.size)} • {file.type}
							</p>
						</div>
					</div>
					<button
						type="button"
						className="btn btn-ghost btn-sm"
						onClick={onClose}
						title="Close file info"
					>
						<X className="h-4 w-4" />
					</button>
				</div>

				{/* Tabs */}
				<div className="tabs tabs-bordered mt-4">
					<button
						type="button"
						className={`tab tab-bordered ${activeTab === "overview" ? "tab-active" : ""}`}
						onClick={() => setActiveTab("overview")}
					>
						Overview
					</button>
					<button
						type="button"
						className={`tab tab-bordered ${activeTab === "segments" ? "tab-active" : ""}`}
						onClick={() => setActiveTab("segments")}
					>
						Segments
					</button>
					{metadata?.nested_sources && metadata.nested_sources.length > 0 && (
						<button
							type="button"
							className={`tab tab-bordered ${activeTab === "nested-sources" ? "tab-active" : ""}`}
							onClick={() => setActiveTab("nested-sources")}
						>
							Nested Sources
						</button>
					)}
					<button
						type="button"
						className={`tab tab-bordered ${activeTab === "source" ? "tab-active" : ""}`}
						onClick={() => setActiveTab("source")}
					>
						Source
					</button>
				</div>

				{/* Content */}
				<div className="flex-1 overflow-auto py-4">{renderContent()}</div>
			</div>

			{/* Backdrop */}
			<button type="button" className="modal-backdrop" onClick={onClose} aria-label="Close modal" />
		</dialog>
	);
}
