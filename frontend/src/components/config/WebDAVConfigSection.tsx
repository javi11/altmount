import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, WebDAVConfig } from "../../types/config";

interface WebDAVFormData extends WebDAVConfig {
	mount_path: string;
}

interface WebDAVConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: WebDAVConfig | { mount_path: string }) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function WebDAVConfigSection({
	config,
	onUpdate,
	isReadOnly = false, // WebDAV credentials are editable by default
	isUpdating = false,
}: WebDAVConfigSectionProps) {
	const [formData, setFormData] = useState<WebDAVFormData>({
		...config.webdav,
		mount_path: config.mount_path,
	});
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData({
			...config.webdav,
			mount_path: config.mount_path,
		});
		setHasChanges(false);
	}, [config.webdav, config.mount_path]);

	const handleInputChange = (field: keyof WebDAVFormData, value: string | boolean | number) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		const currentConfig = {
			...config.webdav,
			mount_path: config.mount_path,
		};
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(currentConfig));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			// Update WebDAV config
			const webdavData = {
				port: formData.port,
				user: formData.user,
				password: formData.password,
			};
			await onUpdate("webdav", webdavData);
			
			// Update mount_path separately if changed
			if (formData.mount_path !== config.mount_path) {
				await onUpdate("mount_path", { mount_path: formData.mount_path });
			}
			
			setHasChanges(false);
		}
	};
	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">WebDAV Server Settings</h3>
			
			{/* Mount Path Configuration */}
			<fieldset className="fieldset">
				<legend className="fieldset-legend">WebDAV Mount Path</legend>
				<input
					type="text"
					className="input"
					value={formData.mount_path}
					disabled={isReadOnly}
					onChange={(e) => handleInputChange("mount_path", e.target.value)}
					placeholder="/mnt/altmount"
				/>
				<p className="label">
					Absolute path where WebDAV is mounted. Required when ARRs is enabled. 
					This path will be stripped from file paths when communicating with Radarr/Sonarr.
				</p>
			</fieldset>
			
			<div className="grid grid-cols-1 gap-4 md:grid-cols-3">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Port</legend>
					<input
						type="number"
						className="input"
						value={formData.port}
						readOnly={isReadOnly}
						onChange={(e) => handleInputChange("port", Number.parseInt(e.target.value, 10) || 0)}
					/>
					<p className="label">WebDAV server port for client connections</p>
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
