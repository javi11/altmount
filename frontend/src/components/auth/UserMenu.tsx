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
								{isAdmin && <div className="badge badge-primary badge-xs">Admin</div>}
								<div className="text-base-content/50 text-xs capitalize">via {user.provider}</div>
							</div>
						</div>
					</div>
				</li>

				<div className="divider my-1" />

				{/* Menu items */}
				{isAdmin && (
					<li>
						<a
							href="/admin"
							className="flex items-center gap-3 py-2 transition-colors hover:bg-base-200"
						>
							<Users className="h-4 w-4" />
							<span>Manage Users</span>
							<div className="badge badge-secondary badge-sm ml-auto">Admin</div>
						</a>
					</li>
				)}

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
			</ul>
		</div>
	);
}
