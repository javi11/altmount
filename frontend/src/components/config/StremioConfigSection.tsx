import { Info, Save, Tv } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, StremioConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface StremioConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: StremioConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function StremioConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: StremioConfigSectionProps) {
	const [formData, setFormData] = useState<StremioConfig>({
		enabled: config.stremio?.enabled ?? false,
		nzb_ttl_hours: config.stremio?.nzb_ttl_hours ?? 24,
	});
	const [hasChanges, setHasChanges] = useState(false);

	useEffect(() => {
		const newFormData: StremioConfig = {
			enabled: config.stremio?.enabled ?? false,
			nzb_ttl_hours: config.stremio?.nzb_ttl_hours ?? 24,
		};
		setFormData(newFormData);
		setHasChanges(false);
	}, [config.stremio?.enabled, config.stremio?.nzb_ttl_hours]);

	const handleToggle = (value: boolean) => {
		const updated = { ...formData, enabled: value };
		setFormData(updated);
		setHasChanges(
			value !== (config.stremio?.enabled ?? false) ||
				updated.nzb_ttl_hours !== (config.stremio?.nzb_ttl_hours ?? 24),
		);
	};

	const handleTTLChange = (value: number) => {
		const updated = { ...formData, nzb_ttl_hours: value };
		setFormData(updated);
		setHasChanges(
			updated.enabled !== (config.stremio?.enabled ?? false) ||
				value !== (config.stremio?.nzb_ttl_hours ?? 24),
		);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("stremio", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">Stremio Integration</h3>
				<p className="break-words text-base-content/50 text-sm">
					Enable the NZB stream endpoint so Stremio addons can upload an NZB and receive instant
					HTTP stream URLs for playback.
				</p>
			</div>

			<div className="space-y-8">
				{/* Enable / Disable */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Tv className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Endpoint
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="flex items-center justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h5 className="break-words font-bold text-sm">Enable Stremio Endpoint</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Activates{" "}
								<code className="rounded bg-base-300 px-1 py-0.5 font-mono text-[10px]">
									POST /api/nzb/streams
								</code>
								. When disabled the endpoint returns 404.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary mt-1 shrink-0"
							checked={formData.enabled}
							disabled={isReadOnly}
							onChange={(e) => handleToggle(e.target.checked)}
						/>
					</div>
				</div>

				{/* Cache TTL */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Info className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Cache
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">NZB Result Cache TTL (hours)</legend>
						<input
							type="number"
							className="input w-32"
							min={0}
							value={formData.nzb_ttl_hours}
							disabled={isReadOnly}
							onChange={(e) => handleTTLChange(Math.max(0, Number(e.target.value)))}
						/>
						<p className="label">
							How long a processed NZB result is cached before re-processing on the next request.
							Set to <strong>0</strong> to cache forever.
						</p>
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
