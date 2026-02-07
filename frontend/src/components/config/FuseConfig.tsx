import { HardDrive, Play, Save, Square } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { apiClient } from "../../api/client";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useConfig, useUpdateConfigSection } from "../../hooks/useConfig";
import type { FuseConfig as FuseConfigType } from "../../types/config";

export function FuseConfig() {
	const { data: config } = useConfig();
	const updateConfig = useUpdateConfigSection();

	const [status, setStatus] = useState<"stopped" | "starting" | "running" | "error">("stopped");
	const [path, setPath] = useState("");
	const [formData, setFormData] = useState<Partial<FuseConfigType>>({
		enabled: false,
		allow_other: true,
		debug: false,
		attr_timeout_seconds: 1,
		entry_timeout_seconds: 1,
		max_download_workers: 15,
		max_cache_size_mb: 128,
		max_read_ahead_mb: 128,
		// Metadata cache defaults
		metadata_cache_enabled: false,
		stat_cache_size: 10000,
		dir_cache_size: 1000,
		negative_cache_size: 5000,
		stat_cache_ttl_seconds: 30,
		dir_cache_ttl_seconds: 60,
		negative_cache_ttl_seconds: 10,
	});

	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [isEnabledToggleSaving, setIsEnabledToggleSaving] = useState(false);
	const { showToast } = useToast();
	const { confirmAction } = useConfirm();

	// Initialize form data from config
	useEffect(() => {
		if (config?.fuse) {
			setFormData(config.fuse);
			if (config.fuse.mount_path) {
				setPath(config.fuse.mount_path);
			} else if (config.mount_path) {
				setPath(config.mount_path);
			}
		}
	}, [config]);

	const fetchStatus = useCallback(async () => {
		try {
			const response = await apiClient.getFuseStatus();
			setStatus(response.status);
			// Only update path from status if we don't have one set locally/from config yet?
			// Or maybe status path is the source of truth for running instance.
			if (response.path && response.status !== "stopped") {
				setPath(response.path);
			}
		} catch (err) {
			console.error("Failed to fetch FUSE status:", err);
		}
	}, []);

	useEffect(() => {
		fetchStatus();
		const interval = setInterval(fetchStatus, 5000);
		return () => clearInterval(interval);
	}, [fetchStatus]);

	const handleSave = async () => {
		try {
			await updateConfig.mutateAsync({
				section: "fuse",
				config: { fuse: formData },
			});
			return true;
		} catch (_err) {
			setError("Failed to save configuration.");
			return false;
		}
	};

	const handleStart = async () => {
		setIsLoading(true);
		setError(null);
		try {
			// Save config first to ensure options are persisted
			// We also update mount_path in formData for consistency
			const updatedData = { ...formData, mount_path: path };
			await updateConfig.mutateAsync({
				section: "fuse",
				config: { fuse: updatedData },
			});

			await apiClient.startFuseMount(path);
			await fetchStatus();
		} catch (err) {
			setError("Failed to start mount. Check logs for details.");
			console.error(err);
		} finally {
			setIsLoading(false);
		}
	};

	const handleStop = async () => {
		setIsLoading(true);
		setError(null);
		try {
			await apiClient.stopFuseMount();
			await fetchStatus();
		} catch (err) {
			setError("Failed to stop mount.");
			console.error(err);
		} finally {
			setIsLoading(false);
		}
	};

	const isRunning = status === "running" || status === "starting";

	const handleEnabledChange = async (enabled: boolean) => {
		// If disabling and mount is running, ask for confirmation
		if (!enabled && isRunning) {
			const confirmed = await confirmAction(
				"Disable FUSE Mount",
				"The mount is currently active. Disabling will stop the active mount. Continue?",
				{ type: "warning", confirmText: "Disable & Unmount", confirmButtonClass: "btn-warning" },
			);
			if (!confirmed) return;
		}

		setIsEnabledToggleSaving(true);
		setFormData((prev) => ({ ...prev, enabled }));

		try {
			// If disabling and running, stop the mount first
			if (!enabled && isRunning) {
				await apiClient.stopFuseMount();
				await fetchStatus();
			}

			// Save the config
			await updateConfig.mutateAsync({
				section: "fuse",
				config: { fuse: { ...formData, enabled } },
			});

			showToast({
				type: "success",
				title: enabled ? "FUSE mount enabled" : "FUSE mount disabled",
				message: enabled
					? "Configure mount path and start the mount"
					: "FUSE mount has been disabled",
			});
		} catch (err) {
			// Revert on error
			setFormData((prev) => ({ ...prev, enabled: !enabled }));
			showToast({
				type: "error",
				title: `Failed to ${enabled ? "enable" : "disable"} FUSE mount`,
				message: err instanceof Error ? err.message : "Unknown error",
			});
		} finally {
			setIsEnabledToggleSaving(false);
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">FUSE Mount Configuration</h3>
			<p className="text-sm opacity-70">
				Mount altmount directly to a local directory for high performance access.
			</p>

			{/* Enable FUSE Mount Toggle */}
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Enable FUSE Mount</legend>
				<label className="label cursor-pointer">
					<span className="label-text">
						Enable native FUSE mount
						{isEnabledToggleSaving && <span className="loading loading-spinner loading-xs ml-2" />}
					</span>
					<input
						type="checkbox"
						className="checkbox"
						checked={formData.enabled ?? false}
						disabled={isEnabledToggleSaving}
						onChange={(e) => handleEnabledChange(e.target.checked)}
					/>
				</label>
				<p className="label">
					Mount altmount directly to a local directory using native FUSE
					{isEnabledToggleSaving && " (Saving...)"}
				</p>
			</fieldset>

			{formData.enabled && (
				<>
					{/* Mount Settings */}
					<div className="space-y-4">
						<h4 className="font-medium text-base">Mount Settings</h4>

						<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Mount Path</legend>
								<input
									type="text"
									placeholder="/mnt/altmount"
									className="input w-full"
									value={path}
									onChange={(e) => setPath(e.target.value)}
									disabled={isRunning}
								/>
								<p className="label">Local filesystem path where FUSE mount will be created</p>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Allow Other Users</legend>
								<label className="label cursor-pointer">
									<span className="label-text">Allow other users to access mount</span>
									<input
										type="checkbox"
										className="checkbox"
										checked={formData.allow_other ?? true}
										onChange={(e) => setFormData({ ...formData, allow_other: e.target.checked })}
										disabled={isRunning}
									/>
								</label>
								<p className="label">Allow other users (like Docker/Plex) to access the mount</p>
							</fieldset>
						</div>
					</div>

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
										value={formData.attr_timeout_seconds ?? 1}
										onChange={(e) =>
											setFormData({
												...formData,
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
											setFormData({
												...formData,
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
											setFormData({
												...formData,
												max_read_ahead_mb: Number.parseInt(e.target.value, 10) || 0,
											})
										}
										disabled={isRunning}
									/>
									<span className="btn no-animation join-item">MB</span>
								</div>
								<p className="label">
									Maximum data the kernel will request ahead (FUSE max_readahead)
								</p>
							</fieldset>
						</div>
					</div>

					{/* Streaming Cache Settings */}
					<div className="space-y-4">
						<h4 className="font-medium text-base">Streaming Cache Settings</h4>

						<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Max Download Workers</legend>
								<input
									type="number"
									className="input"
									value={formData.max_download_workers ?? 15}
									onChange={(e) =>
										setFormData({
											...formData,
											max_download_workers: Number.parseInt(e.target.value, 10) || 0,
										})
									}
									disabled={isRunning}
								/>
								<p className="label">Concurrent download workers for this mount</p>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Max Cache Size</legend>
								<div className="join">
									<input
										type="number"
										className="input join-item w-full"
										value={formData.max_cache_size_mb ?? 128}
										onChange={(e) =>
											setFormData({
												...formData,
												max_cache_size_mb: Number.parseInt(e.target.value, 10) || 0,
											})
										}
										disabled={isRunning}
									/>
									<span className="btn no-animation join-item">MB</span>
								</div>
								<p className="label">Read-ahead cache size per file</p>
							</fieldset>
						</div>
					</div>

					{/* Metadata Cache Settings */}
					<div className="space-y-4">
						<h4 className="font-medium text-base">Metadata Cache Settings</h4>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">Enable Metadata Cache</legend>
							<label className="label cursor-pointer">
								<span className="label-text">Cache file and directory metadata</span>
								<input
									type="checkbox"
									className="checkbox"
									checked={formData.metadata_cache_enabled ?? false}
									onChange={(e) =>
										setFormData({ ...formData, metadata_cache_enabled: e.target.checked })
									}
									disabled={isRunning}
								/>
							</label>
							<p className="label">
								Reduces filesystem lookups by caching file attributes and directory listings
							</p>
						</fieldset>

						{formData.metadata_cache_enabled && (
							<>
								<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Stat Cache Size</legend>
										<div className="join">
											<input
												type="number"
												className="input join-item w-full"
												value={formData.stat_cache_size ?? 10000}
												onChange={(e) =>
													setFormData({
														...formData,
														stat_cache_size: Number.parseInt(e.target.value, 10) || 0,
													})
												}
												disabled={isRunning}
											/>
											<span className="btn no-animation join-item">entries</span>
										</div>
										<p className="label">Max cached file metadata entries</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Dir Cache Size</legend>
										<div className="join">
											<input
												type="number"
												className="input join-item w-full"
												value={formData.dir_cache_size ?? 1000}
												onChange={(e) =>
													setFormData({
														...formData,
														dir_cache_size: Number.parseInt(e.target.value, 10) || 0,
													})
												}
												disabled={isRunning}
											/>
											<span className="btn no-animation join-item">entries</span>
										</div>
										<p className="label">Max cached directory listings</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Negative Cache Size</legend>
										<div className="join">
											<input
												type="number"
												className="input join-item w-full"
												value={formData.negative_cache_size ?? 5000}
												onChange={(e) =>
													setFormData({
														...formData,
														negative_cache_size: Number.parseInt(e.target.value, 10) || 0,
													})
												}
												disabled={isRunning}
											/>
											<span className="btn no-animation join-item">entries</span>
										</div>
										<p className="label">Max cached "not found" results</p>
									</fieldset>
								</div>

								<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
									<fieldset className="fieldset">
										<legend className="fieldset-legend">Stat Cache TTL</legend>
										<div className="join">
											<input
												type="number"
												className="input join-item w-full"
												value={formData.stat_cache_ttl_seconds ?? 30}
												onChange={(e) =>
													setFormData({
														...formData,
														stat_cache_ttl_seconds: Number.parseInt(e.target.value, 10) || 0,
													})
												}
												disabled={isRunning}
											/>
											<span className="btn no-animation join-item">sec</span>
										</div>
										<p className="label">File metadata cache lifetime</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Dir Cache TTL</legend>
										<div className="join">
											<input
												type="number"
												className="input join-item w-full"
												value={formData.dir_cache_ttl_seconds ?? 60}
												onChange={(e) =>
													setFormData({
														...formData,
														dir_cache_ttl_seconds: Number.parseInt(e.target.value, 10) || 0,
													})
												}
												disabled={isRunning}
											/>
											<span className="btn no-animation join-item">sec</span>
										</div>
										<p className="label">Directory listing cache lifetime</p>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Negative Cache TTL</legend>
										<div className="join">
											<input
												type="number"
												className="input join-item w-full"
												value={formData.negative_cache_ttl_seconds ?? 10}
												onChange={(e) =>
													setFormData({
														...formData,
														negative_cache_ttl_seconds: Number.parseInt(e.target.value, 10) || 0,
													})
												}
												disabled={isRunning}
											/>
											<span className="btn no-animation join-item">sec</span>
										</div>
										<p className="label">"Not found" cache lifetime</p>
									</fieldset>
								</div>
							</>
						)}
					</div>

					{/* Advanced Options */}
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
									onChange={(e) => setFormData({ ...formData, debug: e.target.checked })}
									disabled={isRunning}
								/>
							</label>
							<p className="label">Enable verbose debug output for troubleshooting</p>
						</fieldset>
					</div>

					{error && (
						<div className="alert alert-error">
							<span>{error}</span>
						</div>
					)}

					{/* Mount Status */}
					<div
						className={`alert ${status === "running" ? "alert-success" : status === "error" ? "alert-error" : "alert-warning"}`}
					>
						<HardDrive className="h-6 w-6" />
						<div>
							<div className="font-bold">
								{status === "running"
									? "Mounted"
									: status === "starting"
										? "Starting..."
										: status === "error"
											? "Error"
											: "Not Mounted"}
							</div>
							{path && status !== "stopped" && <div className="text-sm">Mount point: {path}</div>}
						</div>
						{isRunning ? (
							<button
								type="button"
								className="btn btn-sm btn-outline"
								onClick={handleStop}
								disabled={isLoading}
							>
								{isLoading ? (
									<span className="loading loading-spinner loading-xs" />
								) : (
									<Square className="h-4 w-4" />
								)}
								{isLoading ? "Stopping..." : "Unmount"}
							</button>
						) : (
							<button
								type="button"
								className="btn btn-sm btn-primary"
								onClick={handleStart}
								disabled={isLoading || !path}
							>
								{isLoading ? (
									<span className="loading loading-spinner loading-xs" />
								) : (
									<Play className="h-4 w-4" />
								)}
								{isLoading ? "Starting..." : "Mount"}
							</button>
						)}
					</div>

					{/* Save Button */}
					{!isRunning && (
						<div className="flex justify-end">
							<button
								type="button"
								className="btn btn-primary"
								onClick={handleSave}
								disabled={updateConfig.isPending}
							>
								{updateConfig.isPending ? (
									<span className="loading loading-spinner loading-sm" />
								) : (
									<Save className="h-4 w-4" />
								)}
								{updateConfig.isPending ? "Saving..." : "Save Changes"}
							</button>
						</div>
					)}
				</>
			)}
		</div>
	);
}
