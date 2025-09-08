import { Copy, RefreshCw, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useAuth, useRegenerateAPIKey } from "../../hooks/useAuth";
import type { ConfigResponse, LogFormData } from "../../types/config";

interface SystemConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: LogFormData) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function SystemConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: SystemConfigSectionProps) {
	const [formData, setFormData] = useState<LogFormData>({
		file: config.log.file,
		level: config.log.level,
		max_size: config.log.max_size,
		max_age: config.log.max_age,
		max_backups: config.log.max_backups,
		compress: config.log.compress,
	});
	const [hasChanges, setHasChanges] = useState(false);

	// API Key functionality
	const { user, refreshToken } = useAuth();
	const regenerateAPIKey = useRegenerateAPIKey();
	const { confirmAction } = useConfirm();
	const { showToast } = useToast();

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = {
			file: config.log.file,
			level: config.log.level,
			max_size: config.log.max_size,
			max_age: config.log.max_age,
			max_backups: config.log.max_backups,
			compress: config.log.compress,
		};
		setFormData(newFormData);
		setHasChanges(false);
	}, [
		config.log.file,
		config.log.level,
		config.log.max_size,
		config.log.max_age,
		config.log.max_backups,
		config.log.compress,
	]);

	const handleInputChange = (field: keyof LogFormData, value: string | number | boolean) => {
		const newData = { ...formData, [field]: value };
		setFormData(newData);
		const configData = {
			file: config.log.file,
			level: config.log.level,
			max_size: config.log.max_size,
			max_age: config.log.max_age,
			max_backups: config.log.max_backups,
			compress: config.log.compress,
		};
		setHasChanges(JSON.stringify(newData) !== JSON.stringify(configData));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("log", formData);
			setHasChanges(false);
		}
	};

	const handleCopyAPIKey = async () => {
		if (user?.api_key) {
			try {
				await navigator.clipboard.writeText(user.api_key);
				showToast({
					type: "success",
					title: "Success",
					message: "API key copied to clipboard",
				});
			} catch (_error) {
				showToast({
					type: "error",
					title: "Error",
					message: "Failed to copy API key",
				});
			}
		}
	};

	const handleRegenerateAPIKey = async () => {
		const confirmed = await confirmAction(
			"Regenerate API Key",
			"This will generate a new API key and invalidate the current one. Make sure to update any applications using the old key.",
			{
				type: "warning",
				confirmText: "Regenerate API Key",
				confirmButtonClass: "btn-warning",
			},
		);

		if (confirmed) {
			try {
				await regenerateAPIKey.mutateAsync();
				// Refresh user data to get the new API key and update the UI
				await refreshToken();
				showToast({
					type: "success",
					title: "Success",
					message: "API key regenerated successfully",
				});
			} catch (_error) {
				showToast({
					type: "error",
					title: "Error",
					message: "Failed to regenerate API key",
				});
			}
		}
	};
	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">System</h3>
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Log Level</legend>
				<select
					className="select"
					value={formData.level}
					disabled={isReadOnly}
					onChange={(e) => handleInputChange("level", e.target.value)}
				>
					<option value="debug">Debug</option>
					<option value="info">Info</option>
					<option value="warn">Warning</option>
					<option value="error">Error</option>
				</select>
				<p className="label">Set the minimum logging level for the system</p>
			</fieldset>

			{/* API Key Section */}
			<fieldset className="fieldset">
				<legend className="fieldset-legend">API Key</legend>
				<div className="space-y-3">
					<div className="flex items-center space-x-2">
						<input
							type="text"
							className="input flex-1"
							value={user?.api_key ? user.api_key : "No API key generated"}
							readOnly
							disabled
						/>
						{user?.api_key && (
							<button
								type="button"
								className="btn btn-outline btn-sm"
								onClick={handleCopyAPIKey}
								title="Copy API key to clipboard"
							>
								<Copy className="h-4 w-4" />
							</button>
						)}
					</div>
					<div className="flex items-center space-x-2">
						<button
							type="button"
							className="btn btn-warning btn-sm"
							onClick={handleRegenerateAPIKey}
							disabled={regenerateAPIKey.isPending}
						>
							{regenerateAPIKey.isPending ? (
								<span className="loading loading-spinner loading-sm" />
							) : (
								<RefreshCw className="h-4 w-4" />
							)}
							{regenerateAPIKey.isPending ? "Regenerating..." : "Regenerate API Key"}
						</button>
					</div>
					<p className="label">
						Personal API key for authentication. Keep this secure and don't share it with others.
					</p>
				</div>
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
