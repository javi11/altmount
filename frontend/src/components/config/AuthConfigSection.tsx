import { AlertTriangle, Save, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import type { AuthConfig, ConfigResponse } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

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
		<div className="space-y-10">
			<div>
				<h3 className="text-lg font-bold text-base-content tracking-tight">Security & Access</h3>
				<p className="text-sm text-base-content/50 break-words">Control how users authenticate with the AltMount web interface.</p>
			</div>

			<div className="space-y-8">
				{/* Login Required Toggle */}
				<div className="rounded-2xl border border-base-300 bg-base-200/30 p-6 space-y-6">
					<div className="flex items-center gap-2">
						<ShieldCheck className="h-4 w-4 opacity-40" />
						<h4 className="font-bold text-[10px] text-base-content/40 uppercase tracking-widest">Authentication</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="flex items-center justify-between gap-4 items-start">
						<div className="min-w-0 flex-1">
							<h5 className="font-bold text-sm break-words">Require Login</h5>
							<p className="text-[11px] text-base-content/50 break-words mt-1 leading-relaxed">
								Force users to sign in before accessing the dashboard or settings.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary shrink-0 mt-1"
							checked={formData.login_required}
							disabled={isReadOnly}
							onChange={(e) => handleToggle(e.target.checked)}
						/>
					</div>

					{!formData.login_required && (
						<div className="alert rounded-xl border border-warning/20 bg-warning/5 py-3 px-4 animate-in zoom-in-95 items-start">
							<AlertTriangle className="h-5 w-5 text-warning shrink-0 mt-0.5" />
							<div className="min-w-0">
								<div className="font-bold text-xs uppercase tracking-wider text-warning">Security Risk</div>
								<div className="text-[11px] leading-relaxed mt-1 opacity-80 break-words">
									Your interface is currently public. Anyone with network access can change your 
									configuration and download clients. Ensure you have external security (e.g., VPN).
								</div>
							</div>
						</div>
					)}
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
