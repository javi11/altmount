import { Copy, RefreshCw, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useRegenerateAPIKey } from "../../hooks/useAuth";
import type { ConfigResponse, LogFormData } from "../../types/config";

interface SystemFormData extends LogFormData {
	db_path: string;
	api_prefix: string;
}

interface SystemConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: unknown) => Promise<void>;
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
	const [formData, setFormData] = useState<SystemFormData>({
		file: config.log.file,
		level: config.log.level,
		max_size: config.log.max_size,
		max_age: config.log.max_age,
		max_backups: config.log.max_backups,
		compress: config.log.compress,
		db_path: config.database.path,
		api_prefix: config.api.prefix,
	});
	const [hasChanges, setHasChanges] = useState(false);

	// API Key functionality
	const regenerateAPIKey = useRegenerateAPIKey();
	const { confirmAction } = useConfirm();
	const { showToast } = useToast();

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = {
			file: config.log.file,
			level: config.log.level,
			max_size: config.log.max_size,
			max_age: config.log.max_age,
			max_backups: config.log.max_backups,
			compress: config.log.compress,
			db_path: config.database.path,
			api_prefix: config.api.prefix,
		};
		setFormData(newFormData);
		setHasChanges(false);
	}, [config.log, config.database.path, config.api.prefix]);

	const handleInputChange = (field: keyof SystemFormData, value: string | number | boolean) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		const currentConfig = {
			file: config.log.file,
			level: config.log.level,
			max_size: config.log.max_size,
			max_age: config.log.max_age,
			max_backups: config.log.max_backups,
			compress: config.log.compress,
			db_path: config.database.path,
			api_prefix: config.api.prefix,
		};
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(currentConfig));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			// Update Logging
			const logData: LogFormData = {
				file: formData.file,
				level: formData.level,
				max_size: formData.max_size,
				max_age: formData.max_age,
				max_backups: formData.max_backups,
				compress: formData.compress,
			};

			const currentLogConfig = {
				file: config.log.file,
				level: config.log.level,
				max_size: config.log.max_size,
				max_age: config.log.max_age,
				max_backups: config.log.max_backups,
				compress: config.log.compress,
			};

			if (JSON.stringify(logData) !== JSON.stringify(currentLogConfig)) {
				await onUpdate("log", logData);
			}

			// Update Database Path
			if (formData.db_path !== config.database.path) {
				await onUpdate("database", { path: formData.db_path });
			}

			// Update API Prefix
			if (formData.api_prefix !== config.api.prefix) {
				await onUpdate("api", { prefix: formData.api_prefix });
			}

			setHasChanges(false);
		}
	};

	const handleCopyAPIKey = async () => {
		if (config.api_key) {
			try {
				await navigator.clipboard.writeText(config.api_key);
				showToast({
					type: "success",
					title: "Success",
					message: "API key copied to clipboard",
				});
			} catch (_error) {
				showToast({
					type: "error",
					title: "Error",
					message: "Failed to copy API key",
				});
			}
		}
	};

	const handleRegenerateAPIKey = async () => {
		const confirmed = await confirmAction(
			"Regenerate API Key",
			"This will generate a new API key and invalidate the current one. Make sure to update any applications using the old key.",
			{
				type: "warning",
				confirmText: "Regenerate API Key",
				confirmButtonClass: "btn-warning",
			},
		);

		if (confirmed) {
			try {
				await regenerateAPIKey.mutateAsync();
				if (onRefresh) {
					await onRefresh();
				}
				showToast({
					type: "success",
					title: "Success",
					message: "API key regenerated successfully",
				});
			} catch (_error) {
				showToast({
					type: "error",
					title: "Error",
					message: "Failed to regenerate API key",
				});
			}
		}
	};

	return (
		<div className="space-y-10">
			{/* Application Identity Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
						App Identity
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">Database Path</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50 font-mono"
							value={formData.db_path}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("db_path", e.target.value)}
							placeholder="/app/data/altmount.db"
						/>
						<p className="label text-[10px] italic opacity-60">Location of the SQLite database.</p>
					</fieldset>

					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">API Prefix</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50 font-mono"
							value={formData.api_prefix}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("api_prefix", e.target.value)}
							placeholder="/api"
						/>
						<p className="label text-[10px] italic opacity-60">Base path for all REST endpoints.</p>
					</fieldset>
				</div>

				{/* API Key Management */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6">
					<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
						<div className="min-w-0 flex-1">
							<span className="mb-1 block font-bold text-sm">System API Key</span>
							<div className="flex items-center gap-2">
								<input
									type="text"
									className="input input-xs flex-1 bg-base-100 font-mono"
									value={config.api_key || "No key generated"}
									readOnly
									disabled
								/>
								{config.api_key && (
									<button type="button" className="btn btn-ghost btn-xs" onClick={handleCopyAPIKey}>
										<Copy className="h-3 w-3" />
									</button>
								)}
							</div>
						</div>
						<button
							type="button"
							className="btn btn-warning btn-sm"
							onClick={handleRegenerateAPIKey}
							disabled={regenerateAPIKey.isPending}
						>
							{regenerateAPIKey.isPending ? (
								<span className="loading loading-spinner loading-xs" />
							) : (
								<RefreshCw className="h-3.5 w-3.5" />
							)}
							Rotate Key
						</button>
					</div>
				</div>
			</section>

			{/* Logging Configuration Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
						Diagnostics & Logs
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">Log Level</legend>
						<select
							className="select select-bordered w-full bg-base-200/50 font-mono"
							value={formData.level}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("level", e.target.value)}
						>
							<option value="debug">DEBUG</option>
							<option value="info">INFO</option>
							<option value="warn">WARNING</option>
							<option value="error">ERROR</option>
						</select>
					</fieldset>

					<fieldset className="fieldset min-w-0 sm:col-span-2">
						<legend className="fieldset-legend font-semibold">Log File Path</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50 font-mono"
							value={formData.file}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("file", e.target.value)}
						/>
					</fieldset>
				</div>

				<div className="grid grid-cols-1 gap-6 rounded-2xl border border-base-300 bg-base-200/30 p-6 sm:grid-cols-2 lg:grid-cols-4">
					<div className="space-y-1">
						<span className="block font-bold text-[10px] uppercase opacity-50">Max Size (MB)</span>
						<input
							type="number"
							className="input input-sm w-full bg-base-100 font-mono"
							value={formData.max_size}
							disabled={isReadOnly}
							onChange={(e) =>
								handleInputChange("max_size", Number.parseInt(e.target.value, 10) || 100)
							}
						/>
					</div>
					<div className="space-y-1">
						<span className="block font-bold text-[10px] uppercase opacity-50">Max Age (Days)</span>
						<input
							type="number"
							className="input input-sm w-full bg-base-100 font-mono"
							value={formData.max_age}
							disabled={isReadOnly}
							onChange={(e) =>
								handleInputChange("max_age", Number.parseInt(e.target.value, 10) || 28)
							}
						/>
					</div>
					<div className="space-y-1">
						<span className="block font-bold text-[10px] uppercase opacity-50">Retention</span>
						<input
							type="number"
							className="input input-sm w-full bg-base-100 font-mono"
							value={formData.max_backups}
							disabled={isReadOnly}
							onChange={(e) =>
								handleInputChange("max_backups", Number.parseInt(e.target.value, 10) || 3)
							}
						/>
					</div>
					<div className="flex flex-col justify-center">
						<label className="label cursor-pointer justify-start gap-3 p-0">
							<input
								type="checkbox"
								className="checkbox checkbox-sm checkbox-primary"
								checked={formData.compress}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("compress", e.target.checked)}
							/>
							<span className="label-text font-semibold text-xs">Compress Rotated Logs</span>
						</label>
					</div>
				</div>
			</section>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end border-base-200 border-t pt-6">
					<button
						type="button"
						className={`btn btn-primary btn-md px-10 ${hasChanges ? "shadow-lg shadow-primary/20" : ""}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
					>
						{isUpdating ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Save className="h-4 w-4" />
						)}
						Save System Configuration
					</button>
				</div>
			)}
		</div>
	);
}
