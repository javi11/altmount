import { Activity, Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, StreamCheckConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface StreamCheckConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: StreamCheckConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

const DEFAULTS: StreamCheckConfig = {
	enabled: false,
	segment_sample_percentage: 5,
	max_connections: 10,
	timeout_seconds: 15,
	acceptable_missing_percentage: 0,
	cache_ttl_minutes: 30,
	max_batch: 50,
};

function resolveConfig(c: StreamCheckConfig | undefined): StreamCheckConfig {
	return {
		enabled: c?.enabled ?? DEFAULTS.enabled,
		segment_sample_percentage: c?.segment_sample_percentage ?? DEFAULTS.segment_sample_percentage,
		max_connections: c?.max_connections ?? DEFAULTS.max_connections,
		timeout_seconds: c?.timeout_seconds ?? DEFAULTS.timeout_seconds,
		acceptable_missing_percentage:
			c?.acceptable_missing_percentage ?? DEFAULTS.acceptable_missing_percentage,
		cache_ttl_minutes: c?.cache_ttl_minutes ?? DEFAULTS.cache_ttl_minutes,
		max_batch: c?.max_batch ?? DEFAULTS.max_batch,
	};
}

function clampNum(raw: string, lo: number, hi: number): number {
	const n = Number(raw);
	if (!Number.isFinite(n)) return lo;
	return Math.min(hi, Math.max(lo, n));
}

export function StreamCheckConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: StreamCheckConfigSectionProps) {
	const [formData, setFormData] = useState<StreamCheckConfig>(resolveConfig(config.stream_check));
	const [hasChanges, setHasChanges] = useState(false);

	useEffect(() => {
		setFormData(resolveConfig(config.stream_check));
		setHasChanges(false);
	}, [config.stream_check]);

	const update = (patch: Partial<StreamCheckConfig>) => {
		const updated = { ...formData, ...patch };
		setFormData(updated);
		setHasChanges(JSON.stringify(updated) !== JSON.stringify(resolveConfig(config.stream_check)));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("stream_check", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="min-w-0 space-y-10">
			<div className="min-w-0 space-y-8">
				{/* Enable / Disable */}
				<div className="min-w-0 space-y-6 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Activity className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Endpoint
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="flex items-center justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h5 className="break-words font-bold text-sm">Enable Stream Check</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Activates the{" "}
								<code className="rounded bg-base-300 px-1 py-0.5 font-mono text-[10px]">
									POST /api/nzb/check
								</code>{" "}
								endpoint. Clients (e.g. AIOStreams) send an NZB URL and receive an availability
								verdict — sampled NNTP STAT, no import — so dead or incomplete releases can be
								filtered before playback.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary mt-1 shrink-0"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => update({ enabled: e.target.checked })}
						/>
					</div>
				</div>

				{/* Verification tuning */}
				{formData.enabled && (
					<div className="fade-in slide-in-from-top-2 min-w-0 animate-in space-y-6 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
						<div className="flex items-center gap-2">
							<Activity className="h-4 w-4 text-base-content/60" />
							<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
								Verification
							</h4>
							<div className="h-px flex-1 bg-base-300/50" />
						</div>

						<div className="grid min-w-0 grid-cols-1 gap-6 sm:grid-cols-2">
							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend">Segment Sample %</legend>
								<input
									type="number"
									className="input w-full min-w-0 max-w-full"
									min={1}
									max={100}
									value={formData.segment_sample_percentage}
									disabled={isReadOnly}
									onChange={(e) =>
										update({ segment_sample_percentage: clampNum(e.target.value, 1, 100) })
									}
								/>
								<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
									Percentage of a release's segments to STAT-sample (1–100). Lower is faster and
									cheaper.
								</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend">Max Connections</legend>
								<input
									type="number"
									className="input w-full min-w-0 max-w-full"
									min={1}
									max={100}
									value={formData.max_connections}
									disabled={isReadOnly}
									onChange={(e) => update({ max_connections: clampNum(e.target.value, 1, 100) })}
								/>
								<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
									Concurrent NNTP STAT requests per check.
								</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend">STAT Timeout (seconds)</legend>
								<input
									type="number"
									className="input w-full min-w-0 max-w-full"
									min={1}
									max={600}
									value={formData.timeout_seconds}
									disabled={isReadOnly}
									onChange={(e) => update({ timeout_seconds: clampNum(e.target.value, 1, 600) })}
								/>
								<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
									Per-segment STAT timeout before a segment is treated as missing.
								</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend">Acceptable Missing %</legend>
								<input
									type="number"
									className="input w-full min-w-0 max-w-full"
									min={0}
									max={100}
									step={0.5}
									value={formData.acceptable_missing_percentage}
									disabled={isReadOnly}
									onChange={(e) =>
										update({ acceptable_missing_percentage: clampNum(e.target.value, 0, 100) })
									}
								/>
								<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
									Share of sampled segments allowed missing before a release is reported{" "}
									<strong>dead</strong> rather than <strong>degraded</strong> (a few may be
									PAR2-recoverable).
								</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend">Cache TTL (minutes)</legend>
								<input
									type="number"
									className="input w-full min-w-0 max-w-full"
									min={0}
									value={formData.cache_ttl_minutes}
									disabled={isReadOnly}
									onChange={(e) =>
										update({ cache_ttl_minutes: clampNum(e.target.value, 0, 100000) })
									}
								/>
								<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
									How long a verdict is cached to avoid re-checking the same release. Use{" "}
									<strong>0</strong> to disable caching.
								</p>
							</fieldset>

							<fieldset className="fieldset min-w-0">
								<legend className="fieldset-legend">Max Batch Size</legend>
								<input
									type="number"
									className="input w-full min-w-0 max-w-full"
									min={1}
									max={1000}
									value={formData.max_batch}
									disabled={isReadOnly}
									onChange={(e) => update({ max_batch: clampNum(e.target.value, 1, 1000) })}
								/>
								<p className="label min-w-0 max-w-full whitespace-normal break-words text-base-content/50 text-xs">
									Maximum number of NZBs verified per request.
								</p>
							</fieldset>
						</div>
					</div>
				)}
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
