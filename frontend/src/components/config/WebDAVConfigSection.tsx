import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, WebDAVConfig } from "../../types/config";

interface WebDAVConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: WebDAVConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function WebDAVConfigSection({
	config,
	onUpdate,
	isReadOnly = false, // WebDAV credentials are editable by default
	isUpdating = false,
}: WebDAVConfigSectionProps) {
	const [formData, setFormData] = useState<WebDAVConfig>(config.webdav);
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData(config.webdav);
		setHasChanges(false);
	}, [config.webdav]);

	const handleInputChange = (
		field: keyof WebDAVConfig,
		value: string | boolean | number,
	) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(config.webdav));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("webdav", formData);
			setHasChanges(false);
		}
	};
	return (
		<div className="space-y-4">
			<h3 className="text-lg font-semibold">WebDAV Server Settings</h3>
			<div className="grid grid-cols-1 md:grid-cols-3 gap-4">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Port</legend>
					<input
						type="number"
						className="input"
						value={formData.port}
						readOnly={true} // Port always requires restart
					/>
					<p className="label">Server restart required to change port</p>
				</fieldset>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Username</legend>
					<input
						type="text"
						className="input"
						value={formData.user}
						readOnly={isReadOnly}
						onChange={(e) => handleInputChange("user", e.target.value)}
					/>
				</fieldset>
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Password</legend>
					<input
						type="password"
						className="input"
						value={formData.password}
						readOnly={isReadOnly}
						onChange={(e) => handleInputChange("password", e.target.value)}
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
						checked={formData.debug}
						disabled={isReadOnly}
						onChange={(e) => handleInputChange("debug", e.target.checked)}
					/>
				</label>
			</fieldset>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end">
					<button
						type="button"
						className="btn btn-primary"
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
