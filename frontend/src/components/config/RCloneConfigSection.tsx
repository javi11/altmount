import { Save, Eye, EyeOff } from "lucide-react";
import { useEffect, useState } from "react";
import type { ConfigResponse, RCloneVFSFormData } from "../../types/config";

interface RCloneConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: RCloneVFSFormData) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function RCloneConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: RCloneConfigSectionProps) {
	const [formData, setFormData] = useState<RCloneVFSFormData>({
		vfs_enabled: config.rclone.vfs_enabled,
		vfs_url: config.rclone.vfs_url,
		vfs_user: config.rclone.vfs_user,
		vfs_pass: "",
	});
	const [hasChanges, setHasChanges] = useState(false);
	const [showVFSPassword, setShowVFSPassword] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = {
			vfs_enabled: config.rclone.vfs_enabled,
			vfs_url: config.rclone.vfs_url,
			vfs_user: config.rclone.vfs_user,
			vfs_pass: "",
		};
		setFormData(newFormData);
		setHasChanges(false);
	}, [
		config.rclone.vfs_enabled,
		config.rclone.vfs_url,
		config.rclone.vfs_user,
	]);

	const handleInputChange = (field: keyof RCloneVFSFormData, value: string | boolean) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		
		// Check for changes by comparing against original config
		const configData = {
			vfs_enabled: config.rclone.vfs_enabled,
			vfs_url: config.rclone.vfs_url,
			vfs_user: config.rclone.vfs_user,
			vfs_pass: "",
		};
		
		// Always consider changes if VFS password is entered
		const vfsPasswordChanged = newData.vfs_pass !== "";
		const otherFieldsChanged = 
			newData.vfs_enabled !== configData.vfs_enabled ||
			newData.vfs_url !== configData.vfs_url ||
			newData.vfs_user !== configData.vfs_user;
		
		setHasChanges(vfsPasswordChanged || otherFieldsChanged);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			// Only send non-empty values for VFS password
			const updateData: Partial<RCloneVFSFormData> = {
				vfs_enabled: formData.vfs_enabled,
				vfs_url: formData.vfs_url,
				vfs_user: formData.vfs_user,
			};
			
			if (formData.vfs_pass.trim() !== "") {
				updateData.vfs_pass = formData.vfs_pass;
			}
			
			await onUpdate("rclone", updateData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">RClone VFS Configuration</h3>
			
			{/* VFS Settings */}
			<div className="space-y-4">
				<h4 className="font-medium text-base">VFS Notification Settings</h4>
				
				<fieldset className="fieldset">
					<legend className="fieldset-legend">Enable VFS Notifications</legend>
					<label className="label cursor-pointer">
						<span className="label-text">Enable RClone VFS cache refresh notifications</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.vfs_enabled}
							disabled={isReadOnly}
							onChange={(e) => handleInputChange("vfs_enabled", e.target.checked)}
						/>
					</label>
					<p className="label">
						Automatically notify RClone VFS when new files are imported to refresh the cache
					</p>
				</fieldset>

				{formData.vfs_enabled && (
					<>
						<fieldset className="fieldset">
							<legend className="fieldset-legend">VFS URL</legend>
							<input
								type="text"
								className="input"
								value={formData.vfs_url}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("vfs_url", e.target.value)}
								placeholder="http://localhost:5572"
							/>
							<p className="label">
								RClone RC API URL (e.g., http://localhost:5572)
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">VFS Username</legend>
							<input
								type="text"
								className="input"
								value={formData.vfs_user}
								disabled={isReadOnly}
								onChange={(e) => handleInputChange("vfs_user", e.target.value)}
								placeholder="Enter VFS username (optional)"
							/>
							<p className="label">
								Username for RClone RC API authentication (optional)
							</p>
						</fieldset>

						<fieldset className="fieldset">
							<legend className="fieldset-legend">VFS Password</legend>
							<div className="relative">
								<input
									type={showVFSPassword ? "text" : "password"}
									className="input pr-10"
									value={formData.vfs_pass}
									disabled={isReadOnly}
									onChange={(e) => handleInputChange("vfs_pass", e.target.value)}
									placeholder={config.rclone.vfs_pass_set ? "VFS password is set (enter new to change)" : "Enter VFS password (optional)"}
								/>
								<button
									type="button"
									className="-translate-y-1/2 btn btn-ghost btn-xs absolute top-1/2 right-2"
									onClick={() => setShowVFSPassword(!showVFSPassword)}
								>
									{showVFSPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
								</button>
							</div>
							<p className="label">
								Password for RClone RC API authentication (optional)
								{config.rclone.vfs_pass_set && " (currently set)"}
							</p>
						</fieldset>
					</>
				)}
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