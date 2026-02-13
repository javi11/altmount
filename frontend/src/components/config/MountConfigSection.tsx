import {
	AlertTriangle,
	Eye,
	EyeOff,
	HardDrive,
	Play,
	Save,
	Square,
	TestTube,
	Zap,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "../../api/client";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import type { FuseStatus } from "../../types/api";
import type {
	ConfigResponse,
	FuseConfig as FuseConfigType,
	MountStatus,
	MountType,
	RCloneMountFormData,
} from "../../types/config";

interface MountConfigSectionProps {
	config: ConfigResponse;
	onUpdate: (section: string, data: Record<string, unknown>) => Promise<void>;
	isUpdating: boolean;
}

export function MountConfigSection({ config, onUpdate, isUpdating }: MountConfigSectionProps) {
	const [mountType, setMountType] = useState<MountType>(config.mount_type || "none");
	const [mountPath, setMountPath] = useState(config.mount_path || "");
	const [hasChanges, setHasChanges] = useState(false);
	const subSectionDataRef = useRef<Record<string, unknown>>({});
	const { showToast } = useToast();
	const { confirmAction } = useConfirm();

	// Mount status state (unified for rclone + fuse)
	const [rcloneMountStatus, setRcloneMountStatus] = useState<MountStatus | null>(null);
	const [fuseStatus, setFuseStatus] = useState<FuseStatus | null>(null);
	const [isMountLoading, setIsMountLoading] = useState(false);

	// Sync from config when it changes externally
	useEffect(() => {
		setMountType(config.mount_type || "none");
		setMountPath(config.mount_path || "");
		setHasChanges(false);
		// Initialize sub-section ref from config so save always has data
		const type = config.mount_type || "none";
		if (type === "rclone") {
			subSectionDataRef.current = buildRCloneMountFormData(config) as unknown as Record<
				string,
				unknown
			>;
		} else if (type === "fuse" && config.fuse) {
			subSectionDataRef.current = config.fuse as unknown as Record<string, unknown>;
		} else if (type === "rclone_external") {
			subSectionDataRef.current = {
				rc_enabled: true,
				rc_url: config.rclone.rc_url || "",
				vfs_name: config.rclone.vfs_name || "altmount",
				rc_port: config.rclone.rc_port || 5572,
				rc_user: config.rclone.rc_user || "",
				rc_pass: "",
			};
		} else {
			subSectionDataRef.current = {};
		}
	}, [config]);

	// Fetch mount status based on current mount type
	const fetchMountStatus = useCallback(async () => {
		if (mountType === "rclone") {
			try {
				const response = await fetch("/api/rclone/mount/status");
				const result = await response.json();
				if (result.success && result.data) {
					setRcloneMountStatus(result.data);
				}
			} catch {
				// Silently ignore
			}
		} else if (mountType === "fuse") {
			try {
				const response = await apiClient.getFuseStatus();
				setFuseStatus(response);
			} catch {
				// Silently ignore
			}
		}
	}, [mountType]);

	useEffect(() => {
		fetchMountStatus();
		const interval = setInterval(fetchMountStatus, 5000);
		return () => clearInterval(interval);
	}, [fetchMountStatus]);

	const handleMountTypeChange = async (newType: MountType) => {
		if (mountType !== "none" && mountType !== newType) {
			const confirmed = await confirmAction(
				"Switch Mount Type",
				"Switching mount type will change the active mount system. If a mount is currently running, it may need to be stopped manually. Continue?",
				{ type: "warning", confirmText: "Switch", confirmButtonClass: "btn-warning" },
			);
			if (!confirmed) return;
		}
		setMountType(newType);
		subSectionDataRef.current = {};
		setHasChanges(true);
	};

	const handleMountPathChange = (value: string) => {
		setMountPath(value);
		setHasChanges(true);
	};

	const handleSubSectionChange = useCallback((data: Record<string, unknown>) => {
		subSectionDataRef.current = data;
		setHasChanges(true);
	}, []);

	const handleSave = async () => {
		try {
			const payload: Record<string, unknown> = {
				mount_type: mountType,
				mount_path: mountPath,
			};
			if (mountType === "rclone" && Object.keys(subSectionDataRef.current).length > 0) {
				payload.rclone = subSectionDataRef.current;
			} else if (mountType === "fuse" && Object.keys(subSectionDataRef.current).length > 0) {
				payload.fuse = subSectionDataRef.current;
			} else if (
				mountType === "rclone_external" &&
				Object.keys(subSectionDataRef.current).length > 0
			) {
				payload.rclone = subSectionDataRef.current;
			}
			await onUpdate("mount", payload);
			setHasChanges(false);
			showToast({
				type: "success",
				title: "Mount configuration saved",
				message: `Mount type set to ${mountType === "none" ? "disabled" : mountType}`,
			});
		} catch (err) {
			showToast({
				type: "error",
				title: "Failed to save mount configuration",
				message: err instanceof Error ? err.message : "Unknown error",
			});
		}
	};

	// Mount start/stop handlers
	const handleStartMount = async () => {
		setIsMountLoading(true);
		try {
			// Save config before starting (for FUSE, include mount_path in form data)
			const payload: Record<string, unknown> = {
				mount_type: mountType,
				mount_path: mountPath,
			};
			if (mountType === "fuse" && Object.keys(subSectionDataRef.current).length > 0) {
				payload.fuse = { ...subSectionDataRef.current, mount_path: mountPath };
			} else if (mountType === "rclone" && Object.keys(subSectionDataRef.current).length > 0) {
				payload.rclone = subSectionDataRef.current;
			}
			await onUpdate("mount", payload);
			setHasChanges(false);

			if (mountType === "rclone") {
				const response = await fetch("/api/rclone/mount/start", { method: "POST" });
				const result = await response.json();
				if (result.success) {
					setRcloneMountStatus(result.data);
					showToast({ type: "success", title: "Mount started" });
				} else {
					showToast({
						type: "error",
						title: "Failed to start mount",
						message: result.message,
					});
				}
			} else if (mountType === "fuse") {
				await apiClient.startFuseMount(mountPath);
				await fetchMountStatus();
				showToast({ type: "success", title: "Mount started" });
			}
		} catch (err) {
			showToast({
				type: "error",
				title: "Error starting mount",
				message: err instanceof Error ? err.message : "Unknown error",
			});
		} finally {
			setIsMountLoading(false);
		}
	};

	const handleStopMount = async () => {
		setIsMountLoading(true);
		try {
			if (mountType === "rclone") {
				const response = await fetch("/api/rclone/mount/stop", { method: "POST" });
				const result = await response.json();
				if (result.success) {
					setRcloneMountStatus({ mounted: false, mount_point: "" });
					showToast({ type: "success", title: "Mount stopped" });
				} else {
					showToast({
						type: "error",
						title: "Failed to stop mount",
						message: result.message,
					});
				}
			} else if (mountType === "fuse") {
				await apiClient.stopFuseMount();
				await fetchMountStatus();
				showToast({ type: "success", title: "Mount stopped" });
			}
		} catch (err) {
			showToast({
				type: "error",
				title: "Error stopping mount",
				message: err instanceof Error ? err.message : "Unknown error",
			});
		} finally {
			setIsMountLoading(false);
		}
	};

	const handleForceStopMount = async () => {
		const confirmed = await confirmAction(
			"Force Unmount",
			"This will forcefully unmount the FUSE filesystem using system commands. This should only be used when the normal unmount fails or the mount is unresponsive. Continue?",
			{ type: "warning", confirmText: "Force Unmount", confirmButtonClass: "btn-error" },
		);
		if (!confirmed) return;

		setIsMountLoading(true);
		try {
			await apiClient.forceStopFuseMount();
			await fetchMountStatus();
			showToast({ type: "success", title: "Mount force unmounted" });
		} catch (err) {
			showToast({
				type: "error",
				title: "Force unmount failed",
				message: err instanceof Error ? err.message : "Unknown error",
			});
		} finally {
			setIsMountLoading(false);
		}
	};

	// Determine if mount is running
	const isMounted =
		mountType === "rclone"
			? rcloneMountStatus?.mounted === true
			: mountType === "fuse"
				? fuseStatus?.status === "running" || fuseStatus?.status === "starting"
				: false;

	const isFuseError = mountType === "fuse" && fuseStatus?.status === "error";

	// Whether to show mount controls
	const showMountControls = mountType === "rclone" || mountType === "fuse";

	// Mount status display values
	const mountStatusLabel =
		mountType === "rclone"
			? rcloneMountStatus?.mounted
				? "Mounted"
				: "Not Mounted"
			: mountType === "fuse"
				? fuseStatus?.status === "running"
					? "Mounted"
					: fuseStatus?.status === "starting"
						? "Starting..."
						: fuseStatus?.status === "error"
							? "Error"
							: "Not Mounted"
				: "";

	const mountStatusAlertClass =
		mountType === "rclone"
			? rcloneMountStatus?.mounted
				? "alert-success"
				: "alert-warning"
			: mountType === "fuse"
				? fuseStatus?.status === "running"
					? "alert-success"
					: fuseStatus?.status === "error"
						? "alert-error"
						: "alert-warning"
				: "alert-warning";

	const mountTypeOptions: { value: MountType; label: string; description: string }[] = [
		{ value: "none", label: "Disabled", description: "No mount system active" },
		{
			value: "rclone",
			label: "Internal RClone",
			description: "Mount via built-in RClone with VFS cache",
		},
		{
			value: "fuse",
			label: "AltMount Native",
			description: "High-performance native FUSE mount",
		},
		{
			value: "rclone_external",
			label: "External RClone",
			description: "Connect to an external RClone RC server",
		},
	];

	return (
		<div className="space-y-6">
			{/* Mount Type Selector */}
			<div className="space-y-4">
				<h3 className="font-semibold text-lg">Mount Type</h3>
				<p className="text-sm opacity-70">
					Choose how to mount the AltMount filesystem. Only one mount system can be active at a
					time.
				</p>

				<div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
					{mountTypeOptions.map((option) => (
						<label
							key={option.value}
							className={`card cursor-pointer border-2 transition-colors ${
								mountType === option.value
									? "border-primary bg-primary/5"
									: "border-base-300 hover:border-base-content/20"
							}`}
						>
							<div className="card-body p-4">
								<div className="flex items-center gap-3">
									<input
										type="radio"
										name="mount_type"
										className="radio radio-primary"
										checked={mountType === option.value}
										onChange={() => handleMountTypeChange(option.value)}
									/>
									<div>
										<div className="font-medium">{option.label}</div>
										<div className="text-sm opacity-70">{option.description}</div>
									</div>
								</div>
							</div>
						</label>
					))}
				</div>
			</div>

			{/* Mount Path (shown for all types except none) */}
			{mountType !== "none" && (
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Mount Path</legend>
					<input
						type="text"
						className="input"
						value={mountPath}
						onChange={(e) => handleMountPathChange(e.target.value)}
						placeholder="/mnt/altmount"
					/>
					<p className="label">
						Local filesystem path where the mount will be created
						{mountType === "rclone_external" && " (used for symlink resolution)"}
					</p>
				</fieldset>
			)}

			{/* Conditional sub-sections based on mount type */}
			{mountType === "rclone" && (
				<RCloneMountSubSection config={config} onFormDataChange={handleSubSectionChange} />
			)}

			{mountType === "fuse" && (
				<FuseMountSubSection
					config={config}
					isRunning={isMounted}
					onFormDataChange={handleSubSectionChange}
				/>
			)}

			{mountType === "rclone_external" && (
				<ExternalRCloneSubSection config={config} onFormDataChange={handleSubSectionChange} />
			)}

			{/* Unified Save Button */}
			{mountType !== "none" && (
				<div className="flex justify-end">
					<button
						type="button"
						className={`btn btn-primary ${hasChanges ? "animate-pulse" : ""}`}
						onClick={handleSave}
						disabled={isUpdating || !mountPath}
					>
						{isUpdating ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Save className="h-4 w-4" />
						)}
						{isUpdating ? "Saving..." : "Save Configuration"}
					</button>
				</div>
			)}

			{/* Unified Mount Status + Start/Stop */}
			{showMountControls && (
				<div className={`alert ${mountStatusAlertClass}`}>
					<HardDrive className="h-6 w-6" />
					<div>
						<div className="font-bold">{mountStatusLabel}</div>
						{mountType === "rclone" &&
							rcloneMountStatus?.mounted &&
							rcloneMountStatus.mount_point && (
								<div className="text-sm">Mount point: {rcloneMountStatus.mount_point}</div>
							)}
						{mountType === "rclone" && rcloneMountStatus?.error && (
							<div className="text-sm">{rcloneMountStatus.error}</div>
						)}
						{mountType === "fuse" && fuseStatus?.status !== "stopped" && mountPath && (
							<div className="text-sm">Mount point: {mountPath}</div>
						)}
						{mountType === "fuse" && fuseStatus?.health_error && (
							<div className="mt-1 flex items-center gap-1 text-sm">
								<AlertTriangle className="h-3 w-3" />
								{fuseStatus.health_error}
							</div>
						)}
					</div>
					<div className="flex gap-2">
						{isFuseError ? (
							<button
								type="button"
								className="btn btn-sm btn-error btn-outline"
								onClick={handleForceStopMount}
								disabled={isMountLoading}
							>
								{isMountLoading ? (
									<span className="loading loading-spinner loading-xs" />
								) : (
									<Zap className="h-4 w-4" />
								)}
								Force Unmount
							</button>
						) : isMounted ? (
							<>
								<button
									type="button"
									className="btn btn-sm btn-outline"
									onClick={handleStopMount}
									disabled={isMountLoading}
								>
									{isMountLoading ? (
										<span className="loading loading-spinner loading-xs" />
									) : (
										<Square className="h-4 w-4" />
									)}
									Unmount
								</button>
								{mountType === "fuse" && (
									<button
										type="button"
										className="btn btn-sm btn-error btn-outline"
										onClick={handleForceStopMount}
										disabled={isMountLoading}
									>
										<Zap className="h-4 w-4" />
										Force
									</button>
								)}
							</>
						) : (
							<button
								type="button"
								className="btn btn-sm btn-primary"
								onClick={handleStartMount}
								disabled={isMountLoading || !mountPath}
							>
								{isMountLoading ? (
									<span className="loading loading-spinner loading-xs" />
								) : (
									<Play className="h-4 w-4" />
								)}
								Mount
							</button>
						)}
					</div>
				</div>
			)}
		</div>
	);
}

