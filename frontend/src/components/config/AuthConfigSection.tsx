import { AlertTriangle, Save } from "lucide-react";
import { useEffect, useState } from "react";
import type { AuthConfig, ConfigResponse } from "../../types/config";

interface AuthConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: AuthConfig) => Promise<void>;
	isReadOnly?: boolean;
	isUpdating?: boolean;
}

export function AuthConfigSection({
	config,
	onUpdate,
	isReadOnly = false,
	isUpdating = false,
}: AuthConfigSectionProps) {
	const [formData, setFormData] = useState<AuthConfig>({
		login_required: config.auth.login_required,
	});
	const [hasChanges, setHasChanges] = useState(false);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = {
			login_required: config.auth.login_required,
		};
		setFormData(newFormData);
		setHasChanges(false);
	}, [config.auth.login_required]);

	const handleToggle = (value: boolean) => {
		const newData = { ...formData, login_required: value };
		setFormData(newData);
		setHasChanges(value !== config.auth.login_required);
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			await onUpdate("auth", formData);
			setHasChanges(false);
		}
	};

	return (
		<div className="space-y-4">
			<h3 className="font-semibold text-lg">Authentication</h3>

			{/* Login Required Setting */}
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Login Required</legend>
				<div className="space-y-3">
					<label className="label cursor-pointer">
						<span className="label-text">
							Require user authentication to access the web application
						</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.login_required}
							disabled={isReadOnly}
							onChange={(e) => handleToggle(e.target.checked)}
						/>
					</label>
					<p className="label">
						When enabled, users must log in to access the web interface. When disabled, anyone can
						access the application without authentication.
					</p>

					{!formData.login_required && (
						<div className="alert alert-warning">
							<AlertTriangle className="h-6 w-6" />
							<div>
								<div className="font-bold">Security Warning</div>
								<div className="text-sm">
									Disabling login requirement will allow anyone to access your application without
									authentication. Only disable this if you have other security measures in place
									(firewall, VPN, etc.).
								</div>
							</div>
						</div>
					)}
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
