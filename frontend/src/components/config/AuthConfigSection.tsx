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
					<label className="label cursor-pointer justify-start gap-4">
						<span className="label-text">
							Require user authentication
						</span>
						<input
							type="checkbox"
							className="checkbox"
							checked={formData.login_required}
							disabled={isReadOnly}
							onChange={(e) => handleToggle(e.target.checked)}
						/>
					</label>
					<p className="label text-xs sm:text-sm">
						Enforces login requirement for the web interface.
					</p>

					{!formData.login_required && (
						<div className="alert alert-warning py-3 sm:py-4">
							<AlertTriangle className="h-6 w-6" />
							<div className="text-xs sm:text-sm">
								<div className="font-bold">Security Warning</div>
								<p>
									Disabling login requirement allows anyone to access your application.
								</p>
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
						className="btn btn-primary w-full sm:w-auto"
						onClick={handleSave}
						disabled={!hasChanges || isUpdating}
					>
						{isUpdating ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Save className="h-4 w-4" />
						)}
						Save Changes
					</button>
				</div>
			)}
		</div>
	);
}
