import { useState } from "react";
import { Outlet } from "react-router-dom";
import { Navbar } from "./Navbar";
import { Sidebar } from "./Sidebar";

export function Layout() {
	const [sidebarOpen, setSidebarOpen] = useState(false);

	return (
		<div className="drawer lg:drawer-open">
			<input
				id="sidebar-toggle"
				type="checkbox"
				className="drawer-toggle"
				checked={sidebarOpen}
				onChange={(e) => setSidebarOpen(e.target.checked)}
			/>

			<div className="drawer-content flex flex-col">
				{/* Navbar */}
				<Navbar onMenuClick={() => setSidebarOpen(!sidebarOpen)} />

				{/* Page content */}
				<main className="flex-1 p-4 lg:p-6">
					<Outlet />
				</main>
			</div>

			{/* Sidebar */}
			<div className="drawer-side">
				<label
					htmlFor="sidebar-toggle"
					aria-label="close sidebar"
					className="drawer-overlay"
					onClick={() => setSidebarOpen(false)}
					onKeyUp={(e) => {
						if (e.key === "Escape") {
							setSidebarOpen(false);
						}
					}}
				/>
				<Sidebar />
			</div>
		</div>
	);
}
