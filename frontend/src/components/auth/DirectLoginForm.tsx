import type React from "react";
import { useState } from "react";
import { useAuth } from "../../hooks/useAuth";

interface DirectLoginFormProps {
	onSuccess?: () => void;
}

export function DirectLoginForm({ onSuccess }: DirectLoginFormProps) {
	const { login, isLoading, error } = useAuth();
	const [formData, setFormData] = useState({
		username: "",
		password: "",
	});

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();

		if (!formData.username || !formData.password) {
			return;
		}

		const success = await login(formData.username, formData.password);
		if (success && onSuccess) {
			onSuccess();
		}
	};

	const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
		setFormData((prev) => ({
			...prev,
			[e.target.name]: e.target.value,
		}));
	};

	return (
		<form onSubmit={handleSubmit} className="space-y-6">
			<div>
				<label htmlFor="username" className="block font-medium text-gray-700 text-sm">
					Username or Email
				</label>
				<input
					id="username"
					name="username"
					type="text"
					autoComplete="username"
					required
					value={formData.username}
					onChange={handleChange}
					className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 placeholder-gray-400 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-blue-500"
					placeholder="Enter your username or email"
				/>
			</div>

			<div>
				<label htmlFor="password" className="block font-medium text-gray-700 text-sm">
					Password
				</label>
				<input
					id="password"
					name="password"
					type="password"
					autoComplete="current-password"
					required
					value={formData.password}
					onChange={handleChange}
					className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 placeholder-gray-400 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-blue-500"
					placeholder="Enter your password"
				/>
			</div>

			{error && (
				<div className="rounded-md bg-red-50 p-4">
					<div className="flex">
						<div className="flex-shrink-0">
							<svg className="h-5 w-5 text-red-400" viewBox="0 0 20 20" fill="currentColor">
								<title>Error icon</title>
								<path
									fillRule="evenodd"
									d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z"
									clipRule="evenodd"
								/>
							</svg>
						</div>
						<div className="ml-3">
							<h3 className="font-medium text-red-800 text-sm">Login Failed</h3>
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
					disabled={isLoading || !formData.username || !formData.password}
					className={`flex w-full justify-center rounded-md border border-transparent px-4 py-2 font-medium text-sm text-white shadow-sm ${
						isLoading || !formData.username || !formData.password
							? "cursor-not-allowed bg-gray-400"
							: "bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
					}`}
				>
					{isLoading ? (
						<div className="flex items-center">
							<div className="mr-2 h-4 w-4 animate-spin rounded-full border-white border-b-2" />
							Signing in...
						</div>
					) : (
						"Sign in"
					)}
				</button>
			</div>
		</form>
	);
}
