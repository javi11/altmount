import { User } from "lucide-react";
import type React from "react";
import { useState } from "react";
import { useAuth } from "../../hooks/useAuth";

interface RegisterFormProps {
	onSuccess?: () => void;
}

export function RegisterForm({ onSuccess }: RegisterFormProps) {
	const { register, isLoading, error } = useAuth();
	const [formData, setFormData] = useState({
		username: "",
		email: "",
		password: "",
		confirmPassword: "",
	});
	const [validationErrors, setValidationErrors] = useState<Record<string, string>>({});

	const validateForm = (): boolean => {
		const errors: Record<string, string> = {};

		if (!formData.username || formData.username.length < 3) {
			errors.username = "Username must be at least 3 characters long";
		}

		if (!formData.password || formData.password.length < 12) {
			errors.password = "Password must be at least 12 characters long";
		}

		if (formData.password !== formData.confirmPassword) {
			errors.confirmPassword = "Passwords do not match";
		}

		if (formData.email && !/\S+@\S+\.\S+/.test(formData.email)) {
			errors.email = "Email address is invalid";
		}

		setValidationErrors(errors);
		return Object.keys(errors).length === 0;
	};

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();

		if (!validateForm()) {
			return;
		}

		const success = await register(
			formData.username,
			formData.email || undefined,
			formData.password,
		);

		if (success && onSuccess) {
			onSuccess();
		}
	};

	const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
		const { name, value } = e.target;
		setFormData((prev) => ({
			...prev,
			[name]: value,
		}));

		// Clear validation error when user starts typing
		if (validationErrors[name]) {
			setValidationErrors((prev) => ({
				...prev,
				[name]: "",
			}));
		}
	};

	return (
		<form onSubmit={handleSubmit} className="space-y-6">
			<div className="rounded-md border border-blue-200 bg-blue-50 p-4">
				<div className="flex">
					<div className="flex-shrink-0">
						<User />
					</div>
					<div className="ml-3">
						<h3 className="font-medium text-blue-800 text-sm">First User Registration</h3>
						<div className="mt-2 text-blue-700 text-sm">
							<p>
								You're registering as the first user and will be granted administrator privileges.
							</p>
						</div>
					</div>
				</div>
			</div>

			<div>
				<label htmlFor="username" className="block font-medium text-gray-700 text-sm">
					Username *
				</label>
				<input
					id="username"
					name="username"
					type="text"
					autoComplete="username"
					required
					value={formData.username}
					onChange={handleChange}
					className={`mt-1 block w-full rounded-md border px-3 py-2 placeholder-gray-400 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-blue-500 ${
						validationErrors.username ? "border-red-300" : "border-gray-300"
					}`}
					placeholder="Choose a username (min 3 characters)"
				/>
				{validationErrors.username && (
					<p className="mt-1 text-red-600 text-sm">{validationErrors.username}</p>
				)}
			</div>

			<div>
				<label htmlFor="email" className="block font-medium text-gray-700 text-sm">
					Email (optional)
				</label>
				<input
					id="email"
					name="email"
					type="email"
					autoComplete="email"
					value={formData.email}
					onChange={handleChange}
					className={`mt-1 block w-full rounded-md border px-3 py-2 placeholder-gray-400 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-blue-500 ${
						validationErrors.email ? "border-red-300" : "border-gray-300"
					}`}
					placeholder="Enter your email address"
				/>
				{validationErrors.email && (
					<p className="mt-1 text-red-600 text-sm">{validationErrors.email}</p>
				)}
			</div>

			<div>
				<label htmlFor="password" className="block font-medium text-gray-700 text-sm">
					Password *
				</label>
				<input
					id="password"
					name="password"
					type="password"
					autoComplete="new-password"
					required
					value={formData.password}
					onChange={handleChange}
					className={`mt-1 block w-full rounded-md border px-3 py-2 placeholder-gray-400 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-blue-500 ${
						validationErrors.password ? "border-red-300" : "border-gray-300"
					}`}
					placeholder="Choose a secure password (min 12 characters)"
				/>
				{validationErrors.password && (
					<p className="mt-1 text-red-600 text-sm">{validationErrors.password}</p>
				)}
			</div>

			<div>
				<label htmlFor="confirmPassword" className="block font-medium text-gray-700 text-sm">
					Confirm Password *
				</label>
				<input
					id="confirmPassword"
					name="confirmPassword"
					type="password"
					autoComplete="new-password"
					required
					value={formData.confirmPassword}
					onChange={handleChange}
					className={`mt-1 block w-full rounded-md border px-3 py-2 placeholder-gray-400 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-blue-500 ${
						validationErrors.confirmPassword ? "border-red-300" : "border-gray-300"
					}`}
					placeholder="Confirm your password"
				/>
				{validationErrors.confirmPassword && (
					<p className="mt-1 text-red-600 text-sm">{validationErrors.confirmPassword}</p>
				)}
			</div>

			{error && (
				<div className="rounded-md bg-red-50 p-4">
					<div className="flex">
						<div className="flex-shrink-0">
							<svg
								className="h-5 w-5 text-red-400"
								viewBox="0 0 20 20"
								fill="currentColor"
								aria-hidden="true"
							>
								<path
									fillRule="evenodd"
									d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z"
									clipRule="evenodd"
								/>
							</svg>
						</div>
						<div className="ml-3">
							<h3 className="font-medium text-red-800 text-sm">Registration Failed</h3>
							<div className="mt-2 text-red-700 text-sm">
								<p>{error}</p>
							</div>
						</div>
					</div>
				</div>
			)}

			<div>
				<button
					type="submit"
					disabled={isLoading}
					className={`flex w-full justify-center rounded-md border border-transparent px-4 py-2 font-medium text-sm text-white shadow-sm ${
						isLoading
							? "cursor-not-allowed bg-gray-400"
							: "bg-green-600 hover:bg-green-700 focus:outline-none focus:ring-2 focus:ring-green-500 focus:ring-offset-2"
					}`}
				>
					{isLoading ? (
						<div className="flex items-center">
							<div className="mr-2 h-4 w-4 animate-spin rounded-full border-white border-b-2" />
							Creating account...
						</div>
					) : (
						"Create Admin Account"
					)}
				</button>
			</div>
		</form>
	);
}
