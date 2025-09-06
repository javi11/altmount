import {
	Activity,
	AlertTriangle,
	Database,
	ExternalLink,
	Folder,
	Heart,
	Home,
	List,
	Settings,
} from "lucide-react";
import { NavLink } from "react-router-dom";
import { useHealthStats, useQueueStats } from "../../hooks/useApi";
import { useAuth } from "../../hooks/useAuth";

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
		<aside className="min-h-full w-64 bg-base-200">
			<div className="p-4">
				<div className="mb-8 flex items-center space-x-3">
					<div className="avatar placeholder">
						<div className="flex h-12 w-12 items-center justify-center overflow-hidden">
							<img src="/logo.png" alt="AltMount Logo" className="h-12 w-12 object-contain" />
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
									`flex items-center space-x-3 rounded-lg px-4 py-3 transition-colors ${
										isActive ? "bg-primary text-primary-content" : "hover:bg-base-300"
									}`
								}
							>
								<item.icon className="h-5 w-5" />
								<span className="flex-1">{item.name}</span>
								{badgeCount > 0 && (
									<div className={`badge badge-sm ${badgeColor}`}>{badgeCount}</div>
								)}
							</NavLink>
						);
					})}
				</nav>

				{/* System info section */}
				<div className="mt-8 border-base-300 border-t pt-6">
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
								<div className="text-base-content/70 text-sm">
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
								<div className="text-error text-sm">{healthStats.corrupted}</div>
							</div>
						)}
					</div>
				</div>

				{/* Version and GitHub section */}
				<div className="mt-4 border-base-300 border-t pt-4">
					<div className="space-y-2">
						<div className="flex items-center justify-between">
							<span className="text-base-content/70 text-sm">Version</span>
							<span className="font-mono text-base-content text-sm">
								{__APP_VERSION__}
								{__GIT_COMMIT__ !== "unknown" && (
									<span className="text-base-content/50"> ({__GIT_COMMIT__.slice(0, 7)})</span>
								)}
							</span>
						</div>
						<a
							href={__GITHUB_URL__}
							target="_blank"
							rel="noopener noreferrer"
							className="flex items-center space-x-2 text-base-content/70 text-sm transition-colors hover:text-base-content"
						>
							<ExternalLink className="h-4 w-4" />
							<span>GitHub Repository</span>
						</a>
					</div>
				</div>
			</div>
		</aside>
	);
}
