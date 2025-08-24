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
			<h3 className="text-lg font-semibold">Streaming & Download Configuration</h3>
			<div className="grid grid-cols-1 md:grid-cols-3 gap-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Max Range Size</legend>
					<input
						type="number"
						className="input"
						value={formData.max_range_size}
						readOnly={isReadOnly}
						min={0}
						step={1048576} // 1MB steps
						onChange={(e) =>
							handleInputChange("max_range_size", parseInt(e.target.value) || 0)
						}
					/>
					<p className="label">
						Higher of this range the streaming will be chunked.
					</p>
					<BytesDisplay bytes={formData.max_range_size} mode="badge" />
				</fieldset>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Streaming Chunk Size</legend>
					<input
						type="number"
						className="input"
						value={formData.streaming_chunk_size}
						readOnly={isReadOnly}
						min={0}
						step={1048576} // 1MB steps
						onChange={(e) =>
							handleInputChange(
								"streaming_chunk_size",
								parseInt(e.target.value) || 0,
							)
						}
					/>
					<p className="label">
						The limit of memory to be used for each streaming operation (Range
						of articles to download ahead)
					</p>
					<BytesDisplay bytes={formData.streaming_chunk_size} mode="badge" />
				</fieldset>
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
							handleInputChange(
								"max_download_workers",
								parseInt(e.target.value) || 1,
							)
						}
					/>
					<p className="label">
						Number of concurrent download threads
					</p>
				</fieldset>
			</div>
			<div className="alert alert-info">
				<div>
					<div className="font-bold">Note</div>
					<div className="text-sm">
						These settings control how files are streamed and chunked during
						download. Higher values may improve performance but use more memory.
						Download workers control the number of concurrent downloads from NNTP servers.
						If you don't understand these settings, it's recommended to keep the
						default values.
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
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
