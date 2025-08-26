import { useMutation } from "@tanstack/react-query";
import { apiClient } from "../api/client";
import { useAuth as useAuthContext } from "../contexts/AuthContext";

// Re-export the auth context hook for convenience
export const useAuth = useAuthContext;

// Additional authentication utility hooks

// Hook to check if user is admin
export function useIsAdmin() {
	const { user, isAuthenticated } = useAuth();
	return isAuthenticated && user?.is_admin === true;
}

// Hook to check if user is authenticated
export function useIsAuthenticated() {
	const { isAuthenticated } = useAuth();
	return isAuthenticated;
}

// Hook to get user data (returns null if not authenticated)
export function useUser() {
	const { user, isAuthenticated } = useAuth();
	return isAuthenticated ? user : null;
}

// Hook to check registration status
export function useRegistrationStatus() {
	const { checkRegistrationStatus } = useAuth();
	return { checkRegistrationStatus };
}

// Hook to regenerate API key
export function useRegenerateAPIKey() {
	return useMutation({
		mutationFn: () => apiClient.regenerateAPIKey(),
		onError: (error) => {
			console.error("Failed to regenerate API key:", error);
		},
	});
}
