import { Save } from "lucide-react";
import { useEffect, useState } from "react";
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
