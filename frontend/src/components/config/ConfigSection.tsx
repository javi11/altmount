import { AlertTriangle, Check, Save, X } from "lucide-react";
import type { ReactNode } from "react";
import { LoadingSpinner } from "../ui/LoadingSpinner";

interface ConfigSectionProps {
	title: string;
	description: string;
	icon: string;
	canEdit: boolean;
	requiresRestart?: boolean;
	hasChanges?: boolean;
	isLoading?: boolean;
	error?: string;
	children: ReactNode;
	onSave?: () => void;
	onReset?: () => void;
}

export function ConfigSection({
	title,
	description,
	icon,
	canEdit,
	requiresRestart,
	hasChanges = false,
	isLoading = false,
	error,
	children,
	onSave,
	onReset,
}: ConfigSectionProps) {
	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body">
				{/* Section Header */}
				<div className="mb-6">
					<div className="flex items-center justify-between mb-4">
						<div className="flex items-center space-x-3">
							<span className="text-2xl">{icon}</span>
							<div>
								<h2 className="text-2xl font-bold">{title}</h2>
								<p className="text-base-content/70">{description}</p>
							</div>
						</div>

						{/* Action Buttons */}
						{canEdit && hasChanges && (
							<div className="flex items-center space-x-2">
								<button
									type="button"
									className="btn btn-ghost btn-sm"
									onClick={onReset}
									disabled={isLoading}
								>
									<X className="h-4 w-4" />
									Reset
								</button>
								<button
									type="button"
									className="btn btn-primary btn-sm"
									onClick={onSave}
									disabled={isLoading}
								>
									{isLoading ? (
										<LoadingSpinner size="sm" />
									) : (
										<Save className="h-4 w-4" />
									)}
									Save
								</button>
							</div>
						)}
					</div>

					{/* Section Status */}
					<div className="flex items-center space-x-2">
						{canEdit ? (
							<div className="badge badge-success">
								<Check className="h-3 w-3 mr-1" />
								Editable
							</div>
						) : (
							<div className="badge badge-ghost">
								<X className="h-3 w-3 mr-1" />
								Read Only
							</div>
						)}

						{requiresRestart && (
							<div className="badge badge-warning">
								<AlertTriangle className="h-3 w-3 mr-1" />
								Requires Restart
							</div>
						)}

						{hasChanges && (
							<div className="badge badge-info">
								<AlertTriangle className="h-3 w-3 mr-1" />
								Unsaved Changes
							</div>
						)}
					</div>
				</div>

				{/* Error Alert */}
				{error && (
					<div className="alert alert-error mb-4">
						<X className="h-6 w-6" />
						<div>
							<div className="font-bold">Configuration Error</div>
							<div className="text-sm">{error}</div>
						</div>
					</div>
				)}

				{/* Content */}
				<div className={`${isLoading ? "opacity-50 pointer-events-none" : ""}`}>
					{children}
				</div>
			</div>
		</div>
	);
}
