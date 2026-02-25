import { AlertCircle } from "lucide-react";
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
		<form onSubmit={handleSubmit} className="space-y-4">
			<fieldset className="fieldset">
				<legend className="fieldset-legend">Username or Email</legend>
				<input
					id="username"
					name="username"
					type="text"
					autoComplete="username"
					required
					value={formData.username}
					onChange={handleChange}
					className="input w-full"
					placeholder="Enter your username or email"
				/>
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">Password</legend>
				<input
					id="password"
					name="password"
					type="password"
					autoComplete="current-password"
					required
					value={formData.password}
					onChange={handleChange}
					className="input w-full"
					placeholder="Enter your password"
				/>
			</fieldset>

			{error && (
				<div role="alert" className="alert alert-error">
					<AlertCircle className="h-5 w-5" aria-hidden="true" />
					<div>
						<div className="font-medium">Login Failed</div>
						<div className="text-sm">{error}</div>
					</div>
				</div>
			)}

			<button
				type="submit"
				disabled={isLoading || !formData.username || !formData.password}
				className="btn btn-primary w-full"
			>
				{isLoading ? (
					<>
						<span className="loading loading-spinner loading-sm" />
						Signing in...
					</>
				) : (
					"Sign in"
				)}
			</button>
		</form>
	);
}
