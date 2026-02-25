import { ChevronDown, KeyRound, LogOut, User } from "lucide-react";
import { useState } from "react";
import { apiClient } from "../../api/client";
import { useAuth } from "../../hooks/useAuth";

export function UserMenu() {
	const { user, logout, isLoading, loginRequired } = useAuth();

	const [passwordModal, setPasswordModal] = useState(false);
	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const [passwordError, setPasswordError] = useState<string | null>(null);
	const [passwordUpdating, setPasswordUpdating] = useState(false);

	if (!user) {
		return null;
	}

	const handleLogout = async () => {
		try {
			await logout();
		} catch (error) {
			console.error("Logout failed:", error);
		}
	};

	const openPasswordModal = () => {
		setPasswordModal(true);
		setCurrentPassword("");
		setNewPassword("");
		setConfirmPassword("");
		setPasswordError(null);
	};

	const closePasswordModal = () => {
		setPasswordModal(false);
		setCurrentPassword("");
		setNewPassword("");
		setConfirmPassword("");
		setPasswordError(null);
	};

	const submitPasswordChange = async () => {
		if (newPassword.length < 12) {
			setPasswordError("Password must be at least 12 characters");
			return;
		}
		if (newPassword !== confirmPassword) {
			setPasswordError("Passwords do not match");
			return;
		}

		try {
			setPasswordUpdating(true);
			setPasswordError(null);
			await apiClient.changeOwnPassword({
				current_password: currentPassword,
				new_password: newPassword,
			});
			closePasswordModal();
		} catch (err) {
			setPasswordError(err instanceof Error ? err.message : "Failed to update password");
		} finally {
			setPasswordUpdating(false);
		}
	};

	return (
		<>
			<div className="dropdown dropdown-end">
				<button
					type="button"
					tabIndex={0}
					className="btn btn-ghost gap-2 transition-colors duration-200 hover:bg-base-200"
				>
					{/* Avatar */}
					<div className="avatar placeholder">
						<User className="h-5 w-5" />
					</div>

					{/* User info - hidden on small screens */}
					<div className="hidden flex-col items-start lg:flex">
						<div className="font-medium text-base-content text-sm">{user.name}</div>
					</div>

					<ChevronDown className="h-4 w-4 text-base-content/60" />
				</button>

				{/* Dropdown menu */}
				<ul className="dropdown-content menu z-[50] w-64 rounded-box border border-base-300 bg-base-100 p-2 shadow-xl">
					{/* User info header */}
					<li className="menu-title px-4 py-2">
						<div className="flex items-center gap-3">
							<div className="flex flex-col">
								<div className="font-semibold text-base-content text-sm">{user.name}</div>
								{user.email && <div className="text-base-content/60 text-xs">{user.email}</div>}
								<div className="mt-1 flex items-center gap-1">
									<div className="badge badge-primary badge-xs">Admin</div>
									<div className="text-base-content/50 text-xs capitalize">via {user.provider}</div>
								</div>
							</div>
						</div>
					</li>

					<div className="divider my-1" />

					{/* Change password — only for direct auth users */}
					{user.provider === "direct" && (
						<li>
							<button
								type="button"
								onClick={openPasswordModal}
								className="flex items-center gap-3 py-2 transition-colors hover:bg-base-200"
							>
								<KeyRound className="h-4 w-4" />
								<span>Change Password</span>
							</button>
						</li>
					)}

					{loginRequired !== false && (
						<>
							<div className="divider my-1" />

							{/* Logout */}
							<li>
								<button
									type="button"
									onClick={handleLogout}
									disabled={isLoading}
									className="flex items-center gap-3 py-2 text-error transition-colors hover:bg-error/10 disabled:cursor-not-allowed disabled:text-base-content/70"
								>
									<LogOut className="h-4 w-4" />
									<span>{isLoading ? "Logging out..." : "Logout"}</span>
									{isLoading && <span className="loading loading-spinner loading-xs ml-auto" />}
								</button>
							</li>
						</>
					)}
				</ul>
			</div>

			{/* Change Password Modal */}
			{passwordModal && (
				<dialog className="modal modal-open">
					<div className="modal-box">
						<h3 className="font-bold text-lg">Change Password</h3>
						<p className="py-2 text-base-content/70 text-sm">
							Enter your current password and choose a new one.
						</p>

						<div className="space-y-4 py-4">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">Current Password</legend>
								<input
									type="password"
									className="input w-full"
									value={currentPassword}
									onChange={(e) => setCurrentPassword(e.target.value)}
									placeholder="Enter current password"
									disabled={passwordUpdating}
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">New Password</legend>
								<input
									type="password"
									className="input w-full"
									value={newPassword}
									onChange={(e) => setNewPassword(e.target.value)}
									placeholder="Enter new password (min 12 characters)"
									disabled={passwordUpdating}
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Confirm New Password</legend>
								<input
									type="password"
									className="input w-full"
									value={confirmPassword}
									onChange={(e) => setConfirmPassword(e.target.value)}
									placeholder="Confirm new password"
									disabled={passwordUpdating}
								/>
							</fieldset>

							{passwordError && (
								<div className="alert alert-error py-2 text-sm">
									<div>{passwordError}</div>
								</div>
							)}
						</div>

						<div className="modal-action">
							<button
								type="button"
								onClick={closePasswordModal}
								className="btn btn-ghost"
								disabled={passwordUpdating}
							>
								Cancel
							</button>
							<button
								type="button"
								onClick={submitPasswordChange}
								disabled={passwordUpdating}
								className={`btn btn-primary ${passwordUpdating ? "loading" : ""}`}
							>
								{passwordUpdating ? "Updating..." : "Update Password"}
							</button>
						</div>
					</div>
					<button type="button" className="modal-backdrop" onClick={closePasswordModal} />
				</dialog>
			)}
		</>
	);
}
