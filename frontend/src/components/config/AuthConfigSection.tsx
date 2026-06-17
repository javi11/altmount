import { AlertTriangle, KeyRound, Save, ShieldCheck, UserCheck } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { apiClient } from "../../api/client";
import type { AuthConfig, ConfigResponse } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface CredentialForm {
	username: string;
	password: string;
	confirmPassword: string;
}

interface RegistrationStatus {
	registration_enabled: boolean;
	user_count: number;
}

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
	const [registrationStatus, setRegistrationStatus] = useState<RegistrationStatus | null>(null);
	const [credentialForm, setCredentialForm] = useState<CredentialForm>({
		username: "",
		password: "",
		confirmPassword: "",
	});
	const [credentialError, setCredentialError] = useState<string | null>(null);
	const [isRegistering, setIsRegistering] = useState(false);

	const fetchRegistrationStatus = useCallback(async () => {
		try {
			const status = await apiClient.checkRegistrationStatus();
			setRegistrationStatus(status);
		} catch {
			// Non-fatal — credential form will not show
		}
	}, []);

	useEffect(() => {
		void fetchRegistrationStatus();
	}, [fetchRegistrationStatus]);

	// Sync form data when config changes from external sources (reload)
	useEffect(() => {
		const newFormData = { login_required: config.auth.login_required };
		setFormData(newFormData);
		setHasChanges(false);
	}, [config.auth.login_required]);

	const handleToggle = (value: boolean) => {
		const newData = { ...formData, login_required: value };
		setFormData(newData);
		setHasChanges(value !== config.auth.login_required);
		setCredentialError(null);
	};

	const needsCredentialSetup =
		formData.login_required && registrationStatus !== null && registrationStatus.user_count === 0;

	const credentialsAlreadyExist =
		formData.login_required && registrationStatus !== null && registrationStatus.user_count > 0;

	const validateCredentials = (): string | null => {
		if (credentialForm.username.trim().length < 3) {
			return "Username must be at least 3 characters.";
		}
		if (credentialForm.password.length < 8) {
			return "Password must be at least 8 characters.";
		}
		if (credentialForm.password !== credentialForm.confirmPassword) {
			return "Passwords do not match.";
		}
		return null;
	};

	const handleSave = async () => {
		if (!onUpdate || !hasChanges) return;

		if (needsCredentialSetup) {
			const validationError = validateCredentials();
			if (validationError) {
				setCredentialError(validationError);
				return;
			}

			setIsRegistering(true);
			setCredentialError(null);
			try {
				await apiClient.register(
					credentialForm.username.trim(),
					undefined,
					credentialForm.password,
				);
				await fetchRegistrationStatus();
			} catch (err) {
				setCredentialError(
					err instanceof Error ? err.message : "Failed to create credentials. Try again.",
				);
				setIsRegistering(false);
				return;
			}
			setIsRegistering(false);
		}

		await onUpdate("auth", formData);
		setHasChanges(false);
	};

	return (
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">Security & Access</h3>
				<p className="break-words text-base-content/50 text-sm">
					Control how users authenticate with the AltMount web interface.
				</p>
			</div>

			<div className="space-y-8">
				{/* Login Required Toggle */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<ShieldCheck className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Authentication
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="flex items-start items-center justify-between gap-4">
						<div className="min-w-0 flex-1">
							<h5 className="break-words font-bold text-sm">Require Login</h5>
							<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
								Force users to sign in before accessing the dashboard or settings.
							</p>
						</div>
						<input
							type="checkbox"
							className="toggle toggle-primary mt-1 shrink-0"
							checked={formData.login_required}
							disabled={isReadOnly}
							onChange={(e) => handleToggle(e.target.checked)}
						/>
					</div>

					{!formData.login_required && (
						<div className="alert zoom-in-95 animate-in items-start rounded-xl border border-warning/20 bg-warning/5 px-4 py-3">
							<AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warning" />
							<div className="min-w-0">
								<div className="font-bold text-warning text-xs uppercase tracking-wider">
									Security Risk
								</div>
								<div className="mt-1 break-words text-[11px] leading-relaxed opacity-80">
									Your interface is currently public. Anyone with network access can change your
									configuration and download clients. Ensure you have external security (e.g., VPN).
								</div>
							</div>
						</div>
					)}

					{/* Credential setup — shown when enabling login with no existing users */}
					{needsCredentialSetup && (
						<div className="zoom-in-95 animate-in space-y-4 rounded-xl border-2 border-primary/20 bg-primary/5 p-4">
							<div className="flex items-center gap-2">
								<KeyRound className="h-4 w-4 text-primary" />
								<span className="font-bold text-primary text-xs uppercase tracking-widest">
									Set Up Admin Credentials
								</span>
							</div>
							<p className="text-[11px] text-base-content/60 leading-relaxed">
								No users exist yet. Create your admin username and password before enabling login
								— you'll need these to sign in.
							</p>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Username</legend>
								<input
									type="text"
									className="input w-full"
									placeholder="admin"
									value={credentialForm.username}
									disabled={isReadOnly}
									onChange={(e) =>
										setCredentialForm((f) => ({ ...f, username: e.target.value }))
									}
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Password</legend>
								<input
									type="password"
									className="input w-full"
									placeholder="Min. 8 characters"
									value={credentialForm.password}
									disabled={isReadOnly}
									onChange={(e) =>
										setCredentialForm((f) => ({ ...f, password: e.target.value }))
									}
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Confirm Password</legend>
								<input
									type="password"
									className="input w-full"
									placeholder="Repeat password"
									value={credentialForm.confirmPassword}
									disabled={isReadOnly}
									onChange={(e) =>
										setCredentialForm((f) => ({ ...f, confirmPassword: e.target.value }))
									}
								/>
							</fieldset>

							{credentialError && (
								<div className="alert alert-error items-start rounded-xl px-4 py-3">
									<AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
									<span className="text-[11px]">{credentialError}</span>
								</div>
							)}
						</div>
					)}

					{/* Status badge — shown when login is required and users already exist */}
					{credentialsAlreadyExist && (
						<div className="zoom-in-95 animate-in flex items-center gap-2 rounded-xl border border-success/20 bg-success/5 px-4 py-3">
							<UserCheck className="h-4 w-4 shrink-0 text-success" />
							<span className="text-[11px] text-success">
								Credentials configured — manage password from the user menu.
							</span>
						</div>
					)}
				</div>
			</div>

			{/* Save Button */}
			{!isReadOnly && (
				<div className="flex justify-end border-base-200 border-t pt-4">
					<button
						type="button"
						className={`btn btn-primary px-10 shadow-lg shadow-primary/20 ${!hasChanges && "btn-ghost border-base-300"}`}
						onClick={handleSave}
						disabled={!hasChanges || isUpdating || isRegistering}
					>
						{isUpdating || isRegistering ? (
							<LoadingSpinner size="sm" />
						) : (
							<Save className="h-4 w-4" />
						)}
						{isRegistering
							? "Creating credentials..."
							: isUpdating
								? "Saving..."
								: "Save Changes"}
					</button>
				</div>
			)}
		</div>
	);
}
