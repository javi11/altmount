import { Save, Info } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, FuseConfig } from "../../types/config";

interface FuseConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: FuseConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function FuseConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: FuseConfigSectionProps) {
	const [formData, setFormData] = useState<FuseConfig>({
		enabled: config.fuse?.enabled ?? false,
		mount_point: config.fuse?.mount_point ?? "",
		readahead: config.fuse?.readahead ?? "128K",
		uid: config.fuse?.uid ?? 1000,
		gid: config.fuse?.gid ?? 1000,
	});
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		setFormData({
			enabled: config.fuse?.enabled ?? false,
			mount_point: config.fuse?.mount_point ?? "",
			readahead: config.fuse?.readahead ?? "128K",
			uid: config.fuse?.uid ?? 1000,
			gid: config.fuse?.gid ?? 1000,
		});
		setHasChanges(false);
	}, [config.fuse]);

	const handleInputChange = (field: keyof FuseConfig, value: string | boolean | number) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		
		const currentConfig = {
			enabled: config.fuse?.enabled ?? false,
			mount_point: config.fuse?.mount_point ?? "",
			readahead: config.fuse?.readahead ?? "128K",
			uid: config.fuse?.uid ?? 1000,
			gid: config.fuse?.gid ?? 1000,
		};
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(currentConfig));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("fuse", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-6">
			{/* Info Box */}
			<div className="alert alert-info shadow-sm">
				<Info className="h-5 w-5 flex-shrink-0" />
				<div>
					<h3 className="font-bold">Native FUSE Mount</h3>
					<div className="text-sm">
						Mount AltMount directly as a local filesystem using FUSE. This offers better performance
						and integration than WebDAV for local access on Linux systems.
					</div>
				</div>
			</div>

			<div className="form-control">
				<label className="label cursor-pointer justify-start gap-4">
					<input
						type="checkbox"
						className="toggle toggle-primary"
						checked={formData.enabled}
						onChange={(e) => handleInputChange("enabled", e.target.checked)}
						disabled={isReadOnly}
					/>
					<span className="label-text font-medium text-lg">Enable FUSE Mount</span>
				</label>
				<p className="label-text-alt ml-14">
					Enable the native FUSE filesystem mount. Requires FUSE libraries to be installed on the system.
				</p>
			</div>

			<div className="divider"></div>

			<div className="grid grid-cols-1 gap-6 md:grid-cols-2">
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Mount Point</legend>
					<input
						type="text"
						className="input w-full"
						value={formData.mount_point}
						disabled={isReadOnly || !formData.enabled}
						onChange={(e) => handleInputChange("mount_point", e.target.value)}
						placeholder="/mnt/altmount-fuse"
					/>
					<p className="label">
						Absolute path where the filesystem will be mounted. Directory must exist or be creatable.
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Readahead Size</legend>
					<input
						type="text"
						className="input w-full"
						value={formData.readahead}
						disabled={isReadOnly || !formData.enabled}
						onChange={(e) => handleInputChange("readahead", e.target.value)}
						placeholder="128K"
					/>
					<p className="label">
						Read-ahead buffer size (e.g., 128K, 4M). Larger values can improve streaming performance.
					</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Owner UID</legend>
					<input
						type="number"
						className="input w-full"
						value={formData.uid}
						disabled={isReadOnly || !formData.enabled}
						onChange={(e) => handleInputChange("uid", Number.parseInt(e.target.value, 10) || 0)}
						placeholder="1000"
					/>
					<p className="label">User ID that will own the mounted files.</p>
				</fieldset>

				<fieldset className="fieldset">
					<legend className="fieldset-legend">Owner GID</legend>
					<input
						type="number"
						className="input w-full"
						value={formData.gid}
						disabled={isReadOnly || !formData.enabled}
						onChange={(e) => handleInputChange("gid", Number.parseInt(e.target.value, 10) || 0)}
						placeholder="1000"
					/>
					<p className="label">Group ID that will own the mounted files.</p>
				</fieldset>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end pt-4">
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
