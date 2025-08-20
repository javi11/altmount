import { Bell, Menu, Settings, User } from "lucide-react";
import { useSystemHealth } from "../../hooks/useApi";
import { HealthBadge } from "../ui/StatusBadge";

interface NavbarProps {
	onMenuClick: () => void;
}

export function Navbar({ onMenuClick }: NavbarProps) {
	const { data: systemHealth } = useSystemHealth();

	return (
		<div className="navbar bg-base-100 shadow-lg lg:px-6">
			<div className="navbar-start">
				<button
					type="button"
					className="btn btn-square btn-ghost lg:hidden"
					onClick={onMenuClick}
				>
					<Menu className="h-6 w-6" />
				</button>
				<h1 className="text-xl font-bold hidden sm:block">
					AltMount Dashboard
				</h1>
			</div>

			<div className="navbar-center">
				<h1 className="text-lg font-bold sm:hidden">AltMount</h1>
			</div>

			<div className="navbar-end">
				{/* System health indicator */}
				{systemHealth && (
					<div className="mr-4 hidden sm:block">
						<HealthBadge status={systemHealth.status} className="badge-sm" />
					</div>
				)}

				{/* Notifications */}
				<div className="dropdown dropdown-end">
					<button type="button" tabIndex={0} className="btn btn-ghost btn-circle">
						<div className="indicator">
							<Bell className="h-5 w-5" />
							<span className="badge badge-xs badge-primary indicator-item"></span>
						</div>
					</button>
					<div
						className="dropdown-content card card-compact bg-base-100 shadow-lg w-64 mt-3"
					>
						<div className="card-body">
							<span className="font-bold text-lg">Notifications</span>
							<span className="text-info">No new notifications</span>
						</div>
					</div>
				</div>

				{/* Settings */}
				<div className="dropdown dropdown-end">
					<button type="button" tabIndex={0} className="btn btn-ghost btn-circle">
						<Settings className="h-5 w-5" />
					</button>
					<ul
						className="dropdown-content menu bg-base-100 shadow-lg rounded-box w-52 mt-3"
					>
						<li>
							<span className="flex items-center">
								<User className="h-4 w-4" />
								Profile
							</span>
						</li>
						<li>
							<span className="flex items-center">
								<Settings className="h-4 w-4" />
								Settings
							</span>
						</li>
						<li>
							<hr />
						</li>
						<li>
							<span>Logout</span>
						</li>
					</ul>
				</div>
			</div>
		</div>
	);
}