// ─── Internal RClone Mount Sub-Section ──────────────────────────────────────

interface RCloneSubSectionProps {
	config: ConfigResponse;
	onFormDataChange: (data: Record<string, unknown>) => void;
}

function RCloneMountSubSection({ config, onFormDataChange }: RCloneSubSectionProps) {
	const [mountFormData, setMountFormData] = useState<RCloneMountFormData>(
		buildRCloneMountFormData(config),
	);

	useEffect(() => {
		setMountFormData(buildRCloneMountFormData(config));
	}, [config.rclone, config]);

	const handleMountInputChange = (
		field: keyof RCloneMountFormData,
		value: string | boolean | number | Record<string, string>,
	) => {
		setMountFormData((prev) => {
			const next = { ...prev, [field]: value };
			onFormDataChange(next as unknown as Record<string, unknown>);
			return next;
		});
	};

	return (
		<div className="space-y-4">
			<div className="divider" />
			<h3 className="font-semibold text-lg">RClone Mount Settings</h3>

			{/* Basic Mount Settings */}
			<div className="space-y-4">
				<h5 className="font-medium text-base-content/70 text-sm">Basic Mount Settings</h5>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Allow Other Users</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Allow other users to access mount</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.allow_other}
								onChange={(e) => handleMountInputChange("allow_other", e.target.checked)}
							/>
						</label>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Allow Non-Empty</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Allow mounting over non-empty directories</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.allow_non_empty}
								onChange={(e) => handleMountInputChange("allow_non_empty", e.target.checked)}
							/>
						</label>
					</fieldset>
				</div>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Read Only</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Mount as read-only</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.read_only}
								onChange={(e) => handleMountInputChange("read_only", e.target.checked)}
							/>
						</label>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Enable Syslog</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Log to syslog</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.syslog}
								onChange={(e) => handleMountInputChange("syslog", e.target.checked)}
							/>
						</label>
					</fieldset>
				</div>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Timeout</legend>
						<input
							type="text"
							className="input"
							value={mountFormData.timeout}
							onChange={(e) => handleMountInputChange("timeout", e.target.value)}
							placeholder="10m"
						/>
						<p className="label text-xs">I/O timeout (e.g., 10m, 30s)</p>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Log Level</legend>
						<select
							className="select"
							value={mountFormData.log_level}
							onChange={(e) => handleMountInputChange("log_level", e.target.value)}
						>
							<option value="DEBUG">DEBUG</option>
							<option value="INFO">INFO</option>
							<option value="WARN">WARN</option>
							<option value="ERROR">ERROR</option>
						</select>
					</fieldset>
				</div>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">User ID (UID)</legend>
						<input
							type="number"
							className="input"
							value={mountFormData.uid}
							onChange={(e) =>
								handleMountInputChange("uid", Number.parseInt(e.target.value, 10) || 1000)
							}
							min="0"
							max="65535"
						/>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Group ID (GID)</legend>
						<input
							type="number"
							className="input"
							value={mountFormData.gid}
							onChange={(e) =>
								handleMountInputChange("gid", Number.parseInt(e.target.value, 10) || 1000)
							}
							min="0"
							max="65535"
						/>
					</fieldset>
				</div>
			</div>

			{/* VFS Cache Settings */}
			<div className="space-y-4">
				<h5 className="font-medium text-base-content/70 text-sm">VFS Cache Settings</h5>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Cache Directory</legend>
					<input
						type="text"
						className="input"
						value={mountFormData.cache_dir}
						onChange={(e) => handleMountInputChange("cache_dir", e.target.value)}
						placeholder="Defaults to <rclone_path>/cache"
					/>
				</fieldset>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Cache Mode</legend>
						<select
							className="select"
							value={mountFormData.vfs_cache_mode}
							onChange={(e) => handleMountInputChange("vfs_cache_mode", e.target.value)}
						>
							<option value="off">Off</option>
							<option value="minimal">Minimal</option>
							<option value="writes">Writes</option>
							<option value="full">Full</option>
						</select>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Cache Max Size</legend>
						<input
							type="text"
							className="input"
							value={mountFormData.vfs_cache_max_size}
							onChange={(e) => handleMountInputChange("vfs_cache_max_size", e.target.value)}
							placeholder="50G"
						/>
					</fieldset>
				</div>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Cache Max Age</legend>
					<input
						type="text"
						className="input"
						value={mountFormData.vfs_cache_max_age}
						onChange={(e) => handleMountInputChange("vfs_cache_max_age", e.target.value)}
						placeholder="504h"
					/>
				</fieldset>
			</div>

			{/* Performance Settings */}
			<div className="space-y-4">
				<h5 className="font-medium text-base-content/70 text-sm">Performance Settings</h5>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Buffer Size</legend>
						<input
							type="text"
							className="input"
							value={mountFormData.buffer_size}
							onChange={(e) => handleMountInputChange("buffer_size", e.target.value)}
							placeholder="32M"
						/>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">VFS Read Ahead</legend>
						<input
							type="text"
							className="input"
							value={mountFormData.vfs_read_ahead}
							onChange={(e) => handleMountInputChange("vfs_read_ahead", e.target.value)}
							placeholder="128M"
						/>
					</fieldset>
				</div>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Read Chunk Size</legend>
						<input
							type="text"
							className="input"
							value={mountFormData.read_chunk_size}
							onChange={(e) => handleMountInputChange("read_chunk_size", e.target.value)}
							placeholder="32M"
						/>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Read Chunk Size Limit</legend>
						<input
							type="text"
							className="input"
							value={mountFormData.read_chunk_size_limit}
							onChange={(e) => handleMountInputChange("read_chunk_size_limit", e.target.value)}
							placeholder="2G"
						/>
					</fieldset>
				</div>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Directory Cache Time</legend>
						<input
							type="text"
							className="input"
							value={mountFormData.dir_cache_time}
							onChange={(e) => handleMountInputChange("dir_cache_time", e.target.value)}
							placeholder="10m"
						/>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Transfers</legend>
						<input
							type="number"
							className="input"
							value={mountFormData.transfers}
							onChange={(e) =>
								handleMountInputChange("transfers", Number.parseInt(e.target.value, 10) || 4)
							}
							min="1"
							max="32"
						/>
					</fieldset>
				</div>
			</div>

			{/* Advanced Settings */}
			<div className="space-y-4">
				<h5 className="font-medium text-base-content/70 text-sm">Advanced Settings</h5>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Async Read</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Enable async read operations</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.async_read}
								onChange={(e) => handleMountInputChange("async_read", e.target.checked)}
							/>
						</label>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">No Checksum</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Skip checksum verification</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.no_checksum}
								onChange={(e) => handleMountInputChange("no_checksum", e.target.checked)}
							/>
						</label>
					</fieldset>
				</div>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">No Mod Time</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Don't read/write modification time</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.no_mod_time}
								onChange={(e) => handleMountInputChange("no_mod_time", e.target.checked)}
							/>
						</label>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">VFS Fast Fingerprint</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Use fast fingerprinting</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={mountFormData.vfs_fast_fingerprint}
								onChange={(e) => handleMountInputChange("vfs_fast_fingerprint", e.target.checked)}
							/>
						</label>
					</fieldset>
				</div>
			</div>
		</div>
	);
}

