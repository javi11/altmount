import type { ReactNode } from "react";
import { createContext, useContext, useEffect, useReducer } from "react";
import { apiClient } from "../api/client";
import type { User } from "../types/api";

// Auth state interface
interface AuthState {
	user: User | null;
	isLoading: boolean;
	isAuthenticated: boolean;
	loginRequired: boolean | null; // null = not yet loaded
	error: string | null;
}

// Auth actions
type AuthAction =
	| { type: "AUTH_START" }
	| { type: "AUTH_SUCCESS"; payload: User }
	| { type: "AUTH_ERROR"; payload: string }
	| { type: "AUTH_LOGOUT" }
	| { type: "AUTH_CLEAR_ERROR" }
	| { type: "AUTH_SKIP" } // Auth disabled - treat as authenticated anonymous admin
	| { type: "SET_LOGIN_REQUIRED"; payload: boolean };

// Initial state
const initialState: AuthState = {
	user: null,
	isLoading: false,
	isAuthenticated: false,
	loginRequired: null, // Not yet loaded
	error: null,
};

// Auth reducer
function authReducer(state: AuthState, action: AuthAction): AuthState {
	switch (action.type) {
		case "AUTH_START":
			return {
				...state,
				isLoading: true,
				error: null,
			};
		case "AUTH_SUCCESS":
			return {
				...state,
				user: action.payload,
				isLoading: false,
				isAuthenticated: true,
				error: null,
			};
		case "AUTH_ERROR":
			return {
				...state,
				user: null,
				isLoading: false,
				isAuthenticated: false,
				error: action.payload,
			};
		case "AUTH_LOGOUT":
			return {
				...state,
				user: null,
				isLoading: false,
				isAuthenticated: false,
				error: null,
			};
		case "AUTH_SKIP":
			// Auth is disabled - treat user as authenticated anonymous admin
			return {
				...state,
				user: {
					id: "anonymous",
					name: "Admin",
					provider: "none",
					is_admin: true,
				} as User,
				isLoading: false,
				isAuthenticated: true,
				error: null,
			};
		case "AUTH_CLEAR_ERROR":
			return {
				...state,
				error: null,
			};
		case "SET_LOGIN_REQUIRED":
			return {
				...state,
				loginRequired: action.payload,
			};
		default:
			return state;
	}
}

// Auth context interface
interface AuthContextType extends AuthState {
	login: (username: string, password: string) => Promise<boolean>;
	register: (username: string, email: string | undefined, password: string) => Promise<boolean>;
	logout: () => Promise<void>;
	refreshToken: () => Promise<void>;
	clearError: () => void;
	checkRegistrationStatus: () => Promise<{
		registration_enabled: boolean;
		user_count: number;
	}>;
}

// Create context
const AuthContext = createContext<AuthContextType | undefined>(undefined);

// Auth provider props
interface AuthProviderProps {
	children: ReactNode;
}

// Auth provider component
export function AuthProvider({ children }: AuthProviderProps) {
	const [state, dispatch] = useReducer(authReducer, initialState);

	// Check for existing authentication on mount
	useEffect(() => {
		const checkAuth = async () => {
			try {
				// First check if login is required
				const authConfig = await apiClient.getAuthConfig();
				dispatch({ type: "SET_LOGIN_REQUIRED", payload: authConfig.login_required });

				// If login is not required, treat as authenticated anonymous admin
				if (!authConfig.login_required) {
					dispatch({ type: "AUTH_SKIP" });
					return;
				}

				// If login is required, check for existing authentication
				dispatch({ type: "AUTH_START" });
				const user = await apiClient.getCurrentUser();
				dispatch({ type: "AUTH_SUCCESS", payload: user });
			} catch (_error) {
				// If getCurrentUser fails, user is not authenticated
				// Don't dispatch error for this case since it's expected
				dispatch({ type: "AUTH_LOGOUT" });
			}
		};

		checkAuth();
	}, []);

	// Login function (direct authentication)
	const login = async (username: string, password: string): Promise<boolean> => {
		try {
			dispatch({ type: "AUTH_START" });
			const response = await apiClient.login(username, password);
			if (response.user) {
				dispatch({ type: "AUTH_SUCCESS", payload: response.user });
				return true;
			}
			dispatch({ type: "AUTH_ERROR", payload: "Login failed" });
			return false;
		} catch (error) {
			const errorMessage = error instanceof Error ? error.message : "Login failed";
			dispatch({ type: "AUTH_ERROR", payload: errorMessage });
			return false;
		}
	};

	// Register function (first user only)
	const register = async (
		username: string,
		email: string | undefined,
		password: string,
	): Promise<boolean> => {
		try {
			dispatch({ type: "AUTH_START" });
			const response = await apiClient.register(username, email, password);
			if (response.user) {
				dispatch({ type: "AUTH_SUCCESS", payload: response.user });
				return true;
			}
			dispatch({ type: "AUTH_ERROR", payload: "Registration failed" });
			return false;
		} catch (error) {
			const errorMessage = error instanceof Error ? error.message : "Registration failed";
			dispatch({ type: "AUTH_ERROR", payload: errorMessage });
			return false;
		}
	};

	// Check registration status
	const checkRegistrationStatus = async (): Promise<{
		registration_enabled: boolean;
		user_count: number;
	}> => {
		return await apiClient.checkRegistrationStatus();
	};

	// Logout function
	const logout = async () => {
		try {
			dispatch({ type: "AUTH_START" });
			await apiClient.logout();
			dispatch({ type: "AUTH_LOGOUT" });
		} catch (_error) {
			// Even if logout fails on server, clear local state
			dispatch({ type: "AUTH_LOGOUT" });
		}
	};

	// Refresh token function
	const refreshToken = async () => {
		try {
			dispatch({ type: "AUTH_START" });
			await apiClient.refreshToken();
			// After refresh, get current user to update state
			const user = await apiClient.getCurrentUser();
			dispatch({ type: "AUTH_SUCCESS", payload: user });
		} catch (error) {
			const errorMessage = error instanceof Error ? error.message : "Token refresh failed";
			dispatch({ type: "AUTH_ERROR", payload: errorMessage });
		}
	};

	// Clear error function
	const clearError = () => {
		dispatch({ type: "AUTH_CLEAR_ERROR" });
	};

	const contextValue: AuthContextType = {
		...state,
		login,
		register,
		logout,
		refreshToken,
		clearError,
		checkRegistrationStatus,
	};

	return <AuthContext.Provider value={contextValue}>{children}</AuthContext.Provider>;
}

// Hook to use auth context
export function useAuth() {
	const context = useContext(AuthContext);
	if (context === undefined) {
		throw new Error("useAuth must be used within an AuthProvider");
	}
	return context;
}
