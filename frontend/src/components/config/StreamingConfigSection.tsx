import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, StreamingConfig } from "../../types/config";

interface StreamingConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: StreamingConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function StreamingConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: StreamingConfigSectionProps) {
	const [formData, setFormData] = useState<StreamingConfig>(config.streaming);
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.streaming);
		setHasChanges(false);
	}, [config.streaming]);

	const handleInputChange = (field: keyof StreamingConfig, value: number) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.streaming));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("streaming", formData);
			setHasChanges(false);
		}
	};
	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">Streaming & Download Configuration</h3>
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Download Workers</legend>
				<input
					type="number"
					className="input"
					value={formData.max_download_workers}
					readOnly={isReadOnly}
					min={1}
					max={50}
					onChange={(e) =>
						handleInputChange("max_download_workers", Number.parseInt(e.target.value, 10) || 1)
					}
				/>
				<p className="label">Number of concurrent download threads</p>
			</fieldset>
			<div className="alert alert-info">
				<div>
					<div className="font-bold">Note</div>
					<div className="text-sm">
						Download workers control the number of concurrent downloads from NNTP servers. If you
						don't understand these settings, it's recommended to keep the default values.
					</div>
				</div>
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
						Save Changes
					</button>
				</div>
			)}
		</div>
	);
}
