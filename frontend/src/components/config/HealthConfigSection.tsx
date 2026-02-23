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
				<h3 className="font-bold text-base-content text-lg tracking-tight">Auto-Repair System</h3>
				<p className="break-words text-base-content/50 text-sm">
					Monitor and automatically repair corrupted library files.
				</p>
			</div>

			<div className="space-y-8">
				{/* Enable Health Toggle */}
				<div className="rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h4 className="break-words font-bold text-base-content text-sm">Master Engine</h4>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Activate background monitoring and automatic re-downloads.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary mt-1 shrink-0"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("enabled", e.target.checked)}
						/>
					</div>

					{formData.enabled && (
						<div className="fade-in slide-in-from-top-2 mt-6 animate-in border-base-300/50 border-t pt-6">
							<div className="rounded-xl border border-primary/10 bg-primary/5 p-4">
								<div className="mb-3 flex items-center gap-2">
									<Info className="h-4 w-4 shrink-0 text-primary" />
									<span className="break-words font-black text-primary text-xs uppercase tracking-widest">
										Workflow Overview
									</span>
								</div>
								<ul className="space-y-3">
									<li className="flex gap-3 text-xs leading-relaxed">
										<span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/20 font-black text-xs">
											1
										</span>
										<span className="min-w-0 flex-1 break-words">
											Discover files via periodic library sync.
										</span>
									</li>
									<li className="flex gap-3 text-xs leading-relaxed">
										<span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/20 font-black text-xs">
											2
										</span>
										<span className="min-w-0 flex-1 break-words">
											Validate Usenet integrity using sampling or deep checks.
										</span>
									</li>
									<li className="flex gap-3 text-xs leading-relaxed">
										<span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/20 font-black text-xs">
											3
										</span>
										<span className="min-w-0 flex-1 break-words font-bold">
											Unhealthy files are automatically replaced in Sonarr/Radarr.
										</span>
									</li>
								</ul>
							</div>
						</div>
					)}
				</div>

				{/* Directory Configuration */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<fieldset className="fieldset">
						<legend className="fieldset-legend whitespace-normal break-words font-semibold md:whitespace-nowrap">
							Library Parent Directory
						</legend>
						<div className="flex flex-col gap-3">
							<input
								type="text"
								className={`input input-bordered w-full bg-base-100 font-mono text-sm ${validationError && formData.enabled ? "input-error" : ""}`}
								value={formData.library_dir || ""}
								disabled={isReadOnly}
								placeholder="/media/library"
								onChange={(e) => handleInputChange("library_dir", e.target.value || undefined)}
							/>
							<div className="mt-2 whitespace-normal text-base-content/50 text-xs leading-relaxed">
								Path where your permanent media folders (/movies, /tv) are located. Required for
								mapping virtual files to physical ARR library paths.
							</div>
						</div>
					</fieldset>

					<div className="divider text-base-content/70" />

					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h4 className="break-words font-bold text-base-content text-sm">Orphan Cleanup</h4>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Purge database records and metadata for files missing from storage.
							</p>
						</div>
						<input
							type="checkbox"
							className="checkbox checkbox-primary checkbox-sm mt-1 shrink-0"
							checked={formData.cleanup_orphaned_metadata ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("cleanup_orphaned_metadata", e.target.checked)}
						/>
					</div>

					<div className="flex justify-start">
						<button
							type="button"
							className="btn btn-ghost btn-sm border-base-300 bg-base-100 hover:bg-base-200"
							onClick={handleDryRun}
							disabled={!formData.library_dir?.trim() || dryRunLoading || isReadOnly}
						>
							{dryRunLoading ? <LoadingSpinner size="sm" /> : <TestTube className="h-3 w-3" />}
							Dry Run Test
						</button>
					</div>

					{dryRunResult && (
						<div
							className={`alert zoom-in-95 animate-in rounded-xl border p-4 ${dryRunResult.would_cleanup ? "border-warning/20 bg-warning/5" : "border-info/20 bg-info/5"}`}
						>
							<div className="w-full space-y-3">
								<h5 className="flex items-center gap-2 font-black text-base-content/80 text-xs uppercase tracking-widest">
									<TestTube className="h-3 w-3" /> Potential Cleanup Results
								</h5>
								<div className="grid grid-cols-3 gap-2 text-center">
									<div className="rounded-lg border border-base-300/50 bg-base-100 p-2">
										<div className="font-bold font-mono text-lg">
											{dryRunResult.orphaned_metadata_count}
										</div>
										<div className="font-black text-[8px] text-base-content/70 uppercase">
											Metadata
										</div>
									</div>
									<div className="rounded-lg border border-base-300/50 bg-base-100 p-2">
										<div className="font-bold font-mono text-lg">
											{dryRunResult.orphaned_library_files}
										</div>
										<div className="font-black text-[8px] text-base-content/70 uppercase">
											Links
										</div>
									</div>
									<div className="rounded-lg border border-base-300/50 bg-base-100 p-2">
										<div className="font-bold font-mono text-lg">
											{dryRunResult.database_records_to_clean}
										</div>
										<div className="font-black text-[8px] text-base-content/70 uppercase">
											Records
										</div>
									</div>
								</div>
							</div>
						</div>
					)}
				</div>

				{/* Advanced Performance & Logic */}
				<div className="collapse-arrow collapse rounded-2xl border-2 border-base-300/80 bg-base-200/60">
					<input type="checkbox" />
					<div className="collapse-title font-bold text-base-content/80 text-sm uppercase tracking-widest">
						Performance & Deep Validation
					</div>
					<div className="collapse-content space-y-8">
						<div className="grid grid-cols-1 gap-8 pt-4 sm:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend break-words font-semibold">
									Validation Intensity
								</legend>
								<label className="label cursor-pointer items-start justify-start gap-3">
									<input
										type="checkbox"
										className="checkbox checkbox-sm checkbox-primary mt-1 shrink-0"
										checked={formData.check_all_segments ?? false}
										disabled={isReadOnly}
										onChange={(e) => handleInputChange("check_all_segments", e.target.checked)}
									/>
									<div className="min-w-0 flex-1">
										<span className="label-text break-words font-medium text-xs">
											Verify Every Segment (100%)
										</span>
										<p className="label mt-1 break-words text-base-content/70 text-xs leading-relaxed">
											Thorough but very slow for large libraries.
										</p>
									</div>
								</label>
							</fieldset>

							{!formData.check_all_segments && (
								<fieldset className="fieldset">
									<legend className="fieldset-legend break-words font-semibold">
										Ghost File Detection
									</legend>
									<label className="label cursor-pointer items-start justify-start gap-3">
										<input
											type="checkbox"
											className="checkbox checkbox-sm checkbox-primary mt-1 shrink-0"
											checked={formData.verify_data ?? false}
											disabled={isReadOnly}
											onChange={(e) => handleInputChange("verify_data", e.target.checked)}
										/>
										<div className="min-w-0 flex-1">
											<span className="label-text break-words font-medium text-xs">
												Hybrid Data Verification
											</span>
											<p className="label mt-1 break-words text-base-content/70 text-xs leading-relaxed">
												Reads 1 byte from each checked segment to confirm Usenet data exists.
											</p>
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
									<div className="font-black font-mono text-lg text-primary">
										{formData.segment_sample_percentage}%
									</div>
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
										onChange={(e) =>
											handleInputChange(
												"segment_sample_percentage",
												Number.parseInt(e.target.value, 10),
											)
										}
									/>
									<div className="flex justify-between px-2 font-black text-base-content/50 text-xs">
										<span>1% (FAST)</span>
										<span>25%</span>
										<span>50%</span>
										<span>75%</span>
										<span>100% (SLOW)</span>
									</div>
								</div>
							</div>
						)}

						{/* Acceptable Missing Percentage Slider */}
						{formData.acceptable_missing_segments_percentage !== undefined && (
							<div className="space-y-6">
								<div className="flex items-center justify-between">
									<div className="flex items-center gap-2">
										<h5 className="font-bold text-xs">Acceptable Missing Threshold</h5>
										<div
											className="tooltip tooltip-right"
											data-tip="Tolerance for missing data. Files with missing segments below this percentage will be marked as healthy instead of corrupted. Useful for ignoring tiny losses in credits or non-critical parts."
										>
											<Info className="h-3.5 w-3.5 text-base-content/60" />
										</div>
									</div>
									<div className="font-black font-mono text-lg text-primary">
										{formData.acceptable_missing_segments_percentage}%
									</div>
								</div>
								<div className="space-y-4">
									<input
										type="range"
										min="0"
										max="10"
										value={formData.acceptable_missing_segments_percentage}
										className="range range-primary range-sm"
										step="0.1"
										disabled={isReadOnly}
										onChange={(e) =>
											handleInputChange(
												"acceptable_missing_segments_percentage",
												Number.parseFloat(e.target.value),
											)
										}
									/>
									<div className="flex justify-between px-2 font-black text-base-content/50 text-xs">
										<span>0% (STRICT)</span>
										<span>2.5%</span>
										<span>5%</span>
										<span>7.5%</span>
										<span>10% (RELAXED)</span>
									</div>
								</div>
							</div>
						)}

						<div className="grid grid-cols-1 gap-6 pb-4 sm:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">Parallel Processing</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.max_concurrent_jobs}
									readOnly={isReadOnly}
									min={1}
									max={100}
									onChange={(e) =>
										handleInputChange(
											"max_concurrent_jobs",
											Number.parseInt(e.target.value, 10) || 1,
										)
									}
								/>
								<p className="label break-words text-base-content/70 text-xs">
									Max files processed at once.
								</p>
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
									onChange={(e) =>
										handleInputChange(
											"library_sync_interval_minutes",
											Number.parseInt(e.target.value, 10) || 0,
										)
									}
								/>
								<p className="label break-words text-base-content/70 text-xs">
									How often to scan your library for new files.
								</p>
							</fieldset>
						</div>

						<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">
									Health Check Loop Interval (Sec)
								</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.check_interval_seconds}
									readOnly={isReadOnly}
									min={1}
									onChange={(e) =>
										handleInputChange(
											"check_interval_seconds",
											Number.parseInt(e.target.value, 10) || 5,
										)
									}
								/>
								<p className="label break-words text-base-content/70 text-xs">
									Idle time between background health check cycles.
								</p>
							</fieldset>
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">Sync Concurrency</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.library_sync_concurrency}
									readOnly={isReadOnly}
									min={0}
									onChange={(e) =>
										handleInputChange(
											"library_sync_concurrency",
											Number.parseInt(e.target.value, 10) || 0,
										)
									}
								/>
								<p className="label break-words text-base-content/70 text-xs">
									Max parallel file scans during sync (0 = auto).
								</p>
							</fieldset>
						</div>
					</div>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && hasChanges && (
				<div className="flex flex-col items-end gap-4 border-base-200 border-t pt-6">
					{validationError && (
						<div className="alert alert-error rounded-xl px-4 py-2 font-bold text-xs shadow-sm">
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
