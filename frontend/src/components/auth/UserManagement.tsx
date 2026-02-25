import { KeyRound, RefreshCcw, User as UserIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { apiClient } from "../../api/client";
import type { User } from "../../types/api";

export function UserManagement() {
	const [users, setUsers] = useState<User[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [updatingUserId, setUpdatingUserId] = useState<string | null>(null);

	// Password change modal state
	const [passwordModal, setPasswordModal] = useState<{ userId: string; userName: string } | null>(
		null,
	);
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const [passwordError, setPasswordError] = useState<string | null>(null);
	const [passwordUpdating, setPasswordUpdating] = useState(false);

	const loadUsers = useCallback(async () => {
		try {
			setLoading(true);
			setError(null);
			const userData = await apiClient.getUsers();
			setUsers(userData);
		} catch (err) {
			setError(err instanceof Error ? err.message : "Failed to load users");
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		loadUsers();
	}, [loadUsers]);

	const toggleAdminStatus = async (userId: string, currentStatus: boolean) => {
		try {
			setUpdatingUserId(userId);
			await apiClient.updateUserAdmin(userId, { is_admin: !currentStatus });

			// Update local state
			setUsers(
				users.map((user) => (user.id === userId ? { ...user, is_admin: !currentStatus } : user)),
			);
		} catch (err) {
			setError(err instanceof Error ? err.message : "Failed to update user");
		} finally {
			setUpdatingUserId(null);
		}
	};

	const openPasswordModal = (userId: string, userName: string) => {
		setPasswordModal({ userId, userName });
		setNewPassword("");
		setConfirmPassword("");
		setPasswordError(null);
	};

	const closePasswordModal = () => {
		setPasswordModal(null);
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
		if (!passwordModal) return;

		try {
			setPasswordUpdating(true);
			setPasswordError(null);
			await apiClient.changeUserPassword(passwordModal.userId, { new_password: newPassword });
			closePasswordModal();
		} catch (err) {
			setPasswordError(err instanceof Error ? err.message : "Failed to update password");
		} finally {
			setPasswordUpdating(false);
		}
	};

	if (loading) {
		return (
			<div className="flex justify-center py-8">
				<div className="loading loading-spinner loading-lg text-primary" />
			</div>
		);
	}

	if (error) {
		return (
			<div className="alert alert-error">
				<div>{error}</div>
				<button type="button" onClick={loadUsers} className="btn btn-sm btn-outline">
					Try again
				</button>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<div className="flex items-center justify-between">
				<h2 className="font-bold text-2xl">User Management</h2>
				<button type="button" onClick={loadUsers} className="btn btn-sm btn-secondary">
					<RefreshCcw className="h-4 w-4" /> Refresh
				</button>
			</div>

			<div className="card bg-base-100 shadow-xl">
				<div className="card-body p-0">
					<ul className="divide-y divide-base-300">
						{users.map((user) => (
							<li key={user.id} className="px-6 py-4">
								<div className="flex items-center justify-between">
									<div className="flex items-center">
										<div className="avatar placeholder">
											<UserIcon className="h-5 w-5" />
										</div>
										<div className="ml-4">
											<div className="flex items-center gap-2">
												<p className="font-medium text-sm">{user.name}</p>
												{user.is_admin && <div className="badge badge-primary badge-sm">Admin</div>}
											</div>
											<p className="text-base-content/70 text-sm">{user.email}</p>
											<p className="text-base-content/50 text-xs capitalize">
												via {user.provider}
												{user.last_login && (
													<span className="ml-2">
														• Last login: {new Date(user.last_login).toLocaleDateString()}
													</span>
												)}
											</p>
										</div>
									</div>

									<div className="flex items-center gap-2">
										{user.provider === "direct" && (
											<button
												type="button"
												onClick={() => openPasswordModal(user.id, user.name)}
												className="btn btn-sm btn-outline"
											>
												<KeyRound className="h-4 w-4" />
												Change Password
											</button>
										)}
										<button
											type="button"
											onClick={() => toggleAdminStatus(user.id, user.is_admin)}
											disabled={updatingUserId === user.id}
											className={`btn btn-sm ${
												user.is_admin ? "btn-error btn-outline" : "btn-success btn-outline"
											} ${updatingUserId === user.id ? "loading" : ""}`}
										>
											{updatingUserId === user.id
												? "Updating..."
												: user.is_admin
													? "Remove Admin"
													: "Make Admin"}
										</button>
									</div>
								</div>
							</li>
						))}
					</ul>
				</div>
			</div>

			{users.length === 0 && (
				<div className="py-8 text-center text-base-content/60">No users found.</div>
			)}

			{/* Change Password Modal */}
			{passwordModal && (
				<dialog className="modal modal-open">
					<div className="modal-box">
						<h3 className="font-bold text-lg">Change Password</h3>
						<p className="py-2 text-base-content/70 text-sm">
							Set a new password for <span className="font-medium">{passwordModal.userName}</span>
						</p>

						<div className="space-y-4 py-4">
							<fieldset className="fieldset">
								<legend className="fieldset-legend">New Password</legend>
								<input
									type="password"
									className="input w-full"
									value={newPassword}
									onChange={(e) => setNewPassword(e.target.value)}
									placeholder="Enter new password"
									disabled={passwordUpdating}
								/>
							</fieldset>

							<fieldset className="fieldset">
								<legend className="fieldset-legend">Confirm Password</legend>
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
		</div>
	);
}
