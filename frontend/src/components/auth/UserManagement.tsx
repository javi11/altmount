import { useCallback, useEffect, useState } from "react";
import { RefreshCcw, User as UserIcon } from "lucide-react";
import { apiClient } from "../../api/client";
import type { User } from "../../types/api";

export function UserManagement() {
	const [users, setUsers] = useState<User[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [updatingUserId, setUpdatingUserId] = useState<string | null>(null);

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
				users.map((user) =>
					user.id === userId ? { ...user, is_admin: !currentStatus } : user,
				),
			);
		} catch (err) {
			setError(err instanceof Error ? err.message : "Failed to update user");
		} finally {
			setUpdatingUserId(null);
		}
	};

	if (loading) {
		return (
			<div className="flex justify-center py-8">
				<div className="loading loading-spinner loading-lg text-primary"></div>
			</div>
		);
	}

	if (error) {
		return (
			<div className="alert alert-error">
				<div>{error}</div>
				<button
					type="button"
					onClick={loadUsers}
					className="btn btn-sm btn-outline"
				>
					Try again
				</button>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<div className="flex justify-between items-center">
				<h2 className="text-2xl font-bold">User Management</h2>
				<button
					type="button"
					onClick={loadUsers}
					className="btn btn-sm btn-secondary"
				>
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
											<UserIcon className="w-5 h-5" />
										</div>
										<div className="ml-4">
											<div className="flex items-center gap-2">
												<p className="text-sm font-medium">{user.name}</p>
												{user.is_admin && (
													<div className="badge badge-primary badge-sm">
														Admin
													</div>
												)}
											</div>
											<p className="text-sm text-base-content/70">
												{user.email}
											</p>
											<p className="text-xs text-base-content/50 capitalize">
												via {user.provider}
												{user.last_login && (
													<span className="ml-2">
														• Last login:{" "}
														{new Date(user.last_login).toLocaleDateString()}
													</span>
												)}
											</p>
										</div>
									</div>

									<div className="flex items-center gap-2">
										<button
											type="button"
											onClick={() => toggleAdminStatus(user.id, user.is_admin)}
											disabled={updatingUserId === user.id}
											className={`btn btn-xs ${
												user.is_admin
													? "btn-error btn-outline"
													: "btn-success btn-outline"
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
				<div className="text-center py-8 text-base-content/60">
					No users found.
				</div>
			)}
		</div>
	);
}
