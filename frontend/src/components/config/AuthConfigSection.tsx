import { AlertTriangle, KeyRound, Save, ShieldCheck, UserCheck } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { apiClient } from "../../api/client";
import { useAuth } from "../../hooks/useAuth";
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
	const { user } = useAuth();
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

	// Change-password form (used when logged in as a direct-auth user)
	const [changePasswordForm, setChangePasswordForm] = useState({
		currentPassword: "",
		newPassword: "",
		confirmPassword: "",
	});
	const [changePasswordError, setChangePasswordError] = useState<string | null>(null);
	const [changePasswordSuccess, setChangePasswordSuccess] = useState(false);
	const [isChangingPassword, setIsChangingPassword] = useState(false);

	// Reset-password form (used when auth is disabled — no current password required)
	const [resetPasswordForm, setResetPasswordForm] = useState({
		username: "",
		newPassword: "",
		confirmPassword: "",
	});
	const [resetPasswordError, setResetPasswordError] = useState<string | null>(null);
	const [resetPasswordSuccess, setResetPasswordSuccess] = useState(false);
	const [isResettingPassword, setIsResettingPassword] = useState(false);

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

	const isDirectUser = user?.provider === "direct";

	const handleChangePassword = async () => {
		if (changePasswordForm.newPassword.length < 8) {
			setChangePasswordError("New password must be at least 8 characters.");
			return;
		}
		if (changePasswordForm.newPassword !== changePasswordForm.confirmPassword) {
			setChangePasswordError("Passwords do not match.");
			return;
		}
		setIsChangingPassword(true);
		setChangePasswordError(null);
		setChangePasswordSuccess(false);
		try {
			await apiClient.changeOwnPassword({
				current_password: changePasswordForm.currentPassword,
				new_password: changePasswordForm.newPassword,
			});
			setChangePasswordSuccess(true);
			setChangePasswordForm({ currentPassword: "", newPassword: "", confirmPassword: "" });
		} catch (err) {
			setChangePasswordError(
				err instanceof Error ? err.message : "Failed to update password. Try again.",
			);
		} finally {
			setIsChangingPassword(false);
		}
	};

	const handleResetPassword = async () => {
		if (resetPasswordForm.username.trim().length < 1) {
			setResetPasswordError("Username is required.");
			return;
		}
		if (resetPasswordForm.newPassword.length < 8) {
			setResetPasswordError("New password must be at least 8 characters.");
			return;
		}
		if (resetPasswordForm.newPassword !== resetPasswordForm.confirmPassword) {
			setResetPasswordError("Passwords do not match.");
			return;
		}
		setIsResettingPassword(true);
		setResetPasswordError(null);
		setResetPasswordSuccess(false);
		try {
			await apiClient.resetAdminPassword(
				resetPasswordForm.username.trim(),
				resetPasswordForm.newPassword,
			);
			setResetPasswordSuccess(true);
			setResetPasswordForm({ username: "", newPassword: "", confirmPassword: "" });
		} catch (err) {
			setResetPasswordError(
				err instanceof Error ? err.message : "Failed to reset password. Check the username.",
			);
		} finally {
			setIsResettingPassword(false);
		}
	};

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

					{/* Credentials exist: inline change-password when logged in, info message when anonymous */}
					{credentialsAlreadyExist && (
						<div className="zoom-in-95 animate-in space-y-4 rounded-xl border-2 border-success/20 bg-success/5 p-4">
							<div className="flex items-center gap-2">
								<UserCheck className="h-4 w-4 text-success" />
								<span className="font-bold text-success text-xs uppercase tracking-widest">
									Credentials Configured
								</span>
							</div>

							{isDirectUser ? (
								<>
									<p className="text-[11px] text-base-content/60 leading-relaxed">
										Update your admin password below.
									</p>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Current Password</legend>
										<input
											type="password"
											className="input w-full"
											placeholder="Enter current password"
											value={changePasswordForm.currentPassword}
											disabled={isReadOnly || isChangingPassword}
											onChange={(e) =>
												setChangePasswordForm((f) => ({
													...f,
													currentPassword: e.target.value,
												}))
											}
										/>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">New Password</legend>
										<input
											type="password"
											className="input w-full"
											placeholder="Min. 8 characters"
											value={changePasswordForm.newPassword}
											disabled={isReadOnly || isChangingPassword}
											onChange={(e) =>
												setChangePasswordForm((f) => ({
													...f,
													newPassword: e.target.value,
												}))
											}
										/>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Confirm New Password</legend>
										<input
											type="password"
											className="input w-full"
											placeholder="Repeat new password"
											value={changePasswordForm.confirmPassword}
											disabled={isReadOnly || isChangingPassword}
											onChange={(e) =>
												setChangePasswordForm((f) => ({
													...f,
													confirmPassword: e.target.value,
												}))
											}
										/>
									</fieldset>

									{changePasswordError && (
										<div className="alert alert-error items-start rounded-xl px-4 py-3">
											<AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
											<span className="text-[11px]">{changePasswordError}</span>
										</div>
									)}

									{changePasswordSuccess && (
										<div className="alert alert-success items-start rounded-xl px-4 py-3">
											<UserCheck className="mt-0.5 h-4 w-4 shrink-0" />
											<span className="text-[11px]">Password updated successfully.</span>
										</div>
									)}

									{!isReadOnly && (
										<div className="flex justify-end">
											<button
												type="button"
												className="btn btn-sm btn-success"
												onClick={handleChangePassword}
												disabled={
													isChangingPassword ||
													!changePasswordForm.currentPassword ||
													!changePasswordForm.newPassword
												}
											>
												{isChangingPassword ? (
													<LoadingSpinner size="sm" />
												) : (
													<KeyRound className="h-3 w-3" />
												)}
												{isChangingPassword ? "Updating..." : "Update Password"}
											</button>
										</div>
									)}
								</>
							) : (
								<>
									<p className="text-[11px] text-base-content/60 leading-relaxed">
										Reset the password for an existing account before enabling login.
									</p>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Username</legend>
										<input
											type="text"
											className="input w-full"
											placeholder="Enter the username"
											value={resetPasswordForm.username}
											disabled={isReadOnly || isResettingPassword}
											onChange={(e) =>
												setResetPasswordForm((f) => ({ ...f, username: e.target.value }))
											}
										/>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">New Password</legend>
										<input
											type="password"
											className="input w-full"
											placeholder="Min. 8 characters"
											value={resetPasswordForm.newPassword}
											disabled={isReadOnly || isResettingPassword}
											onChange={(e) =>
												setResetPasswordForm((f) => ({ ...f, newPassword: e.target.value }))
											}
										/>
									</fieldset>

									<fieldset className="fieldset">
										<legend className="fieldset-legend">Confirm New Password</legend>
										<input
											type="password"
											className="input w-full"
											placeholder="Repeat new password"
											value={resetPasswordForm.confirmPassword}
											disabled={isReadOnly || isResettingPassword}
											onChange={(e) =>
												setResetPasswordForm((f) => ({
													...f,
													confirmPassword: e.target.value,
												}))
											}
										/>
									</fieldset>

									{resetPasswordError && (
										<div className="alert alert-error items-start rounded-xl px-4 py-3">
											<AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
											<span className="text-[11px]">{resetPasswordError}</span>
										</div>
									)}

									{resetPasswordSuccess && (
										<div className="alert alert-success items-start rounded-xl px-4 py-3">
											<UserCheck className="mt-0.5 h-4 w-4 shrink-0" />
											<span className="text-[11px]">Password reset successfully.</span>
										</div>
									)}

									{!isReadOnly && (
										<div className="flex justify-end">
											<button
												type="button"
												className="btn btn-sm btn-success"
												onClick={handleResetPassword}
												disabled={
													isResettingPassword ||
													!resetPasswordForm.username ||
													!resetPasswordForm.newPassword
												}
											>
												{isResettingPassword ? (
													<LoadingSpinner size="sm" />
												) : (
													<KeyRound className="h-3 w-3" />
												)}
												{isResettingPassword ? "Resetting..." : "Reset Password"}
											</button>
										</div>
									)}
								</>
							)}
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
