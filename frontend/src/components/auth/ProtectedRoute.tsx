import type { ReactNode } from "react";
import { useAuth, useIsAdmin } from "../../hooks/useAuth";
import { LoginPage } from "./LoginPage";

interface ProtectedRouteProps {
	children: ReactNode;
	requireAdmin?: boolean;
}

export function ProtectedRoute({ children, requireAdmin = false }: ProtectedRouteProps) {
	const { isAuthenticated, isLoading, loginRequired, user } = useAuth();
	const isAdmin = useIsAdmin();

	// Show loading spinner while checking authentication or loading config
	if (isLoading || loginRequired === null) {
		return (
			<div className="flex min-h-screen items-center justify-center bg-base-100">
				<div className="loading loading-spinner loading-lg" />
			</div>
		);
	}

	// If login is not required, allow access to all routes without authentication
	if (!loginRequired) {
		return <>{children}</>;
	}

	// From here on, login is required
	// Show login page if not authenticated
	if (!isAuthenticated) {
		return <LoginPage />;
	}

	// Show unauthorized message if admin required but user is not admin
	if (requireAdmin && !isAdmin) {
		return (
			<div className="flex min-h-screen items-center justify-center bg-base-100">
				<div className="w-full max-w-md space-y-4 text-center">
					<div className="text-6xl">🚫</div>
					<h2 className="font-bold text-2xl text-base-content">Access Denied</h2>
					<p className="text-base-content/70">
						You need administrator privileges to access this page.
					</p>
					<p className="text-base-content/70 text-sm">
						Current user: {user?.name} ({user?.provider})
					</p>
				</div>
			</div>
		);
	}

	// Render children if authenticated (and admin if required)
	return <>{children}</>;
}
