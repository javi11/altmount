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
import { KeyValueEditor } from "../ui/KeyValueEditor";

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
		vfs_cache_poll_interval: config.rclone.vfs_cache_poll_interval || "1m",
		vfs_read_chunk_size: config.rclone.vfs_read_chunk_size || "32M",
		vfs_read_chunk_size_limit: config.rclone.vfs_read_chunk_size_limit || "2G",
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
		links: config.rclone.links || false,
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
			vfs_cache_poll_interval: config.rclone.vfs_cache_poll_interval || "1m",
			vfs_read_chunk_size: config.rclone.vfs_read_chunk_size || "32M",
			vfs_read_chunk_size_limit: config.rclone.vfs_read_chunk_size_limit || "2G",
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
			links: config.rclone.links || false,
		};
		setMountFormData(newMountFormData);
		setHasMountChanges(false);

		setMountPath(config.mount_path || "/mnt/remotes/altmount");
		setHasMountPathChanges(false);
	}, [config]);

	const fetchMountStatus = useCallback(async () => {
		try {
			const response = await fetch("/api/mount/status");
			if (response.ok) {
				const data = await response.json();
				setMountStatus(data.data);
			}
		} catch (error) {
			console.error("Failed to fetch mount status:", error);
		}
	}, []);

	useEffect(() => {
		fetchMountStatus();
		const interval = setInterval(fetchMountStatus, 5000);
		return () => clearInterval(interval);
	}, [fetchMountStatus]);

	const handleInputChange = (
		field: keyof RCloneRCFormData,
		value: string | number | boolean | Record<string, string>,
	) => {
		const newFormData = { ...formData, [field]: value };
		setFormData(newFormData);

		// Compare with initial config to see if there are changes
		const initialFormData = {
			rc_enabled: config.rclone.rc_enabled,
			rc_url: config.rclone.rc_url,
			vfs_name: config.rclone.vfs_name || "altmount",
			rc_port: config.rclone.rc_port,
			rc_user: config.rclone.rc_user,
			rc_pass: "",
			rc_options: config.rclone.rc_options,
		};
		setHasChanges(JSON.stringify(newFormData) !== JSON.stringify(initialFormData));
	};

	const handleMountInputChange = (
		field: keyof RCloneMountFormData,
		value: string | number | boolean | Record<string, string>,
	) => {
		const newMountFormData = { ...mountFormData, [field]: value };
		setMountFormData(newMountFormData);

		// Compare with initial config to see if there are changes
		const initialMountFormData = {
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
			vfs_cache_poll_interval: config.rclone.vfs_cache_poll_interval || "1m",
			vfs_read_chunk_size: config.rclone.vfs_read_chunk_size || "32M",
			vfs_read_chunk_size_limit: config.rclone.vfs_read_chunk_size_limit || "2G",
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
			links: config.rclone.links || false,
		};
		setHasMountChanges(JSON.stringify(newMountFormData) !== JSON.stringify(initialMountFormData));
	};

	const handleMountPathChange = (value: string) => {
		setMountPath(value);
		setHasMountPathChanges(value !== config.mount_path);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			const saveDelta: RCloneRCFormData = { ...formData };
			// Don't send empty password if it hasn't changed
			if (saveDelta.rc_pass === "") {
				delete (saveDelta as any).rc_pass;
			}
			await onUpdate("rclone", saveDelta as any);
			setHasChanges(false);
		}
	};

	const handleSaveMount = async () => {
		if (onUpdate && (hasMountChanges || hasMountPathChanges)) {
			// We need to send both together if they changed
			await onUpdate("rclone", {
				rclone: mountFormData,
				mount_path: mountPath,
			} as any);
			setHasMountChanges(false);
			setHasMountPathChanges(false);
		}
	};

	const handleRCEnabledChange = async (enabled: boolean) => {
		if (onUpdate) {
			setIsRCToggleSaving(true);
			try {
				await onUpdate("rclone", { rc_enabled: enabled } as any);
			} finally {
				setIsRCToggleSaving(false);
			}
		}
	};

	const handleMountEnabledChange = async (enabled: boolean) => {
		if (onUpdate) {
			setIsMountToggleSaving(true);
			try {
				await onUpdate("rclone", { mount_enabled: enabled } as any);
			} finally {
				setIsMountToggleSaving(false);
			}
		}
	};

	const handleTestConnection = async () => {
		setIsTestingConnection(true);
		setTestResult(null);
		try {
			const response = await fetch("/api/mount/test-rc", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(formData),
			});
			const data = await response.json();
			setTestResult({
				success: data.success,
				message: data.success
					? "Connection successful!"
					: data.error?.message || "Connection failed",
			});
		} catch (_error) {
			setTestResult({
				success: false,
				message: "Failed to connect to RC server",
			});
		} finally {
			setIsTestingConnection(false);
		}
	};

	const handleStartMount = async () => {
		const confirmed = await confirmAction(
			"Start RClone Mount",
			`This will attempt to mount the WebDAV filesystem at ${mountPath}. Continue?`,
		);
		if (!confirmed) return;

		setIsMountLoading(true);
		try {
			const response = await fetch("/api/mount/start", { method: "POST" });
			if (response.ok) {
				showToast({
					type: "success",
					title: "Mount Started",
					message: "RClone mount initiated successfully",
				});
				fetchMountStatus();
			} else {
				const errorData = await response.json();
				showToast({
					type: "error",
					title: "Mount Failed",
					message: errorData.error?.message || "Failed to start mount",
				});
			}
		} catch (_error) {
			showToast({
				type: "error",
				title: "Error",
				message: "Failed to communicate with API",
			});
		} finally {
			setIsMountLoading(false);
		}
	};

	const handleStopMount = async () => {
		const confirmed = await confirmAction(
			"Stop RClone Mount",
			"This will unmount the WebDAV filesystem. Any applications accessing it may experience errors. Continue?",
			{ type: "warning", confirmText: "Stop Mount" },
		);
		if (!confirmed) return;

		setIsMountLoading(true);
		try {
			const response = await fetch("/api/mount/stop", { method: "POST" });
			if (response.ok) {
				showToast({
					type: "info",
					title: "Mount Stopped",
					message: "RClone mount stopped successfully",
				});
				fetchMountStatus();
			} else {
				const errorData = await response.json();
				showToast({
					type: "error",
					title: "Stop Failed",
					message: errorData.error?.message || "Failed to stop mount",
				});
			}
		} catch (_error) {
			showToast({
				type: "error",
				title: "Error",
				message: "Failed to communicate with API",
			});
		} finally {
			setIsMountLoading(false);
		}
	};

	return (
		<div className="space-y-10">
			<div className="min-w-0">
				<h3 className="font-bold text-base-content text-lg tracking-tight">RClone Filesystem</h3>
				<p className="break-words text-base-content/50 text-sm">
					Manage the virtual mount and Remote Control (RC) interface.
				</p>
			</div>

			<div className="space-y-8">
				{/* Mount Configuration Section */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<HardDrive className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Mount Configuration
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Enable Internal Mount</legend>
						<label className="label cursor-pointer">
							<span className="label-text">
								Let AltMount manage and mount the virtual filesystem automatically
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
						<p className="label">
							{isMountToggleSaving
								? "Saving..."
								: "Highly recommended for all-in-one Docker setups"}
						</p>
					</fieldset>

					{mountFormData.mount_enabled && (
						<>
							<div className="divider opacity-50" />

							<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Mount Point Path</legend>
									<input
										type="text"
										className="input"
										value={mountPath}
										disabled={isReadOnly}
										onChange={(e) => handleMountPathChange(e.target.value)}
										placeholder="/mnt/remotes/altmount"
									/>
									<p className="label text-xs">
										Absolute path where the filesystem will be mounted.
									</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Mount Log Level</legend>
									<select
										className="select"
										value={mountFormData.log_level}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("log_level", e.target.value)}
									>
										<option value="DEBUG">DEBUG (Verbose)</option>
										<option value="INFO">INFO (Standard)</option>
										<option value="NOTICE">NOTICE (Alerts)</option>
										<option value="ERROR">ERROR (Critical)</option>
									</select>
									<p className="label text-xs">Verbosity of RClone mount logs.</p>
								</fieldset>
							</div>

							<div className="grid grid-cols-1 gap-6 md:grid-cols-3">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">UID</legend>
									<input
										type="number"
										className="input"
										value={mountFormData.uid}
										disabled={isReadOnly}
										onChange={(e) =>
											handleMountInputChange("uid", Number.parseInt(e.target.value, 10) || 1000)
										}
										placeholder="1000"
									/>
									<p className="label text-xs">User ID for files.</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">GID</legend>
									<input
										type="number"
										className="input"
										value={mountFormData.gid}
										disabled={isReadOnly}
										onChange={(e) =>
											handleMountInputChange("gid", Number.parseInt(e.target.value, 10) || 1000)
										}
										placeholder="1000"
									/>
									<p className="label text-xs">Group ID for files.</p>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">Umask</legend>
									<input
										type="text"
										className="input"
										value={mountFormData.umask}
										disabled={isReadOnly}
										onChange={(e) => handleMountInputChange("umask", e.target.value)}
										placeholder="002"
									/>
									<p className="label text-xs">File permission mask.</p>
								</fieldset>
							</div>

							<div className="space-y-4">
								<h5 className="font-medium text-base-content/70 text-sm">Security & Flags</h5>
								<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Allow Other</legend>
										<label className="label cursor-pointer">
											<span className="label-text">Enable shared access</span>
											<input
												type="checkbox"
												className="checkbox"
												checked={mountFormData.allow_other}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("allow_other", e.target.checked)}
											/>
										</label>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Allow Non-Empty</legend>
										<label className="label cursor-pointer">
											<span className="label-text">Mount over files</span>
											<input
												type="checkbox"
												className="checkbox"
												checked={mountFormData.allow_non_empty}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange("allow_non_empty", e.target.checked)
												}
											/>
										</label>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Read Only</legend>
										<label className="label cursor-pointer">
											<span className="label-text">Disable writing</span>
											<input
												type="checkbox"
												className="checkbox"
												checked={mountFormData.read_only}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("read_only", e.target.checked)}
											/>
										</label>
									</fieldset>
								</div>
							</div>

							<div className="space-y-4">
								<h5 className="font-medium text-base-content/70 text-sm">VFS Cache Settings</h5>
								<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Cache Mode</legend>
										<select
											className="select"
											value={mountFormData.vfs_cache_mode}
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("vfs_cache_mode", e.target.value)}
										>
											<option value="off">off (No cache)</option>
											<option value="minimal">minimal (Metadata only)</option>
											<option value="writes">writes (Only modified files)</option>
											<option value="full">full (Read & Write cache)</option>
										</select>
										<p className="label text-xs">Determines how much data RClone caches locally.</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Cache Directory</legend>
										<input
											type="text"
											className="input"
											value={mountFormData.cache_dir}
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("cache_dir", e.target.value)}
											placeholder="/config/cache"
										/>
										<p className="label text-xs">
											Path for cached data (defaults to config/cache).
										</p>
									</fieldset>
								</div>

								<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Max Cache Size</legend>
										<input
											type="text"
											className="input"
											value={mountFormData.vfs_cache_max_size}
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("vfs_cache_max_size", e.target.value)}
											placeholder="50G"
										/>
										<p className="label text-xs">Maximum cache size (e.g., 50G, 1T).</p>
									</fieldset>

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
										<p className="label text-xs">Maximum cache age (e.g., 504h, 7d).</p>
									</fieldset>
								</div>

								<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Cache Poll Interval</legend>
										<input
											type="text"
											className="input"
											value={mountFormData.vfs_cache_poll_interval}
											disabled={isReadOnly}
											onChange={(e) =>
												handleMountInputChange("vfs_cache_poll_interval", e.target.value)
											}
											placeholder="1m"
										/>
										<p className="label text-xs">
											Interval to poll for remote changes (e.g., 1m, 5s).
										</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Read Ahead</legend>
										<input
											type="text"
											className="input"
											value={mountFormData.vfs_read_ahead}
											disabled={isReadOnly}
											onChange={(e) => handleMountInputChange("vfs_read_ahead", e.target.value)}
											placeholder="128M"
										/>
										<p className="label text-xs">Read ahead size (e.g., 128M, 256M).</p>
									</fieldset>
								</div>
							</div>

							<div className="space-y-4">
								<h5 className="font-medium text-base-content/70 text-sm">Performance Settings</h5>
								<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Read Chunk Size</legend>
										<input
											type="text"
											className="input"
											value={mountFormData.vfs_read_chunk_size}
											disabled={isReadOnly}
											onChange={(e) =>
												handleMountInputChange("vfs_read_chunk_size", e.target.value)
											}
											placeholder="32M"
										/>
										<p className="label text-xs">Initial read chunk size (e.g., 32M, 64M).</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Read Chunk Size Limit</legend>
										<input
											type="text"
											className="input"
											value={mountFormData.vfs_read_chunk_size_limit}
											disabled={isReadOnly}
											onChange={(e) =>
												handleMountInputChange("vfs_read_chunk_size_limit", e.target.value)
											}
											placeholder="2G"
										/>
										<p className="label text-xs">Maximum read chunk size (e.g., 2G, 4G).</p>
									</fieldset>
								</div>

								<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
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
										<p className="label text-xs">Directory cache time (e.g., 10m, 1h).</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Transfers</legend>
										<input
											type="number"
											className="input"
											value={mountFormData.transfers}
											disabled={isReadOnly}
											onChange={(e) =>
												handleMountInputChange(
													"transfers",
													Number.parseInt(e.target.value, 10) || 4,
												)
											}
											min="1"
											max="32"
										/>
										<p className="label text-xs">Number of parallel transfers (1-32).</p>
									</fieldset>
								</div>
							</div>

							<div className="space-y-4">
								<h5 className="font-medium text-base-content/70 text-sm">Advanced Flags</h5>
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
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">No Mod Time</legend>
										<label className="label cursor-pointer">
											<span className="label-text">Don't write mod time</span>
											<input
												type="checkbox"
												className="checkbox"
												checked={mountFormData.no_mod_time}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("no_mod_time", e.target.checked)}
											/>
										</label>
									</fieldset>
								</div>
							</div>

							{/* Custom Mount Options */}
							<div className="space-y-4">
								<h5 className="font-medium text-base-content/70 text-sm">Custom Mount Options</h5>
								<p className="text-[11px] text-base-content/50">
									Arbitrary flags to pass to the rclone mount command. (e.g.,{" "}
									<code>no-modtime: true</code>)
								</p>
								<KeyValueEditor
									value={mountFormData.mount_options}
									disabled={isReadOnly}
									onChange={(val) => handleMountInputChange("mount_options", val)}
									keyPlaceholder="Flag (e.g. no-modtime)"
									valuePlaceholder="Value (e.g. true)"
								/>
							</div>

							{/* Mount Status & Actions */}
							<div className="divider opacity-50" />

							{mountStatus && (
								<div className={`alert ${mountStatus.mounted ? "alert-success" : "alert-warning"}`}>
									<HardDrive className="h-6 w-6" />
									<div>
										<div className="font-bold">
											{mountStatus.mounted ? "Mounted" : "Not Mounted"}
										</div>
										{mountStatus.mounted && mountStatus.mount_point && (
											<div className="text-sm">Mount point: {mountStatus.mount_point}</div>
										)}
										{mountStatus.error && <div className="text-sm">{mountStatus.error}</div>}
									</div>
									<div className="flex gap-2">
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
												Stop Mount
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
												Start Mount
											</button>
										)}
									</div>
								</div>
							)}

							{!isReadOnly && (
								<div className="flex justify-end pt-4">
									<button
										type="button"
										className={`btn btn-primary px-10 ${hasMountChanges || hasMountPathChanges ? "shadow-lg shadow-primary/20" : "btn-ghost"}`}
										onClick={handleSaveMount}
										disabled={
											(!hasMountChanges && !hasMountPathChanges) || isUpdating || isMountLoading
										}
									>
										{isUpdating ? (
											<span className="loading loading-spinner loading-sm" />
										) : (
											<Save className="h-4 w-4" />
										)}
										Save Mount Changes
									</button>
								</div>
							)}
						</>
					)}
				</div>

				{/* RC Configuration Section */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<TestTube className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Remote Control (RC)
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Enable RC Connection</legend>
						<label className="label cursor-pointer">
							<span className="label-text">
								Enable connection for cache refresh notifications
								{mountFormData.mount_enabled && (
									<span className="badge badge-info badge-sm ml-2">Managed by mount</span>
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
					</fieldset>

					{(formData.rc_enabled || mountFormData.mount_enabled) && (
						<>
							<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
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
												: "http://localhost:5572"
										}
									/>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">RC Port</legend>
									<input
										type="number"
										className="input"
										value={formData.rc_port}
										disabled={isReadOnly || mountFormData.mount_enabled}
										onChange={(e) =>
											handleInputChange("rc_port", Number.parseInt(e.target.value, 10) || 5572)
										}
									/>
								</fieldset>
							</div>

							<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend">RC Username</legend>
									<input
										type="text"
										className="input"
										value={formData.rc_user}
										disabled={isReadOnly || mountFormData.mount_enabled}
										onChange={(e) => handleInputChange("rc_user", e.target.value)}
									/>
								</fieldset>

								<fieldset className="fieldset">
									<legend className="fieldset-legend">RC Password</legend>
									<div className="relative">
										<input
											type={showRCPassword ? "text" : "password"}
											className="input pr-10"
											value={formData.rc_pass}
											disabled={isReadOnly || mountFormData.mount_enabled}
											onChange={(e) => handleInputChange("rc_pass", e.target.value)}
											placeholder={config.rclone.rc_pass_set ? "********" : "admin"}
										/>
										<button
											type="button"
											className="btn btn-ghost btn-xs -translate-y-1/2 absolute top-1/2 right-2"
											onClick={() => setShowRCPassword(!showRCPassword)}
										>
											{showRCPassword ? (
												<EyeOff className="h-4 w-4" />
											) : (
												<Eye className="h-4 w-4" />
											)}
										</button>
									</div>
								</fieldset>
							</div>

							{/* Custom RC Options */}
							<div className="space-y-4">
								<h5 className="font-medium text-base-content/70 text-sm">Custom RC Options</h5>
								<KeyValueEditor
									value={formData.rc_options || {}}
									disabled={isReadOnly || mountFormData.mount_enabled}
									onChange={(val) => handleInputChange("rc_options", val)}
									keyPlaceholder="Option (e.g. rc-web-gui)"
									valuePlaceholder="Value (e.g. true)"
								/>
							</div>

							{!isReadOnly && !mountFormData.mount_enabled && (
								<div className="flex justify-end gap-3 pt-4">
									<button
										type="button"
										className="btn btn-outline"
										onClick={handleTestConnection}
										disabled={isTestingConnection}
									>
										{isTestingConnection && <span className="loading loading-spinner loading-xs" />}
										Test Connection
									</button>
									<button
										type="button"
										className={`btn btn-primary px-10 ${hasChanges ? "shadow-lg shadow-primary/20" : "btn-ghost"}`}
										onClick={handleSave}
										disabled={!hasChanges || isUpdating}
									>
										{isUpdating && <span className="loading loading-spinner loading-sm" />}
										Save RC Changes
									</button>
								</div>
							)}
						</>
					)}
				</div>
			</div>

			{/* Test Result Alert */}
			{testResult && (
				<div
					className={`alert ${testResult.success ? "alert-success" : "alert-error"} fade-in slide-in-from-top-4 animate-in`}
				>
					<span>{testResult.message}</span>
				</div>
			)}
		</div>
	);
}
