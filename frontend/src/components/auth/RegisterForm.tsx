import { AlertCircle, User } from "lucide-react";
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
		<form onSubmit={handleSubmit} className="space-y-4">
			<div role="alert" className="alert alert-info">
				<User className="h-5 w-5" aria-hidden="true" />
				<div>
					<div className="font-medium text-sm">First User Registration</div>
					<div className="text-sm">
						You're registering as the first user and will be granted administrator privileges.
					</div>
				</div>
			</div>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">Username *</legend>
				<input
					id="username"
					name="username"
					type="text"
					autoComplete="username"
					required
					value={formData.username}
					onChange={handleChange}
					className={`input w-full ${validationErrors.username ? "input-error" : ""}`}
					placeholder="Choose a username (min 3 characters)"
				/>
				{validationErrors.username && (
					<p className="text-error text-sm">{validationErrors.username}</p>
				)}
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">Email (optional)</legend>
				<input
					id="email"
					name="email"
					type="email"
					autoComplete="email"
					value={formData.email}
					onChange={handleChange}
					className={`input w-full ${validationErrors.email ? "input-error" : ""}`}
					placeholder="Enter your email address"
				/>
				{validationErrors.email && <p className="text-error text-sm">{validationErrors.email}</p>}
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">Password *</legend>
				<input
					id="password"
					name="password"
					type="password"
					autoComplete="new-password"
					required
					value={formData.password}
					onChange={handleChange}
					className={`input w-full ${validationErrors.password ? "input-error" : ""}`}
					placeholder="Choose a secure password (min 12 characters)"
				/>
				{validationErrors.password && (
					<p className="text-error text-sm">{validationErrors.password}</p>
				)}
			</fieldset>

			<fieldset className="fieldset">
				<legend className="fieldset-legend">Confirm Password *</legend>
				<input
					id="confirmPassword"
					name="confirmPassword"
					type="password"
					autoComplete="new-password"
					required
					value={formData.confirmPassword}
					onChange={handleChange}
					className={`input w-full ${validationErrors.confirmPassword ? "input-error" : ""}`}
					placeholder="Confirm your password"
				/>
				{validationErrors.confirmPassword && (
					<p className="text-error text-sm">{validationErrors.confirmPassword}</p>
				)}
			</fieldset>

			{error && (
				<div role="alert" className="alert alert-error">
					<AlertCircle className="h-5 w-5" aria-hidden="true" />
					<div>
						<div className="font-medium">Registration Failed</div>
						<div className="text-sm">{error}</div>
					</div>
				</div>
			)}

			<button type="submit" disabled={isLoading} className="btn btn-success w-full">
				{isLoading ? (
					<>
						<span className="loading loading-spinner loading-sm" />
						Creating account...
					</>
				) : (
					"Create Admin Account"
				)}
			</button>
		</form>
	);
}
