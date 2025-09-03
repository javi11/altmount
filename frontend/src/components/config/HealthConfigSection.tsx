import { Save, AlertTriangle } from "lucide-react";
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

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.health);
		setHasChanges(false);
	}, [config.health]);

	const handleInputChange = (field: keyof HealthConfig, value: string | boolean | number) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.health));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("health", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">Health Monitoring Settings</h3>
			<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Enable Health Monitoring</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Monitor file health</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("enabled", e.target.checked)}
						/>
					</label>
					<p className="label text-gray-600 text-sm">
						Enable automatic health checking of media files
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Auto-Repair Corrupted Files</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Enable automatic repair</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.auto_repair_enabled}
							disabled={isReadOnly || !formData.enabled}
							onChange={(e) => handleInputChange("auto_repair_enabled", e.target.checked)}
						/>
					</label>
					<div className="alert alert-warning mt-2">
						<AlertTriangle className="h-4 w-4" />
						<div className="text-sm">
							<strong>Warning:</strong> When enabled, corrupted files will automatically 
							trigger re-download through connected ARRs (Radarr/Sonarr), and corrupted files will be automatically DELETED. Disable for manual control.
						</div>
					</div>
				</fieldset>
			</div>

			{/* Advanced Settings (Optional) */}
			{formData.enabled && (
				<details className="collapse bg-base-200">
					<summary className="collapse-title font-medium">Advanced Health Settings</summary>
					<div className="collapse-content">
						<div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
							{formData.max_concurrent_jobs !== undefined && (
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Max Concurrent Jobs</legend>
									<input
										type="number"
										className="input"
										value={formData.max_concurrent_jobs}
										readOnly={isReadOnly}
										min={1}
										max={10}
										onChange={(e) => handleInputChange("max_concurrent_jobs", Number.parseInt(e.target.value, 10) || 1)}
									/>
									<p className="label text-gray-600 text-sm">
										Maximum number of concurrent health check jobs
									</p>
								</fieldset>
							)}

							{formData.max_retries !== undefined && (
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Max Retries</legend>
									<input
										type="number"
										className="input"
										value={formData.max_retries}
										readOnly={isReadOnly}
										min={0}
										max={5}
										onChange={(e) => handleInputChange("max_retries", Number.parseInt(e.target.value, 10) || 0)}
									/>
									<p className="label text-gray-600 text-sm">
										Maximum number of retry attempts for failed checks
									</p>
								</fieldset>
							)}

							{formData.check_all_segments !== undefined && (
								<fieldset className="fieldset">
									<legend className="fieldset-legend">Check All Segments</legend>
									<label className="label cursor-pointer">
										<span className="label-text">Deep segment checking</span>
										<input
											type="checkbox"
											className="checkbox"
											checked={formData.check_all_segments}
											disabled={isReadOnly}
											onChange={(e) => handleInputChange("check_all_segments", e.target.checked)}
										/>
									</label>
									<p className="label text-gray-600 text-sm">
										Check all file segments (slower but more thorough)
									</p>
								</fieldset>
							)}
						</div>
					</div>
				</details>
			)}

			{/* Save Button */}
			{onUpdate && !isReadOnly && hasChanges && (
				<div className="flex justify-end">
					<button
						type="button"
						className={`btn btn-primary ${isUpdating ? "loading" : ""}`}
						disabled={isUpdating}
						onClick={handleSave}
					>
						{isUpdating ? (
							"Saving..."
						) : (
							<>
								<Save className="h-4 w-4" />
								Save Health Settings
							</>
						)}
					</button>
				</div>
			)}
		</div>
	);
}