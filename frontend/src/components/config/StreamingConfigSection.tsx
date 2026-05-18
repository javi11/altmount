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