// ─── FUSE Mount Sub-Section ─────────────────────────────────────────────────

interface FuseSubSectionProps {
	config: ConfigResponse;
	isRunning: boolean;
	onFormDataChange: (data: Record<string, unknown>) => void;
}

function FuseMountSubSection({ config, isRunning, onFormDataChange }: FuseSubSectionProps) {
	const [formData, setFormData] = useState<Partial<FuseConfigType>>({
		allow_other: true,
		debug: false,
		attr_timeout_seconds: 30,
		entry_timeout_seconds: 1,
		max_cache_size_mb: 128,
		max_read_ahead_mb: 128,
		disk_cache_enabled: false,
		disk_cache_path: "/tmp/altmount-vfs-cache",
		disk_cache_max_size_gb: 10,
		disk_cache_expiry_hours: 24,
		chunk_size_mb: 4,
		read_ahead_chunks: 4,
	});

	useEffect(() => {
		if (config.fuse) {
			setFormData(config.fuse);
		}
	}, [config.fuse]);

	const updateField = (updates: Partial<FuseConfigType>) => {
		setFormData((prev) => {
			const next = { ...prev, ...updates };
			onFormDataChange(next as Record<string, unknown>);
			return next;
		});
	};

	return (
		<div className="space-y-4">
			<div className="divider" />
			<h3 className="font-semibold text-lg">FUSE Mount Settings</h3>

			{/* Kernel Cache Settings */}
			<div className="space-y-4">
				<h4 className="font-medium text-base">Kernel Cache Settings</h4>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Attribute Timeout</legend>
						<div className="join">
							<input
								type="number"
								className="input join-item w-full"
								value={formData.attr_timeout_seconds ?? 30}
								onChange={(e) =>
									updateField({
										attr_timeout_seconds: Number.parseInt(e.target.value, 10) || 0,
									})
								}
								disabled={isRunning}
							/>
							<span className="btn no-animation join-item">sec</span>
						</div>
						<p className="label">How long the kernel caches file attributes</p>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Entry Timeout</legend>
						<div className="join">
							<input
								type="number"
								className="input join-item w-full"
								value={formData.entry_timeout_seconds ?? 1}
								onChange={(e) =>
									updateField({
										entry_timeout_seconds: Number.parseInt(e.target.value, 10) || 0,
									})
								}
								disabled={isRunning}
							/>
							<span className="btn no-animation join-item">sec</span>
						</div>
						<p className="label">How long the kernel caches directory lookups</p>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Kernel Read-Ahead</legend>
						<div className="join">
							<input
								type="number"
								className="input join-item w-full"
								value={formData.max_read_ahead_mb ?? 128}
								onChange={(e) =>
									updateField({
										max_read_ahead_mb: Number.parseInt(e.target.value, 10) || 0,
									})
								}
								disabled={isRunning}
							/>
							<span className="btn no-animation join-item">MB</span>
						</div>
						<p className="label">Maximum data the kernel will request ahead</p>
					</fieldset>
				</div>
			</div>

			{/* Streaming Cache */}
			<div className="space-y-4">
				<h4 className="font-medium text-base">Streaming Cache Settings</h4>
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Max Cache Size</legend>
						<div className="join">
							<input
								type="number"
								className="input join-item w-full"
								value={formData.max_cache_size_mb ?? 128}
								onChange={(e) =>
									updateField({
										max_cache_size_mb: Number.parseInt(e.target.value, 10) || 0,
									})
								}
								disabled={isRunning}
							/>
							<span className="btn no-animation join-item">MB</span>
						</div>
						<p className="label">Read-ahead cache size per file</p>
					</fieldset>
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Allow Other Users</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Allow other users to access mount</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={formData.allow_other ?? true}
								onChange={(e) => updateField({ allow_other: e.target.checked })}
								disabled={isRunning}
							/>
						</label>
					</fieldset>
				</div>
			</div>

			{/* VFS Disk Cache */}
			<div className="space-y-4">
				<h4 className="font-medium text-base">VFS Disk Cache Settings</h4>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Enable VFS Disk Cache</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Cache file content to local disk</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.disk_cache_enabled ?? false}
							onChange={(e) => updateField({ disk_cache_enabled: e.target.checked })}
							disabled={isRunning}
						/>
					</label>
				</fieldset>
				{formData.disk_cache_enabled && (
					<>
						<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Cache Path</legend>
								<input
									type="text"
									className="input w-full"
									value={formData.disk_cache_path ?? "/tmp/altmount-vfs-cache"}
									onChange={(e) => updateField({ disk_cache_path: e.target.value })}
									disabled={isRunning}
								/>
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Max Cache Size</legend>
								<div className="join">
									<input
										type="number"
										className="input join-item w-full"
										value={formData.disk_cache_max_size_gb ?? 10}
										onChange={(e) =>
											updateField({
												disk_cache_max_size_gb: Number.parseInt(e.target.value, 10) || 0,
											})
										}
										disabled={isRunning}
									/>
									<span className="btn no-animation join-item">GB</span>
								</div>
							</fieldset>
						</div>
						<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Cache Expiry</legend>
								<div className="join">
									<input
										type="number"
										className="input join-item w-full"
										value={formData.disk_cache_expiry_hours ?? 24}
										onChange={(e) =>
											updateField({
												disk_cache_expiry_hours: Number.parseInt(e.target.value, 10) || 0,
											})
										}
										disabled={isRunning}
									/>
									<span className="btn no-animation join-item">hours</span>
								</div>
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Chunk Size</legend>
								<div className="join">
									<input
										type="number"
										className="input join-item w-full"
										value={formData.chunk_size_mb ?? 4}
										onChange={(e) =>
											updateField({
												chunk_size_mb: Number.parseInt(e.target.value, 10) || 0,
											})
										}
										disabled={isRunning}
									/>
									<span className="btn no-animation join-item">MB</span>
								</div>
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Read-Ahead Chunks</legend>
								<div className="join">
									<input
										type="number"
										className="input join-item w-full"
										value={formData.read_ahead_chunks ?? 4}
										onChange={(e) =>
											updateField({
												read_ahead_chunks: Number.parseInt(e.target.value, 10) || 0,
											})
										}
										disabled={isRunning}
									/>
									<span className="btn no-animation join-item">chunks</span>
								</div>
							</fieldset>
						</div>
					</>
				)}
			</div>

			{/* Debug */}
			<div className="space-y-4">
				<h4 className="font-medium text-base">Advanced Options</h4>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Debug Logging</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Enable debug logging</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.debug ?? false}
							onChange={(e) => updateField({ debug: e.target.checked })}
							disabled={isRunning}
						/>
					</label>
				</fieldset>
			</div>
		</div>
	);
}

