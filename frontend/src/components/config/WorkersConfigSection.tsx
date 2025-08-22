import type { ConfigResponse } from "../../types/config";

interface WorkersConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: any) => void;
	isReadOnly?: boolean;
}

export function WorkersConfigSection({
	config,
	onUpdate,
	isReadOnly = true,
}: WorkersConfigSectionProps) {
	return (
		<div className="space-y-4">
			<h3 className="text-lg font-semibold">Worker Configuration</h3>
			<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Download Workers</legend>
					<input
						type="number"
						className="input"
						value={config.workers.download}
						readOnly={isReadOnly}
						min={1}
						max={20}
						onChange={(e) =>
							onUpdate?.("workers", {
								...config.workers,
								download: Number.parseInt(e.target.value),
							})
						}
					/>
					<p className="label">Number of concurrent download threads</p>
				</fieldset>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Processor Workers</legend>
					<input
						type="number"
						className="input"
						value={config.workers.processor}
						readOnly={isReadOnly}
						min={1}
						max={10}
						onChange={(e) =>
							onUpdate?.("workers", {
								...config.workers,
								processor: Number.parseInt(e.target.value),
							})
						}
					/>
					<p className="label">Number of NZB processing threads</p>
				</fieldset>
			</div>
		</div>
	);
}
