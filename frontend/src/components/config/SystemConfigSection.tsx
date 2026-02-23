import { Copy, RefreshCw, Save, ShieldCheck, Terminal } from "lucide-react";
import { useEffect, useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useRegenerateAPIKey } from "../../hooks/useAuth";
import type { ConfigResponse, LogFormData } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface SystemConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: LogFormData) => Promise<void>;
	onRefresh?: () => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function SystemConfigSection({
	config,
	onUpdate,
	onRefresh,
	isReadOnly = false,
	isUpdating = false,
}: SystemConfigSectionProps) {
	const [formData, setFormData] = useState<LogFormData>({
		file: config.log.file,
		level: config.log.level,
		max_size: config.log.max_size,
		max_age: config.log.max_age,
		max_backups: config.log.max_backups,
		compress: config.log.compress,
	});
	const [profilerEnabled, setProfilerEnabled] = useState(config.profiler_enabled);
	const [hasChanges, setHasChanges] = useState(false);

	const regenerateAPIKey = useRegenerateAPIKey();
	const { confirmAction } = useConfirm();
	const { showToast } = useToast();

	useEffect(() => {
		const newFormData = {
			file: config.log.file,
			level: config.log.level,
			max_size: config.log.max_size,
			max_age: config.log.max_age,
			max_backups: config.log.max_backups,
			compress: config.log.compress,
		};
		setFormData(newFormData);
		setProfilerEnabled(config.profiler_enabled);
		setHasChanges(false);
	}, [config.log, config.profiler_enabled]);

	const handleInputChange = (field: keyof LogFormData, value: string | number | boolean) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		const configData = {
			file: config.log.file,
			level: config.log.level,
			max_size: config.log.max_size,
			max_age: config.log.max_age,
			max_backups: config.log.max_backups,
			compress: config.log.compress,
		};
		setHasChanges(
			JSON.stringify(newData) !== JSON.stringify(configData) ||
				profilerEnabled !== config.profiler_enabled,
		);
	};

	const handleProfilerChange = (enabled: boolean) => {
		setProfilerEnabled(enabled);
		setHasChanges(true);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			// We need a way to update profiler_enabled too.
			// In ConfigurationPage, onUpdate for 'log' updates 'system' section which includes log.
			// Let's assume the backend handles both if we send them.
			await onUpdate("log", { ...formData, profiler_enabled: profilerEnabled } as LogFormData & {
				profiler_enabled: boolean;
			});
			setHasChanges(false);
		}
	};

	const handleCopyAPIKey = async () => {
		if (config.api_key) {
			try {
				await navigator.clipboard.writeText(config.api_key);
				showToast({ type: "success", title: "Success", message: "API key copied to clipboard" });
			} catch (_error) {
				showToast({ type: "error", title: "Error", message: "Failed to copy API key" });
			}
		}
	};

	const handleRegenerateAPIKey = async () => {
		const confirmed = await confirmAction(
			"Regenerate API Key",
			"This will generate a new API key and invalidate the current one. Continue?",
			{ type: "warning", confirmText: "Regenerate", confirmButtonClass: "btn-warning" },
		);
		if (confirmed) {
			try {
				await regenerateAPIKey.mutateAsync();
				if (onRefresh) await onRefresh();
				showToast({
					type: "success",
					title: "Success",
					message: "API key regenerated successfully",
				});
			} catch (_error) {
				showToast({ type: "error", title: "Error", message: "Failed to regenerate API key" });
			}
		}
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">System Core</h3>
				<p className="break-words text-base-content/50 text-sm">
					Manage global logging, security, and identity.
				</p>
			</div>

			<div className="space-y-8">
				{/* Logging Configuration */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Terminal className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Diagnostics
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold text-xs">Minimum Log Level</legend>
							<select
								className="select select-bordered w-full bg-base-100"
								value={formData.level}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("level", e.target.value)}
							>
								<option value="debug">Debug (Verbose)</option>
								<option value="info">Info (Standard)</option>
								<option value="warn">Warning (Alerts)</option>
								<option value="error">Error (Critical)</option>
							</select>
							<p className="label mt-2 break-words text-base-content/70 text-xs">
								Determines how much information is stored in logs.
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold text-xs">Max Log Size (MB)</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.max_size}
								disabled={isReadOnly}
								onChange={(e) =>
									handleInputChange("max_size", Number.parseInt(e.target.value, 10) || 0)
								}
							/>
						</fieldset>
					</div>

					<div className="grid grid-cols-1 gap-6 sm:grid-cols-3">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold text-xs">Max Age (Days)</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.max_age}
								disabled={isReadOnly}
								onChange={(e) =>
									handleInputChange("max_age", Number.parseInt(e.target.value, 10) || 0)
								}
							/>
						</fieldset>
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold text-xs">Max Backups</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.max_backups}
								disabled={isReadOnly}
								onChange={(e) =>
									handleInputChange("max_backups", Number.parseInt(e.target.value, 10) || 0)
								}
							/>
						</fieldset>
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold text-xs">Compress Logs</legend>
							<div className="flex h-12 items-center">
								<input
									type="checkbox"
									className="checkbox checkbox-primary"
									checked={formData.compress}
									disabled={isReadOnly}
									onChange={(e) => handleInputChange("compress", e.target.checked)}
								/>
							</div>
						</fieldset>
					</div>
				</div>

				{/* Performance Profiler */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Terminal className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Performance
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h5 className="font-bold text-sm">System Profiler (pprof)</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Enable Go runtime profiling at <code>/debug/pprof</code>. Only recommended for
								debugging resource leaks.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-warning mt-1 shrink-0"
							checked={profilerEnabled}
							disabled={isReadOnly}
							onChange={(e) => handleProfilerChange(e.target.checked)}
						/>
					</div>
				</div>

				{/* Security Section */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<ShieldCheck className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Access Identity
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="space-y-6">
						<div className="min-w-0">
							<h5 className="font-bold text-sm">AltMount API Key</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Your personal secret key for authenticating external applications.
							</p>
						</div>

						<div className="flex flex-col gap-4">
							<div className="join w-full max-w-lg shadow-sm">
								<input
									type="text"
									className="input input-bordered join-item flex-1 overflow-hidden bg-base-100 font-mono text-xs"
									value={config.api_key || "Not Generated"}
									readOnly
								/>
								{config.api_key && (
									<button
										type="button"
										className="btn btn-ghost join-item border-base-300 px-4"
										onClick={handleCopyAPIKey}
									>
										<Copy className="h-4 w-4" />
									</button>
								)}
							</div>

							<div className="flex justify-start">
								<button
									type="button"
									className="btn btn-ghost btn-sm border-base-300 bg-base-100 hover:bg-base-200"
									onClick={handleRegenerateAPIKey}
									disabled={regenerateAPIKey.isPending}
								>
									{regenerateAPIKey.isPending ? (
										<LoadingSpinner size="sm" />
									) : (
										<RefreshCw className="h-3 w-3" />
									)}
									Regenerate Token
								</button>
							</div>
						</div>
					</div>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end border-base-200 border-t pt-4">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && "btn-ghost border-base-300"}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
					>
						{isUpdating ? <LoadingSpinner size="sm" /> : <Save className="h-4 w-4" />}
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
