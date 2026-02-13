import { AlertTriangle, HardDrive, Play, Save, Square, TestTube } from "lucide-react";
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
	const [mountPath, setMountPath] = useState(config.mount_path || "/mnt/altmount");

	const [mountStatus, setMountStatus] = useState<MountStatus | null>(null);
	const [hasChanges, setHasChanges] = useState(false);
	const [hasMountChanges, setHasMountChanges] = useState(false);
	const [hasMountPathChanges, setHasMountPathChanges] = useState(false);
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
		setMountPath(config.mount_path || "/mnt/altmount");
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
		setHasMountPathChanges(value !== (config.mount_path || "/mnt/altmount"));
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
		<div className="space-y-10">
			{/* Mount Configuration Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
						Automated Mount
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">Enable RClone Mount</legend>
						<label className="label cursor-pointer justify-start gap-4">
							<span className="label-text font-medium">
								Mount WebDAV locally
								{isMountToggleSaving && (
									<span className="loading loading-spinner loading-xs ml-2" />
								)}
							</span>
							<input
								type="checkbox"
								className="checkbox checkbox-primary"
								checked={mountFormData.mount_enabled}
								disabled={isReadOnly || isMountToggleSaving}
								onChange={(e) => handleMountEnabledChange(e.target.checked)}
							/>
						</label>
						<p className="label max-w-2xl whitespace-normal text-xs leading-relaxed opacity-70">
							Automatically manages a background RClone process to expose your Usenet files as a
							local filesystem.
						</p>
						{mountFormData.mount_enabled && hasMountChanges && (
							<div className="alert alert-warning mt-2 py-2 text-xs">
								<AlertTriangle className="h-4 w-4 shrink-0" />
								<span>Changes not saved. Click "Save Mount Settings" below to apply.</span>
							</div>
						)}
					</fieldset>

					{mountFormData.mount_enabled && (
						<div className="fade-in animate-in space-y-10 duration-500">
							{/* 1. Mount Path & Status */}
							<div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
								<div className="lg:col-span-2">
									<fieldset className="fieldset min-w-0">
										<legend className="fieldset-legend font-semibold">Mount Point</legend>
										<input
											type="text"
											className="input w-full bg-base-200/50"
											value={mountPath}
											disabled={isReadOnly}
											onChange={(e) => handleMountPathChange(e.target.value)}
											placeholder="/mnt/altmount"
										/>
										<p className="label text-[10px] opacity-60">
											Absolute path on the host system.
										</p>
									</fieldset>
								</div>

								<div className="flex flex-col justify-end lg:col-span-1">
									{mountStatus && (
										<div
											className={`alert ${mountStatus.mounted ? "alert-success" : "alert-warning"} flex-nowrap py-3 shadow-sm`}
										>
											<HardDrive className="h-5 w-5 shrink-0" />
											<div className="min-w-0 flex-1">
												<div className="font-bold text-xs">
													{mountStatus.mounted ? "System Mounted" : "Not Mounted"}
												</div>
												{mountStatus.mounted && (
													<div className="truncate font-mono text-[9px] opacity-70">
														{mountStatus.mount_point}
													</div>
												)}
											</div>
											{mountStatus.mounted ? (
												<button
													type="button"
													className="btn btn-xs btn-ghost"
													onClick={handleStopMount}
													disabled={isMountLoading}
												>
													{isMountLoading ? (
														<span className="loading loading-spinner loading-xs" />
													) : (
														<Square className="h-3 w-3 text-error" />
													)}
												</button>
											) : (
												<button
													type="button"
													className="btn btn-xs btn-ghost"
													onClick={handleStartMount}
													disabled={isMountLoading || !mountPath}
												>
													{isMountLoading ? (
														<span className="loading loading-spinner loading-xs" />
													) : (
														<Play className="h-3 w-3 text-success" />
													)}
												</button>
											)}
										</div>
									)}
								</div>
							</div>

							{/* 2. Basic Options */}
							<div className="space-y-4">
								<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
									Basic Options
								</h5>
								<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3">
									<div className="space-y-2">
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.allow_other}
												onChange={(e) => handleMountInputChange("allow_other", e.target.checked)}
											/>
											<span className="label-text text-xs">Allow other users</span>
										</label>
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.allow_non_empty}
												onChange={(e) =>
													handleMountInputChange("allow_non_empty", e.target.checked)
												}
											/>
											<span className="label-text text-xs">Allow non-empty mount</span>
										</label>
									</div>
									<div className="space-y-2">
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.read_only}
												onChange={(e) => handleMountInputChange("read_only", e.target.checked)}
											/>
											<span className="label-text text-xs">Read-only mount</span>
										</label>
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.syslog}
												onChange={(e) => handleMountInputChange("syslog", e.target.checked)}
											/>
											<span className="label-text text-xs">Enable syslog</span>
										</label>
									</div>
									<div className="space-y-2">
										<div className="flex items-center gap-2">
											<span className="font-bold text-[10px] uppercase opacity-50">Log Level</span>
											<select
												className="select select-bordered select-xs flex-1 font-mono"
												value={mountFormData.log_level}
												onChange={(e) => handleMountInputChange("log_level", e.target.value)}
											>
												<option value="DEBUG">DEBUG</option>
												<option value="INFO">INFO</option>
												<option value="WARN">WARN</option>
												<option value="ERROR">ERROR</option>
											</select>
										</div>
										<div className="flex items-center gap-2">
											<span className="font-bold text-[10px] uppercase opacity-50">Timeout</span>
											<input
												type="text"
												className="input input-bordered input-xs flex-1 font-mono"
												value={mountFormData.timeout}
												onChange={(e) => handleMountInputChange("timeout", e.target.value)}
											/>
										</div>
									</div>
								</div>
							</div>

							{/* 3. VFS Cache Settings */}
							<div className="space-y-4">
								<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
									VFS Cache Management
								</h5>
								<div className="grid grid-cols-1 gap-6 sm:grid-cols-2 md:grid-cols-4">
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">CACHE MODE</span>
										<select
											className="select select-bordered select-sm w-full"
											value={mountFormData.vfs_cache_mode}
											onChange={(e) => handleMountInputChange("vfs_cache_mode", e.target.value)}
										>
											<option value="off">Off</option>
											<option value="minimal">Minimal</option>
											<option value="writes">Writes</option>
											<option value="full">Full</option>
										</select>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">MAX SIZE</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.vfs_cache_max_size}
											onChange={(e) => handleMountInputChange("vfs_cache_max_size", e.target.value)}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">MAX AGE</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.vfs_cache_max_age}
											onChange={(e) => handleMountInputChange("vfs_cache_max_age", e.target.value)}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">MIN FREE SPACE</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.vfs_cache_min_free_space}
											onChange={(e) =>
												handleMountInputChange("vfs_cache_min_free_space", e.target.value)
											}
										/>
									</div>
									<div className="space-y-1 sm:col-span-2">
										<span className="block font-bold text-[10px] opacity-50">CACHE DIRECTORY</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.cache_dir}
											onChange={(e) => handleMountInputChange("cache_dir", e.target.value)}
											placeholder="/config/cache"
										/>
									</div>
									<div className="space-y-1 sm:col-span-2">
										<span className="block font-bold text-[10px] opacity-50">DISK SPACE TOTAL</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.vfs_disk_space_total}
											onChange={(e) =>
												handleMountInputChange("vfs_disk_space_total", e.target.value)
											}
											placeholder="1T"
										/>
									</div>
								</div>
							</div>

							{/* 4. Performance Settings */}
							<div className="space-y-4">
								<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
									Performance & Chunks
								</h5>
								<div className="grid grid-cols-1 gap-6 sm:grid-cols-2 md:grid-cols-4">
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">BUFFER SIZE</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.buffer_size}
											onChange={(e) => handleMountInputChange("buffer_size", e.target.value)}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">READ AHEAD</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.vfs_read_ahead}
											onChange={(e) => handleMountInputChange("vfs_read_ahead", e.target.value)}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">CHUNK SIZE</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.read_chunk_size}
											onChange={(e) => handleMountInputChange("read_chunk_size", e.target.value)}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">CHUNK LIMIT</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.read_chunk_size_limit}
											onChange={(e) =>
												handleMountInputChange("read_chunk_size_limit", e.target.value)
											}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">TRANSFERS</span>
										<input
											type="number"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.transfers}
											onChange={(e) =>
												handleMountInputChange(
													"transfers",
													Number.parseInt(e.target.value, 10) || 4,
												)
											}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">CHUNK STREAMS</span>
										<input
											type="number"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.vfs_read_chunk_streams}
											onChange={(e) =>
												handleMountInputChange(
													"vfs_read_chunk_streams",
													Number.parseInt(e.target.value, 10) || 4,
												)
											}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">ATTR TIMEOUT</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.attr_timeout}
											onChange={(e) => handleMountInputChange("attr_timeout", e.target.value)}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">DIR CACHE TIME</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.dir_cache_time}
											onChange={(e) => handleMountInputChange("dir_cache_time", e.target.value)}
										/>
									</div>
								</div>
							</div>

							{/* 5. Advanced & Identity */}
							<div className="space-y-4">
								<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
									System & Advanced
								</h5>
								<div className="grid grid-cols-1 gap-6 sm:grid-cols-2 md:grid-cols-4">
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">UID</span>
										<input
											type="number"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.uid}
											onChange={(e) =>
												handleMountInputChange("uid", Number.parseInt(e.target.value, 10) || 1000)
											}
										/>
									</div>
									<div className="space-y-1">
										<span className="block font-bold text-[10px] opacity-50">GID</span>
										<input
											type="number"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.gid}
											onChange={(e) =>
												handleMountInputChange("gid", Number.parseInt(e.target.value, 10) || 1000)
											}
										/>
									</div>
									<div className="space-y-1 sm:col-span-2">
										<span className="block font-bold text-[10px] uppercase opacity-50">Umask</span>
										<input
											type="text"
											className="input input-bordered input-sm w-full font-mono"
											value={mountFormData.umask}
											onChange={(e) => handleMountInputChange("umask", e.target.value)}
										/>
									</div>
									<div className="flex flex-wrap gap-x-6 gap-y-2 pt-2 sm:col-span-4">
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.async_read}
												onChange={(e) => handleMountInputChange("async_read", e.target.checked)}
											/>
											<span className="label-text text-xs">Async read</span>
										</label>
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.no_mod_time}
												onChange={(e) => handleMountInputChange("no_mod_time", e.target.checked)}
											/>
											<span className="label-text text-xs">No mod-time</span>
										</label>
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.no_checksum}
												onChange={(e) => handleMountInputChange("no_checksum", e.target.checked)}
											/>
											<span className="label-text text-xs">No checksum</span>
										</label>
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.vfs_fast_fingerprint}
												onChange={(e) =>
													handleMountInputChange("vfs_fast_fingerprint", e.target.checked)
												}
											/>
											<span className="label-text text-xs">Fast Fingerprint</span>
										</label>
										<label className="label cursor-pointer justify-start gap-3 py-0">
											<input
												type="checkbox"
												className="checkbox checkbox-xs"
												checked={mountFormData.use_mmap}
												onChange={(e) => handleMountInputChange("use_mmap", e.target.checked)}
											/>
											<span className="label-text text-xs">Use Mmap</span>
										</label>
									</div>
									<div className="space-y-1 sm:col-span-4">
										<span className="block font-bold text-[10px] uppercase tracking-widest opacity-50">
											Custom Mount Options (JSON)
										</span>
										<textarea
											className="textarea textarea-bordered textarea-sm min-h-[100px] w-full font-mono text-[10px]"
											value={JSON.stringify(mountFormData.mount_options, null, 2)}
											onChange={(e) => {
												try {
													const parsed = JSON.parse(e.target.value);
													handleMountInputChange("mount_options", parsed);
												} catch (_err) {
													// Allow typing
												}
											}}
											placeholder="{}"
										/>
									</div>
								</div>
							</div>

							{/* Action Buttons */}
							{!isReadOnly && (
								<div className="mt-4 flex justify-end border-base-200 border-t pt-6">
									<button
										type="button"
										className={`btn btn-primary btn-sm px-8 ${hasMountChanges || hasMountPathChanges ? "shadow-md" : ""}`}
										onClick={handleSaveMount}
										disabled={
											(!hasMountChanges && !hasMountPathChanges) || isUpdating || isMountLoading
										}
									>
										{isUpdating || isMountLoading ? (
											<span className="loading loading-spinner loading-xs" />
										) : (
											<Save className="h-3 w-3" />
										)}
										Save Mount Configuration
									</button>
								</div>
							)}
						</div>
					)}
				</div>
			</section>

			{/* RC Configuration Settings */}
			<section className="space-y-6 pt-4">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
						Remote Control API
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">RC Connection</legend>
						<label className="label cursor-pointer justify-start gap-4">
							<span className="label-text font-medium">
								Enable Remote Control
								{mountFormData.mount_enabled && (
									<span className="badge badge-info badge-outline badge-xs ml-2">Auto-managed</span>
								)}
							</span>
							<input
								type="checkbox"
								className="checkbox checkbox-primary"
								checked={mountFormData.mount_enabled || formData.rc_enabled}
								disabled={isReadOnly || mountFormData.mount_enabled || isRCToggleSaving}
								onChange={(e) => handleRCEnabledChange(e.target.checked)}
							/>
						</label>
						<p className="label max-w-2xl whitespace-normal text-xs leading-relaxed opacity-70">
							Required for cache refresh notifications. If Mount is enabled, this is managed
							automatically.
						</p>
					</fieldset>

					{(formData.rc_enabled || mountFormData.mount_enabled) && (
						<div className="space-y-6 rounded-2xl border border-base-300 bg-base-200/30 p-6">
							<div className="grid grid-cols-1 gap-6 sm:grid-cols-2 md:grid-cols-4">
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-bold text-[10px]">RC PORT</legend>
									<input
										type="number"
										className="input input-bordered input-sm w-full font-mono"
										value={formData.rc_port}
										disabled={isReadOnly || mountFormData.mount_enabled}
										onChange={(e) =>
											handleInputChange("rc_port", Number.parseInt(e.target.value, 10) || 5572)
										}
									/>
								</fieldset>
								<fieldset className="fieldset min-w-0 sm:col-span-2">
									<legend className="fieldset-legend font-bold text-[10px]">VFS NAME</legend>
									<input
										type="text"
										className="input input-bordered input-sm w-full font-mono"
										value={formData.vfs_name}
										disabled={isReadOnly}
										onChange={(e) => handleInputChange("vfs_name", e.target.value)}
									/>
								</fieldset>
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-bold text-[10px]">RC URL</legend>
									<input
										type="text"
										className="input input-bordered input-sm w-full font-mono"
										value={formData.rc_url}
										disabled={isReadOnly || mountFormData.mount_enabled}
										onChange={(e) => handleInputChange("rc_url", e.target.value)}
										placeholder="localhost"
									/>
								</fieldset>
							</div>

							<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-bold text-[10px]">RC USER</legend>
									<input
										type="text"
										className="input input-bordered input-sm w-full font-mono"
										value={formData.rc_user}
										disabled={isReadOnly || mountFormData.mount_enabled}
										onChange={(e) => handleInputChange("rc_user", e.target.value)}
										autoComplete="username"
									/>
								</fieldset>
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-bold text-[10px]">
										RC PASSWORD{" "}
										{config.rclone.rc_pass_set && (
											<span className="badge badge-success badge-xs ml-1 origin-left scale-75 font-normal uppercase">
												Set
											</span>
										)}
									</legend>
									<input
										type="password"
										className="input input-bordered input-sm w-full font-mono"
										value={formData.rc_pass}
										disabled={isReadOnly || mountFormData.mount_enabled}
										onChange={(e) => handleInputChange("rc_pass", e.target.value)}
										placeholder="••••••••"
										autoComplete="new-password"
									/>
								</fieldset>
							</div>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-bold text-[10px] uppercase tracking-widest opacity-50">
									Custom RC Options (JSON)
								</legend>
								<textarea
									className="textarea textarea-bordered textarea-sm min-h-[80px] w-full font-mono text-[10px]"
									value={JSON.stringify(formData.rc_options, null, 2)}
									disabled={isReadOnly || mountFormData.mount_enabled}
									onChange={(e) => {
										try {
											const parsed = JSON.parse(e.target.value);
											handleInputChange("rc_options", parsed);
										} catch (_err) {
											// Allow typing, but only update if valid JSON
										}
									}}
									placeholder="{}"
								/>
							</fieldset>
						</div>
					)}

					{!isReadOnly && !mountFormData.mount_enabled && formData.rc_enabled && (
						<div className="flex justify-end gap-2 pt-2">
							<button
								type="button"
								className="btn btn-outline btn-sm"
								onClick={handleTestConnection}
								disabled={isTestingConnection}
							>
								{isTestingConnection ? (
									<span className="loading loading-spinner loading-xs" />
								) : (
									<TestTube className="h-3 w-3" />
								)}
								Test Connection
							</button>
							<button
								type="button"
								className="btn btn-primary btn-sm px-8"
								onClick={handleSave}
								disabled={!hasChanges || isUpdating}
							>
								{isUpdating ? (
									<span className="loading loading-spinner loading-xs" />
								) : (
									<Save className="h-3 w-3" />
								)}
								Save RC Changes
							</button>
						</div>
					)}
				</div>
			</section>

			{testResult && (
				<div
					className={`alert ${testResult.success ? "alert-success" : "alert-error"} py-3 text-xs shadow-sm`}
				>
					<span>{testResult.message}</span>
				</div>
			)}
		</div>
	);
}
