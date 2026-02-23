import { Globe, Info, Key, Save, Server } from "lucide-react";
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
				<h3 className="font-bold text-base-content text-lg tracking-tight">WebDAV Interface</h3>
				<p className="break-words text-base-content/50 text-sm">
					Expose your virtual library over the network via WebDAV protocol.
				</p>
			</div>

			<div className="space-y-8">
				{/* Network Configuration */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Globe className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Network Access
						</h4>
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
							<p className="label mt-2 break-words text-base-content/70 text-xs">
								Required for .strm file generation.
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend font-semibold">Port</legend>
							<input
								type="number"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.port}
								readOnly={isReadOnly}
								onChange={(e) =>
									handleInputChange("port", Number.parseInt(e.target.value, 10) || 0)
								}
							/>
							<p className="label mt-2 break-words text-base-content/70 text-xs">
								TCP port for server binding.
							</p>
						</fieldset>
					</div>
				</div>

				{/* Credentials Section */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Key className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Security
						</h4>
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
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Server className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							System Integration
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<fieldset className="fieldset">
						<legend className="fieldset-legend whitespace-normal break-words font-semibold md:whitespace-nowrap">
							WebDAV Mount Path
						</legend>
						<div className="flex flex-col gap-3">
							<input
								type="text"
								className="input input-bordered w-full bg-base-100 font-mono text-sm"
								value={formData.mount_path}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("mount_path", e.target.value)}
								placeholder="/mnt/remotes/altmount"
							/>
							<div className="mt-2 whitespace-normal text-base-content/50 text-xs leading-relaxed">
								Path where WebDAV is mounted. This is used to resolve ARR paths back to virtual
								files. Required for healthy repairs.
							</div>
						</div>
					</fieldset>

					<div className="fade-in slide-in-from-bottom-2 animate-in rounded-xl border border-info/20 bg-info/5 p-5">
						<div className="flex items-start gap-4">
							<Info className="mt-0.5 h-5 w-5 shrink-0 text-info" />
							<div className="min-w-0 flex-1 space-y-4">
								<div className="min-w-0">
									<h5 className="break-words font-bold text-info text-xs uppercase tracking-wider">
										Pro-Tip: Integrated Mounting
									</h5>
									<p className="mt-1 break-words text-[11px] leading-relaxed opacity-80">
										Avoid manual setup! Use our built-in high-performance mount engine for automatic
										connectivity and lifecycle management.
									</p>
								</div>
								<Link to="/config/mount" className="btn btn-info btn-sm h-8 px-4 shadow-sm">
									Configure Auto-Mount
								</Link>
							</div>
						</div>
					</div>
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end border-base-200 border-t pt-4">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && "btn-ghost border-base-300"}`}
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
