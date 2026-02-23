import { Menu, Settings } from "lucide-react";
import { Link } from "react-router-dom";
import { UserMenu } from "../auth/UserMenu";

export function Navbar() {
	return (
		<div className="navbar border-base-200 border-b bg-base-100 px-4 shadow-lg lg:px-6">
			<div className="navbar-start">
				<label
					htmlFor="sidebar-toggle"
					className="btn btn-square btn-ghost transition-colors hover:bg-base-200 lg:hidden"
				>
					<Menu className="h-5 w-5" />
				</label>

				{/* Logo and title */}
				<div className="ml-2 flex items-center gap-3 lg:ml-0">
					<div className="flex flex-col">
						<h1 className="hidden font-bold text-base-content text-xl lg:block">Dashboard</h1>
					</div>
				</div>
			</div>

			<div className="navbar-center lg:hidden">
				<div className="flex items-center gap-2">
					<img src="/logo.png" alt="AltMount Logo" className="h-7 w-7 object-contain" />
					<h1 className="font-bold text-base-content text-lg">AltMount</h1>
				</div>
			</div>

			<div className="navbar-end">
				<div className="flex items-center gap-2">
					{/* Mobile Quick Config Link */}
					<Link
						to="/config"
						className="btn btn-ghost btn-circle lg:hidden"
						aria-label="Configuration"
					>
						<Settings className="h-5 w-5 text-base-content/80" />
					</Link>
					{/* User Menu */}
					<UserMenu />
				</div>
			</div>
		</div>
	);
}
