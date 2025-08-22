import {
	Activity,
	AlertTriangle,
	Database,
	Folder,
	Heart,
	Home,
	List,
	Settings,
} from "lucide-react";
import { NavLink } from "react-router-dom";
import { useAuth } from "../../hooks/useAuth";
import { useHealthStats, useQueueStats } from "../../hooks/useApi";

const navigation = [
	{
		name: "Dashboard",
		href: "/",
		icon: Home,
	},
	{
		name: "Queue",
		href: "/queue",
		icon: List,
	},
	{
		name: "Health",
		href: "/health",
		icon: Heart,
	},
	{
		name: "Files",
		href: "/files",
		icon: Folder,
	},
	{
		name: "Configuration",
		href: "/config",
		icon: Settings,
		adminOnly: true,
	},
];

export function Sidebar() {
	const { user } = useAuth();
	const { data: queueStats } = useQueueStats();
	const { data: healthStats } = useHealthStats();

	// Filter navigation items based on admin status
	const visibleNavigation = navigation.filter(
		(item) => !item.adminOnly || (user?.is_admin ?? false),
	);

	const getBadgeCount = (path: string) => {
		switch (path) {
			case "/queue":
				return queueStats ? queueStats.total_failed : 0;
			case "/health":
				return healthStats ? healthStats.corrupted + healthStats.partial : 0;
			default:
				return 0;
		}
	};

	const getBadgeColor = (path: string, count: number) => {
		if (count === 0) return "";
		switch (path) {
			case "/queue":
				return "badge-error";
			case "/health":
				return "badge-warning";
			default:
				return "badge-info";
		}
	};

	return (
		<aside className="bg-base-200 w-64 min-h-full">
			<div className="p-4">
				<div className="flex items-center space-x-3 mb-8">
					<div className="avatar placeholder">
						<div className=" w-12 h-12 flex items-center justify-center overflow-hidden">
							<img
								src="/logo.png"
								alt="AltMount Logo"
								className="w-12 h-12 object-contain"
							/>
						</div>
					</div>
					<div>
						<h2 className="font-bold text-lg">AltMount</h2>
					</div>
				</div>

				<nav className="space-y-2">
					{visibleNavigation.map((item) => {
						const badgeCount = getBadgeCount(item.href);
						const badgeColor = getBadgeColor(item.href, badgeCount);

						return (
							<NavLink
								key={item.name}
								to={item.href}
								className={({ isActive }) =>
									`flex items-center space-x-3 px-4 py-3 rounded-lg transition-colors ${
										isActive
											? "bg-primary text-primary-content"
											: "hover:bg-base-300"
									}`
								}
							>
								<item.icon className="h-5 w-5" />
								<span className="flex-1">{item.name}</span>
								{badgeCount > 0 && (
									<div className={`badge badge-sm ${badgeColor}`}>
										{badgeCount}
									</div>
								)}
							</NavLink>
						);
					})}
				</nav>

				{/* System info section */}
				<div className="mt-8 pt-6 border-t border-base-300">
					<div className="space-y-4">
						<div className="flex items-center justify-between">
							<div className="flex items-center space-x-2">
								<Activity className="h-4 w-4 text-success" />
								<span className="text-sm">Status</span>
							</div>
							<div className="badge badge-success badge-sm">Online</div>
						</div>

						{queueStats && (
							<div className="flex items-center justify-between">
								<div className="flex items-center space-x-2">
									<Database className="h-4 w-4" />
									<span className="text-sm">Queue</span>
								</div>
								<div className="text-sm text-base-content/70">
									{queueStats.total_processing} / {queueStats.total_queued}
								</div>
							</div>
						)}

						{healthStats && healthStats.corrupted > 0 && (
							<div className="flex items-center justify-between">
								<div className="flex items-center space-x-2">
									<AlertTriangle className="h-4 w-4 text-error" />
									<span className="text-sm">Issues</span>
								</div>
								<div className="text-sm text-error">
									{healthStats.corrupted}
								</div>
							</div>
						)}
					</div>
				</div>
			</div>
		</aside>
	);
}
