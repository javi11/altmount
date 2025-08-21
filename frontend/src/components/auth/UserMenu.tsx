import { ChevronDown, LogOut, User, Users } from "lucide-react";
import { useAuth, useIsAdmin } from "../../hooks/useAuth";

export function UserMenu() {
	const { user, logout, isLoading } = useAuth();
	const isAdmin = useIsAdmin();

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


	return (
		<div className="dropdown dropdown-end">
			<button
			    type="button" 
				tabIndex={0} 
				className="btn btn-ghost gap-2 hover:bg-base-200 transition-colors duration-200"
			>
				{/* Avatar */}
				<div className="avatar placeholder">
					<User className="w-5 h-5" />
				</div>

				{/* User info - hidden on small screens */}
				<div className="hidden lg:flex flex-col items-start">
					<div className="text-sm font-medium text-base-content">
						{user.name}
					</div>
				</div>

				<ChevronDown className="h-4 w-4 text-base-content/60" />
			</button>

			{/* Dropdown menu */}
			<ul className="dropdown-content menu bg-base-100 rounded-box z-50 w-64 p-2 shadow-xl border border-base-200">
				{/* User info header */}
				<li className="menu-title px-4 py-2">
					<div className="flex items-center gap-3">
						<div className="flex flex-col">
							<div className="font-semibold text-sm text-base-content">
								{user.name}
							</div>
							{user.email && (
								<div className="text-xs text-base-content/60">
									{user.email}
								</div>
							)}
							<div className="flex items-center gap-1 mt-1">
								{isAdmin && (
									<div className="badge badge-primary badge-xs">
										Admin
									</div>
								)}
								<div className="text-xs text-base-content/50 capitalize">
									via {user.provider}
								</div>
							</div>
						</div>
					</div>
				</li>

				<div className="divider my-1"></div>

				{/* Menu items */}
				{isAdmin && (
					<li>
						<a 
							href="/admin"
							className="flex items-center gap-3 py-2 hover:bg-base-200 transition-colors"
						>
							<Users className="h-4 w-4" />
							<span>Manage Users</span>
							<div className="badge badge-secondary badge-sm ml-auto">Admin</div>
						</a>
					</li>
				)}

				<div className="divider my-1"></div>

				{/* Logout */}
				<li>
					<button
						type="button"
						onClick={handleLogout}
						disabled={isLoading}
						className="flex items-center gap-3 py-2 text-error hover:bg-error/10 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
					>
						<LogOut className="h-4 w-4" />
						<span>{isLoading ? "Logging out..." : "Logout"}</span>
						{isLoading && (
							<span className="loading loading-spinner loading-xs ml-auto"></span>
						)}
					</button>
				</li>
			</ul>
		</div>
	);
}