import { Download, HardDrive, History, Save, ShieldAlert, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useBatchExportNZB } from "../../hooks/useConfig";
import type { ConfigResponse, MetadataBackupConfig, MetadataConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface MetadataConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: MetadataConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function MetadataConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: MetadataConfigSectionProps) {
	const [formData, setFormData] = useState<MetadataConfig>(config.metadata);
	const [hasChanges, setHasChanges] = useState(false);
	const batchExport = useBatchExportNZB();

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.metadata);
		setHasChanges(false);
	}, [config.metadata]);

	const handleInputChange = (field: keyof MetadataConfig, value: string) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.metadata));
	};

	const handleCheckboxChange = (field: keyof MetadataConfig, value: boolean) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.metadata));
	};

	const handleBackupChange = (
		field: keyof MetadataBackupConfig,
		value: string | number | boolean,
	) => {
		const newData = {
			...formData,
			backup: {
				...formData.backup,
				[field]: value,
			},
		};
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.metadata));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("metadata", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">Metadata Engine</h3>
				<p className="break-words text-base-content/50 text-sm">
					Configure how AltMount stores and manages virtual file metadata.
				</p>
			</div>

			<div className="space-y-8">
				{/* Storage Path */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<HardDrive className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Primary Storage
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend whitespace-normal font-semibold md:whitespace-nowrap">
							Metadata Root Directory
						</legend>
						<div className="flex flex-col gap-3">
							<input
								type="text"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.root_path}
								readOnly={isReadOnly}
								onChange={(e) => handleInputChange("root_path", e.target.value)}
								placeholder="/path/to/metadata"
								required
							/>
							<div className="mt-2 whitespace-normal text-base-content/50 text-xs leading-relaxed">
								Path where .meta files (pointers to Usenet articles) will be saved. (Required)
							</div>
						</div>
					</fieldset>
				</div>

				{/* Backup Options */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<History className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Mirroring
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="space-y-6">
						<div className="flex items-center justify-between gap-4">
							<div className="min-w-0 flex-1">
								<h5 className="font-bold text-sm">Automatic Backups</h5>
								<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
									Mirrors all metadata files to an external directory for disaster recovery.
								</p>
							</div>
							<input
								type="checkbox"
								className="toggle toggle-primary toggle-sm"
								checked={formData.backup?.enabled ?? false}
								disabled={isReadOnly}
								onChange={(e) => handleBackupChange("enabled", e.target.checked)}
							/>
						</div>

						{formData.backup?.enabled && (
							<div className="fade-in slide-in-from-top-2 animate-in space-y-6 pt-2">
								<fieldset className="fieldset">
									<legend className="fieldset-legend font-semibold">Backup Target Path</legend>
									<input
										type="text"
										className="input input-bordered w-full bg-base-100 font-mono text-sm"
										value={formData.backup?.path ?? ""}
										disabled={isReadOnly}
										onChange={(e) => handleBackupChange("path", e.target.value)}
										placeholder="/path/to/backups"
									/>
								</fieldset>

								<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
									<fieldset className="fieldset">
										<legend className="fieldset-legend font-semibold">Interval (Hours)</legend>
										<input
											type="number"
											className="input input-bordered w-full bg-base-100 font-mono text-sm"
											value={formData.backup?.interval_hours ?? 24}
											disabled={isReadOnly}
											onChange={(e) =>
												handleBackupChange(
													"interval_hours",
													Number.parseInt(e.target.value, 10) || 24,
												)
											}
											min="1"
										/>
									</fieldset>
									<fieldset className="fieldset">
										<legend className="fieldset-legend font-semibold">Retention (Count)</legend>
										<input
											type="number"
											className="input input-bordered w-full bg-base-100 font-mono text-sm"
											value={formData.backup?.keep_backups ?? 10}
											disabled={isReadOnly}
											onChange={(e) =>
												handleBackupChange(
													"keep_backups",
													Number.parseInt(e.target.value, 10) || 10,
												)
											}
											min="1"
										/>
									</fieldset>
								</div>
							</div>
						)}
					</div>
				</div>

				{/* Retention Logic */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Trash2 className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Source Cleanup
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="space-y-4">
						<label className="label cursor-pointer items-start justify-start gap-4">
							<input
								type="checkbox"
								className="checkbox checkbox-primary checkbox-sm mt-1 shrink-0"
								checked={formData.delete_source_nzb_on_removal ?? false}
								disabled={isReadOnly}
								onChange={(e) =>
									handleCheckboxChange("delete_source_nzb_on_removal", e.target.checked)
								}
							/>
							<div className="min-w-0 flex-1">
								<span className="block whitespace-normal break-words font-bold text-xs">
									Purge Source NZB
								</span>
								<span className="mt-1 block whitespace-normal break-words text-base-content/50 text-xs leading-relaxed">
									Delete original NZB file when metadata is manually removed from AltMount.
								</span>
							</div>
						</label>

						<label className="label cursor-pointer items-start justify-start gap-4">
							<input
								type="checkbox"
								className="checkbox checkbox-primary checkbox-sm mt-1 shrink-0"
								checked={formData.delete_failed_nzb ?? true}
								disabled={isReadOnly}
								onChange={(e) => handleCheckboxChange("delete_failed_nzb", e.target.checked)}
							/>
							<div className="min-w-0 flex-1">
								<span className="block whitespace-normal break-words font-bold text-xs">
									Clean Failed NZBs
								</span>
								<span className="mt-1 block whitespace-normal break-words text-base-content/50 text-xs leading-relaxed">
									Permanently delete NZBs that fail processing instead of moving to 'failed' folder.
								</span>
							</div>
						</label>

						<label className="label cursor-pointer items-start justify-start gap-4">
							<input
								type="checkbox"
								className="checkbox checkbox-error checkbox-sm mt-1 shrink-0"
								checked={formData.delete_completed_nzb ?? false}
								disabled={isReadOnly}
								onChange={(e) => handleCheckboxChange("delete_completed_nzb", e.target.checked)}
							/>
							<div className="min-w-0 flex-1">
								<div className="flex items-center gap-2">
									<span className="whitespace-normal break-words font-bold text-xs">
										Aggressive Cleanup
									</span>
									<div className="badge badge-error badge-xs shrink-0 font-black text-[8px] uppercase">
										Dangerous
									</div>
								</div>
								<span className="mt-1 block whitespace-normal break-words text-base-content/50 text-xs leading-relaxed">
									Delete original NZB immediately after metadata generation. Cannot re-scan without
									re-upload.
								</span>
							</div>
						</label>
					</div>
				</div>

				{/* Utility Actions */}
				<div className="space-y-6 rounded-2xl border border-warning/20 bg-warning/5 p-6">
					<div className="flex items-center gap-2 text-warning">
						<ShieldAlert className="h-4 w-4" />
						<h4 className="font-bold text-xs uppercase tracking-widest">Maintenance Utility</h4>
					</div>

					<div className="space-y-4">
						<div className="min-w-0">
							<h5 className="font-bold text-sm">Disaster Recovery Export</h5>
							<p className="mt-1 break-words text-[11px] leading-relaxed opacity-70">
								Generates a single ZIP containing all your metadata as raw NZB files. Essential for
								migration or manual reconstruction.
							</p>
						</div>
						<button
							type="button"
							className="btn btn-warning btn-sm px-8 shadow-sm"
							onClick={() => batchExport.mutate("/")}
							disabled={batchExport.isPending || !formData.root_path.trim()}
						>
							{batchExport.isPending ? (
								<LoadingSpinner size="sm" />
							) : (
								<Download className="h-4 w-4" />
							)}
							Batch Export NZBs
						</button>
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
						disabled={!hasChanges || isUpdating || !formData.root_path.trim()}
					>
						{isUpdating ? <LoadingSpinner size="sm" /> : <Save className="h-4 w-4" />}
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
