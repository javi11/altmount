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

				{/* Rclone Mount Instructions */}
				<div className="mt-4 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg">
					<h4 className="font-medium text-sm mb-2">How to create the mount using rclone:</h4>
					<div className="text-sm text-gray-600 dark:text-gray-400 space-y-2">
						<p>1. Install rclone if not already installed</p>
						<p>2. Create the mount directory:</p>
						<code className="block bg-gray-100 dark:bg-gray-700 p-2 rounded text-xs font-mono">
							sudo mkdir -p {formData.mount_path || "/mnt/altmount"}
						</code>
						<p>3. Mount the WebDAV server:</p>
						<code className="block bg-gray-100 dark:bg-gray-700 p-2 rounded text-xs font-mono whitespace-pre-wrap">
							rclone mount webdav: {formData.mount_path || "/mnt/altmount"} \
							--webdav-url=http://localhost:{formData.port || "8080"}/webdav \
							--webdav-user={formData.user || "username"} \
							--webdav-pass={formData.password || "password"} \
							--async-read=true \
							--allow-non-empty \
							--allow-other \
							--rc \
							--rc-no-auth \
							--rc-addr=0.0.0.0:5573 \
							--vfs-read-ahead=128M \
							--vfs-read-chunk-size=32M \
							--vfs-read-chunk-size-limit=2G \
							--vfs-cache-mode=full \
							--vfs-cache-max-age=504h \
							--vfs-cache-max-size=50G \
							--vfs-cache-poll-interval=30s \
							--buffer-size=32M \
							--dir-cache-time=10m \
							--timeout=10m \
							--umask=002 \
							--syslog \
							--daemon
						</code>
						<p className="text-xs text-gray-500 dark:text-gray-500">
							Note: Replace the placeholder values with your actual WebDAV configuration.
							The <code>--allow-other</code> flag requires additional system configuration.
						</p>
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
