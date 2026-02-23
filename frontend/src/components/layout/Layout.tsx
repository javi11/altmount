import { Outlet } from "react-router-dom";
import { Navbar } from "./Navbar";
import { Sidebar } from "./Sidebar";

export function Layout() {
	return (
		<div className="drawer lg:drawer-open">
			<input id="sidebar-toggle" type="checkbox" className="drawer-toggle" />

			<div className="drawer-content flex flex-col">
				{/* Navbar */}
				<Navbar />

				{/* Page content */}
				<main className="flex-1 p-3 md:p-4">
					<Outlet />
				</main>
			</div>

			{/* Sidebar */}
			<div className="drawer-side">
				<label htmlFor="sidebar-toggle" aria-label="close sidebar" className="drawer-overlay" />
				<Sidebar />
			</div>
		</div>
	);
}
