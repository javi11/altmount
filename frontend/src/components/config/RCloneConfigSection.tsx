import { Eye, EyeOff, HardDrive, Play, Save, Square, TestTube } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import type {
	ConfigResponse,
	MountStatus,
	RCloneMountFormData,
	RCloneVFSFormData,
} from "../../types/config";

interface RCloneConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (
		section: string,
		data: RCloneVFSFormData | RCloneMountFormData | { mount_path: string },
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
	const [formData, setFormData] = useState<RCloneVFSFormData>({
		vfs_enabled: config.rclone.vfs_enabled,
		vfs_url: config.rclone.vfs_url,
		vfs_user: config.rclone.vfs_user,
		vfs_pass: "",
	});

	const [mountFormData, setMountFormData] = useState<RCloneMountFormData>({
		mount_enabled: config.rclone.mount_enabled || false,
		mount_options: config.rclone.mount_options || {},
		// Mount Configuration
		rc_port: config.rclone.rc_port || 5572,
		log_level: config.rclone.log_level || "INFO",
		uid: config.rclone.uid || 1000,
		gid: config.rclone.gid || 1000,
		umask: config.rclone.umask || "0022",
		buffer_size: config.rclone.buffer_size || "10M",
		attr_timeout: config.rclone.attr_timeout || "1s",
		transfers: config.rclone.transfers || 4,
		// VFS Cache Settings
		cache_dir: config.rclone.cache_dir || "/mnt/Data/CloneCache",
		vfs_cache_mode: config.rclone.vfs_cache_mode || "full",
		vfs_cache_max_size: config.rclone.vfs_cache_max_size || "100G",
		vfs_cache_max_age: config.rclone.vfs_cache_max_age || "100h",
		read_chunk_size: config.rclone.read_chunk_size || "128M",
		read_chunk_size_limit: config.rclone.read_chunk_size_limit || "off",
		vfs_read_ahead: config.rclone.vfs_read_ahead || "128k",
		dir_cache_time: config.rclone.dir_cache_time || "5m",
		vfs_cache_poll_interval: config.rclone.vfs_cache_poll_interval || "1m",
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
	const [showVFSPassword, setShowVFSPassword] = useState(false);
	const [isTestingConnection, setIsTestingConnection] = useState(false);
	const [isTestingMount, setIsTestingMount] = useState(false);
	const [testResult, setTestResult] = useState<{
		success: boolean;
		message: string;
	} | null>(null);

	// Collapsible sections state
	const [showMountConfig, setShowMountConfig] = useState(true);
	const [showVFSCache, setShowVFSCache] = useState(false);
	const [showAdvanced, setShowAdvanced] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = {
			vfs_enabled: config.rclone.vfs_enabled,
			vfs_url: config.rclone.vfs_url,
			vfs_user: config.rclone.vfs_user,
			vfs_pass: "",
		};
		setFormData(newFormData);
		setHasChanges(false);

		const newMountFormData = {
			mount_enabled: config.rclone.mount_enabled || false,
			mount_options: config.rclone.mount_options || {},
			// Mount Configuration
			rc_port: config.rclone.rc_port || 5572,
			log_level: config.rclone.log_level || "INFO",
			uid: config.rclone.uid || 1000,
			gid: config.rclone.gid || 1000,
			umask: config.rclone.umask || "0022",
			buffer_size: config.rclone.buffer_size || "10M",
			attr_timeout: config.rclone.attr_timeout || "1s",
			transfers: config.rclone.transfers || 4,
			// VFS Cache Settings
			cache_dir: config.rclone.cache_dir || "/mnt/Data/CloneCache",
			vfs_cache_mode: config.rclone.vfs_cache_mode || "full",
			vfs_cache_max_size: config.rclone.vfs_cache_max_size || "100G",
			vfs_cache_max_age: config.rclone.vfs_cache_max_age || "100h",
			read_chunk_size: config.rclone.read_chunk_size || "128M",
			read_chunk_size_limit: config.rclone.read_chunk_size_limit || "off",
			vfs_read_ahead: config.rclone.vfs_read_ahead || "128k",
			dir_cache_time: config.rclone.dir_cache_time || "5m",
			vfs_cache_poll_interval: config.rclone.vfs_cache_poll_interval || "1m",
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

	const handleInputChange = (field: keyof RCloneVFSFormData, value: string | boolean) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);

		// Check for changes by comparing against original config
		const configData = {
			vfs_enabled: config.rclone.vfs_enabled,
			vfs_url: config.rclone.vfs_url,
			vfs_user: config.rclone.vfs_user,
			vfs_pass: "",
		};

		// Always consider changes if VFS password is entered
		const vfsPasswordChanged = newData.vfs_pass !== "";
		const otherFieldsChanged =
			newData.vfs_enabled !== configData.vfs_enabled ||
			newData.vfs_url !== configData.vfs_url ||
			newData.vfs_user !== configData.vfs_user;

		setHasChanges(vfsPasswordChanged || otherFieldsChanged);
	};

	const handleMountInputChange = (
		field: keyof RCloneMountFormData,
		value: string | boolean | number | Record<string, string>,
	) => {
		const newData = { ...mountFormData, [field]: value };
		setMountFormData(newData);

		// Always mark as changed when any field is modified
		setHasMountChanges(true);
	};

	const handleMountPathChange = (value: string) => {
		setMountPath(value);
		setHasMountPathChanges(value !== (config.mount_path || "/mnt/altmount"));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			// Only send non-empty values for VFS password
			const updateData: RCloneVFSFormData = {
				vfs_enabled: formData.vfs_enabled ?? false,
				vfs_url: formData.vfs_url || "",
				vfs_user: formData.vfs_user || "",
				vfs_pass: formData.vfs_pass.trim() !== "" ? formData.vfs_pass : "",
			};

			await onUpdate("rclone", updateData);
			setHasChanges(false);
		}
	};

	const handleSaveMount = async () => {
		if (onUpdate) {
			// Save mount configuration changes
			if (hasMountChanges) {
				await onUpdate("rclone", mountFormData);
				setHasMountChanges(false);
			}

			// Save mount path changes
			if (hasMountPathChanges) {
				await onUpdate("mount_path", { mount_path: mountPath });
				setHasMountPathChanges(false);
			}

			// Refresh mount status after saving
			if (mountFormData.mount_enabled) {
				fetchMountStatus();
			}
		}
	};

	const handleStartMount = async () => {
		try {
			const response = await fetch("/api/rclone/mount/start", {
				method: "POST",
			});
			const result = await response.json();
			if (result.success) {
				setMountStatus(result.data);
			} else {
				alert(`Failed to start mount: ${result.message}`);
			}
		} catch (error) {
			alert(`Error starting mount: ${error}`);
		}
	};

	const handleStopMount = async () => {
		try {
			const response = await fetch("/api/rclone/mount/stop", {
				method: "POST",
			});
			const result = await response.json();
			if (result.success) {
				setMountStatus({ mounted: false, mount_point: "" });
			} else {
				alert(`Failed to stop mount: ${result.message}`);
			}
		} catch (error) {
			alert(`Error stopping mount: ${error}`);
		}
	};

	const handleTestMount = async () => {
		setIsTestingMount(true);
		setTestResult(null);

		try {
			const response = await fetch("/api/rclone/mount/test", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
				},
				body: JSON.stringify({
					mount_point: mountPath,
					mount_options: mountFormData.mount_options,
				}),
			});

			const result = await response.json();
			setTestResult({
				success: result.success,
				message:
					result.message || (result.success ? "Mount configuration is valid" : "Mount test failed"),
			});
		} catch (error) {
			setTestResult({
				success: false,
				message: error instanceof Error ? error.message : "Network error occurred",
			});
		} finally {
			setIsTestingMount(false);
		}
	};

	const handleTestConnection = async () => {
		setIsTestingConnection(true);
		setTestResult(null);

		try {
			const response = await fetch("/api/config/rclone/test", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
				},
				body: JSON.stringify({
					vfs_enabled: formData.vfs_enabled,
					vfs_url: formData.vfs_url,
					vfs_user: formData.vfs_user,
					vfs_pass: formData.vfs_pass,
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

			{/* VFS Notification Settings */}
			<div className="space-y-4">
				<h4 className="font-medium text-base">VFS Notification Settings</h4>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Enable VFS Notifications</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Enable RClone VFS cache refresh notifications</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.vfs_enabled}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("vfs_enabled", e.target.checked)}
						/>
					</label>
					<p className="label">
						Automatically notify RClone VFS when new files are imported to refresh the cache
					</p>
				</fieldset>

				{formData.vfs_enabled && (
					<>
						<fieldset className="fieldset">
							<legend className="fieldset-legend">VFS URL</legend>
							<input
								type="text"
								className="input"
								value={formData.vfs_url}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("vfs_url", e.target.value)}
								placeholder="http://localhost:5572"
							/>
							<p className="label">RClone RC API URL (e.g., http://localhost:5572)</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">VFS Username</legend>
							<input
								type="text"
								className="input"
								value={formData.vfs_user}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("vfs_user", e.target.value)}
								placeholder="Enter VFS username (optional)"
							/>
							<p className="label">Username for RClone RC API authentication (optional)</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">VFS Password</legend>
							<div className="relative">
								<input
									type={showVFSPassword ? "text" : "password"}
									className="input pr-10"
									value={formData.vfs_pass}
									disabled={isReadOnly}
									onChange={(e) => handleInputChange("vfs_pass", e.target.value)}
									placeholder={
										config.rclone.vfs_pass_set
											? "VFS password is set (enter new to change)"
											: "Enter VFS password (optional)"
									}
								/>
								<button
									type="button"
									className="-translate-y-1/2 btn btn-ghost btn-xs absolute top-1/2 right-2"
									onClick={() => setShowVFSPassword(!showVFSPassword)}
								>
									{showVFSPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
								</button>
							</div>
							<p className="label">
								Password for RClone RC API authentication (optional)
								{config.rclone.vfs_pass_set && " (currently set)"}
							</p>
						</fieldset>

						{!isReadOnly && (
							<div className="flex gap-2">
								<button
									type="button"
									className="btn btn-outline"
									onClick={handleTestConnection}
									disabled={isTestingConnection || !formData.vfs_url}
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
									{isUpdating ? "Saving..." : "Save VFS Changes"}
								</button>
							</div>
						)}
					</>
				)}
			</div>

			{/* Mount Enable Section */}
			<div className="mt-8 space-y-4">
				<h4 className="font-medium text-base">Internal Mount Configuration</h4>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Enable Internal Mount</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Enable internal RClone mount for WebDAV</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={mountFormData.mount_enabled}
							disabled={isReadOnly}
							onChange={(e) => handleMountInputChange("mount_enabled", e.target.checked)}
						/>
					</label>
					<p className="label">
						Mount the AltMount WebDAV as a local filesystem using internal RClone (requires rclone
						binary)
					</p>
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
								placeholder="/mnt/altmount"
							/>
							<p className="label">
								Local filesystem path where WebDAV will be mounted (also editable from WebDAV
								section)
							</p>
						</fieldset>

						{/* Mount Configuration Section - Collapsible */}
						<div className="collapse-arrow collapse bg-base-200">
							<input
								type="checkbox"
								checked={showMountConfig}
								onChange={() => setShowMountConfig(!showMountConfig)}
							/>
							<div className="collapse-title flex items-center gap-2 font-medium text-base">
								<HardDrive className="h-5 w-5" />
								Mount Configuration
							</div>
							<div className="collapse-content">
								<div className="space-y-4 pt-4">
									{/* First Row */}
									<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">RC Port</legend>
											<input
												type="number"
												className="input"
												value={mountFormData.rc_port}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange(
														"rc_port",
														Number.parseInt(e.target.value, 10) || 5572,
													)
												}
											/>
											<p className="label">RClone RC port</p>
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
												<option value="NOTICE">NOTICE</option>
												<option value="ERROR">ERROR</option>
											</select>
											<p className="label">Log verbosity level</p>
										</fieldset>
									</div>

									{/* Second Row */}
									<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">User ID (PUID)</legend>
											<input
												type="number"
												className="input"
												value={mountFormData.uid}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange("uid", Number.parseInt(e.target.value, 10) || 1000)
												}
											/>
											<p className="label">User ID for mounted files (0 = current user)</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">Group ID (PGID)</legend>
											<input
												type="number"
												className="input"
												value={mountFormData.gid}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange("gid", Number.parseInt(e.target.value, 10) || 1000)
												}
											/>
											<p className="label">Group ID for mounted files (0 = current group)</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">UMASK</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.umask}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("umask", e.target.value)}
												placeholder="0022"
											/>
											<p className="label">Umask</p>
										</fieldset>
									</div>

									{/* Third Row */}
									<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">Buffer Size</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.buffer_size}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("buffer_size", e.target.value)}
												placeholder="10M"
											/>
											<p className="label">Buffer size (This caches to memory, be wary!)</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">Attribute Caching Timeout</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.attr_timeout}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("attr_timeout", e.target.value)}
												placeholder="1s"
											/>
											<p className="label">
												How long the kernel caches file attributes (size, modification time, etc.)
											</p>
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
											/>
											<p className="label">Number of file transfers to run in parallel</p>
										</fieldset>
									</div>
								</div>
							</div>
						</div>

						{/* VFS Cache Settings Section - Collapsible */}
						<div className="collapse-arrow collapse bg-base-200">
							<input
								type="checkbox"
								checked={showVFSCache}
								onChange={() => setShowVFSCache(!showVFSCache)}
							/>
							<div className="collapse-title flex items-center gap-2 font-medium text-base">
								<HardDrive className="h-5 w-5" />
								VFS Cache Settings
							</div>
							<div className="collapse-content">
								<div className="space-y-4 pt-4">
									{/* First Row */}
									<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">Cache Directory</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.cache_dir}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("cache_dir", e.target.value)}
												placeholder="/mnt/Data/CloneCache"
											/>
											<p className="label">
												Directory for rclone cache files (leave empty for system default)
											</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Cache Mode</legend>
											<select
												className="select"
												value={mountFormData.vfs_cache_mode}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("vfs_cache_mode", e.target.value)}
											>
												<option value="off">Off</option>
												<option value="minimal">Minimal - Minimal caching</option>
												<option value="writes">Writes - Cache writes only</option>
												<option value="full">Full - Cache reads and writes</option>
											</select>
											<p className="label">VFS caching mode for performance optimization</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Cache Max Size</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.vfs_cache_max_size}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange("vfs_cache_max_size", e.target.value)
												}
												placeholder="100G"
											/>
											<p className="label">
												Maximum cache size (e.g., 1G, 500M, leave empty for unlimited)
											</p>
										</fieldset>
									</div>

									{/* Second Row */}
									<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Cache Max Age</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.vfs_cache_max_age}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange("vfs_cache_max_age", e.target.value)
												}
												placeholder="100h"
											/>
											<p className="label">Maximum age of cache entries (e.g., 1h, 30m)</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">Read Chunk Size</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.read_chunk_size}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("read_chunk_size", e.target.value)}
												placeholder="128M"
											/>
											<p className="label">Size of data chunks to read (e.g., 128M, 64M)</p>
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
												placeholder="off"
											/>
											<p className="label">Limit Read Chunk Size</p>
										</fieldset>
									</div>

									{/* Third Row */}
									<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Read Ahead</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.vfs_read_ahead}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("vfs_read_ahead", e.target.value)}
												placeholder="128k"
											/>
											<p className="label">Read ahead buffer size (e.g., 128k, 256k)</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">Directory Cache Time</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.dir_cache_time}
												disabled={isReadOnly}
												onChange={(e) => handleMountInputChange("dir_cache_time", e.target.value)}
												placeholder="5m"
											/>
											<p className="label">How long to cache directory listings (e.g., 5m, 10m)</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Cache Poll Interval</legend>
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
											<p className="label">How often VFS cache dir gets cleaned</p>
										</fieldset>
									</div>

									{/* Fourth Row */}
									<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Cache Min Free Space</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.vfs_cache_min_free_space}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange("vfs_cache_min_free_space", e.target.value)
												}
												placeholder="1G"
											/>
											<p className="label">
												Target minimum free space on the disk containing the cache
											</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Disk Space Total</legend>
											<input
												type="text"
												className="input"
												value={mountFormData.vfs_disk_space_total}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange("vfs_disk_space_total", e.target.value)
												}
												placeholder="1G"
											/>
											<p className="label">Specify the total space of disk</p>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">VFS Read Chunk Streams</legend>
											<input
												type="number"
												className="input"
												value={mountFormData.vfs_read_chunk_streams}
												disabled={isReadOnly}
												onChange={(e) =>
													handleMountInputChange(
														"vfs_read_chunk_streams",
														Number.parseInt(e.target.value, 10) || 4,
													)
												}
											/>
											<p className="label">The number of parallel streams to read at once</p>
										</fieldset>
									</div>
								</div>
							</div>
						</div>

						{/* Advanced Settings Section - Collapsible */}
						<div className="collapse-arrow collapse bg-base-200">
							<input
								type="checkbox"
								checked={showAdvanced}
								onChange={() => setShowAdvanced(!showAdvanced)}
							/>
							<div className="collapse-title flex items-center gap-2 font-medium text-base">
								<HardDrive className="h-5 w-5" />
								Advanced Settings
							</div>
							<div className="collapse-content">
								<div className="space-y-4 pt-4">
									<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
										<fieldset className="fieldset">
											<legend className="fieldset-legend">No Modification Time</legend>
											<label className="label cursor-pointer">
												<span className="label-text">Don't read/write modification times</span>
												<input
													type="checkbox"
													className="checkbox"
													checked={mountFormData.no_mod_time}
													disabled={isReadOnly}
													onChange={(e) => handleMountInputChange("no_mod_time", e.target.checked)}
												/>
											</label>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">No Checksum</legend>
											<label className="label cursor-pointer">
												<span className="label-text">Don't checksum files on upload</span>
												<input
													type="checkbox"
													className="checkbox"
													checked={mountFormData.no_checksum}
													disabled={isReadOnly}
													onChange={(e) => handleMountInputChange("no_checksum", e.target.checked)}
												/>
											</label>
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">Async Read</legend>
											<label className="label cursor-pointer">
												<span className="label-text">Use asynchronous reads</span>
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
											<legend className="fieldset-legend">VFS Fast Fingerprint</legend>
											<label className="label cursor-pointer">
												<span className="label-text">
													Use fast (less accurate) fingerprints for change detection
												</span>
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
										</fieldset>

										<fieldset className="fieldset">
											<legend className="fieldset-legend">Use Mmap</legend>
											<label className="label cursor-pointer">
												<span className="label-text">
													Use memory-mapped I/O for better performance
												</span>
												<input
													type="checkbox"
													className="checkbox"
													checked={mountFormData.use_mmap}
													disabled={isReadOnly}
													onChange={(e) => handleMountInputChange("use_mmap", e.target.checked)}
												/>
											</label>
										</fieldset>
									</div>
								</div>
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
									{mountStatus.error && (
										<div className="text-error text-sm">{mountStatus.error}</div>
									)}
								</div>
								{mountStatus.mounted ? (
									<button
										type="button"
										className="btn btn-sm btn-outline"
										onClick={handleStopMount}
										disabled={isReadOnly}
									>
										<Square className="h-4 w-4" />
										Stop Mount
									</button>
								) : (
									<button
										type="button"
										className="btn btn-sm btn-primary"
										onClick={handleStartMount}
										disabled={isReadOnly || !mountPath}
									>
										<Play className="h-4 w-4" />
										Start Mount
									</button>
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

			{/* Action Buttons */}
			{!isReadOnly && mountFormData.mount_enabled && (
				<div className="flex justify-end gap-2">
					<button
						type="button"
						className="btn btn-outline"
						onClick={handleTestMount}
						disabled={isTestingMount || !mountPath}
					>
						{isTestingMount ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<TestTube className="h-4 w-4" />
						)}
						{isTestingMount ? "Testing..." : "Test Mount"}
					</button>
					<button
						type="button"
						className="btn btn-primary"
						onClick={handleSaveMount}
						disabled={(!hasMountChanges && !hasMountPathChanges) || isUpdating}
					>
						{isUpdating ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Save className="h-4 w-4" />
						)}
						{isUpdating ? "Saving..." : "Save Mount Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
