import { useEffect, useState } from "react";
import { useAuth } from "../../hooks/useAuth";
import { Logo } from "../ui/Logo";
import { DirectLoginForm } from "./DirectLoginForm";
import { RegisterForm } from "./RegisterForm";

export function LoginPage() {
	const { isAuthenticated, checkRegistrationStatus } = useAuth();
	const [registrationEnabled, setRegistrationEnabled] = useState<boolean>(false);
	const [userCount, setUserCount] = useState<number>(0);
	const [isLoading, setIsLoading] = useState(true);
	const [showRegister, setShowRegister] = useState(false);
	const [hasConnectionError, setHasConnectionError] = useState(false);

	// Check registration status on mount
	useEffect(() => {
		const checkStatus = async () => {
			try {
				const status = await checkRegistrationStatus();
				setRegistrationEnabled(status.registration_enabled);
				setUserCount(status.user_count);
				setHasConnectionError(false);
				// If no users exist, show registration form by default
				if (status.user_count === 0) {
					setShowRegister(true);
				}
			} catch (error) {
				console.error("Failed to check registration status:", error);
				setHasConnectionError(true);
				// On connection error, default to login form (assume users exist)
				setShowRegister(false);
			} finally {
				setIsLoading(false);
			}
		};

		if (!isAuthenticated) {
			checkStatus();
		}
	}, [isAuthenticated, checkRegistrationStatus]);

	// Redirect if already authenticated
	if (isAuthenticated) {
		return null;
	}

	if (isLoading) {
		return (
			<div className="flex min-h-screen items-center justify-center bg-gray-50">
				<div className="h-8 w-8 animate-spin rounded-full border-blue-600 border-b-2" />
			</div>
		);
	}

	return (
		<div className="flex min-h-screen items-center justify-center bg-gray-50 px-4 py-12 sm:px-6 lg:px-8">
			<div className="flex w-full max-w-md flex-col items-center justify-center space-y-8">
				<Logo width={48} height={48} />
				<div className="text-center">
					<h2 className="mt-6 font-extrabold text-3xl text-gray-900">
						{showRegister ? "Create Admin Account" : "Sign in to Altmount"}
					</h2>
					<p className="mt-2 text-sm">
						{hasConnectionError
							? "Cannot connect to server - please check your connection"
							: showRegister
								? "Set up your administrator account to get started"
								: userCount === 0
									? "No users found - please create an admin account"
									: "Enter your credentials to continue"}
					</p>
				</div>

				{!hasConnectionError && (userCount === 0 || showRegister) ? (
					// Registration form (only for first user)
					<div>
						<RegisterForm
							onSuccess={() => {
								// After successful registration, user will be logged in automatically
								// The auth context will handle the redirect
							}}
						/>

						{userCount > 0 && (
							<div className="mt-4 text-center">
								<button
									type="button"
									onClick={() => setShowRegister(false)}
									className="text-blue-600 text-sm hover:text-blue-500"
								>
									Already have an account? Sign in
								</button>
							</div>
						)}
					</div>
				) : (
					// Login form (for existing users)
					<div>
						<DirectLoginForm
							onSuccess={() => {
								// After successful login, user will be redirected automatically
								// The auth context will handle the redirect
							}}
						/>

						{registrationEnabled && (
							<div className="mt-4 text-center">
								<button
									type="button"
									onClick={() => setShowRegister(true)}
									className="text-blue-600 text-sm hover:text-blue-500"
								>
									Need to create an account? Register
								</button>
							</div>
						)}
					</div>
				)}

				<div className="text-center text-xs">
					{hasConnectionError ? (
						<div className="space-y-2">
							<p className="text-red-600">Connection to server failed.</p>
							<button
								type="button"
								onClick={() => window.location.reload()}
								className="text-blue-600 hover:text-blue-500"
							>
								Retry connection
							</button>
						</div>
					) : (
						<>
							<p>By signing in, you agree to use this application responsibly.</p>
							{userCount === 0 && (
								<p className="mt-1 text-blue-600">
									The first user will automatically receive administrator privileges.
								</p>
							)}
						</>
					)}
				</div>
			</div>
		</div>
	);
}
