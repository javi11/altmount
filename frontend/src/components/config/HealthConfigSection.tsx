import { AlertTriangle, Save, TestTube } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, DryRunSyncResult, HealthConfig } from "../../types/config";

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
	const [dryRunLoading, setDryRunLoading] = useState(false);
	const [dryRunResult, setDryRunResult] = useState<DryRunSyncResult | null>(null);

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
		if (data.cleanup_orphaned_files && !data.library_dir?.trim()) {
			return "Library Directory is required when file cleanup is enabled";
		}
		return "";
	};

	// Handle dry run
	const handleDryRun = async () => {
		if (!formData.library_dir?.trim()) {
			return;
		}

		setDryRunLoading(true);
		setDryRunResult(null);

		try {
			const response = await fetch("/api/health/library-sync/dry-run", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
				},
			});

			if (!response.ok) {
				throw new Error(`HTTP error! status: ${response.status}`);
			}

			const data = await response.json();
			if (data.success && data.data) {
				setDryRunResult(data.data);
			} else {
				throw new Error(data.error || "Failed to perform dry run");
			}
		} catch (error) {
			console.error("Dry run failed:", error);
			// Optionally show an error toast here
		} finally {
			setDryRunLoading(false);
		}
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
					{formData.enabled && (
						<div className="mt-3 space-y-2">
							<p className="label font-medium text-sm">How it works:</p>
							<ol className="ml-2 list-inside list-decimal space-y-1 text-sm">
								<li>
									<strong>Periodic sync:</strong> The system periodically syncs with the library
									directory to discover files.
								</li>
								<li>
									<strong>Smart scheduling:</strong> Each record is checked based on its release
									date × 2. For example, if a file was released 1 day ago, the next check will be
									scheduled 1 day after (2 days total).
								</li>
								<li>
									<strong>Health validation:</strong> Files are validated through partial segment
									checks or full checks. If a file is found to be unhealthy, a repair is
									automatically triggered.
								</li>
								<li>
									<strong>Automatic repair:</strong> When unhealthy files are detected, the system
									deletes the file in the ARR application and triggers a redownload to restore file
									integrity.
								</li>
							</ol>
						</div>
					)}
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Library Directory</legend>
					<input
						type="text"
						className={`input ${validationError && formData.enabled ? "input-error border-error" : ""
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
					<p className="label text-sm">
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
						<span className="label-text">Cleanup orphaned files</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.cleanup_orphaned_files ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("cleanup_orphaned_files", e.target.checked)}
						/>
					</label>
					{formData.cleanup_orphaned_files && !formData.library_dir?.trim() && (
						<div className="alert alert-warning mt-2">
							<AlertTriangle className="h-4 w-4" />
							<span className="text-sm">
								Library Directory must be configured to enable file cleanup
							</span>
						</div>
					)}
					{formData.cleanup_orphaned_files && formData.library_dir?.trim() && (
						<div className="alert alert-warning mt-2">
							<AlertTriangle className="h-4 w-4" />
							<div className="text-sm">
								<strong>Important:</strong> When enabled, this feature will automatically DELETE
								orphaned library files and metadata files during sync. This helps prevent storage
								waste from orphaned files, but files will be permanently removed.
							</div>
						</div>
					)}
					<p className="label text-sm">
						Automatically delete orphaned library files and metadata during library sync. When
						disabled, no files will be deleted.
						{formData.cleanup_orphaned_files && (
							<strong className="text-warning">
								{" "}
								Requires Library Directory to be configured.
							</strong>
						)}
					</p>

					{/* Dry Run Button */}
					<div className="mt-4">
						<button
							type="button"
							className="btn btn-outline btn-sm"
							onClick={handleDryRun}
							disabled={!formData.library_dir?.trim() || dryRunLoading || isReadOnly}
						>
							{dryRunLoading ? (
								<>
									<span className="loading loading-spinner loading-sm" />
									Running...
								</>
							) : (
								<>
									<TestTube className="h-4 w-4" />
									Test Sync (Dry Run)
								</>
							)}
						</button>
						<p className="label mt-1 text-xs">
							Preview what would be deleted without actually deleting anything
						</p>
					</div>

					{/* Dry Run Results */}
					{dryRunResult && (
						<div
							className={`alert ${dryRunResult.would_cleanup ? "alert-warning" : "alert-info"} mt-4`}
						>
							<div className="w-full">
								<div className="flex items-start justify-between">
									<div className="flex items-center gap-2">
										<TestTube className="h-5 w-5" />
										<h4 className="font-semibold">Dry Run Results</h4>
									</div>
								</div>
								<div className="mt-3 space-y-2 text-sm">
									<div className="grid grid-cols-2 gap-2">
										<div>Metadata files to be deleted:</div>
										<div className="font-mono font-semibold">
											{dryRunResult.orphaned_metadata_count} files
										</div>
										<div>Library files to be deleted:</div>
										<div className="font-mono font-semibold">
											{dryRunResult.orphaned_library_files} files
										</div>
										<div>Database Records:</div>
										<div className="font-mono font-semibold">
											{dryRunResult.database_records_to_clean} records
										</div>
									</div>
									<div className="divider my-2" />
									<div className="font-semibold">
										{dryRunResult.would_cleanup ? (
											<span className="text-warning">
												These files WOULD BE DELETED in a real sync (cleanup enabled)
											</span>
										) : (
											<span className="text-info">
												No files would be deleted (cleanup disabled)
											</span>
										)}
									</div>
								</div>
							</div>
						</div>
					)}
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
								<p className="label text-sm">
									Time between automatic health checks (default: 5 seconds)
								</p>
							</fieldset>
						)}
						{formData.max_connections_for_health_checks !== undefined && (
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Max Connections for Health Checks</legend>
								<input
									type="number"
									className="input"
									value={formData.max_connections_for_health_checks}
									readOnly={isReadOnly}
									min={1}
									max={10}
									onChange={(e) =>
										handleInputChange(
											"max_connections_for_health_checks",
											Number.parseInt(e.target.value, 10) || 1,
										)
									}
								/>
								<p className="label text-sm">
									Maximum concurrent connections used during health check operations
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
							<p className="label text-sm">
								When disabled, use a sampling approach for faster processing. Enable for thorough
								validation of all segments (slower).
							</p>
						</fieldset>
						{formData.segment_sample_percentage !== undefined && !formData.check_all_segments && (
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Segment Sample Percentage</legend>
								<input
									type="number"
									className="input"
									value={formData.segment_sample_percentage}
									readOnly={isReadOnly}
									min={1}
									max={100}
									step={1}
									onChange={(e) =>
										handleInputChange(
											"segment_sample_percentage",
											Number.parseInt(e.target.value, 10) || 5,
										)
									}
								/>
								<p className="label text-sm">
									Percentage of segments to check when sampling is enabled (1-100%, default: 5%).
								</p>
							</fieldset>
						)}
						{formData.library_sync_interval_minutes !== undefined && (
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Library Sync Interval (minutes)</legend>
								<input
									type="number"
									className="input"
									value={formData.library_sync_interval_minutes}
									readOnly={isReadOnly}
									min={0}
									max={1440}
									step={30}
									onChange={(e) =>
										handleInputChange(
											"library_sync_interval_minutes",
											Number.parseInt(e.target.value, 10) || 0,
										)
									}
								/>
								<p className="label text-sm">
									How often to sync the library directory to discover new files (0-1440 minutes).
									Set to 0 to disable automatic sync. Default: 360 minutes (6 hours).
								</p>
							</fieldset>
						)}
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
