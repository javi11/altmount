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

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Lenient Import (Skip Health Check)</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Bypass Usenet article validation during import</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.skip_health_check ?? false}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("skip_health_check", e.target.checked)}
						/>
					</label>
					<p className="label text-sm">
						When enabled, files are imported instantly without checking if all segments are
						available. This matches behavior from older versions (Alpha 5). Background health checks
						will still occur later.
					</p>
				</fieldset>

				{/* Import Strategy Configuration */}
				<div className="space-y-4">
					<div>
						<h4 className="font-medium">Import Strategy</h4>
						<p className="text-base-content/70 text-sm">
							Choose how imported files should be made available to external applications. Symlinks
							and STRM files cannot be enabled simultaneously.
						</p>
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Strategy Type</legend>
						<select
							className="select"
							value={formData.import_strategy}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("import_strategy", e.target.value)}
						>
							<option value="NONE">None (Direct Import)</option>
							<option value="SYMLINK">Symlinks</option>
							<option value="STRM">STRM Files</option>
						</select>
						<p className="label">
							{formData.import_strategy === "NONE" &&
								"Files will be only exposed via the WebDAV mount point"}
							{formData.import_strategy === "SYMLINK" &&
								"Create category-based symlinks for easier access by external applications"}
							{formData.import_strategy === "STRM" &&
								"Generate STRM files with HTTP streaming URLs for media players"}
						</p>
					</fieldset>

					{formData.import_strategy !== "NONE" && (
						<fieldset className="fieldset">
							<legend className="fieldset-legend">
								{formData.import_strategy === "SYMLINK" ? "Symlink Directory" : "STRM Directory"}
							</legend>
							<input
								type="text"
								className="input"
								value={formData.import_dir || ""}
								readOnly={isReadOnly}
								placeholder={
									formData.import_strategy === "SYMLINK"
										? "/path/to/symlinks"
										: "/path/to/strm/files"
								}
								onChange={(e) => handleInputChange("import_dir", e.target.value)}
							/>
							<p className="label">
								{formData.import_strategy === "SYMLINK"
									? "Absolute path where symlinks will be created."
									: "Absolute path where STRM files will be created."}
							</p>
						</fieldset>
					)}

					{formData.import_strategy === "SYMLINK" && formData.import_dir && (
						<div className="alert alert-info">
							<div>
								<div className="font-bold">Symlinks Enabled</div>
								<div className="text-sm">
									Imported files will be available as symlinks in{" "}
									<code>{formData.import_dir}/</code> for easier access by external applications.
								</div>
							</div>
						</div>
					)}

					{formData.import_strategy === "STRM" && formData.import_dir && (
						<div className="alert alert-info">
							<div>
								<div className="font-bold">STRM Files Enabled</div>
								<div className="text-sm">
									STRM files will be created in <code>{formData.import_dir}/</code> with HTTP
									streaming URLs. These files support full Range request support for seeking in
									video players.
								</div>
							</div>
						</div>
					)}
				</div>

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
						Percentage of segments to check (1-100%, default: 5%). Set to 100% for thorough
						validation of all segments.
					</p>
				</fieldset>

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
