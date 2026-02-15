import { AlertTriangle, Info, Save, TestTube } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, DryRunSyncResult, HealthConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

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
		if (config.import.import_strategy !== "NONE") {
			if (data.enabled && !data.library_dir?.trim()) {
				return `Library Directory is required when Health System is enabled with ${config.import.import_strategy} strategy`;
			}
			if (data.cleanup_orphaned_metadata && !data.library_dir?.trim()) {
				return "Library Directory is required when file cleanup is enabled";
			}
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
		<div className="space-y-10">
			<div>
				<h3 className="text-lg font-bold text-base-content tracking-tight">Auto-Repair System</h3>
				<p className="text-sm text-base-content/50 break-words">Monitor and automatically repair corrupted library files.</p>
			</div>

			<div className="space-y-8">
				{/* Enable Health Toggle */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6">
					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h4 className="font-bold text-sm text-base-content break-words">Master Engine</h4>
							<p className="text-[11px] text-base-content/50 mt-1 break-words leading-relaxed">
								Activate background monitoring and automatic re-downloads.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary shrink-0 mt-1"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("enabled", e.target.checked)}
						/>
					</div>

					{formData.enabled && (
						<div className="mt-6 border-t border-base-300/50 pt-6 animate-in fade-in slide-in-from-top-2">
							<div className="rounded-xl bg-primary/5 border border-primary/10 p-4">
								<div className="flex items-center gap-2 mb-3">
									<Info className="h-4 w-4 text-primary shrink-0" />
									<span className="font-black text-[10px] uppercase tracking-widest text-primary break-words">Workflow Overview</span>
								</div>
								<ul className="space-y-3">
									<li className="flex gap-3 text-xs leading-relaxed">
										<span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/20 font-black text-[10px]">1</span>
										<span className="break-words min-w-0 flex-1">Discover files via periodic library sync.</span>
									</li>
									<li className="flex gap-3 text-xs leading-relaxed">
										<span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/20 font-black text-[10px]">2</span>
										<span className="break-words min-w-0 flex-1">Validate Usenet integrity using sampling or deep checks.</span>
									</li>
									<li className="flex gap-3 text-xs leading-relaxed">
										<span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/20 font-black text-[10px]">3</span>
										<span className="break-words min-w-0 flex-1 font-bold">Unhealthy files are automatically replaced in Sonarr/Radarr.</span>
									</li>
								</ul>
							</div>
						</div>
					)}
				</div>

				{/* Directory Configuration */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold break-words">Library Parent Directory</legend>
						<div className="flex flex-col gap-3">
							<input
								type="text"
								className={`input input-bordered w-full bg-base-100 font-mono text-sm ${validationError && formData.enabled ? "input-error" : ""}`}
								value={formData.library_dir || ""}
								disabled={isReadOnly}
								placeholder="/media/library"
								onChange={(e) => handleInputChange("library_dir", e.target.value || undefined)}
							/>
							<p className="label text-[10px] text-base-content/50 break-words leading-relaxed">
								Path where your permanent media folders (/movies, /tv) are located. 
								Required for mapping virtual files to physical ARR library paths.
							</p>
						</div>
					</fieldset>

					<div className="divider opacity-50" />

					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h4 className="font-bold text-sm text-base-content break-words">Orphan Cleanup</h4>
							<p className="text-[11px] text-base-content/50 mt-1 break-words leading-relaxed">
								Purge database records and metadata for files missing from storage.
							</p>
						</div>
						<input
							type="checkbox"
							className="checkbox checkbox-primary checkbox-sm shrink-0 mt-1"
							checked={formData.cleanup_orphaned_metadata ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("cleanup_orphaned_metadata", e.target.checked)}
						/>
					</div>

					<div className="flex justify-start">
						<button
							type="button"
							className="btn btn-ghost btn-xs border-base-300 bg-base-100 hover:bg-base-200"
							onClick={handleDryRun}
							disabled={!formData.library_dir?.trim() || dryRunLoading || isReadOnly}
						>
							{dryRunLoading ? <LoadingSpinner size="sm" /> : <TestTube className="h-3 w-3" />}
							Dry Run Test
						</button>
					</div>

					{dryRunResult && (
						<div className={`alert rounded-xl border p-4 animate-in zoom-in-95 ${dryRunResult.would_cleanup ? "bg-warning/5 border-warning/20" : "bg-info/5 border-info/20"}`}>
							<div className="w-full space-y-3">
								<h5 className="font-black text-[10px] uppercase tracking-widest opacity-60 flex items-center gap-2">
									<TestTube className="h-3 w-3" /> Potential Cleanup Results
								</h5>
								<div className="grid grid-cols-3 gap-2 text-center">
									<div className="bg-base-100 rounded-lg p-2 border border-base-300/50">
										<div className="font-mono font-bold text-lg">{dryRunResult.orphaned_metadata_count}</div>
										<div className="text-[8px] opacity-50 uppercase font-black">Metadata</div>
									</div>
									<div className="bg-base-100 rounded-lg p-2 border border-base-300/50">
										<div className="font-mono font-bold text-lg">{dryRunResult.orphaned_library_files}</div>
										<div className="text-[8px] opacity-50 uppercase font-black">Links</div>
									</div>
									<div className="bg-base-100 rounded-lg p-2 border border-base-300/50">
										<div className="font-mono font-bold text-lg">{dryRunResult.database_records_to_clean}</div>
										<div className="text-[8px] opacity-50 uppercase font-black">Records</div>
									</div>
								</div>
							</div>
						</div>
					)}
				</div>

				{/* Advanced Performance & Logic */}
				<div className="collapse collapse-arrow rounded-2xl border border-base-300 bg-base-200/30">
					<input type="checkbox" /> 
					<div className="collapse-title text-sm font-bold opacity-60 uppercase tracking-widest">
						Performance & Deep Validation
					</div>
					<div className="collapse-content space-y-8">
						<div className="pt-4 grid grid-cols-1 sm:grid-cols-2 gap-8">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold break-words">Validation Intensity</legend>
								<label className="label cursor-pointer justify-start gap-3 items-start">
									<input
										type="checkbox"
										className="checkbox checkbox-sm checkbox-primary mt-1 shrink-0"
										checked={formData.check_all_segments ?? false}
										disabled={isReadOnly}
										onChange={(e) => handleInputChange("check_all_segments", e.target.checked)}
									/>
									<div className="min-w-0 flex-1">
										<span className="label-text text-xs font-medium break-words">Verify Every Segment (100%)</span>
										<p className="label text-[10px] opacity-50 mt-1 break-words leading-relaxed">Thorough but very slow for large libraries.</p>
									</div>
								</label>
							</fieldset>

							{!formData.check_all_segments && (
								<fieldset className="fieldset">
									<legend className="fieldset-legend font-semibold break-words">Ghost File Detection</legend>
									<label className="label cursor-pointer justify-start gap-3 items-start">
										<input
											type="checkbox"
											className="checkbox checkbox-sm checkbox-primary mt-1 shrink-0"
											checked={formData.verify_data ?? false}
											disabled={isReadOnly}
											onChange={(e) => handleInputChange("verify_data", e.target.checked)}
										/>
										<div className="min-w-0 flex-1">
											<span className="label-text text-xs font-medium break-words">Hybrid Data Verification</span>
											<p className="label text-[10px] opacity-50 mt-1 break-words leading-relaxed">Reads 1 byte from each checked segment to confirm Usenet data exists.</p>
										</div>
									</label>
								</fieldset>
							)}
						</div>

						{/* Sample Percentage Slider */}
						{!formData.check_all_segments && formData.segment_sample_percentage !== undefined && (
							<div className="space-y-6">
								<div className="flex items-center justify-between">
									<h5 className="font-bold text-xs">Sampling Percentage</h5>
									<div className="font-mono font-black text-primary text-lg">{formData.segment_sample_percentage}%</div>
								</div>
								<div className="space-y-4">
									<input
										type="range"
										min="1"
										max="100"
										value={formData.segment_sample_percentage}
										className="range range-primary range-sm"
										step="1"
										disabled={isReadOnly}
										onChange={(e) => handleInputChange("segment_sample_percentage", Number.parseInt(e.target.value, 10))}
									/>
									<div className="flex justify-between px-2 text-[9px] font-black opacity-30">
										<span>1% (FAST)</span>
										<span>25%</span>
										<span>50%</span>
										<span>75%</span>
										<span>100% (SLOW)</span>
									</div>
								</div>
							</div>
						)}

						<div className="grid grid-cols-1 sm:grid-cols-2 gap-6 pb-4">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">Parallel Processing</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.max_concurrent_jobs}
									readOnly={isReadOnly}
									min={1}
									max={100}
									onChange={(e) => handleInputChange("max_concurrent_jobs", Number.parseInt(e.target.value, 10) || 1)}
								/>
								<p className="label text-[10px] opacity-50 break-words">Max files processed at once.</p>
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">Sync Interval (Minutes)</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.library_sync_interval_minutes}
									readOnly={isReadOnly}
									min={0}
									max={1440}
									onChange={(e) => handleInputChange("library_sync_interval_minutes", Number.parseInt(e.target.value, 10) || 0)}
								/>
								<p className="label text-[10px] opacity-50 break-words">How often to scan your library for new files.</p>
							</fieldset>
						</div>

						<div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">Health Check Loop Interval (Sec)</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.check_interval_seconds}
									readOnly={isReadOnly}
									min={1}
									onChange={(e) => handleInputChange("check_interval_seconds", Number.parseInt(e.target.value, 10) || 5)}
								/>
								<p className="label text-[10px] opacity-50 break-words">Idle time between background health check cycles.</p>
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">Sync Concurrency</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.library_sync_concurrency}
									readOnly={isReadOnly}
									min={0}
									onChange={(e) => handleInputChange("library_sync_concurrency", Number.parseInt(e.target.value, 10) || 0)}
								/>
								<p className="label text-[10px] opacity-50 break-words">Max parallel file scans during sync (0 = auto).</p>
							</fieldset>
						</div>
					</div>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && hasChanges && (
				<div className="flex flex-col items-end gap-4 border-t border-base-200 pt-6">
					{validationError && (
						<div className="alert alert-error rounded-xl py-2 px-4 text-xs font-bold shadow-sm">
							<AlertTriangle className="h-4 w-4" />
							<span className="break-words">{validationError}</span>
						</div>
					)}
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${isUpdating ? "loading" : ""}`}
						disabled={isUpdating || !!validationError}
						onClick={handleSave}
					>
						{!isUpdating && <Save className="h-4 w-4" />}
						{isUpdating ? "Saving..." : "Save Settings"}
					</button>
				</div>
			)}
		</div>
	);
}
