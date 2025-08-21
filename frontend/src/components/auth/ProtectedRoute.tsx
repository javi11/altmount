import type { ReactNode } from "react";
import { useAuth, useIsAdmin } from "../../hooks/useAuth";
import { LoginPage } from "./LoginPage";

interface ProtectedRouteProps {
	children: ReactNode;
	requireAdmin?: boolean;
}

export function ProtectedRoute({ children, requireAdmin = false }: ProtectedRouteProps) {
	const { isAuthenticated, isLoading, user } = useAuth();
	const isAdmin = useIsAdmin();

	// Show loading spinner while checking authentication
	if (isLoading) {
		return (
			<div className="min-h-screen flex items-center justify-center">
				<div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
			</div>
		);
	}

	// Show login page if not authenticated
	if (!isAuthenticated) {
		return <LoginPage />;
	}

	// Show unauthorized message if admin required but user is not admin
	if (requireAdmin && !isAdmin) {
		return (
			<div className="min-h-screen flex items-center justify-center bg-gray-50">
				<div className="max-w-md w-full text-center space-y-4">
					<div className="text-6xl">🚫</div>
					<h2 className="text-2xl font-bold text-gray-900">Access Denied</h2>
					<p className="text-gray-600">
						You need administrator privileges to access this page.
					</p>
					<p className="text-sm text-gray-500">
						Current user: {user?.name} ({user?.provider})
					</p>
				</div>
			</div>
		);
	}

	// Render children if authenticated (and admin if required)
	return <>{children}</>;
}