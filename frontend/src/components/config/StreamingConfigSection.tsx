import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, StreamingConfig } from "../../types/config";
import { BytesDisplay } from "../ui/BytesDisplay";

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
			<div className="grid grid-cols-1 gap-4 md:grid-cols-2">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Max Cache Size</legend>
					<input
						type="number"
						className="input"
						value={formData.max_cache_size_mb}
						readOnly={isReadOnly}
						min={1}
						max={1024} // 1GB max
						step={1} // 1MB steps
						onChange={(e) =>
							handleInputChange("max_cache_size_mb", Number.parseInt(e.target.value, 10) || 1)
						}
					/>
					<p className="label">Maximum cache size in MB for ahead download chunks</p>
					<BytesDisplay bytes={formData.max_cache_size_mb * 1024 * 1024} mode="badge" />
				</fieldset>
			</div>
			<div className="alert alert-info">
				<div>
					<div className="font-bold">Note</div>
					<div className="text-sm">
						Cache size controls memory usage for ahead downloading of segments. If you don't
						understand this setting, it's recommended to keep the default value.
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
