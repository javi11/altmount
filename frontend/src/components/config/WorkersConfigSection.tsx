import type { ConfigResponse } from "../../types/config";

interface ImportConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: any) => void;
	isReadOnly?: boolean;
}

export function ImportConfigSection({
	config,
	onUpdate,
	isReadOnly = true,
}: ImportConfigSectionProps) {
	return (
		<div className="space-y-4">
			<h3 className="text-lg font-semibold">Import Processing Configuration</h3>
			<div className="grid grid-cols-1 gap-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Processor Workers</legend>
					<input
						type="number"
						className="input"
						value={config.import.max_processor_workers}
						readOnly={isReadOnly}
						min={1}
						max={10}
						onChange={(e) =>
							onUpdate?.("import", {
								...config.import,
								max_processor_workers: Number.parseInt(e.target.value),
							})
						}
					/>
					<p className="label">Number of NZB processing threads</p>
				</fieldset>
			</div>
		</div>
	);
}
