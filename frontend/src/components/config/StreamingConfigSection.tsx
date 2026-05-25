import { Info, Save } from "lucide-react";
import { useEffect, useState } from "react";
import type {
	ConfigResponse,
	FailureMaskingConfig,
	SegmentCacheConfig,
	StreamingConfig,
} from "../../types/config";

interface StreamingConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: StreamingConfig | SegmentCacheConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function StreamingConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: StreamingConfigSectionProps) {
	const [streamingData, setStreamingData] = useState<StreamingConfig>(config.streaming);
	const [cacheData, setCacheData] = useState<SegmentCacheConfig>(config.segment_cache);
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setStreamingData(config.streaming);
		setCacheData(config.segment_cache);
		setHasChanges(false);
	}, [config.streaming, config.segment_cache]);

	const checkChanges = (newStreaming: StreamingConfig, newCache: SegmentCacheConfig) => {
		const streamingChanged = JSON.stringify(newStreaming) !== JSON.stringify(config.streaming);
		const cacheChanged = JSON.stringify(newCache) !== JSON.stringify(config.segment_cache);
		setHasChanges(streamingChanged || cacheChanged);
	};

	const handleStreamingChange = (field: keyof StreamingConfig, value: number) => {
		const newData = { ...streamingData, [field]: value };
		setStreamingData(newData);
		checkChanges(newData, cacheData);
	};

	const handleMaskingChange = (field: keyof FailureMaskingConfig, value: boolean | number) => {
		const newData = {
			...streamingData,
			failure_masking: {
				...streamingData.failure_masking,
				[field]: value,
			},
		};
		setStreamingData(newData);
		checkChanges(newData, cacheData);
	};

	const handlePar2RepairChange = (value: boolean) => {
		const newData = { ...streamingData, par2_repair: value };
		setStreamingData(newData);
		checkChanges(newData, cacheData);
	};

	const handleHealChange = (
		field: keyof NonNullable<StreamingConfig["par2_streaming_heal"]>,
		value: boolean | number,
	) => {
		const newData = {
			...streamingData,
			par2_streaming_heal: {
				...streamingData.par2_streaming_heal,
				[field]: value,
			},
		};
		setStreamingData(newData);
		checkChanges(newData, cacheData);
	};

	const handleRepairStoreChange = (
		field: keyof NonNullable<StreamingConfig["par2_repair_store"]>,
		value: number,
	) => {
		const newData = {
			...streamingData,
			par2_repair_store: {
				...streamingData.par2_repair_store,
				[field]: value,
			},
		};
		setStreamingData(newData);
		checkChanges(newData, cacheData);
	};

	const heal = streamingData.par2_streaming_heal ?? {};
	const store = streamingData.par2_repair_store ?? {};
	const par2RepairEnabled = streamingData.par2_repair === true;
	const healEnabled = heal.enabled === true;

	const handleCacheChange = (field: keyof SegmentCacheConfig, value: boolean | string | number) => {
		const newData = { ...cacheData, [field]: value };
		setCacheData(newData);
		checkChanges(streamingData, newData);
	};

	const handleSave = async () => {
		if (!onUpdate || !hasChanges) return;

		const streamingChanged = JSON.stringify(streamingData) !== JSON.stringify(config.streaming);
		const cacheChanged = JSON.stringify(cacheData) !== JSON.stringify(config.segment_cache);

		if (streamingChanged) {
			await onUpdate("streaming", streamingData);
		}
		if (cacheChanged) {
			await onUpdate("segment_cache", cacheData);
		}
		setHasChanges(false);
	};

	return (
		<div className="space-y-10">
			{/* Playback Tuning */}
			<div>
				<h3 className="font-bold text-base-content text-lg">Playback Tuning</h3>
				<p className="text-base-content/50 text-sm">
					Optimize how AltMount streams media to your players.
				</p>
			</div>

			<div className="space-y-8">
				{/* Prefetch Slider */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
						<div className="min-w-0">
							<h4 className="font-bold text-base-content text-sm">Segment Prefetch</h4>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Number of Usenet articles to download ahead of current playback position.
							</p>
						</div>
						<div className="flex shrink-0 items-center gap-3">
							<span className="font-black font-mono text-primary text-xl">
								{streamingData.max_prefetch}
							</span>
							<span className="font-bold text-base-content/60 text-xs uppercase">segments</span>
						</div>
					</div>

					<div className="space-y-4">
						<input
							type="range"
							min="1"
							max="100"
							value={streamingData.max_prefetch}
							step="1"
							className="range range-primary range-sm w-full [&::-webkit-slider-runnable-track]:rounded-full"
							disabled={isReadOnly}
							onChange={(e) =>
								handleStreamingChange("max_prefetch", Number.parseInt(e.target.value, 10))
							}
						/>
						<div className="flex justify-between px-2 font-black text-base-content/50 text-xs">
							<span>1</span>
							<span>20</span>
							<span>40</span>
							<span>60</span>
							<span>80</span>
							<span>100</span>
						</div>
					</div>
				</div>

				{/* Guidance */}
				<div className="alert items-start rounded-2xl border border-info/20 bg-info/5 p-4 shadow-sm">
					<Info className="mt-0.5 h-5 w-5 shrink-0 text-info" />
					<div className="min-w-0 flex-1">
						<div className="font-bold text-info text-xs uppercase tracking-wider">
							Performance Note
						</div>
						<div className="mt-1 break-words text-[11px] leading-relaxed opacity-80">
							Higher values improve stability on slow connections but increase initial memory usage.
							Default (30) is recommended for most 4K streaming scenarios.
						</div>
					</div>
				</div>
			</div>

			{/* Failure Masking */}
			<div className="border-base-200 border-t pt-10">
				<h3 className="font-bold text-base-content text-lg">Failure Masking</h3>
				<p className="text-base-content/50 text-sm">
					Automatically hide files from the mount if they fail to stream too many times.
				</p>
			</div>

			<div className="space-y-8">
				{/* Masking Toggle */}
				<div className="flex items-center justify-between rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Enable Masking</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							When enabled, files that repeatedly fail health checks while streaming are hidden.
						</p>
					</div>
					<input
						type="checkbox"
						className="toggle toggle-primary"
						checked={streamingData.failure_masking.enabled}
						disabled={isReadOnly}
						onChange={(e) => handleMaskingChange("enabled", e.target.checked)}
					/>
				</div>

				{/* Threshold Slider */}
				<div
					className={`space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!streamingData.failure_masking.enabled ? "opacity-50" : ""}`}
				>
					<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
						<div className="min-w-0">
							<h4 className="font-bold text-base-content text-sm">Failure Threshold</h4>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Number of failures before the file is hidden from the mount.
							</p>
						</div>
						<div className="flex shrink-0 items-center gap-3">
							<span className="font-black font-mono text-primary text-xl">
								{streamingData.failure_masking.threshold}
							</span>
							<span className="font-bold text-base-content/60 text-xs uppercase">failures</span>
						</div>
					</div>

					<div className="space-y-4">
						<input
							type="range"
							min="1"
							max="10"
							value={streamingData.failure_masking.threshold}
							step="1"
							className="range range-primary range-sm w-full [&::-webkit-slider-runnable-track]:rounded-full"
							disabled={isReadOnly || !streamingData.failure_masking.enabled}
							onChange={(e) =>
								handleMaskingChange("threshold", Number.parseInt(e.target.value, 10))
							}
						/>
						<div className="flex justify-between px-2 font-black text-base-content/50 text-xs">
							<span>1</span>
							<span>3</span>
							<span>5</span>
							<span>7</span>
							<span>10</span>
						</div>
					</div>
				</div>

				{/* Guidance */}
				<div className="alert items-start rounded-2xl border border-info/20 bg-info/5 p-4 shadow-sm">
					<Info className="mt-0.5 h-5 w-5 shrink-0 text-info" />
					<div className="min-w-0 flex-1">
						<div className="font-bold text-info text-xs uppercase tracking-wider">
							Repair Workflow
						</div>
						<div className="mt-1 break-words text-[11px] leading-relaxed opacity-80">
							Masking a file makes it appear as "missing" to your mount. This triggers Sonarr or
							Radarr to attempt a repair or redownload. The threshold protects against one-off
							network glitches.
						</div>
					</div>
				</div>
			</div>

			{/* PAR2 Self-Heal */}
			<div className="border-base-200 border-t pt-10">
				<h3 className="font-bold text-base-content text-lg">PAR2 Self-Heal</h3>
				<p className="text-base-content/50 text-sm">
					Reconstruct missing Usenet segments from the file's PAR2 recovery data instead of
					re-downloading the whole release. Recovered segments go to a small independent in-memory
					store, so this works without the segment cache — including rclone VFS cache setups.
				</p>
			</div>

			<div className="space-y-8">
				{/* PAR2 repair toggle */}
				<div className="flex items-center justify-between rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Enable PAR2 Self-Heal</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							On a streaming failure, rebuild the missing segments from PAR2 recovery data in the
							background before falling back to a Sonarr/Radarr re-download.
						</p>
					</div>
					<input
						type="checkbox"
						className="toggle toggle-primary"
						checked={par2RepairEnabled}
						disabled={isReadOnly}
						onChange={(e) => handlePar2RepairChange(e.target.checked)}
					/>
				</div>

				{/* RAM bounds: concurrency + max file size */}
				<div
					className={`space-y-3 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!par2RepairEnabled ? "opacity-50" : ""}`}
				>
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Max Concurrent Repairs</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Each reconstruction streams the file straight into the PAR2 recovery grid, holding
							about its full size in memory (~1×). This caps how many run at once to bound peak RAM.
							Default 1.
						</p>
					</div>
					<input
						type="number"
						min="1"
						className="input input-bordered w-full"
						value={streamingData.par2_max_concurrent_repairs ?? 1}
						disabled={isReadOnly || !par2RepairEnabled}
						onChange={(e) =>
							handleStreamingChange(
								"par2_max_concurrent_repairs",
								Number.parseInt(e.target.value, 10) || 1,
							)
						}
					/>
					<div className="min-w-0 pt-2">
						<h4 className="font-bold text-base-content text-sm">Max Repair File Size (MB)</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Skip self-heal (fall back to a re-download) for files larger than this, so a few huge
							files can't exhaust RAM. 0 means unlimited.
						</p>
					</div>
					<input
						type="number"
						min="0"
						className="input input-bordered w-full"
						value={streamingData.par2_max_repair_file_size_mb ?? 0}
						disabled={isReadOnly || !par2RepairEnabled}
						onChange={(e) =>
							handleStreamingChange(
								"par2_max_repair_file_size_mb",
								Number.parseInt(e.target.value, 10) || 0,
							)
						}
					/>
				</div>

				{/* Mid-stream heal toggle */}
				<div
					className={`flex items-center justify-between rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!par2RepairEnabled ? "opacity-50" : ""}`}
				>
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Seamless Mid-Stream Heal</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Instead of failing a read that hits a missing segment, block briefly while the file is
							reconstructed from PAR2, then keep playing. Reconstruction is reactive — it runs only
							when a read actually reaches a hole, so opening a stream never pre-downloads the file.
						</p>
					</div>
					<input
						type="checkbox"
						className="toggle toggle-primary"
						checked={healEnabled}
						disabled={isReadOnly || !par2RepairEnabled}
						onChange={(e) => handleHealChange("enabled", e.target.checked)}
					/>
				</div>

				{/* Block-on-repair seconds */}
				<div
					className={`space-y-3 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!healEnabled ? "opacity-50" : ""}`}
				>
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">
							Block-on-Repair Timeout (seconds)
						</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Maximum time a stalled read waits for self-heal before falling back to the failure
							path. Keep it below your player/VFS read timeout. Default 90.
						</p>
					</div>
					<input
						type="number"
						min="1"
						max="600"
						className="input input-bordered w-full"
						value={heal.block_on_repair_seconds ?? 90}
						disabled={isReadOnly || !healEnabled}
						onChange={(e) =>
							handleHealChange("block_on_repair_seconds", Number.parseInt(e.target.value, 10) || 0)
						}
					/>
				</div>

				{/* Min file size + media only */}
				<div
					className={`space-y-3 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!healEnabled ? "opacity-50" : ""}`}
				>
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Minimum File Size (MB)</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Skip proactive heal for files smaller than this. Default 50.
						</p>
					</div>
					<input
						type="number"
						min="0"
						className="input input-bordered w-full"
						value={heal.min_file_size_mb ?? 50}
						disabled={isReadOnly || !healEnabled}
						onChange={(e) =>
							handleHealChange("min_file_size_mb", Number.parseInt(e.target.value, 10) || 0)
						}
					/>
					<label className="flex cursor-pointer items-center justify-between pt-2">
						<span className="font-bold text-base-content text-sm">Media files only</span>
						<input
							type="checkbox"
							className="toggle toggle-primary"
							checked={heal.media_only !== false}
							disabled={isReadOnly || !healEnabled}
							onChange={(e) => handleHealChange("media_only", e.target.checked)}
						/>
					</label>
				</div>

				{/* Info box */}
				<div className="alert items-start rounded-2xl border border-info/20 bg-info/5 p-4 shadow-sm">
					<Info className="mt-0.5 h-5 w-5 shrink-0 text-info" />
					<div className="min-w-0 flex-1">
						<div className="font-bold text-info text-xs uppercase tracking-wider">Limitations</div>
						<div className="mt-1 break-words text-[11px] leading-relaxed opacity-80">
							PAR2 reconstruction needs the whole surviving file, so a hole can only be filled after
							that data is fetched. Sequential playback with proactive repair is usually seamless;
							random seeks into an unhealed region, or very large files, may still pause or fail.
						</div>
					</div>
				</div>
			</div>

			{/* PAR2 Repair Store */}
			<div className="border-base-200 border-t pt-10">
				<h3 className="font-bold text-base-content text-lg">PAR2 Repair Store</h3>
				<p className="text-base-content/50 text-sm">
					The independent in-memory landing zone for reconstructed segments. The reader serves
					recovered bytes from here, so self-heal works even when the on-disk segment cache is
					disabled (e.g. rclone VFS cache setups). It is kept small on purpose — recovered segments
					only need to live long enough for the client to re-read the healed range.
				</p>
			</div>

			<div className="space-y-8">
				{/* Max store size */}
				<div
					className={`space-y-3 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!par2RepairEnabled ? "opacity-50" : ""}`}
				>
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Max Store Size (MB)</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Caps the total reconstructed-segment bytes held in memory. When full, the oldest
							recovered segments are evicted first (LRU). Default 512.
						</p>
					</div>
					<input
						type="number"
						min="1"
						className="input input-bordered w-full"
						value={store.max_size_mb ?? 512}
						disabled={isReadOnly || !par2RepairEnabled}
						onChange={(e) =>
							handleRepairStoreChange("max_size_mb", Number.parseInt(e.target.value, 10) || 512)
						}
					/>
				</div>

				{/* Expiry minutes */}
				<div
					className={`space-y-3 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!par2RepairEnabled ? "opacity-50" : ""}`}
				>
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Segment Expiry (minutes)</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Drop recovered segments older than this. They only need to survive long enough for the
							player to re-read the freshly healed range. Default 60.
						</p>
					</div>
					<input
						type="number"
						min="1"
						className="input input-bordered w-full"
						value={store.expiry_minutes ?? 60}
						disabled={isReadOnly || !par2RepairEnabled}
						onChange={(e) =>
							handleRepairStoreChange("expiry_minutes", Number.parseInt(e.target.value, 10) || 60)
						}
					/>
				</div>

				{/* Info box */}
				<div className="alert items-start rounded-2xl border border-info/20 bg-info/5 p-4 shadow-sm">
					<Info className="mt-0.5 h-5 w-5 shrink-0 text-info" />
					<div className="min-w-0 flex-1">
						<div className="font-bold text-info text-xs uppercase tracking-wider">Sizing</div>
						<div className="mt-1 break-words text-[11px] leading-relaxed opacity-80">
							This store only holds the handful of segments PAR2 reconstructs for an actively
							streaming file — not the whole file. The defaults suit most setups; raise the size
							only if you heal many large files concurrently.
						</div>
					</div>
				</div>
			</div>

			{/* Segment Cache */}
			<div className="border-base-200 border-t pt-10">
				<h3 className="font-bold text-base-content text-lg">Segment Cache</h3>
				<p className="text-base-content/50 text-sm">
					Cache decoded Usenet segments on disk so repeated reads avoid network round-trips.
				</p>
				<p className="mt-1 text-base-content/60 text-sm">
					The segment cache applies regardless of the mount option chosen. It is recommended to
					disable it if rclone VFS cache is also enabled.
				</p>
			</div>

			<div className="space-y-8">
				{/* Enabled toggle */}
				<div className="flex items-center justify-between rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Enable Segment Cache</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							When enabled, decoded segments are stored on disk and shared by FUSE and WebDAV.
						</p>
					</div>
					<input
						type="checkbox"
						className="toggle toggle-primary"
						checked={cacheData.enabled === true}
						disabled={isReadOnly}
						onChange={(e) => handleCacheChange("enabled", e.target.checked)}
					/>
				</div>

				{/* Cache Path */}
				<div
					className={`space-y-3 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!cacheData.enabled ? "opacity-50" : ""}`}
				>
					<div className="min-w-0">
						<h4 className="font-bold text-base-content text-sm">Cache Path</h4>
						<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
							Directory where cached segment data is stored. Use a fast disk (SSD/NVMe) for best
							results.
						</p>
					</div>
					<input
						type="text"
						className="input input-bordered w-full"
						value={cacheData.cache_path}
						disabled={isReadOnly || !cacheData.enabled}
						placeholder="/tmp/altmount-segcache"
						onChange={(e) => handleCacheChange("cache_path", e.target.value)}
					/>
				</div>

				{/* Max Size slider */}
				<div
					className={`space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!cacheData.enabled ? "opacity-50" : ""}`}
				>
					<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
						<div className="min-w-0">
							<h4 className="font-bold text-base-content text-sm">Maximum Cache Size</h4>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Maximum disk space the segment cache may use before evicting old entries.
							</p>
						</div>
						<div className="flex shrink-0 items-center gap-3">
							<span className="font-black font-mono text-primary text-xl">
								{cacheData.max_size_gb}
							</span>
							<span className="font-bold text-base-content/60 text-xs uppercase">GB</span>
						</div>
					</div>

					<div className="space-y-4">
						<input
							type="range"
							min="1"
							max="1000"
							value={cacheData.max_size_gb}
							step="1"
							className="range range-primary range-sm w-full [&::-webkit-slider-runnable-track]:rounded-full"
							disabled={isReadOnly || !cacheData.enabled}
							onChange={(e) =>
								handleCacheChange("max_size_gb", Number.parseInt(e.target.value, 10))
							}
						/>
						<div className="flex justify-between px-2 font-black text-base-content/50 text-xs">
							<span>1</span>
							<span>250</span>
							<span>500</span>
							<span>750</span>
							<span>1000</span>
						</div>
					</div>
				</div>

				{/* Expiry slider */}
				<div
					className={`space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6 transition-opacity ${!cacheData.enabled ? "opacity-50" : ""}`}
				>
					<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
						<div className="min-w-0">
							<h4 className="overflow-visible whitespace-normal font-bold text-base-content text-sm">
								Cache Expiry
							</h4>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								How long cached segments are kept before automatic eviction.
							</p>
						</div>
						<div className="mt-1 flex shrink-0 items-center justify-start gap-3 sm:mt-0 sm:justify-end">
							<span className="font-black font-mono text-primary text-xl">
								{cacheData.expiry_hours}
							</span>
							<span className="font-bold text-base-content/60 text-xs uppercase">hours</span>
						</div>
					</div>

					<div className="space-y-4">
						<input
							type="range"
							min="1"
							max="168"
							value={cacheData.expiry_hours}
							step="1"
							className="range range-primary range-sm w-full [&::-webkit-slider-runnable-track]:rounded-full"
							disabled={isReadOnly || !cacheData.enabled}
							onChange={(e) =>
								handleCacheChange("expiry_hours", Number.parseInt(e.target.value, 10))
							}
						/>
						<div className="flex justify-between px-2 font-black text-base-content/50 text-xs">
							<span>1h</span>
							<span>42h</span>
							<span>84h</span>
							<span>126h</span>
							<span>168h</span>
						</div>
					</div>
				</div>

				{/* Info box */}
				<div className="alert items-start rounded-2xl border border-info/20 bg-info/5 p-4 shadow-sm">
					<Info className="mt-0.5 h-5 w-5 shrink-0 text-info" />
					<div className="min-w-0 flex-1">
						<div className="font-bold text-info text-xs uppercase tracking-wider">How It Works</div>
						<div className="mt-1 break-words text-[11px] leading-relaxed opacity-80">
							Each cached entry corresponds to one decoded Usenet article (~750 KB). On a cache hit
							the data is served directly from disk with no network round-trip. Eviction runs
							automatically every 5 minutes, removing expired entries and enforcing the size limit
							via LRU. Files that are currently open are never evicted.
						</div>
					</div>
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
