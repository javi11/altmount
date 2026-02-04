import { Download, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useBatchExportNZB } from "../../hooks/useConfig";
import type { ConfigResponse, MetadataConfig } from "../../types/config";

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

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("metadata", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">Metadata Storage Configuration</h3>
			<div className="grid grid-cols-1 gap-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Root Path</legend>
					<input
						type="text"
						className="input"
						value={formData.root_path}
						readOnly={isReadOnly}
						onChange={(e) => handleInputChange("root_path", e.target.value)}
						placeholder="/path/to/metadata"
						required
					/>
					<p className="label">Directory path where file metadata will be stored (required)</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Deletion Options</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Delete original NZB when metadata is removed</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.delete_source_nzb_on_removal ?? false}
							disabled={isReadOnly}
							onChange={(e) =>
								handleCheckboxChange("delete_source_nzb_on_removal", e.target.checked)
							}
						/>
					</label>
					<p className="label">
						When enabled, the original NZB file will be permanently deleted when its metadata is
						removed
					</p>

					<label className="label mt-4 cursor-pointer">
						<span className="label-text">Delete failed NZB files</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.delete_failed_nzb ?? true}
							disabled={isReadOnly}
							onChange={(e) => handleCheckboxChange("delete_failed_nzb", e.target.checked)}
						/>
					</label>
					<p className="label">
						When enabled, failed NZB files will be permanently deleted. When disabled, they will be
						moved to a 'failed' directory.
					</p>

					<label className="label mt-4 cursor-pointer">
						<span className="label-text">Delete completed NZB files</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.delete_completed_nzb ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleCheckboxChange("delete_completed_nzb", e.target.checked)}
						/>
					</label>
					<p className="label">
						When enabled, the original NZB file will be permanently deleted after successful import.
						Warning: This removes the ability to re-generate metadata without re-uploading the NZB.
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Batch Export</legend>
					<button
						type="button"
						className="btn btn-secondary w-xs"
						onClick={() => batchExport.mutate("/")}
						disabled={batchExport.isPending || !formData.root_path.trim()}
					>
						{batchExport.isPending ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Download className="h-4 w-4" />
						)}
						{batchExport.isPending ? "Exporting..." : "Export All as NZB"}
					</button>
					<p className="label">
						Export all file metadata as NZB files in a single ZIP archive. Archives (RAR/7zip) and
						encrypted files are automatically excluded.
					</p>
				</fieldset>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end">
					<button
						type="button"
						className="btn btn-primary"
						onClick={handleSave}
						disabled={!hasChanges || isUpdating || !formData.root_path.trim()}
					>
						{isUpdating ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Save className="h-4 w-4" />
						)}
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
