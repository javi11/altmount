import { useState, useEffect } from "react";
import { useAuth } from "../../hooks/useAuth";
import { DirectLoginForm } from "./DirectLoginForm";
import { RegisterForm } from "./RegisterForm";
import { Logo } from "../ui/Logo";

export function LoginPage() {
	const { isAuthenticated, checkRegistrationStatus } = useAuth();
	const [registrationEnabled, setRegistrationEnabled] = useState<boolean>(false);
	const [userCount, setUserCount] = useState<number>(0);
	const [isLoading, setIsLoading] = useState(true);
	const [showRegister, setShowRegister] = useState(false);

	// Check registration status on mount
	useEffect(() => {
		const checkStatus = async () => {
			try {
				const status = await checkRegistrationStatus();
				setRegistrationEnabled(status.registration_enabled);
				setUserCount(status.user_count);
				// If no users exist, show registration form by default
				if (status.user_count === 0) {
					setShowRegister(true);
				}
			} catch (error) {
				console.error("Failed to check registration status:", error);
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
			<div className="min-h-screen flex items-center justify-center bg-gray-50">
				<div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
			</div>
		);
	}

	return (
		<div className="min-h-screen flex items-center justify-center bg-gray-50 py-12 px-4 sm:px-6 lg:px-8">
			<div className="max-w-md w-full space-y-8 flex items-center justify-center flex-col">
				<Logo width={48} height={48} />
				<div className="text-center">
					<h2 className="mt-6 text-3xl font-extrabold text-gray-900">
						{showRegister ? "Create Admin Account" : "Sign in to Altmount"}
					</h2>
					<p className="mt-2 text-sm text-gray-600">
						{showRegister 
							? "Set up your administrator account to get started" 
							: userCount === 0 
							? "No users found - please create an admin account" 
							: "Enter your credentials to continue"
						}
					</p>
				</div>

				{userCount === 0 || showRegister ? (
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
									className="text-sm text-blue-600 hover:text-blue-500"
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
									className="text-sm text-blue-600 hover:text-blue-500"
								>
									Need to create an account? Register
								</button>
							</div>
						)}
					</div>
				)}

				<div className="text-center text-xs text-gray-500">
					<p>By signing in, you agree to use this application responsibly.</p>
					{userCount === 0 && (
						<p className="mt-1 text-blue-600">
							The first user will automatically receive administrator privileges.
						</p>
					)}
				</div>
			</div>
		</div>
	);
}