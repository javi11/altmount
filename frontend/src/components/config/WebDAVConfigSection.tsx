import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
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
					Absolute path where WebDAV is mounted. Required when ARRs is enabled. This path will be
					stripped from file paths when communicating with Radarr/Sonarr.
				</p>

				{/* Automated RClone Mount */}
				<div className="mt-4 rounded-lg border border-info/20 bg-info/5 p-4">
					<h4 className="mb-3 font-medium text-info text-sm">🔧 Automated RClone Mount</h4>
					<div className="space-y-3 text-base-content/80 text-sm">
						<p>
							You can now mount this WebDAV server automatically using our integrated RClone
							configuration. No manual command-line setup required!
						</p>

						<div className="flex items-center gap-3">
							<Link to="/config/rclone" className="btn btn-info btn-sm">
								Configure RClone Mount
							</Link>
						</div>

						<div className="space-y-1 text-xs">
							<div className="font-medium text-base-content/70">Benefits:</div>
							<ul className="ml-4 list-disc space-y-0.5 text-base-content/60">
								<li>No manual command-line setup</li>
								<li>Automatic configuration management</li>
								<li>Built-in mount status monitoring</li>
								<li>Easy enable/disable toggle</li>
							</ul>
						</div>
					</div>
				</div>
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