// ─── External RClone Sub-Section ────────────────────────────────────────────

interface ExternalSubSectionProps {
	config: ConfigResponse;
	onFormDataChange: (data: Record<string, unknown>) => void;
}

function ExternalRCloneSubSection({ config, onFormDataChange }: ExternalSubSectionProps) {
	const [formData, setFormData] = useState({
		rc_url: config.rclone.rc_url || "",
		vfs_name: config.rclone.vfs_name || "altmount",
		rc_port: config.rclone.rc_port || 5572,
		rc_user: config.rclone.rc_user || "",
		rc_pass: "",
	});
	const [showPassword, setShowPassword] = useState(false);
	const [isTestingConnection, setIsTestingConnection] = useState(false);
	const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);

	useEffect(() => {
		setFormData({
			rc_url: config.rclone.rc_url || "",
			vfs_name: config.rclone.vfs_name || "altmount",
			rc_port: config.rclone.rc_port || 5572,
			rc_user: config.rclone.rc_user || "",
			rc_pass: "",
		});
	}, [config.rclone]);

	const handleChange = (field: string, value: string | number) => {
		setFormData((prev) => {
			const next = { ...prev, [field]: value };
			onFormDataChange({
				rc_enabled: true,
				rc_url: next.rc_url,
				vfs_name: next.vfs_name,
				rc_port: next.rc_port,
				rc_user: next.rc_user,
				rc_pass: next.rc_pass,
			});
			return next;
		});
	};

	const handleTestConnection = async () => {
		setIsTestingConnection(true);
		setTestResult(null);
		try {
			const response = await fetch("/api/rclone/test", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					rc_enabled: true,
					rc_url: formData.rc_url,
					vfs_name: formData.vfs_name,
					rc_port: formData.rc_port,
					rc_user: formData.rc_user,
					rc_pass: formData.rc_pass,
				}),
			});
			const result = await response.json();
			if (result.success && result.data?.success) {
				setTestResult({
					success: true,
					message: "Connection successful! RC server is accessible.",
				});
			} else {
				setTestResult({
					success: false,
					message: result.data?.error_message || result.message || "Connection failed",
				});
			}
		} catch (err) {
			setTestResult({
				success: false,
				message: err instanceof Error ? err.message : "Network error",
			});
		} finally {
			setIsTestingConnection(false);
		}
	};

	return (
		<div className="space-y-4">
			<div className="divider" />
			<h3 className="font-semibold text-lg">External RClone RC Connection</h3>
			<p className="text-sm opacity-70">
				Connect to an existing external RClone RC server for cache management.
			</p>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">RC URL</legend>
				<input
					type="text"
					className="input"
					value={formData.rc_url}
					onChange={(e) => handleChange("rc_url", e.target.value)}
					placeholder="http://localhost:5572"
				/>
				<p className="label">External RClone RC server URL</p>
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">VFS Name</legend>
				<input
					type="text"
					className="input"
					value={formData.vfs_name}
					onChange={(e) => handleChange("vfs_name", e.target.value)}
					placeholder="altmount"
				/>
				<p className="label">Name of the VFS in the external RClone instance</p>
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">RC Port</legend>
				<input
					type="number"
					className="input"
					value={formData.rc_port}
					onChange={(e) => handleChange("rc_port", Number.parseInt(e.target.value, 10) || 5572)}
					placeholder="5572"
				/>
				<p className="label">Port for RC server</p>
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">RC Username</legend>
				<input
					type="text"
					className="input"
					value={formData.rc_user}
					onChange={(e) => handleChange("rc_user", e.target.value)}
					placeholder="admin"
				/>
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">RC Password</legend>
				<div className="relative">
					<input
						type={showPassword ? "text" : "password"}
						className="input pr-10"
						value={formData.rc_pass}
						onChange={(e) => handleChange("rc_pass", e.target.value)}
						placeholder={
							config.rclone.rc_pass_set ? "Password is set (enter new to change)" : "admin"
						}
					/>
					<button
						type="button"
						className="-translate-y-1/2 btn btn-ghost btn-xs absolute top-1/2 right-2"
						onClick={() => setShowPassword(!showPassword)}
					>
						{showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
					</button>
				</div>
			</fieldset>

			{testResult && (
				<div className={`alert ${testResult.success ? "alert-success" : "alert-error"}`}>
					<span>{testResult.message}</span>
				</div>
			)}

			<div className="flex gap-2">
				<button
					type="button"
					className="btn btn-outline"
					onClick={handleTestConnection}
					disabled={isTestingConnection}
				>
					{isTestingConnection ? (
						<span className="loading loading-spinner loading-sm" />
					) : (
						<TestTube className="h-4 w-4" />
					)}
					{isTestingConnection ? "Testing..." : "Test Connection"}
				</button>
			</div>
		</div>
	);
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function buildRCloneMountFormData(config: ConfigResponse): RCloneMountFormData {
	return {
		mount_enabled: config.rclone.mount_enabled || false,
		mount_options: config.rclone.mount_options || {},
		allow_other: config.rclone.allow_other ?? true,
		allow_non_empty: config.rclone.allow_non_empty ?? true,
		read_only: config.rclone.read_only || false,
		timeout: config.rclone.timeout || "10m",
		syslog: config.rclone.syslog ?? true,
		log_level: config.rclone.log_level || "INFO",
		uid: config.rclone.uid || 1000,
		gid: config.rclone.gid || 1000,
		umask: config.rclone.umask || "002",
		buffer_size: config.rclone.buffer_size || "32M",
		attr_timeout: config.rclone.attr_timeout || "1s",
		transfers: config.rclone.transfers || 4,
		cache_dir: config.rclone.cache_dir || "",
		vfs_cache_mode: config.rclone.vfs_cache_mode || "full",
		vfs_cache_max_size: config.rclone.vfs_cache_max_size || "50G",
		vfs_cache_max_age: config.rclone.vfs_cache_max_age || "504h",
		read_chunk_size: config.rclone.read_chunk_size || "32M",
		read_chunk_size_limit: config.rclone.read_chunk_size_limit || "2G",
		vfs_read_ahead: config.rclone.vfs_read_ahead || "128M",
		dir_cache_time: config.rclone.dir_cache_time || "10m",
		vfs_cache_min_free_space: config.rclone.vfs_cache_min_free_space || "1G",
		vfs_disk_space_total: config.rclone.vfs_disk_space_total || "1G",
		vfs_read_chunk_streams: config.rclone.vfs_read_chunk_streams || 4,
		no_mod_time: config.rclone.no_mod_time || false,
		no_checksum: config.rclone.no_checksum || false,
		async_read: config.rclone.async_read ?? true,
		vfs_fast_fingerprint: config.rclone.vfs_fast_fingerprint || false,
		use_mmap: config.rclone.use_mmap || false,
	};
}
