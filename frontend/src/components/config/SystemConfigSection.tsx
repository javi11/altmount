import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, SystemFormData } from "../../types/config";

interface SystemConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: SystemFormData) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function SystemConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: SystemConfigSectionProps) {
	const [formData, setFormData] = useState<SystemFormData>({
		log_level: config.log_level,
	});
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = {
			log_level: config.log_level,
		};
		setFormData(newFormData);
		setHasChanges(false);
	}, [config.log_level]);

	const handleInputChange = (
		field: keyof SystemFormData,
		value: string,
	) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		const configData = {
			log_level: config.log_level,
		};
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(configData));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("system", formData);
			setHasChanges(false);
		}
	};
	return (
		<div className="space-y-4">
			<h3 className="text-lg font-semibold">System</h3>
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Log Level</legend>
				<select
					className="select"
					value={formData.log_level}
					disabled={isReadOnly}
					onChange={(e) =>
						handleInputChange("log_level", e.target.value)
					}
				>
					<option value="debug">Debug</option>
					<option value="info">Info</option>
					<option value="warn">Warning</option>
					<option value="error">Error</option>
				</select>
				<p className="label">Set the minimum logging level for the system</p>
			</fieldset>

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
