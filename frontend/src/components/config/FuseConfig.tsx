import { HardDrive, Play, Save, Square } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { apiClient } from "../../api/client";
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
	});

	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	// Initialize form data from config
	useEffect(() => {
		if (config?.fuse) {
			setFormData(config.fuse);
			if (config.fuse.mount_path) {
				setPath(config.fuse.mount_path);
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

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">FUSE Mount Configuration</h3>

			<div className="space-y-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Mount Settings</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Enable Auto-Start</span>
						<input
							type="checkbox"
							className="toggle toggle-primary"
							checked={formData.enabled ?? false}
							onChange={(e) => setFormData({ ...formData, enabled: e.target.checked })}
							disabled={isRunning}
						/>
					</label>
					<p className="label">Automatically mount on server startup when path is configured</p>

					<div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend">Mount Path</legend>
							<input
								type="text"
								placeholder="/mnt/altmount"
								className="input"
								value={path}
								onChange={(e) => setPath(e.target.value)}
								disabled={isRunning}
							/>
						</fieldset>

						<div className="form-control">
							<label className="label cursor-pointer">
								<span className="label-text">Allow Other Users</span>
								<input
									type="checkbox"
									className="toggle toggle-primary"
									checked={formData.allow_other ?? true}
									onChange={(e) => setFormData({ ...formData, allow_other: e.target.checked })}
									disabled={isRunning}
								/>
							</label>
							<p className="label">Allow other users (like Docker/Plex) to access the mount</p>
						</div>
					</div>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Kernel Cache Settings</legend>
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
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Streaming Cache Settings</legend>
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
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Advanced Options</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Debug Logging</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.debug ?? false}
							onChange={(e) => setFormData({ ...formData, debug: e.target.checked })}
							disabled={isRunning}
						/>
					</label>
					<p className="label">Enable verbose FUSE debug logging for troubleshooting</p>
				</fieldset>
			</div>

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
								: "Not Mounted"}
					</div>
					{status === "running" && path && <div className="text-sm">Mount point: {path}</div>}
					{error && <div className="text-sm">{error}</div>}
				</div>
				{status === "running" ? (
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
		</div>
	);
}
