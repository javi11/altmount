import { useEffect, useState } from "react";
import type { ConfigResponse, Par2RepairConfig } from "../../types/config";

interface Par2RepairConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: Par2RepairConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

// ratioToPercent renders the stored fraction as a percentage, rounded to two
// decimals so float noise (0.020000000000000004) never reaches the input.
function ratioToPercent(ratio: number | undefined): number {
	return Math.round((ratio ?? 0.02) * 10000) / 100;
}

const defaults: Par2RepairConfig = {
	enabled: true,
	max_repair_ratio: 0.02,
	max_memory_mb: 256,
	max_concurrent_jobs: 1,
	max_patch_store_mb: 0,
};

export function Par2RepairConfigSection({
	config,
	onUpdate,
	isReadOnly,
	isUpdating,
}: Par2RepairConfigSectionProps) {
	const [data, setData] = useState<Par2RepairConfig>({ ...defaults, ...config.par2_repair });
	const [hasChanges, setHasChanges] = useState(false);

	useEffect(() => {
		setData({ ...defaults, ...config.par2_repair });
		setHasChanges(false);
	}, [config.par2_repair]);

	const handleChange = <K extends keyof Par2RepairConfig>(field: K, value: Par2RepairConfig[K]) => {
		const next = { ...data, [field]: value };
		setData(next);
		setHasChanges(JSON.stringify(next) !== JSON.stringify({ ...defaults, ...config.par2_repair }));
	};

	const handleSave = async () => {
		if (!onUpdate || !hasChanges) return;
		await onUpdate("par2_repair", data);
		setHasChanges(false);
	};

	return (
		<div className="space-y-6">
			<div className="alert alert-info">
				<div className="text-sm">
					When a stream hits a missing (taken-down) article, AltMount reconstructs the missing bytes
					in the background from the release's PAR2 recovery files and serves them byte-exact from
					then on. A repair streams the whole release once over the import connection lane; repaired
					data is stored under the metadata directory and survives restarts.
				</div>
			</div>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">Enable PAR2 Repair</legend>
				<label className="label cursor-pointer justify-start gap-3">
					<input
						type="checkbox"
						className="toggle toggle-primary"
						checked={data.enabled ?? true}
						disabled={isReadOnly}
						onChange={(e) => handleChange("enabled", e.target.checked)}
					/>
					<span className="label-text">
						Repair missing articles automatically (playback holes, health checks)
					</span>
				</label>
			</fieldset>

			<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Max Repair Percentage</legend>
					<label className="input input-bordered flex w-full max-w-48 items-center gap-2 bg-base-100">
						<input
							type="number"
							className="grow font-mono text-sm"
							min={0}
							max={100}
							step={0.5}
							value={ratioToPercent(data.max_repair_ratio)}
							disabled={isReadOnly}
							onChange={(e) => handleChange("max_repair_ratio", Number(e.target.value) / 100)}
						/>
						<span className="text-base-content/60">%</span>
					</label>
					<p className="label whitespace-normal">
						Share of a file's bytes a repair may reconstruct (2% matches the zero-fill tolerance).
						The release's PAR2 redundancy is always the hard ceiling.
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Max Memory (MB)</legend>
					<input
						type="number"
						className="input input-bordered w-full max-w-48 bg-base-100 font-mono text-sm"
						min={16}
						value={data.max_memory_mb ?? 256}
						disabled={isReadOnly}
						onChange={(e) => handleChange("max_memory_mb", Number(e.target.value))}
					/>
					<p className="label whitespace-normal">
						Solver memory budget per job. Damage needing more than this is left to ARR replacement.
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Max Concurrent Jobs</legend>
					<input
						type="number"
						className="input input-bordered w-full max-w-48 bg-base-100 font-mono text-sm"
						min={1}
						value={data.max_concurrent_jobs ?? 1}
						disabled={isReadOnly}
						onChange={(e) => handleChange("max_concurrent_jobs", Number(e.target.value))}
					/>
					<p className="label whitespace-normal">
						Each job downloads a full release; more than 1 competes for import bandwidth.
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Patch Store Cap (MB)</legend>
					<input
						type="number"
						className="input input-bordered w-full max-w-48 bg-base-100 font-mono text-sm"
						min={0}
						value={data.max_patch_store_mb ?? 0}
						disabled={isReadOnly}
						onChange={(e) => handleChange("max_patch_store_mb", Number(e.target.value))}
					/>
					<p className="label whitespace-normal">
						Total size of stored repaired data; oldest patches are evicted first. 0 = unlimited.
						Evicted patches regenerate on the next playback.
					</p>
				</fieldset>
			</div>

			<button
				type="button"
				className="btn btn-primary"
				onClick={handleSave}
				disabled={!hasChanges || isUpdating || isReadOnly}
			>
				{isUpdating ? "Saving..." : "Save Changes"}
			</button>
		</div>
	);
}
