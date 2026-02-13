import { Save } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import type { ConfigResponse, WebDAVConfig } from "../../types/config";

interface WebDAVFormData extends WebDAVConfig {
	mount_path: string;
	debug?: boolean;
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
		debug: config.webdav.debug || false,
	});
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData({
			...config.webdav,
			mount_path: config.mount_path,
			debug: config.webdav.debug || false,
		});
		setHasChanges(false);
	}, [config.webdav, config.mount_path]);

	const handleInputChange = (field: keyof WebDAVFormData, value: string | boolean | number) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		const currentConfig = {
			...config.webdav,
			mount_path: config.mount_path,
			debug: config.webdav.debug || false,
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
				host: formData.host || "",
				debug: formData.debug || false,
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
		<div className="space-y-10">
			{/* Server Connection Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Server Connection</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">WebDAV Mount Path</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50"
							value={formData.mount_path}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("mount_path", e.target.value)}
							placeholder="/mnt/altmount"
						/>
						<p className="label text-[10px] opacity-60">Absolute host path where WebDAV is mounted.</p>
					</fieldset>

					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">External Host</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50"
							value={formData.host || ""}
							readOnly={isReadOnly}
							onChange={(e) => handleInputChange("host", e.target.value)}
							placeholder="localhost"
						/>
						<p className="label text-[10px] opacity-60">Hostname for .strm file generation.</p>
					</fieldset>

					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">Port</legend>
						<input
							type="number"
							className="input w-full bg-base-200/50 font-mono"
							value={formData.port}
							readOnly={isReadOnly}
							onChange={(e) => handleInputChange("port", Number.parseInt(e.target.value, 10) || 0)}
						/>
						<p className="label text-[10px] opacity-60">WebDAV server connection port.</p>
					</fieldset>
				</div>
			</section>

			{/* Authentication & Security Section */}
			<section className="space-y-6">
				<div className="flex items-center gap-2">
					<h4 className="font-bold text-[10px] text-base-content/40 text-xs uppercase tracking-widest">Authentication & Debug</h4>
					<div className="h-px flex-1 bg-base-300" />
				</div>

				<div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">Username</legend>
						<input
							type="text"
							className="input w-full bg-base-200/50 font-mono"
							value={formData.user}
							readOnly={isReadOnly}
							onChange={(e) => handleInputChange("user", e.target.value)}
							autoComplete="username"
						/>
					</fieldset>

					<fieldset className="fieldset min-w-0">
						<legend className="fieldset-legend font-semibold">Password</legend>
						<input
							type="password"
							className="input w-full bg-base-200/50 font-mono"
							value={formData.password}
							readOnly={isReadOnly}
							onChange={(e) => handleInputChange("password", e.target.value)}
							placeholder="••••••••"
							autoComplete="current-password"
						/>
					</fieldset>

					<fieldset className="fieldset flex min-w-0 flex-col justify-center">
						<legend className="fieldset-legend font-semibold">Debugging</legend>
						<label className="label cursor-pointer justify-start gap-4">
							<span className="label-text">Enable WebDAV Debug</span>
							<input
								type="checkbox"
								className="checkbox checkbox-primary"
								checked={formData.debug ?? false}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("debug", e.target.checked)}
							/>
						</label>
						<p className="label text-[10px] opacity-60">Verbose logging for WebDAV requests.</p>
					</fieldset>
				</div>
			</section>

			{/* RClone Integration Alert */}
			<div className="alert border-info/20 bg-info/5 shadow-sm">
				<div className="flex-1">
					<h4 className="flex items-center gap-2 font-bold text-info text-sm">
						<Link to="/config/rclone" className="hover:underline">🔧 Integrated RClone Mount Available</Link>
					</h4>
					<p className="mt-1 text-xs leading-relaxed opacity-80">
						We recommend using the integrated RClone service for automatic management, monitoring, and easier setup.
					</p>
				</div>
				<Link to="/config/rclone" className="btn btn-xs btn-info">Configure</Link>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end border-base-200 border-t pt-6">
					<button
						type="button"
						className={`btn btn-primary btn-md px-10 ${hasChanges ? "shadow-lg shadow-primary/20" : ""}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
					>
						{isUpdating ? <span className="loading loading-spinner loading-sm" /> : <Save className="h-4 w-4" />}
						Save WebDAV Configuration
					</button>
				</div>
			)}
		</div>
	);
}
