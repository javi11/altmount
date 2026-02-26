import { Plus, Save, X } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, ImportConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

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

		const normalized = trimmed.startsWith(".")
			? trimmed.toLowerCase()
			: `.${trimmed.toLowerCase()}`;

		if (formData.allowed_file_extensions.includes(normalized)) return;

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
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">NZB Processor</h3>
				<p className="break-words text-base-content/50 text-sm">
					Configure how workers handle new imports and validation.
				</p>
			</div>

			<div className="space-y-8">
				{/* Worker Core Configuration */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Concurrency
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Active Workers</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.max_processor_workers}
								readOnly={isReadOnly}
								min={1}
								max={20}
								onChange={(e) =>
									handleInputChange(
										"max_processor_workers",
										Number.parseInt(e.target.value, 10) || 1,
									)
								}
							/>
							<p className="label break-words text-base-content/70 text-xs">
								Concurrent NZB processing threads.
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">Max Connections (per Worker)</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.max_import_connections}
								readOnly={isReadOnly}
								min={1}
								onChange={(e) =>
									handleInputChange(
										"max_import_connections",
										Number.parseInt(e.target.value, 10) || 10,
									)
								}
							/>
							<p className="label break-words text-base-content/70 text-xs">
								Socket limit per active worker.
							</p>
						</fieldset>
					</div>

					<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Max Download Prefetch</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.max_download_prefetch}
								readOnly={isReadOnly}
								min={1}
								onChange={(e) =>
									handleInputChange(
										"max_download_prefetch",
										Number.parseInt(e.target.value, 10) || 1,
									)
								}
							/>
							<p className="label break-words text-base-content/70 text-xs">
								Segments prefetched ahead for archive analysis.
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Read Timeout (Seconds)</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.read_timeout_seconds}
								readOnly={isReadOnly}
								min={1}
								onChange={(e) =>
									handleInputChange(
										"read_timeout_seconds",
										Number.parseInt(e.target.value, 10) || 300,
									)
								}
							/>
							<p className="label break-words text-base-content/70 text-xs">
								Usenet socket read timeout.
							</p>
						</fieldset>
					</div>
				</div>

				{/* Validation Slider */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Validation
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="space-y-6">
						<div className="flex items-center justify-between">
							<div className="min-w-0">
								<h5 className="font-bold text-sm">Segment Verification</h5>
								<p className="mt-1 break-words text-[11px] text-base-content/50">
									Percentage of Usenet segments to validate before import.
								</p>
							</div>
							<div className="shrink-0 font-black font-mono text-primary text-xl">
								{formData.segment_sample_percentage}%
							</div>
						</div>

						<div className="space-y-4">
							<input
								type="range"
								min="1"
								max="100"
								value={formData.segment_sample_percentage}
								className="range range-primary range-sm"
								step="1"
								disabled={isReadOnly}
								onChange={(e) =>
									handleInputChange(
										"segment_sample_percentage",
										Number.parseInt(e.target.value, 10),
									)
								}
							/>
							<div className="flex justify-between px-2 font-black text-base-content/50 text-xs uppercase tracking-tighter">
								<span>Fast (1%)</span>
								<span>Balanced</span>
								<span>Deep (100%)</span>
							</div>
						</div>

						<div className="divider text-base-content/70" />

						<label className="label cursor-pointer items-start justify-start gap-4">
							<input
								type="checkbox"
								className="toggle toggle-primary toggle-sm mt-1 shrink-0"
								checked={formData.skip_health_check ?? false}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("skip_health_check", e.target.checked)}
							/>
							<div className="min-w-0 flex-1">
								<span className="block whitespace-normal break-words font-bold text-xs">
									Lenient Import (Instant)
								</span>
								<span className="mt-1 block whitespace-normal break-words text-base-content/50 text-xs leading-relaxed">
									Bypass validation entirely for immediate exposure in the library. Recommended only
									for high-completion providers.
								</span>
							</div>
						</label>

						<div className="divider text-base-content/70" />

						<label className="label cursor-pointer items-start justify-start gap-4">
							<input
								type="checkbox"
								className="toggle toggle-primary toggle-sm mt-1 shrink-0"
								checked={formData.allow_nested_rar_extraction ?? true}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("allow_nested_rar_extraction", e.target.checked)}
							/>
							<div className="min-w-0 flex-1">
								<span className="block whitespace-normal break-words font-bold text-xs">
									Nested RAR Extraction
								</span>
								<span className="mt-1 block whitespace-normal break-words text-base-content/50 text-xs leading-relaxed">
									Extract nested RAR archives found inside other RAR or 7zip archives. Disable if
									nested extraction causes issues with your files.
								</span>
							</div>
						</label>
					</div>
				</div>

				{/* Strategy Configuration */}
				<div className="space-y-8 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Library Strategy
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="grid grid-cols-1 gap-8 md:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Strategy Type</legend>
							<select
								className="select select-bordered w-full bg-base-100"
								value={formData.import_strategy}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("import_strategy", e.target.value)}
							>
								<option value="NONE">None (Virtual Only)</option>
								<option value="SYMLINK">Physical Symlinks</option>
								<option value="STRM">STRM URL Files</option>
							</select>
							<p className="label mt-2 break-words text-base-content/70 text-xs leading-relaxed">
								{formData.import_strategy === "NONE" &&
									"Files are only visible through the virtual FUSE/WebDAV mount."}
								{formData.import_strategy === "SYMLINK" &&
									"Creates real .mkv/.mp4 files in a target folder that point to AltMount."}
								{formData.import_strategy === "STRM" &&
									"Generates small .strm text files containing streaming URLs."}
							</p>
						</fieldset>

						{formData.import_strategy !== "NONE" && (
							<fieldset className="fieldset slide-in-from-right-2 animate-in">
								<legend className="fieldset-legend font-semibold">
									{formData.import_strategy === "SYMLINK" ? "Symlink Root" : "STRM Output Root"}
								</legend>
								<input
									type="text"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.import_dir || ""}
									readOnly={isReadOnly}
									placeholder="/path/to/media"
									onChange={(e) => handleInputChange("import_dir", e.target.value)}
								/>
								<p className="label mt-2 break-words text-base-content/70 text-xs">
									Absolute path for strategy output.
								</p>
							</fieldset>
						)}
					</div>

					<div className="divider text-base-content/70" />

					<div className="space-y-6">
						<div>
							<h5 className="font-bold text-sm">NZB Watch Directory</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Monitor a specific folder for new NZB files and import them automatically.
							</p>
						</div>

						<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">Watch Directory Path</legend>
								<input
									type="text"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.watch_dir || ""}
									readOnly={isReadOnly}
									placeholder="/path/to/watch"
									onChange={(e) => handleInputChange("watch_dir", e.target.value)}
								/>
								<p className="label mt-2 break-words text-base-content/70 text-xs">
									Absolute path to monitor.
								</p>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend font-semibold">
									Polling Interval (Seconds)
								</legend>
								<input
									type="number"
									className="input input-bordered w-full bg-base-100 font-mono text-sm"
									value={formData.watch_interval_seconds || 10}
									readOnly={isReadOnly}
									min={1}
									onChange={(e) =>
										handleInputChange(
											"watch_interval_seconds",
											Number.parseInt(e.target.value, 10) || 10,
										)
									}
								/>
								<p className="label mt-2 break-words text-base-content/70 text-xs">
									How often to check for new files.
								</p>
							</fieldset>
						</div>
					</div>
				</div>

				{/* File Extensions */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Filters
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold">Allowed File Extensions</legend>

						<div className="mb-4 flex min-h-[4rem] flex-wrap gap-2 rounded-xl border border-base-300 bg-base-100/50 p-3">
							{formData.allowed_file_extensions.length === 0 ? (
								<span className="w-full self-center text-center text-base-content/60 text-xs italic">
									All file types are currently allowed
								</span>
							) : (
								formData.allowed_file_extensions.map((ext) => (
									<div key={ext} className="badge badge-primary gap-1 px-3 py-3 font-bold text-xs">
										{ext}
										{!isReadOnly && (
											<button
												type="button"
												className="hover:text-white"
												onClick={() => removeExtension(ext)}
											>
												<X className="h-3 w-3" />
											</button>
										)}
									</div>
								))
							)}
						</div>

						{!isReadOnly && (
							<div className="join mb-4 w-full shadow-sm">
								<input
									type="text"
									className="input input-bordered join-item flex-1 bg-base-100 text-sm"
									placeholder="Add e.g. .mp4"
									value={extensionInput}
									onChange={(e) => setExtensionInput(e.target.value)}
									onKeyDown={handleExtensionKeyDown}
								/>
								<button
									type="button"
									className="btn btn-primary join-item px-6"
									onClick={() => addExtension(extensionInput)}
								>
									<Plus className="h-4 w-4" />
								</button>
							</div>
						)}

						<div className="flex flex-wrap gap-2">
							<button
								type="button"
								className="btn btn-sm btn-outline border-base-300 text-base-content/80 hover:opacity-100"
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
								className="btn btn-sm btn-outline border-base-300 text-base-content/80 hover:opacity-100"
								disabled={isReadOnly}
								onClick={() => handleInputChange("allowed_file_extensions", [])}
							>
								Clear All
							</button>
						</div>
					</fieldset>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end border-base-200 border-t pt-4">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && "btn-ghost border-base-300"}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
					>
						{isUpdating ? <LoadingSpinner size="sm" /> : <Save className="h-4 w-4" />}
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
