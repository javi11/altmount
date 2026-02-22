import { Eye, EyeOff, HardDrive, Play, Save, Square, TestTube } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import type {
	ConfigResponse,
	MountStatus,
	RCloneMountFormData,
	RCloneRCFormData,
} from "../../types/config";

interface RCloneConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (
		section: string,
		data:
			| RCloneRCFormData
			| RCloneMountFormData
			| { mount_path: string }
			| { rclone: RCloneMountFormData; mount_path: string },
	) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function RCloneConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: RCloneConfigSectionProps) {
	const [formData, setFormData] = useState<RCloneRCFormData>({
		rc_enabled: config.rclone.rc_enabled,
		rc_url: config.rclone.rc_url,
		vfs_name: config.rclone.vfs_name || "altmount",
		rc_port: config.rclone.rc_port,
		rc_user: config.rclone.rc_user,
		rc_pass: "",
		rc_options: config.rclone.rc_options,
	});

	const [mountFormData, setMountFormData] = useState<RCloneMountFormData>({
		mount_enabled: config.rclone.mount_enabled || false,
		mount_options: config.rclone.mount_options || {},

		// Mount-Specific Settings
		allow_other: config.rclone.allow_other || true,
		allow_non_empty: config.rclone.allow_non_empty || true,
		read_only: config.rclone.read_only || false,
		timeout: config.rclone.timeout || "10m",
		syslog: config.rclone.syslog || true,

		// System and filesystem options
		log_level: config.rclone.log_level || "INFO",
		uid: config.rclone.uid || 1000,
		gid: config.rclone.gid || 1000,
		umask: config.rclone.umask || "002",
		buffer_size: config.rclone.buffer_size || "32M",
		attr_timeout: config.rclone.attr_timeout || "1s",
		transfers: config.rclone.transfers || 4,

		// VFS Cache Settings
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

		// Advanced Settings
		no_mod_time: config.rclone.no_mod_time || false,
		no_checksum: config.rclone.no_checksum || false,
		async_read: config.rclone.async_read || true,
		vfs_fast_fingerprint: config.rclone.vfs_fast_fingerprint || false,
		use_mmap: config.rclone.use_mmap || false,
	});

	// Separate state for mount path since it's a root-level config
	const [mountPath, setMountPath] = useState(config.mount_path || "/mnt/remotes/altmount");

	const [mountStatus, setMountStatus] = useState<MountStatus | null>(null);
	const [hasChanges, setHasChanges] = useState(false);
	const [hasMountChanges, setHasMountChanges] = useState(false);
	const [hasMountPathChanges, setHasMountPathChanges] = useState(false);
	const [showRCPassword, setShowRCPassword] = useState(false);
	const [isTestingConnection, setIsTestingConnection] = useState(false);
	const [testResult, setTestResult] = useState<{
		success: boolean;
		message: string;
	} | null>(null);
	const [isMountLoading, setIsMountLoading] = useState(false);
	const [isMountToggleSaving, setIsMountToggleSaving] = useState(false);
	const [isRCToggleSaving, setIsRCToggleSaving] = useState(false);
	const { showToast } = useToast();
	const { confirmAction } = useConfirm();

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = {
			rc_enabled: config.rclone.rc_enabled,
			rc_url: config.rclone.rc_url,
			vfs_name: config.rclone.vfs_name || "altmount",
			rc_port: config.rclone.rc_port,
			rc_user: config.rclone.rc_user,
			rc_pass: "",
			rc_options: config.rclone.rc_options,
		};
		setFormData(newFormData);
		setHasChanges(false);

		const newMountFormData = {
			mount_enabled: config.rclone.mount_enabled || false,
			mount_options: config.rclone.mount_options || {},

			// Mount-Specific Settings
			allow_other: config.rclone.allow_other || true,
			allow_non_empty: config.rclone.allow_non_empty || true,
			read_only: config.rclone.read_only || false,
			timeout: config.rclone.timeout || "10m",
			syslog: config.rclone.syslog || true,

			// System and filesystem options
			log_level: config.rclone.log_level || "INFO",
			uid: config.rclone.uid || 1000,
			gid: config.rclone.gid || 1000,
			umask: config.rclone.umask || "002",
			buffer_size: config.rclone.buffer_size || "32M",
			attr_timeout: config.rclone.attr_timeout || "1s",
			transfers: config.rclone.transfers || 4,

			// VFS Cache Settings
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

			// Advanced Settings
			no_mod_time: config.rclone.no_mod_time || false,
			no_checksum: config.rclone.no_checksum || false,
			async_read: config.rclone.async_read || true,
			vfs_fast_fingerprint: config.rclone.vfs_fast_fingerprint || false,
			use_mmap: config.rclone.use_mmap || false,
		};
		setMountFormData(newMountFormData);
		setHasMountChanges(false);

		// Sync mount path
		setMountPath(config.mount_path || "/mnt/remotes/altmount");
		setHasMountPathChanges(false);
	}, [config.rclone, config.mount_path]);

	const fetchMountStatus = useCallback(async () => {
		try {
			const response = await fetch("/api/rclone/mount/status");
			const result = await response.json();
			if (result.success && result.data) {
				setMountStatus(result.data);
			}
		} catch (error) {
			console.error("Failed to fetch mount status:", error);
		}
	}, []);

	// Fetch mount status on component mount and when mount is enabled
	useEffect(() => {
		if (config.rclone.mount_enabled) {
			fetchMountStatus();
		}
	}, [config.rclone.mount_enabled, fetchMountStatus]);

	const handleInputChange = (
		field: keyof RCloneRCFormData,
		value: string | boolean | number | Record<string, string>,
	) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);

		// Check for changes by comparing against original config
		const configData = {
			rc_enabled: config.rclone.rc_enabled,
			rc_url: config.rclone.rc_url,
			vfs_name: config.rclone.vfs_name || "altmount",
			rc_port: config.rclone.rc_port,
			rc_user: config.rclone.rc_user,
			rc_pass: "",
			rc_options: config.rclone.rc_options,
		};

		// Always consider changes if RC password is entered
		const rcPasswordChanged = newData.rc_pass !== "";
		const otherFieldsChanged =
			newData.rc_enabled !== configData.rc_enabled ||
			newData.rc_url !== configData.rc_url ||
			newData.vfs_name !== configData.vfs_name ||
			newData.rc_port !== configData.rc_port ||
			newData.rc_user !== configData.rc_user ||
			JSON.stringify(newData.rc_options) !== JSON.stringify(configData.rc_options);

		setHasChanges(rcPasswordChanged || otherFieldsChanged);
	};

	const handleMountInputChange = (
		field: keyof RCloneMountFormData,
		value: string | boolean | number | Record<string, string>,
	) => {
		const newData = { ...mountFormData, [field]: value };
		setMountFormData(newData);

		// Always mark as changed when any field is modified
		setHasMountChanges(true);

		// Note: RC is automatically managed by the backend when mount is enabled
		// No need to manually enable RC here
	};

	const handleMountPathChange = (value: string) => {
		setMountPath(value);
		setHasMountPathChanges(value !== (config.mount_path || "/mnt/remotes/altmount"));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			// Only send non-empty values for RC password
			const updateData: RCloneRCFormData = {
				rc_enabled: formData.rc_enabled ?? false,
				rc_url: formData.rc_url || "",
				vfs_name: formData.vfs_name || "altmount",
				rc_port: formData.rc_port || 5572,
				rc_user: formData.rc_user || "",
				rc_pass: formData.rc_pass.trim() !== "" ? formData.rc_pass : "",
				rc_options: formData.rc_options || {},
			};

			await onUpdate("rclone", updateData);
			setHasChanges(false);
		}
	};

	// Handle RC enabled toggle with auto-save
	const handleRCEnabledChange = async (enabled: boolean) => {
		// Don't allow changes if mount is enabled (RC is managed by mount)
		if (mountFormData.mount_enabled) return;

		// Update local state immediately for UI responsiveness
		setFormData((prev) => ({ ...prev, rc_enabled: enabled }));

		if (!onUpdate) return;

		setIsRCToggleSaving(true);
		try {
			// Save the RC enabled state immediately
			const updateData: RCloneRCFormData = {
				rc_enabled: enabled,
				rc_url: formData.rc_url || "",
				vfs_name: formData.vfs_name || "altmount",
				rc_port: formData.rc_port || 5572,
				rc_user: formData.rc_user || "",
				rc_pass: "", // Don't send password on toggle
				rc_options: formData.rc_options || {},
			};

			await onUpdate("rclone", updateData);

			// Clear the hasChanges flag since we just saved
			setHasChanges(false);

			showToast({
				type: "success",
				title: enabled ? "RC enabled" : "RC disabled",
				message: `RClone Remote Control has been ${enabled ? "enabled" : "disabled"} successfully`,
			});
		} catch (error) {
			// Revert the state on error
			setFormData((prev) => ({ ...prev, rc_enabled: !enabled }));

			showToast({
				type: "error",
				title: `Failed to ${enabled ? "enable" : "disable"} RC`,
				message: error instanceof Error ? error.message : "Unknown error occurred",
			});
		} finally {
			setIsRCToggleSaving(false);
		}
	};

	const handleSaveMount = async () => {
		if (onUpdate) {
			// When mount is enabled and we have any changes, send them together to avoid validation errors
			if (mountFormData.mount_enabled && (hasMountChanges || hasMountPathChanges)) {
				// Create a combined payload for the parent to handle
				// This ensures both mount settings and path are validated together
				await onUpdate("rclone_with_path", {
					rclone: mountFormData,
					mount_path: mountPath,
				});
				setHasMountChanges(false);
				setHasMountPathChanges(false);
			} else {
				// Handle separate updates when mount is disabled
				if (hasMountChanges) {
					await onUpdate("rclone", mountFormData);
					setHasMountChanges(false);
				}

				if (hasMountPathChanges) {
					await onUpdate("mount_path", { mount_path: mountPath });
					setHasMountPathChanges(false);
				}
			}

			// Refresh mount status after saving
			if (mountFormData.mount_enabled) {
				// Set loading state while refreshing mount status
				setIsMountLoading(true);
				try {
					await fetchMountStatus();
				} finally {
					setIsMountLoading(false);
				}
			}
		}
	};

	// Handle mount enabled toggle with auto-save
	const handleMountEnabledChange = async (enabled: boolean) => {
		// If disabling mount and there's an active mount, show confirmation dialog
		if (!enabled && mountStatus?.mounted) {
			const confirmed = await confirmAction(
				"Disable Mount",
				`The mount is currently active". Disabling the mount will stop the active mount and unmount the filesystem. Do you want to continue?`,
				{
					type: "warning",
					confirmText: "Disable & Unmount",
					confirmButtonClass: "btn-warning",
				},
			);

			if (!confirmed) {
				// User cancelled - revert the checkbox state
				return;
			}
		}

		// Set loading state for both enabling and disabling
		setIsMountToggleSaving(true);

		// Update local state immediately for UI responsiveness
		setMountFormData((prev) => ({ ...prev, mount_enabled: enabled }));

		// When enabling mount, don't auto-save - let user configure path first
		if (enabled) {
			// Mark that there are unsaved mount changes
			setHasMountChanges(true);

			showToast({
				type: "info",
				title: "Mount enabled",
				message: "Please configure the mount path and save your changes",
			});

			// Clear loading state after showing message
			setIsMountToggleSaving(false);
			return;
		}

		// Only auto-save when disabling the mount
		if (!onUpdate) {
			setIsMountToggleSaving(false);
			return;
		}
		try {
			// If disabling and mount is active, stop the mount first
			if (!enabled && mountStatus?.mounted) {
				try {
					const response = await fetch("/api/rclone/mount/stop", {
						method: "POST",
					});
					const result = await response.json();

					if (!result.success) {
						throw new Error(result.message || "Failed to stop mount");
					}

					// Update mount status to reflect stopped state
					setMountStatus({ mounted: false, mount_point: "" });

					showToast({
						type: "success",
						title: "Mount stopped",
						message: "Active mount has been stopped successfully",
					});
				} catch (stopError) {
					// Revert state and show error
					setMountFormData((prev) => ({ ...prev, mount_enabled: true }));

					showToast({
						type: "error",
						title: "Failed to stop mount",
						message: stopError instanceof Error ? stopError.message : "Unknown error occurred",
					});
					return;
				}
			}

			// Save the mount disabled state
			await onUpdate("rclone", { ...mountFormData, mount_enabled: false });

			// Clear the hasMountChanges flag since we just saved
			setHasMountChanges(false);

			showToast({
				type: "success",
				title: "Mount disabled",
				message: "RClone mount has been disabled successfully",
			});
		} catch (error) {
			// Revert the state on error
			setMountFormData((prev) => ({ ...prev, mount_enabled: true }));

			showToast({
				type: "error",
				title: "Failed to disable mount",
				message: error instanceof Error ? error.message : "Unknown error occurred",
			});
		} finally {
			setIsMountToggleSaving(false);
		}
	};

	const handleStartMount = async () => {
		setIsMountLoading(true);
		try {
			const response = await fetch("/api/rclone/mount/start", {
				method: "POST",
			});
			const result = await response.json();
			if (result.success) {
				setMountStatus(result.data);
				showToast({
					type: "success",
					title: "Mount started",
					message: "RClone mount has been started successfully",
				});
			} else {
				showToast({
					type: "error",
					title: "Failed to start mount",
					message: result.message || "Unknown error occurred",
				});
			}
		} catch (error) {
			showToast({
				type: "error",
				title: "Error starting mount",
				message: error instanceof Error ? error.message : "Unknown error occurred",
			});
		} finally {
			setIsMountLoading(false);
		}
	};

	const handleStopMount = async () => {
		setIsMountLoading(true);
		try {
			const response = await fetch("/api/rclone/mount/stop", {
				method: "POST",
			});
			const result = await response.json();
			if (result.success) {
				setMountStatus({ mounted: false, mount_point: "" });
				showToast({
					type: "success",
					title: "Mount stopped",
					message: "RClone mount has been stopped successfully",
				});
			} else {
				showToast({
					type: "error",
					title: "Failed to stop mount",
					message: result.message || "Unknown error occurred",
				});
			}
		} catch (error) {
			showToast({
				type: "error",
				title: "Error stopping mount",
				message: error instanceof Error ? error.message : "Unknown error occurred",
			});
		} finally {
			setIsMountLoading(false);
		}
	};

	const handleTestConnection = async () => {
		setIsTestingConnection(true);
		setTestResult(null);

		try {
			const response = await fetch("/api/rclone/test", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
				},
				body: JSON.stringify({
					rc_enabled: formData.rc_enabled,
					rc_url: formData.rc_url,
					vfs_name: formData.vfs_name,
					rc_port: formData.rc_port,
					rc_user: formData.rc_user,
					rc_pass: formData.rc_pass,
					rc_options: formData.rc_options,
					mount_enabled: mountFormData.mount_enabled,
					mount_options: mountFormData.mount_options,
				}),
			});

			const result = await response.json();

			if (result.success && result.data) {
				if (result.data.success) {
					setTestResult({
						success: true,
						message: "Connection successful! RClone RC is accessible.",
					});
				} else {
					setTestResult({
						success: false,
						message: result.data.error_message || "Connection failed",
					});
				}
			} else {
				setTestResult({
					success: false,
					message: result.message || "Test failed",
				});
			}
		} catch (error) {
			setTestResult({
				success: false,
				message: error instanceof Error ? error.message : "Network error occurred",
			});
		} finally {
			setIsTestingConnection(false);
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">RClone Configuration</h3>

			{/* Mount Configuration Section */}
			<div className="mt-8 space-y-4">
				<h4 className="font-medium text-base">Mount Configuration</h4>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Enable RClone Mount</legend>
					<label className="label cursor-pointer">
						<span className="label-text">
							Enable RClone mount for WebDAV
							{isMountToggleSaving && <span className="loading loading-spinner loading-xs ml-2" />}
						</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={mountFormData.mount_enabled}
							disabled={isReadOnly || isMountToggleSaving}
							onChange={(e) => handleMountEnabledChange(e.target.checked)}
						/>
					</label>
					<p className="label">
						Mount the AltMount WebDAV as a local filesystem using RClone
						{isMountToggleSaving && " (Saving...)"}
					</p>
					{mountFormData.mount_enabled && hasMountChanges && (
						<div className="alert alert-warning mt-2">
							<div className="text-sm">
								<div className="font-semibold">Configuration not saved!</div>
								<p className="mt-1">
									Please configure the mount path below and click "Save Mount Changes" to apply your
									settings.
								</p>
							</div>
						</div>
					)}
					{mountFormData.mount_enabled && (
						<div className="alert alert-info mt-2">
							<div className="text-sm">
								<div className="font-semibold">Mount service will automatically:</div>
								<ul className="mt-1 ml-4 list-disc">
									<li>Start and manage the RC server on port 5572</li>
									<li>Configure all necessary RC settings</li>
									<li>Handle mount operations and cache management</li>
								</ul>
							</div>
						</div>
					)}
				</fieldset>

				{mountFormData.mount_enabled && (
					<>
						<fieldset className="fieldset">
							<legend className="fieldset-legend">Mount Point</legend>
							<input
								type="text"
								className="input"
								value={mountPath}
								disabled={isReadOnly}
								onChange={(e) => handleMountPathChange(e.target.value)}
								placeholder="/mnt/remotes/altmount"
							/>
							<p className="label">Local filesystem path where WebDAV will be mounted</p>
						</fieldset>

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
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("allow_other", e.target.checked)}
										/>
									</label>
									<p className="label text-xs">Enables --allow-other for FUSE mount</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Allow Non-Empty</legend>
									<label className="label cursor-pointer">
										<span className="label-text">Allow mounting over non-empty directories</span>
										<input
											type="checkbox"
											className="checkbox"
											checked={mountFormData.allow_non_empty}
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("allow_non_empty", e.target.checked)}
										/>
									</label>
									<p className="label text-xs">Enables --allow-non-empty for mounting</p>
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
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("read_only", e.target.checked)}
										/>
									</label>
									<p className="label text-xs">Prevents write operations to the mount</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Enable Syslog</legend>
									<label className="label cursor-pointer">
										<span className="label-text">Log to syslog</span>
										<input
											type="checkbox"
											className="checkbox"
											checked={mountFormData.syslog}
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("syslog", e.target.checked)}
										/>
									</label>
									<p className="label text-xs">Enables --syslog for system logging</p>
								</fieldset>
							</div>

							<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Timeout</legend>
									<input
										type="text"
										className="input"
										value={mountFormData.timeout}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("timeout", e.target.value)}
										placeholder="10m"
									/>
									<p className="label text-xs">I/O timeout for mount operations (e.g., 10m, 30s)</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Log Level</legend>
									<select
										className="select"
										value={mountFormData.log_level}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("log_level", e.target.value)}
									>
										<option value="DEBUG">DEBUG</option>
										<option value="INFO">INFO</option>
										<option value="WARN">WARN</option>
										<option value="ERROR">ERROR</option>
									</select>
									<p className="label text-xs">Log level for rclone operations</p>
								</fieldset>
							</div>

							<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">User ID (UID)</legend>
									<input
										type="number"
										className="input"
										value={mountFormData.uid}
										disabled={isReadOnly}
										onChange={(e) =>
											handleMountInputChange("uid", Number.parseInt(e.target.value, 10) || 1000)
										}
										min="0"
										max="65535"
									/>
									<p className="label text-xs">User ID for file ownership (default: 1000)</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Group ID (GID)</legend>
									<input
										type="number"
										className="input"
										value={mountFormData.gid}
										disabled={isReadOnly}
										onChange={(e) =>
											handleMountInputChange("gid", Number.parseInt(e.target.value, 10) || 1000)
										}
										min="0"
										max="65535"
									/>
									<p className="label text-xs">Group ID for file ownership (default: 1000)</p>
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
									disabled={isReadOnly}
									onChange={(e) => handleMountInputChange("cache_dir", e.target.value)}
									placeholder="Defaults to <rclone_path>/cache (e.g., /config/cache)"
								/>
								<p className="label text-xs">
									Directory for VFS cache storage (leave empty to use default location)
								</p>
							</fieldset>

							<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Cache Mode</legend>
									<select
										className="select"
										value={mountFormData.vfs_cache_mode}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("vfs_cache_mode", e.target.value)}
									>
										<option value="off">Off</option>
										<option value="minimal">Minimal</option>
										<option value="writes">Writes</option>
										<option value="full">Full</option>
									</select>
									<p className="label text-xs">
										VFS cache mode: full recommended for best performance
									</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Cache Max Size</legend>
									<input
										type="text"
										className="input"
										value={mountFormData.vfs_cache_max_size}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("vfs_cache_max_size", e.target.value)}
										placeholder="50G"
									/>
									<p className="label text-xs">Maximum cache size (e.g., 50G, 1T)</p>
								</fieldset>
							</div>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Cache Max Age</legend>
								<input
									type="text"
									className="input"
									value={mountFormData.vfs_cache_max_age}
									disabled={isReadOnly}
									onChange={(e) => handleMountInputChange("vfs_cache_max_age", e.target.value)}
									placeholder="504h"
								/>
								<p className="label text-xs">Maximum cache age (e.g., 504h, 7d)</p>
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
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("buffer_size", e.target.value)}
										placeholder="32M"
									/>
									<p className="label text-xs">Buffer size for file operations (e.g., 32M, 64M)</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">VFS Read Ahead</legend>
									<input
										type="text"
										className="input"
										value={mountFormData.vfs_read_ahead}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("vfs_read_ahead", e.target.value)}
										placeholder="128M"
									/>
									<p className="label text-xs">VFS read-ahead size (e.g., 128M, 256M)</p>
								</fieldset>
							</div>

							<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Read Chunk Size</legend>
									<input
										type="text"
										className="input"
										value={mountFormData.read_chunk_size}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("read_chunk_size", e.target.value)}
										placeholder="32M"
									/>
									<p className="label text-xs">VFS read chunk size (e.g., 32M, 64M)</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Read Chunk Size Limit</legend>
									<input
										type="text"
										className="input"
										value={mountFormData.read_chunk_size_limit}
										disabled={isReadOnly}
										onChange={(e) =>
											handleMountInputChange("read_chunk_size_limit", e.target.value)
										}
										placeholder="2G"
									/>
									<p className="label text-xs">Maximum read chunk size (e.g., 2G, 4G)</p>
								</fieldset>
							</div>

							<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Directory Cache Time</legend>
									<input
										type="text"
										className="input"
										value={mountFormData.dir_cache_time}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("dir_cache_time", e.target.value)}
										placeholder="10m"
									/>
									<p className="label text-xs">Directory cache time (e.g., 10m, 1h)</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Transfers</legend>
									<input
										type="number"
										className="input"
										value={mountFormData.transfers}
										disabled={isReadOnly}
										onChange={(e) =>
											handleMountInputChange("transfers", Number.parseInt(e.target.value, 10) || 4)
										}
										min="1"
										max="32"
									/>
									<p className="label text-xs">Number of parallel transfers (1-32)</p>
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
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("async_read", e.target.checked)}
										/>
									</label>
									<p className="label text-xs">Enables --async-read for better performance</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">No Checksum</legend>
									<label className="label cursor-pointer">
										<span className="label-text">Skip checksum verification</span>
										<input
											type="checkbox"
											className="checkbox"
											checked={mountFormData.no_checksum}
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("no_checksum", e.target.checked)}
										/>
									</label>
									<p className="label text-xs">Disable checksum verification for speed</p>
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
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("no_mod_time", e.target.checked)}
										/>
									</label>
									<p className="label text-xs">Skip modification time operations</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">VFS Fast Fingerprint</legend>
									<label className="label cursor-pointer">
										<span className="label-text">Use fast fingerprinting</span>
										<input
											type="checkbox"
											className="checkbox"
											checked={mountFormData.vfs_fast_fingerprint}
											disabled={isReadOnly}
											onChange={(e) =>
												handleMountInputChange("vfs_fast_fingerprint", e.target.checked)
											}
										/>
									</label>
									<p className="label text-xs">Enable fast VFS fingerprinting</p>
								</fieldset>
							</div>
						</div>

						{/* Mount Status */}
						{mountStatus && (
							<div className={`alert ${mountStatus.mounted ? "alert-success" : "alert-warning"}`}>
								<HardDrive className="h-6 w-6" />
								<div>
									<div className="font-bold">{mountStatus.mounted ? "Mounted" : "Not Mounted"}</div>
									{mountStatus.mounted && mountStatus.mount_point && (
										<div className="text-sm">Mount point: {mountStatus.mount_point}</div>
									)}
									{mountStatus.error && <div className="text-sm">{mountStatus.error}</div>}
								</div>
								{mountStatus.mounted ? (
									<button
										type="button"
										className="btn btn-sm btn-outline"
										onClick={handleStopMount}
										disabled={isReadOnly || isMountLoading}
									>
										{isMountLoading ? (
											<span className="loading loading-spinner loading-xs" />
										) : (
											<Square className="h-4 w-4" />
										)}
										{isMountLoading ? "Stopping..." : "Stop Mount"}
									</button>
								) : (
									<button
										type="button"
										className="btn btn-sm btn-primary"
										onClick={handleStartMount}
										disabled={isReadOnly || !mountPath || isMountLoading}
									>
										{isMountLoading ? (
											<span className="loading loading-spinner loading-xs" />
										) : (
											<Play className="h-4 w-4" />
										)}
										{isMountLoading ? "Starting..." : "Start Mount"}
									</button>
								)}
							</div>
						)}

						{/* Action Buttons */}
						{!isReadOnly && (
							<div className="flex justify-end gap-2">
								<button
									type="button"
									className={`btn btn-primary ${hasMountChanges || hasMountPathChanges ? "animate-pulse" : ""}`}
									onClick={handleSaveMount}
									disabled={
										(!hasMountChanges && !hasMountPathChanges) || isUpdating || isMountLoading
									}
								>
									{isUpdating || isMountLoading ? (
										<span className="loading loading-spinner loading-sm" />
									) : (
										<Save className="h-4 w-4" />
									)}
									{isUpdating
										? "Saving..."
										: isMountLoading
											? "Checking Mount..."
											: "Save Mount Changes"}
								</button>
							</div>
						)}
					</>
				)}
			</div>

			{/* RC Configuration Settings */}
			<div className="space-y-4">
				<h4 className="font-medium text-base">RC (Remote Control) Settings</h4>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Enable RC Connection</legend>
					<label className="label cursor-pointer">
						<span className="label-text">
							Enable RClone Remote Control for cache notifications
							{mountFormData.mount_enabled && (
								<span className="badge badge-info badge-sm ml-2">Auto-configured by mount</span>
							)}
							{isRCToggleSaving && !mountFormData.mount_enabled && (
								<span className="loading loading-spinner loading-xs ml-2" />
							)}
						</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={mountFormData.mount_enabled || formData.rc_enabled}
							disabled={isReadOnly || mountFormData.mount_enabled || isRCToggleSaving}
							onChange={(e) => handleRCEnabledChange(e.target.checked)}
						/>
					</label>
					<p className="label">
						{mountFormData.mount_enabled
							? "RC server is automatically managed by the mount service"
							: isRCToggleSaving
								? "Saving..."
								: "Enable connection to RClone RC server for cache refresh notifications"}
					</p>
					{mountFormData.mount_enabled && (
						<div className="mt-2 space-y-1">
							<span className="block text-info text-sm">
								Mount service automatically starts and manages the RC server on port 5572
							</span>
							<span className="block text-base-content/70 text-xs">
								RC configuration below is read-only when mount is enabled
							</span>
						</div>
					)}
				</fieldset>

				{(formData.rc_enabled || mountFormData.mount_enabled) && (
					<>
						<fieldset className="fieldset">
							<legend className="fieldset-legend">RC URL</legend>
							<input
								type="text"
								className="input"
								value={mountFormData.mount_enabled ? "" : formData.rc_url}
								disabled={isReadOnly || mountFormData.mount_enabled}
								onChange={(e) => handleInputChange("rc_url", e.target.value)}
								placeholder={
									mountFormData.mount_enabled
										? "Internal server (managed by mount)"
										: "http://localhost:5572 (leave empty to start internal RC server)"
								}
							/>
							<p className="label">
								{mountFormData.mount_enabled
									? "Using internal RC server managed by mount service"
									: "External RClone RC server URL (leave empty to use internal RC server)"}
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">VFS Name</legend>
							<input
								type="text"
								className="input"
								value={formData.vfs_name}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("vfs_name", e.target.value)}
								placeholder="altmount"
							/>
							<p className="label">
								Name of the VFS in RClone (default: altmount). Change this if your external rclone
								mount uses a different name.
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">RC Port</legend>
							<input
								type="number"
								className="input"
								value={formData.rc_port}
								disabled={isReadOnly}
								onChange={(e) =>
									handleInputChange("rc_port", Number.parseInt(e.target.value, 10) || 5572)
								}
								placeholder="5572"
							/>
							<p className="label">Port for RC server (default: 5572)</p>
						</fieldset>

						{!mountFormData.mount_enabled && (
							<>
								<fieldset className="fieldset">
									<legend className="fieldset-legend">RC Username</legend>
									<input
										type="text"
										className="input"
										value={formData.rc_user}
										disabled={isReadOnly}
										onChange={(e) => handleInputChange("rc_user", e.target.value)}
										placeholder="admin"
									/>
									<p className="label">Username for RClone RC API authentication</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">RC Password</legend>
									<div className="relative">
										<input
											type={showRCPassword ? "text" : "password"}
											className="input pr-10"
											value={formData.rc_pass}
											disabled={isReadOnly}
											onChange={(e) => handleInputChange("rc_pass", e.target.value)}
											placeholder={
												config.rclone.rc_pass_set
													? "RC password is set (enter new to change)"
													: "admin"
											}
										/>
										<button
											type="button"
											className="-translate-y-1/2 btn btn-ghost btn-sm absolute top-1/2 right-2"
											onClick={() => setShowRCPassword(!showRCPassword)}
										>
											{showRCPassword ? (
												<EyeOff className="h-4 w-4" />
											) : (
												<Eye className="h-4 w-4" />
											)}
										</button>
									</div>
									<p className="label">
										Password for RClone RC API authentication
										{config.rclone.rc_pass_set && " (currently set)"}
									</p>
								</fieldset>
							</>
						)}

						{!isReadOnly && (
							<div className="flex gap-2">
								{!mountFormData.mount_enabled && (
									<>
										<button
											type="button"
											className="btn btn-outline"
											onClick={handleTestConnection}
											disabled={isTestingConnection || (!formData.rc_url && !formData.rc_enabled)}
										>
											{isTestingConnection ? (
												<span className="loading loading-spinner loading-sm" />
											) : (
												<TestTube className="h-4 w-4" />
											)}
											{isTestingConnection ? "Testing..." : "Test Connection"}
										</button>
										<button
											type="button"
											className="btn btn-primary"
											onClick={handleSave}
											disabled={!hasChanges || isUpdating}
										>
											{isUpdating ? (
												<span className="loading loading-spinner loading-sm" />
											) : (
												<Save className="h-4 w-4" />
											)}
											{isUpdating ? "Saving..." : "Save RC Changes"}
										</button>
									</>
								)}
							</div>
						)}
					</>
				)}
			</div>

			{/* Test Result Alert */}
			{testResult && (
				<div className={`alert ${testResult.success ? "alert-success" : "alert-error"} mt-4`}>
					<div>
						<span>{testResult.message}</span>
					</div>
				</div>
			)}
		</div>
	);
}
