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
		<div className="space-y-10">
			{/* Health System Status */}
			<section className="space-y-4">
				<div className="mb-2 flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
						Service Status
					</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="card border border-base-300 bg-base-200/50 shadow-sm">
					<div className="card-body p-4 sm:p-6">
						<div className="flex items-center justify-between gap-4">
							<div className="flex-1">
								<h3 className="font-bold text-sm sm:text-base">Enable Health Monitoring</h3>
								<p className="mt-1 text-base-content/60 text-xs leading-relaxed">
									Automatically scan your library for corrupted or missing segments and trigger
									repairs.
								</p>
							</div>
							<input
								type="checkbox"
								className="toggle toggle-primary"
								checked={formData.enabled}
								onChange={(e) => handleInputChange("enabled", e.target.checked)}
								disabled={isReadOnly}
							/>
						</div>
					</div>
				</div>
			</section>

			{formData.enabled && (
				<div className="fade-in animate-in space-y-10 duration-500">
					{/* Library Synchronization Section */}
					<section className="space-y-6">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
								Library Discovery
							</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="grid grid-cols-1 gap-8 lg:grid-cols-2">
							<div className="space-y-6">
								<fieldset className="fieldset min-w-0">
									<legend className="fieldset-legend font-semibold">Library Root Path</legend>
									<input
										type="text"
										className={`input w-full bg-base-200/50 ${validationError ? "input-error border-error" : ""}`}
										value={formData.library_dir || ""}
										disabled={isReadOnly}
										placeholder="/media/library"
										onChange={(e) => handleInputChange("library_dir", e.target.value || undefined)}
									/>
									{validationError && (
										<div className="alert alert-error mt-2 py-2 text-[10px]">
											<AlertTriangle className="h-3 w-3 shrink-0" />
											<span>{validationError}</span>
										</div>
									)}
									<p className="label text-[10px] opacity-60">
										Base directory where your media is organized.
									</p>
								</fieldset>

								<div className="flex flex-wrap items-center gap-6">
									<label className="label cursor-pointer justify-start gap-3 p-0">
										<input
											type="checkbox"
											className="checkbox checkbox-sm checkbox-primary"
											checked={formData.cleanup_orphaned_metadata ?? false}
											disabled={isReadOnly}
											onChange={(e) =>
												handleInputChange("cleanup_orphaned_metadata", e.target.checked)
											}
										/>
										<div className="flex flex-col">
											<span className="label-text font-semibold text-xs">
												Auto-Cleanup Orphaned Files
											</span>
											<span className="label-text-alt text-[9px] opacity-60">
												Delete missing library/meta files
											</span>
										</div>
									</label>

									<label className="label cursor-pointer justify-start gap-3 p-0">
										<input
											type="checkbox"
											className="checkbox checkbox-sm checkbox-primary"
											checked={formData.resolve_repair_on_import ?? false}
											disabled={isReadOnly}
											onChange={(e) =>
												handleInputChange("resolve_repair_on_import", e.target.checked)
											}
										/>
										<div className="flex flex-col">
											<span className="label-text font-semibold text-xs">Resolve on Import</span>
											<span className="label-text-alt text-[9px] opacity-60">
												Auto-fix repairs when new file arrives
											</span>
										</div>
									</label>
								</div>
							</div>

							<div className="space-y-4">
								<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
									Test Sync Strategy
								</h5>
								<div className="rounded-xl border border-base-300 bg-base-200/30 p-4">
									<button
										type="button"
										className="btn btn-outline btn-xs mb-4 w-full"
										onClick={handleDryRun}
										disabled={!formData.library_dir?.trim() || dryRunLoading || isReadOnly}
									>
										{dryRunLoading ? (
											<span className="loading loading-spinner loading-xs" />
										) : (
											<TestTube className="h-3 w-3" />
										)}
										Perform Dry Run
									</button>

									{dryRunResult ? (
										<div className="grid grid-cols-2 gap-x-4 gap-y-2 font-mono text-[10px]">
											<span className="uppercase opacity-60">Metadata Orphans:</span>
											<span className="text-right font-bold">
												{dryRunResult.orphaned_metadata_count}
											</span>
											<span className="uppercase opacity-60">Library Orphans:</span>
											<span className="text-right font-bold">
												{dryRunResult.orphaned_library_files}
											</span>
											<span className="uppercase opacity-60">Stale DB Records:</span>
											<span className="text-right font-bold">
												{dryRunResult.database_records_to_clean}
											</span>
										</div>
									) : (
										<p className="py-2 text-center text-[10px] italic opacity-40">
											No test data available.
										</p>
									)}
								</div>
							</div>
						</div>
					</section>

					{/* Performance & Validation Tuning Section */}
					<section className="space-y-6">
						<div className="flex items-center gap-2">
							<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">
								Validation & Performance
							</h4>
							<div className="h-px flex-1 bg-base-300" />
						</div>

						<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-4">
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Max Connections</legend>
								<input
									type="number"
									className="input input-sm w-full bg-base-200/50 font-mono"
									value={formData.max_connections_for_health_checks}
									disabled={isReadOnly}
									min={1}
									onChange={(e) =>
										handleInputChange(
											"max_connections_for_health_checks",
											Number.parseInt(e.target.value, 10) || 1,
										)
									}
								/>
								<p className="label text-[9px] opacity-50">Concurrent NNTP requests.</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Max Concurrent Jobs</legend>
								<input
									type="number"
									className="input input-sm w-full bg-base-200/50 font-mono"
									value={formData.max_concurrent_jobs}
									disabled={isReadOnly}
									min={1}
									onChange={(e) =>
										handleInputChange(
											"max_concurrent_jobs",
											Number.parseInt(e.target.value, 10) || 1,
										)
									}
								/>
								<p className="label text-[9px] opacity-50">Simultaneous file scans.</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Scan Interval (s)</legend>
								<input
									type="number"
									className="input input-sm w-full bg-base-200/50 font-mono"
									value={formData.check_interval_seconds}
									disabled={isReadOnly}
									min={5}
									onChange={(e) =>
										handleInputChange(
											"check_interval_seconds",
											Number.parseInt(e.target.value, 10) || 5,
										)
									}
								/>
								<p className="label text-[9px] opacity-50">Queue processing delay.</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Sync Interval (m)</legend>
								<input
									type="number"
									className="input input-sm w-full bg-base-200/50 font-mono"
									value={formData.library_sync_interval_minutes}
									disabled={isReadOnly}
									min={0}
									onChange={(e) =>
										handleInputChange(
											"library_sync_interval_minutes",
											Number.parseInt(e.target.value, 10) || 0,
										)
									}
								/>
								<p className="label text-[9px] opacity-50">Library discovery frequency.</p>
							</fieldset>
						</div>

						<div className="mt-4 grid grid-cols-1 gap-8 rounded-2xl border border-base-300 bg-base-200/30 p-6 lg:grid-cols-2">
							<div className="space-y-4">
								<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
									Verification Strategy
								</h5>
								<div className="flex flex-wrap gap-x-8 gap-y-4">
									<label className="label cursor-pointer justify-start gap-3 p-0">
										<input
											type="checkbox"
											className="checkbox checkbox-sm checkbox-primary"
											checked={formData.check_all_segments ?? false}
											disabled={isReadOnly}
											onChange={(e) => handleInputChange("check_all_segments", e.target.checked)}
										/>
										<div className="flex flex-col">
											<span className="label-text font-semibold text-xs">
												Deep Check (All Segments)
											</span>
											<span className="label-text-alt text-[9px] opacity-60">
												Full file validation (Slower)
											</span>
										</div>
									</label>

									{!formData.check_all_segments && (
										<label className="label cursor-pointer justify-start gap-3 p-0">
											<input
												type="checkbox"
												className="checkbox checkbox-sm checkbox-primary"
												checked={formData.verify_data ?? false}
												disabled={isReadOnly}
												onChange={(e) => handleInputChange("verify_data", e.target.checked)}
											/>
											<div className="flex flex-col">
												<span className="label-text font-semibold text-xs">
													Hybrid Verification
												</span>
												<span className="label-text-alt text-[9px] opacity-60">
													Read 1-byte per segment
												</span>
											</div>
										</label>
									)}
								</div>
							</div>

							{!formData.check_all_segments && (
								<div className="space-y-4">
									<h5 className="border-base-200 border-b pb-1 font-bold text-[10px] text-base-content/40 uppercase tracking-widest">
										Random Sampling
									</h5>
									<fieldset className="fieldset min-w-0 p-0">
										<div className="flex items-center gap-4">
											<input
												type="range"
												min="1"
												max="100"
												value={formData.segment_sample_percentage}
												className="range range-primary range-xs flex-1"
												onChange={(e) =>
													handleInputChange(
														"segment_sample_percentage",
														Number.parseInt(e.target.value, 10),
													)
												}
											/>
											<span className="w-12 text-right font-bold font-mono text-xs">
												{formData.segment_sample_percentage}%
											</span>
										</div>
										<p className="label mt-1 text-[9px] opacity-50">
											Percentage of segments to randomly verify per file.
										</p>
									</fieldset>
								</div>
							)}
						</div>
					</section>

					{/* Save Button */}
					{!isReadOnly && (
						<div className="flex justify-end border-base-200 border-t pt-6">
							<button
								type="button"
								className={`btn btn-primary btn-md px-10 ${hasChanges ? "shadow-lg shadow-primary/20" : ""}`}
								onClick={handleSave}
								disabled={isUpdating || !!validationError || !hasChanges}
							>
								{isUpdating ? (
									<span className="loading loading-spinner loading-sm" />
								) : (
									<Save className="h-4 w-4" />
								)}
								Save Health Configuration
							</button>
						</div>
					)}
				</div>
			)}
		</div>
	);
}
