import { Menu } from "lucide-react";
import { UserMenu } from "../auth/UserMenu";

export function Navbar() {
	return (
		<div className="navbar bg-base-100 shadow-lg border-b border-base-200 px-4 lg:px-6">
			<div className="navbar-start">
				<label htmlFor="sidebar-toggle" className="btn btn-square btn-ghost lg:hidden hover:bg-base-200 transition-colors">
					<Menu className="h-5 w-5" />
				</label>
				
				{/* Logo and title */}
				<div className="flex items-center gap-3 ml-2 lg:ml-0">
					<div className="flex flex-col">
						<h1 className="text-xl font-bold text-base-content hidden lg:block">
							Dashboard
						</h1>
					</div>
				</div>
			</div>

			<div className="navbar-center lg:hidden">
				<div className="flex items-center gap-2">
					<h1 className="text-lg font-bold text-base-content">AltMount</h1>
				</div>
			</div>

			<div className="navbar-end">
				<div className="flex items-center gap-2">
					{/* User Menu */}
					<UserMenu />
				</div>
			</div>
		</div>
	);
}
