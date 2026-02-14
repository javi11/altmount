import { Copy, RefreshCw, Save, Terminal, ShieldCheck } from "lucide-react";
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
		setHasChanges(false);
	}, [config.log]);

	const handleInputChange = (field: keyof LogFormData, value: string | number | boolean) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		const configData = {
			file: config.log.file, level: config.log.level, max_size: config.log.max_size,
			max_age: config.log.max_age, max_backups: config.log.max_backups, compress: config.log.compress,
		};
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(configData));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("log", formData);
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
				showToast({ type: "success", title: "Success", message: "API key regenerated successfully" });
			} catch (_error) {
				showToast({ type: "error", title: "Error", message: "Failed to regenerate API key" });
			}
		}
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="text-lg font-bold text-base-content tracking-tight">System Core</h3>
				<p className="text-sm text-base-content/50 break-words">Manage global logging, security, and identity.</p>
			</div>

			<div className="space-y-8">
				{/* Logging Configuration */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<div className="flex items-center gap-2">
						<Terminal className="h-4 w-4 opacity-40" />
						<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Diagnostics</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset max-w-sm">
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
						<p className="label text-[10px] opacity-50 break-words mt-2">Determines how much information is stored in logs.</p>
					</fieldset>
				</div>

				{/* Security Section */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<div className="flex items-center gap-2">
						<ShieldCheck className="h-4 w-4 opacity-40" />
						<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Access Identity</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="space-y-6">
						<div className="min-w-0">
							<h5 className="font-bold text-sm">AltMount API Key</h5>
							<p className="text-[11px] text-base-content/50 break-words mt-1 leading-relaxed">
								Your personal secret key for authenticating external applications.
							</p>
						</div>

						<div className="flex flex-col gap-4">
							<div className="join w-full shadow-sm max-w-lg">
								<input
									type="text"
									className="input input-bordered join-item flex-1 bg-base-100 font-mono text-xs overflow-hidden"
									value={config.api_key || "Not Generated"}
									readOnly
								/>
								{config.api_key && (
									<button type="button" className="btn btn-ghost border-base-300 join-item px-4" onClick={handleCopyAPIKey}>
										<Copy className="h-4 w-4" />
									</button>
								)}
							</div>

							<div className="flex justify-start">
								<button
									type="button"
									className="btn btn-ghost btn-xs border-base-300 bg-base-100 hover:bg-base-200"
									onClick={handleRegenerateAPIKey}
									disabled={regenerateAPIKey.isPending}
								>
									{regenerateAPIKey.isPending ? <LoadingSpinner size="sm" /> : <RefreshCw className="h-3 w-3" />}
									Regenerate Token
								</button>
							</div>
						</div>
					</div>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end pt-4 border-t border-base-200">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && 'btn-ghost border-base-300'}`}
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
