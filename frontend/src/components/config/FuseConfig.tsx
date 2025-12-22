import { Activity, Download, FolderOpen, HardDrive, Play, Save, Square } from "lucide-react";
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
		<div className="card bg-base-100 shadow-xl">
			<div className="card-body">
				<h2 className="card-title flex items-center gap-2">
					<FolderOpen className="h-6 w-6" />
					altmount Native Mount
				</h2>
				<p className="text-sm opacity-70">
					Mount altmount directly to a local directory for high performance access.
				</p>

				<div className="divider" />

				<div className="space-y-6">
					<section>
						<h3 className="mb-4 flex items-center gap-2 font-medium text-lg">
							<HardDrive className="h-5 w-5" />
							Mount Settings
						</h3>
						<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
							<div className="form-control w-full">
								<label className="label">
									<span className="label-text">Mount Path</span>
								</label>
								<input
									type="text"
									placeholder="/mnt/altmount"
									className="input input-bordered w-full"
									value={path}
									onChange={(e) => setPath(e.target.value)}
									disabled={isRunning}
								/>
							</div>

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
								<label className="label">
									<span className="label-text-alt opacity-70">
										Allow other users (like Docker/Plex) to access the mount
									</span>
								</label>
							</div>
						</div>
					</section>

					<section>
						<h3 className="mb-4 flex items-center gap-2 font-medium text-lg">
							<Activity className="h-5 w-5" />
							Kernel Cache Settings
						</h3>
						<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
							<div className="form-control">
								<label className="label">
									<span className="label-text">Attribute Timeout</span>
								</label>
								<div className="join">
									<input
										type="number"
										className="input input-bordered join-item w-full"
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
								<label className="label">
									<span className="label-text-alt opacity-70">
										How long the kernel caches file attributes
									</span>
								</label>
							</div>

							<div className="form-control">
								<label className="label">
									<span className="label-text">Entry Timeout</span>
								</label>
								<div className="join">
									<input
										type="number"
										className="input input-bordered join-item w-full"
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
								<label className="label">
									<span className="label-text-alt opacity-70">
										How long the kernel caches directory lookups
									</span>
								</label>
							</div>

							<div className="form-control">
								<label className="label">
									<span className="label-text">Kernel Read-Ahead</span>
								</label>
								<div className="join">
									<input
										type="number"
										className="input input-bordered join-item w-full"
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
								<label className="label">
									<span className="label-text-alt opacity-70">
										Maximum data the kernel will request ahead (FUSE max_readahead)
									</span>
								</label>
							</div>
						</div>
					</section>

					<section>
						<h3 className="mb-4 flex items-center gap-2 font-medium text-lg">
							<Download className="h-5 w-5" />
							Streaming Cache Settings
						</h3>
						<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
							<div className="form-control">
								<label className="label">
									<span className="label-text">Max Download Workers</span>
								</label>
								<input
									type="number"
									className="input input-bordered"
									value={formData.max_download_workers ?? 15}
									onChange={(e) =>
										setFormData({
											...formData,
											max_download_workers: Number.parseInt(e.target.value, 10) || 0,
										})
									}
									disabled={isRunning}
								/>
								<label className="label">
									<span className="label-text-alt opacity-70">
										Concurrent download workers for this mount
									</span>
								</label>
							</div>

							<div className="form-control">
								<label className="label">
									<span className="label-text">Max Cache Size</span>
								</label>
								<div className="join">
									<input
										type="number"
										className="input input-bordered join-item w-full"
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
								<label className="label">
									<span className="label-text-alt opacity-70">Read-ahead cache size per file</span>
								</label>
							</div>
						</div>
					</section>

					<section>
						<div className="form-control max-w-xs">
							<label className="label cursor-pointer">
								<span className="label-text">Debug Logging</span>
								<input
									type="checkbox"
									className="checkbox checkbox-primary"
									checked={formData.debug ?? false}
									onChange={(e) => setFormData({ ...formData, debug: e.target.checked })}
									disabled={isRunning}
								/>
							</label>
						</div>
					</section>
				</div>

				{error && (
					<div className="alert alert-error mt-4">
						<span>{error}</span>
					</div>
				)}

				<div className="card-actions mt-6 items-center justify-end">
					<div
						className="badge badge-lg mr-4 font-bold uppercase"
						data-theme={status === "running" ? "success" : status === "error" ? "error" : "neutral"}
					>
						{status}
					</div>

					{!isRunning && (
						<button
							className="btn btn-ghost mr-2"
							onClick={handleSave}
							disabled={updateConfig.isPending}
						>
							<Save className="mr-2 h-4 w-4" />
							Save Settings
						</button>
					)}

					{isRunning ? (
						<button className="btn btn-error" onClick={handleStop} disabled={isLoading}>
							{isLoading ? (
								<span className="loading loading-spinner" />
							) : (
								<Square className="h-4 w-4" />
							)}
							Unmount
						</button>
					) : (
						<button className="btn btn-primary" onClick={handleStart} disabled={isLoading || !path}>
							{isLoading ? (
								<span className="loading loading-spinner" />
							) : (
								<Play className="h-4 w-4" />
							)}
							Mount
						</button>
					)}
				</div>
			</div>
		</div>
	);
}
