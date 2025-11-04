import { AlertTriangle, Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, HealthConfig } from "../../types/config";

interface HealthConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: HealthConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function HealthConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: HealthConfigSectionProps) {
	const [formData, setFormData] = useState<HealthConfig>(config.health);
	const [hasChanges, setHasChanges] = useState(false);
	const [validationError, setValidationError] = useState<string>("");

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.health);
		setHasChanges(false);
		setValidationError("");
	}, [config.health]);

	// Validate form data
	const validateFormData = (data: HealthConfig): string => {
		if (data.enabled && !data.library_dir?.trim()) {
			return "Library Directory is required when Health System is enabled";
		}
		if (data.cleanup_orphaned_metadata && !data.library_dir?.trim()) {
			return "Library Directory is required when metadata cleanup is enabled";
		}
		return "";
	};

	const handleInputChange = (
		field: keyof HealthConfig,
		value: string | boolean | number | undefined,
	) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.health));
		setValidationError(validateFormData(newData));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges && !validationError) {
			await onUpdate("health", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">Health & Repair</h3>
			<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Health System</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Enable health system</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("enabled", e.target.checked)}
						/>
					</label>
					<div className="alert alert-warning mt-2">
						<AlertTriangle className="h-4 w-4" />
						<div className="text-sm">
							<strong>Warning:</strong> When enabled, the health system will monitor file integrity
							and automatically trigger re-downloads through connected ARRs (Radarr/Sonarr) for
							corrupted files. Corrupted files will be automatically DELETED during repair. Disable
							to turn off all health monitoring.
						</div>
					</div>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Library Directory</legend>
					<input
						type="text"
						className={`input ${
							validationError && formData.enabled ? "input-error border-error" : ""
						}`}
						value={formData.library_dir || ""}
						disabled={isReadOnly}
						placeholder="/media/library"
						onChange={(e) => handleInputChange("library_dir", e.target.value || undefined)}
					/>
					{validationError && formData.enabled && (
						<div className="alert alert-error mt-2">
							<AlertTriangle className="h-4 w-4" />
							<span className="text-sm">{validationError}</span>
						</div>
					)}
					<p className="label text-gray-600 text-sm">
						Path to your organized media library that contains symlinks pointing to altmount files.
						When a repair is triggered, the system will search for symlinks in this directory and
						use the library path for ARR rescan instead of the mount path.
						{formData.enabled && (
							<strong className="text-error"> Required when Health System is enabled.</strong>
						)}
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Cleanup Settings</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Cleanup orphaned metadata files</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.cleanup_orphaned_metadata ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("cleanup_orphaned_metadata", e.target.checked)}
						/>
					</label>
					{formData.cleanup_orphaned_metadata && !formData.library_dir?.trim() && (
						<div className="alert alert-warning mt-2">
							<AlertTriangle className="h-4 w-4" />
							<span className="text-sm">
								Library Directory must be configured to enable metadata cleanup
							</span>
						</div>
					)}
					<p className="label text-gray-600 text-sm">
						Automatically delete metadata files that don't have corresponding symlinks in the
						library directory. This helps prevent storage waste from orphaned metadata files.
						{formData.cleanup_orphaned_metadata && (
							<strong className="text-warning">
								{" "}
								Requires Library Directory to be configured.
							</strong>
						)}
					</p>
				</fieldset>
			</div>

			{/* Advanced Settings (Optional) */}
			<details className="collapse bg-base-200">
				<summary className="collapse-title font-medium">Advanced Settings</summary>
				<div className="collapse-content">
					<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
						{formData.check_interval_seconds !== undefined && (
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Check Interval (seconds)</legend>
								<input
									type="number"
									className="input"
									value={formData.check_interval_seconds}
									readOnly={isReadOnly}
									min={5}
									max={86400}
									step={1}
									onChange={(e) =>
										handleInputChange(
											"check_interval_seconds",
											Number.parseInt(e.target.value, 10) || 5,
										)
									}
								/>
								<p className="label text-gray-600 text-sm">
									Time between automatic health checks (default: 5 seconds)
								</p>
							</fieldset>
						)}
						{formData.max_connections_for_repair !== undefined && (
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Max Connections for Repair</legend>
								<input
									type="number"
									className="input"
									value={formData.max_connections_for_repair}
									readOnly={isReadOnly}
									min={1}
									max={10}
									onChange={(e) =>
										handleInputChange(
											"max_connections_for_repair",
											Number.parseInt(e.target.value, 10) || 1,
										)
									}
								/>
								<p className="label text-gray-600 text-sm">
									Maximum concurrent connections used during repair operations
								</p>
							</fieldset>
						)}
						<fieldset className="fieldset">
							<legend className="fieldset-legend">Check All Segments</legend>
							<label className="label cursor-pointer">
								<span className="label-text">Deep segment checking</span>
								<input
									type="checkbox"
									className="checkbox"
									checked={formData.check_all_segments ?? false}
									disabled={isReadOnly}
									onChange={(e) => handleInputChange("check_all_segments", e.target.checked)}
								/>
							</label>
							<p className="label text-gray-600 text-sm">
								When disabled, use a sampling approach for faster processing. Enable for thorough
								validation of all segments (slower).
							</p>
						</fieldset>
					</div>
				</div>
			</details>

			{/* Save Button */}
			{onUpdate && !isReadOnly && hasChanges && (
				<div className="flex flex-col items-end gap-2">
					{validationError && (
						<div className="alert alert-error">
							<AlertTriangle className="h-4 w-4" />
							<span className="text-sm">{validationError}</span>
						</div>
					)}
					<button
						type="button"
						className={`btn btn-primary ${isUpdating ? "loading" : ""}`}
						disabled={isUpdating || !!validationError}
						onClick={handleSave}
					>
						{isUpdating ? (
							"Saving..."
						) : (
							<>
								<Save className="h-4 w-4" />
								Save Settings
							</>
						)}
					</button>
				</div>
			)}
		</div>
	);
}
