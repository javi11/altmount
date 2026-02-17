import { Info, Save, Server, Globe, Key } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import type { ConfigResponse, WebDAVConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

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
	isReadOnly = false,
	isUpdating = false,
}: WebDAVConfigSectionProps) {
	const [formData, setFormData] = useState<WebDAVFormData>({
		...config.webdav,
		mount_path: config.mount_path,
	});
	const [hasChanges, setHasChanges] = useState(false);

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
			const webdavData = {
				port: formData.port,
				user: formData.user,
				password: formData.password,
				host: formData.host || "",
			};
			await onUpdate("webdav", webdavData);
			if (formData.mount_path !== config.mount_path) {
				await onUpdate("mount_path", { mount_path: formData.mount_path });
			}
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="text-lg font-bold text-base-content tracking-tight">WebDAV Interface</h3>
				<p className="text-sm text-base-content/50 break-words">Expose your virtual library over the network via WebDAV protocol.</p>
			</div>

			<div className="space-y-8">
				{/* Network Configuration */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<div className="flex items-center gap-2">
						<Globe className="h-4 w-4 opacity-40" />
						<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Network Access</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">External Hostname</legend>
							<input
								type="text"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.host || ""}
								readOnly={isReadOnly}
								onChange={(e) => handleInputChange("host", e.target.value)}
								placeholder="localhost"
							/>
							<p className="label text-[10px] opacity-50 break-words mt-2">Required for .strm file generation.</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Port</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.port}
								readOnly={isReadOnly}
								onChange={(e) => handleInputChange("port", Number.parseInt(e.target.value, 10) || 0)}
							/>
							<p className="label text-[10px] opacity-50 break-words mt-2">TCP port for server binding.</p>
						</fieldset>
					</div>
				</div>

				{/* Credentials Section */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<div className="flex items-center gap-2">
						<Key className="h-4 w-4 opacity-40" />
						<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Security</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Username</legend>
							<input
								type="text"
								className="input input-bordered w-full bg-base-100 text-sm"
								value={formData.user}
								readOnly={isReadOnly}
								onChange={(e) => handleInputChange("user", e.target.value)}
							/>
						</fieldset>
						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Password</legend>
							<input
								type="password"
								className="input input-bordered w-full bg-base-100 text-sm"
								value={formData.password}
								readOnly={isReadOnly}
								onChange={(e) => handleInputChange("password", e.target.value)}
							/>
						</fieldset>
					</div>
				</div>

				{/* Mount Path Integration */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<div className="flex items-center gap-2">
						<Server className="h-4 w-4 opacity-40" />
						<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">System Integration</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend font-semibold break-words">WebDAV Mount Path</legend>
						<div className="flex flex-col gap-3">
							<input
								type="text"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.mount_path}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("mount_path", e.target.value)}
								placeholder="/mnt/remotes/altmount"
							/>
							<div className="label p-0">
								<span className="text-[10px] text-base-content/50 break-words leading-relaxed">
									Path where WebDAV is mounted. This is used to resolve ARR paths back to virtual files.
									Required for healthy repairs.
								</span>
							</div>
						</div>
					</fieldset>

					<div className="rounded-xl border border-info/20 bg-info/5 p-5 animate-in fade-in slide-in-from-bottom-2">
						<div className="flex gap-4 items-start">
							<Info className="h-5 w-5 text-info shrink-0 mt-0.5" />
							<div className="space-y-4 min-w-0 flex-1">
								<div className="min-w-0">
									<h5 className="font-bold text-xs uppercase tracking-wider text-info break-words">Pro-Tip: Integrated Mounting</h5>
									<p className="text-[11px] leading-relaxed mt-1 opacity-80 break-words">
										Avoid manual setup! Use our built-in high-performance mount engine for automatic
										connectivity and lifecycle management.
									</p>
								</div>
								<Link to="/config/mount" className="btn btn-info btn-xs px-4 shadow-sm h-8">
									Configure Auto-Mount
								</Link>
							</div>
						</div>
					</div>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end pt-4 border-t border-base-200">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && 'btn-ghost border-base-300'}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
					>
						{isUpdating ? <LoadingSpinner size="sm" /> : <Save className="h-4 w-4" />}
						{isUpdating ? "Saving..." : "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
