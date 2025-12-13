import {
	Activity,
	AlertTriangle,
	Bug,
	Database,
	ExternalLink,
	Folder,
	Heart,
	Home,
	List,
	Settings,
	Upload,
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
		name: "Import",
		href: "/import",
		icon: Upload,
	},
	{
		name: "Configuration",
		href: "/config",
		icon: Settings,
		adminOnly: true,
	},
];

export function Sidebar() {
	const { user, loginRequired } = useAuth();
	const { data: queueStats } = useQueueStats();
	const { data: healthStats } = useHealthStats();

	// Filter navigation items based on admin status
	// If login is not required, show all items including admin-only
	const visibleNavigation = navigation.filter(
		(item) => !item.adminOnly || !loginRequired || (user?.is_admin ?? false),
	);

	const getBadgeCount = (path: string) => {
		switch (path) {
			case "/queue":
				return queueStats ? queueStats.total_processing + queueStats.total_failed : 0;
			case "/health":
				return healthStats ? healthStats.corrupted : 0;
			default:
				return 0;
		}
	};

	const getBadgeColor = (path: string, count: number) => {
		if (count === 0) return "";
		switch (path) {
			case "/queue": {
				// Show error badge if there are failed items, otherwise warning for processing
				if (queueStats && queueStats.total_failed > 0) {
					return "badge-error";
				}
				return queueStats && queueStats.total_processing > 0 ? "badge-warning" : "badge-info";
			}
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
									{(() => {
										const totalItems =
											queueStats.total_processing +
											queueStats.total_completed +
											queueStats.total_failed;
										const pendingItems = queueStats.total_queued - totalItems;
										const activeItems = queueStats.total_processing + pendingItems;

										if (activeItems > 0) {
											return `${activeItems} active`;
										}
										return "idle";
									})()}
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
							<div className="text-base-content/70 text-sm">Version</div>
							<div className="font-mono text-base-content text-sm">
								{__APP_VERSION__}
								{__GIT_COMMIT__ !== "unknown" && (
									<span className="text-base-content/50"> ({__GIT_COMMIT__.slice(0, 7)})</span>
								)}
							</div>
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
						<a
							href={`${__GITHUB_URL__}/issues`}
							target="_blank"
							rel="noopener noreferrer"
							className="flex items-center space-x-2 text-base-content/70 text-sm transition-colors hover:text-base-content"
						>
							<Bug className="h-4 w-4" />
							<span>Report Issues</span>
						</a>
					</div>
				</div>
			</div>
		</aside>
	);
}
