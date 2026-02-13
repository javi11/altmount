import { Download, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useBatchExportNZB } from "../../hooks/useConfig";
import type { ConfigResponse, MetadataBackupConfig, MetadataConfig } from "../../types/config";

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

	const handleInputChange = (field: keyof MetadataConfig, value: string | boolean) => {
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
			{/* Storage Location Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Storage Location</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">Metadata Root Path</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50"
							value={formData.root_path}
							readOnly={isReadOnly}
							onChange={(e) => handleInputChange("root_path", e.target.value)}
							placeholder="/path/to/metadata"
							required
						/>
						<p className="label text-[10px] opacity-60">Directory path where file metadata (.meta) is stored.</p>
					</fieldset>

					<div className="flex flex-col justify-end pb-1">
						<button
							type="button"
							className="btn btn-outline btn-sm w-full lg:w-auto"
							onClick={() => batchExport.mutate("/")}
							disabled={batchExport.isPending || !formData.root_path.trim()}
						>
							{batchExport.isPending ? <span className="loading loading-spinner loading-xs" /> : <Download className="h-3.5 w-3.5" />}
							Export All Metadata as NZB
						</button>
					</div>
				</div>
			</section>

			{/* Deletion & Cleanup Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Auto-Cleanup & Deletion</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6 rounded-2xl border border-base-300 bg-base-200/30 p-6 sm:grid-cols-2 lg:grid-cols-3">
					<label className="label cursor-pointer justify-start gap-4 p-0">
						<input
							type="checkbox"
							className="checkbox checkbox-sm checkbox-primary"
							checked={formData.delete_source_nzb_on_removal ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("delete_source_nzb_on_removal", e.target.checked)}
						/>
						<div className="flex flex-col">
							<span className="label-text font-semibold text-xs">Sync NZB Deletion</span>
							<span className="label-text-alt text-[9px] opacity-60">Delete original NZB with metadata</span>
						</div>
					</label>

					<label className="label cursor-pointer justify-start gap-4 p-0">
						<input
							type="checkbox"
							className="checkbox checkbox-sm checkbox-primary"
							checked={formData.delete_failed_nzb ?? true}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("delete_failed_nzb", e.target.checked)}
						/>
						<div className="flex flex-col">
							<span className="label-text font-semibold text-xs">Cleanup Failed Imports</span>
							<span className="label-text-alt text-[9px] opacity-60">Delete NZB if import fails</span>
						</div>
					</label>

					<label className="label cursor-pointer justify-start gap-4 p-0">
						<input
							type="checkbox"
							className="checkbox checkbox-sm checkbox-primary"
							checked={formData.delete_completed_nzb ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("delete_completed_nzb", e.target.checked)}
						/>
						<div className="flex flex-col">
							<span className="label-text font-semibold text-xs">Cleanup Successful Imports</span>
							<span className="label-text-alt text-[9px] opacity-60">Delete NZB after success</span>
						</div>
					</label>
				</div>
			</section>

			{/* Mirroring & Backup Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Mirroring & Backups</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="space-y-6">
					<div className="flex items-center gap-4">
						<input
							type="checkbox"
							className="toggle toggle-primary toggle-sm"
							checked={formData.backup?.enabled ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleBackupChange("enabled", e.target.checked)}
						/>
						<div>
							<span className="font-bold text-sm">Enable Periodic Metadata Mirroring</span>
							<p className="text-[10px] opacity-60">Periodically mirror .meta files to an alternate directory.</p>
						</div>
					</div>

					{formData.backup?.enabled && (
						<div className="slide-in-from-top-2 grid animate-in grid-cols-1 gap-6 duration-300 md:grid-cols-2 lg:grid-cols-4">
							<fieldset className="fieldset min-w-0 md:col-span-2">
								<legend className="fieldset-legend font-semibold">Backup Target Path</legend>
								<input
									type="text"
									className="input w-full bg-base-200/50"
									value={formData.backup?.path ?? ""}
									disabled={isReadOnly}
									onChange={(e) => handleBackupChange("path", e.target.value)}
									placeholder="/path/to/backups"
								/>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Interval (Hours)</legend>
								<input
									type="number"
									className="input w-full bg-base-200/50 font-mono"
									value={formData.backup?.interval_hours ?? 24}
									disabled={isReadOnly}
									onChange={(e) => handleBackupChange("interval_hours", Number.parseInt(e.target.value, 10) || 24)}
									min="1"
								/>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend font-semibold">Retention Count</legend>
								<input
									type="number"
									className="input w-full bg-base-200/50 font-mono"
									value={formData.backup?.keep_backups ?? 10}
									disabled={isReadOnly}
									onChange={(e) => handleBackupChange("keep_backups", Number.parseInt(e.target.value, 10) || 10)}
									min="1"
								/>
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
						disabled={!hasChanges || isUpdating || !formData.root_path.trim()}
					>
						{isUpdating ? <span className="loading loading-spinner loading-sm" /> : <Save className="h-4 w-4" />}
						Save Metadata Configuration
					</button>
				</div>
			)}
		</div>
	);
}
