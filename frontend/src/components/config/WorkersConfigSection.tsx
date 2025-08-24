import { Save, AlertTriangle } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, ImportConfig } from "../../types/config";

interface ImportConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: ImportConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function ImportConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: ImportConfigSectionProps) {
	const [formData, setFormData] = useState<ImportConfig>(config.import);
	const [hasChanges, setHasChanges] = useState(false);
	const [restartRequired, setRestartRequired] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.import);
		setHasChanges(false);
		// Reset restart required when config is reloaded (e.g., after server restart)
		setRestartRequired(false);
	}, [config.import]);

	const handleInputChange = (field: keyof ImportConfig, value: number) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.import));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("import", formData);
			setHasChanges(false);
			// Set restart required flag when processor worker count changes
			if (formData.max_processor_workers !== config.import.max_processor_workers) {
				setRestartRequired(true);
			}
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="text-lg font-semibold">Import Processing Configuration</h3>
			<div className="grid grid-cols-1 gap-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Processor Workers</legend>
					<input
						type="number"
						className="input"
						value={formData.max_processor_workers}
						readOnly={isReadOnly}
						min={1}
						max={20}
						onChange={(e) =>
							handleInputChange("max_processor_workers", parseInt(e.target.value) || 1)
						}
					/>
					<p className="label">
						Number of concurrent NZB processing threads for import operations
					</p>
				</fieldset>
			</div>
			
			<div className="alert alert-warning">
				<AlertTriangle className="h-5 w-5" />
				<div>
					<div className="font-bold">Server Restart Required</div>
					<div className="text-sm">
						Changes to processor worker count require a server restart to take effect. 
						This setting controls how many NZB files can be processed simultaneously 
						during import operations. Higher values may improve import speed but will 
						use more system resources.
					</div>
				</div>
			</div>

			{/* Restart Required Notice (shown after saving changes) */}
			{restartRequired && (
				<div className="alert alert-error">
					<AlertTriangle className="h-5 w-5" />
					<div>
						<div className="font-bold">Configuration Saved - Restart Server Now</div>
						<div className="text-sm">
							Your processor worker count has been updated in the configuration. 
							Restart the AltMount server for the changes to take effect.
						</div>
					</div>
				</div>
			)}

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end">
					<button
						type="button"
						className="btn btn-primary"
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
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
