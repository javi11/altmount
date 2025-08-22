import type { ConfigResponse } from "../../types/config";

interface WebDAVConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: any) => void;
	isReadOnly?: boolean;
}

export function WebDAVConfigSection({
	config,
	onUpdate,
	isReadOnly = true,
}: WebDAVConfigSectionProps) {
	return (
		<div className="space-y-4">
			<h3 className="text-lg font-semibold">WebDAV Server Settings</h3>
			<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Username</legend>
					<input
						type="text"
						className="input"
						value={config.webdav.user}
						readOnly={isReadOnly}
						onChange={(e) =>
							onUpdate?.("webdav", {
								...config.webdav,
								user: e.target.value,
							})
						}
					/>
				</fieldset>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Password</legend>
					<input
						type="password"
						className="input"
						value={config.webdav.password}
						readOnly={isReadOnly}
						onChange={(e) =>
							onUpdate?.("webdav", {
								...config.webdav,
								password: e.target.value,
							})
						}
					/>
					<p className="label">WebDAV server password</p>
				</fieldset>
			</div>
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Debug Mode</legend>
				<label className="cursor-pointer label">
					<span className="label-text">Enable WebDAV debug logging</span>
					<input
						type="checkbox"
						className="checkbox"
						checked={config.webdav.debug}
						disabled={isReadOnly}
						onChange={(e) =>
							onUpdate?.("webdav", {
								...config.webdav,
								debug: e.target.checked,
							})
						}
					/>
				</label>
			</fieldset>
		</div>
	);
}