import { Save, X } from "lucide-react";
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
	const [extensionInput, setExtensionInput] = useState("");

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.import);
		setHasChanges(false);
	}, [config.import]);

	const handleInputChange = (
		field: keyof ImportConfig,
		value: number | boolean | string | string[],
	) => {
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

	// Tag management functions
	const addExtension = (extension: string) => {
		const trimmed = extension.trim();
		if (!trimmed) return;

		// Ensure extension starts with a dot
		const normalized = trimmed.startsWith(".")
			? trimmed.toLowerCase()
			: `.${trimmed.toLowerCase()}`;

		// Check if already exists
		if (formData.allowed_file_extensions.includes(normalized)) {
			return;
		}

		const newExtensions = [...formData.allowed_file_extensions, normalized];
		handleInputChange("allowed_file_extensions", newExtensions);
		setExtensionInput("");
	};

	const removeExtension = (extension: string) => {
		const newExtensions = formData.allowed_file_extensions.filter((ext) => ext !== extension);
		handleInputChange("allowed_file_extensions", newExtensions);
	};

	const handleExtensionKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
		if (e.key === "Enter") {
			e.preventDefault();
			addExtension(extensionInput);
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

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Max Import Connections</legend>
					<input
						type="number"
						className="input"
						value={formData.max_import_connections}
						readOnly={isReadOnly}
						min={1}
						onChange={(e) =>
							handleInputChange("max_import_connections", Number.parseInt(e.target.value, 10) || 10)
						}
					/>
					<p className="label">
						Maximum concurrent connections for each active processor worker. Example: If you have 2
						processor workers and you set this to 5, each worker will have a maximum of 5 concurrent
						connections.
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Import Cache Size (MB)</legend>
					<input
						type="number"
						className="input"
						value={formData.import_cache_size_mb}
						readOnly={isReadOnly}
						min={16}
						max={512}
						onChange={(e) =>
							handleInputChange("import_cache_size_mb", Number.parseInt(e.target.value, 10) || 64)
						}
					/>
					<p className="label">Cache size in MB for archive analysis.</p>
				</fieldset>

				{/* Symlink Configuration */}
				<div className="space-y-4">
					<div>
						<h4 className="font-medium">Symlink Configuration</h4>
						<p className="text-base-content/70 text-sm">
							Create symlinks to imported files organized by category. This allows applications to
							access files via symlinks instead of the mount path.
						</p>
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Enable Symlinks</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Create category-based symlinks for imported files</span>
							<input
								type="checkbox"
								className="toggle toggle-primary"
								checked={formData.symlink_enabled}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("symlink_enabled", e.target.checked)}
							/>
						</label>
						<p className="label">
							When enabled, symlinks will be created in category-specific folders within the symlink
							directory
						</p>
					</fieldset>

					{formData.symlink_enabled && (
						<fieldset className="fieldset">
							<legend className="fieldset-legend">Symlink Directory</legend>
							<input
								type="text"
								className="input"
								value={formData.symlink_dir || ""}
								readOnly={isReadOnly}
								placeholder="/path/to/symlinks"
								onChange={(e) => handleInputChange("symlink_dir", e.target.value)}
							/>
							<p className="label">
								Absolute path where symlinks will be created. Symlinks will be organized in
								subdirectories by category (e.g., /symlinks/movies/, /symlinks/tv/)
							</p>
						</fieldset>
					)}

					{formData.symlink_enabled && formData.symlink_dir && (
						<div className="alert alert-info">
							<div>
								<div className="font-bold">Symlinks Enabled</div>
								<div className="text-sm">
									Imported files will be available as symlinks in{" "}
									<code>{formData.symlink_dir}/[category]/</code> for easier access by external
									applications.
								</div>
							</div>
						</div>
					)}
				</div>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Full Segment Validation</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Validate all segments (slower but thorough)</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.full_segment_validation}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("full_segment_validation", e.target.checked)}
						/>
					</label>
					<p className="label">
						When disabled, use a sampling approach for faster processing. Enable for thorough
						validation of all segments (slower).
					</p>
				</fieldset>

				{!formData.full_segment_validation && (
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Segment Sample Percentage</legend>
						<input
							type="number"
							className="input"
							value={formData.segment_sample_percentage}
							readOnly={isReadOnly}
							min={1}
							max={100}
							step={1}
							onChange={(e) =>
								handleInputChange(
									"segment_sample_percentage",
									Number.parseInt(e.target.value, 10) || 5,
								)
							}
						/>
						<p className="label">
							Percentage of segments to check when sampling is enabled (1-100%, default: 5%). Lower
							percentages are faster but less thorough.
						</p>
					</fieldset>
				)}

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Allowed File Extensions</legend>

					{/* Tag display area */}
					<div className="mb-3 flex min-h-[3rem] flex-wrap gap-2 rounded-lg border border-base-300 bg-base-100 p-2">
						{formData.allowed_file_extensions.length === 0 ? (
							<span className="text-base-content/60 text-sm">
								No extensions specified (all files allowed)
							</span>
						) : (
							formData.allowed_file_extensions.map((ext) => (
								<div key={ext} className="badge badge-primary gap-2">
									<span>{ext}</span>
									{!isReadOnly && (
										<button
											type="button"
											className="btn btn-ghost btn-xs h-4 min-h-0 w-4 p-0"
											onClick={() => removeExtension(ext)}
											aria-label={`Remove ${ext}`}
										>
											<X className="h-3 w-3" />
										</button>
									)}
								</div>
							))
						)}
					</div>

					{/* Input field for adding new extensions */}
					{!isReadOnly && (
						<div className="mb-3">
							<div className="flex gap-2">
								<input
									type="text"
									className="input input-sm flex-1"
									placeholder="Type extension and press Enter (e.g., .mp4)"
									value={extensionInput}
									onChange={(e) => setExtensionInput(e.target.value)}
									onKeyDown={handleExtensionKeyDown}
								/>
								<button
									type="button"
									className="btn btn-primary btn-sm"
									onClick={() => addExtension(extensionInput)}
									disabled={!extensionInput.trim()}
								>
									Add
								</button>
							</div>
						</div>
					)}

					{/* Preset buttons */}
					<div className="flex gap-2">
						<button
							type="button"
							className="btn btn-sm btn-outline"
							disabled={isReadOnly}
							onClick={() => {
								const videoDefaults = [
									".mp4",
									".mkv",
									".avi",
									".mov",
									".wmv",
									".flv",
									".webm",
									".m4v",
									".mpg",
									".mpeg",
									".m2ts",
									".ts",
									".vob",
									".3gp",
									".3g2",
									".h264",
									".h265",
									".hevc",
									".ogv",
									".ogm",
									".strm",
									".iso",
									".img",
									".divx",
									".xvid",
									".rm",
									".rmvb",
									".asf",
									".asx",
									".wtv",
									".mk3d",
									".dvr-ms",
								];
								handleInputChange("allowed_file_extensions", videoDefaults);
							}}
						>
							Reset to Video Defaults
						</button>
						<button
							type="button"
							className="btn btn-sm btn-outline"
							disabled={isReadOnly}
							onClick={() => handleInputChange("allowed_file_extensions", [])}
						>
							Clear (Allow All)
						</button>
					</div>

					<p className="label">
						Add file extensions to allow during import validation. Press Enter or click Add to add
						an extension. Leave empty to allow all file types. Default: common video file
						extensions.
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
