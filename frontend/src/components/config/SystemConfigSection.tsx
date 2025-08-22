import type { ConfigResponse } from "../../types/config";

interface SystemConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: any) => void;
	isReadOnly?: boolean;
}

export function SystemConfigSection({
	config,
	onUpdate,
	isReadOnly = true,
}: SystemConfigSectionProps) {
	return (
		<div className="space-y-4">
			<h3 className="text-lg font-semibold">System Paths</h3>
			<div className="space-y-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Watch Path</legend>
					<input
						type="text"
						className="input"
						value={config.watch_path}
						readOnly={isReadOnly}
						onChange={(e) =>
							onUpdate?.("system", {
								watch_path: e.target.value,
							})
						}
					/>
					<p className="label">Directory containing the files to be imported</p>
				</fieldset>
			</div>
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Global Debug Mode</legend>
				<label className="cursor-pointer label">
					<span className="label-text">Enable system-wide debug logging</span>
					<input
						type="checkbox"
						className="checkbox"
						checked={config.debug}
						disabled={isReadOnly}
						onChange={(e) =>
							onUpdate?.("system", {
								debug: e.target.checked,
							})
						}
					/>
				</label>
			</fieldset>
		</div>
	);
}
