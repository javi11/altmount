import { Save } from "lucide-react";
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

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.import);
		setHasChanges(false);
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
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">Import Processing Configuration</h3>
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
							handleInputChange("max_processor_workers", Number.parseInt(e.target.value, 10) || 1)
						}
					/>
					<p className="label">Number of concurrent NZB processing threads for import operations</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Queue Processing Interval (Seconds)</legend>
					<input
						type="number"
						className="input"
						value={formData.queue_processing_interval_seconds}
						readOnly={isReadOnly}
						min={1}
						max={300}
						onChange={(e) =>
							handleInputChange(
								"queue_processing_interval_seconds",
								Number.parseInt(e.target.value, 10) || 5,
							)
						}
					/>
					<p className="label">
						How often workers check for new queue items (1-300 seconds). Changes require service
						restart.
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
