import { ArrowUpCircle, Box, ExternalLink, RefreshCw, Tag } from "lucide-react";
import { useApplyUpdate, useUpdateStatus } from "../../hooks/useUpdate";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function UpdateSection() {
	const { data: updateStatus, isLoading, error, refetch } = useUpdateStatus();
	const applyUpdate = useApplyUpdate();

	if (isLoading) {
		return (
			<div className="flex min-h-[200px] items-center justify-center">
				<LoadingSpinner size="lg" />
			</div>
		);
	}

	if (error) {
		return (
			<div className="alert alert-error">
				<div>Failed to load update status: {error.message}</div>
			</div>
		);
	}

	return (
		<div className="space-y-10">
			<div>
				<h3 className="font-bold text-base-content text-lg tracking-tight">Updates</h3>
				<p className="break-words text-base-content/50 text-sm">
					Manage application version and updates.
				</p>
			</div>

			<div className="space-y-8">
				{/* Version Info */}
				<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
					<div className="flex items-center gap-2">
						<Tag className="h-4 w-4 text-base-content/60" />
						<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
							Version
						</h4>
						<div className="h-px flex-1 bg-base-300/50" />
					</div>

					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
						<div className="flex flex-col gap-1">
							<span className="text-base-content/50 text-xs font-semibold uppercase tracking-wider">
								Current Version
							</span>
							<span className="font-mono text-base-content text-sm">
								{updateStatus?.current_version ?? "—"}
							</span>
						</div>

						<div className="flex flex-col gap-1">
							<span className="text-base-content/50 text-xs font-semibold uppercase tracking-wider">
								Latest Version
							</span>
							<span className="font-mono text-base-content text-sm">
								{updateStatus?.latest_version ?? "—"}
							</span>
						</div>
					</div>

					{updateStatus?.update_available && (
						<div className="alert alert-info">
							<ArrowUpCircle className="h-5 w-5 shrink-0" />
							<div>
								<div className="font-bold">Update Available</div>
								<div className="text-sm">
									A new version ({updateStatus.latest_version}) is ready to install.
								</div>
							</div>
						</div>
					)}

					{updateStatus && !updateStatus.update_available && (
						<div className="alert alert-success">
							<div className="text-sm">You are running the latest version.</div>
						</div>
					)}
				</div>

				{/* Docker notice or apply update */}
				{updateStatus?.update_available && (
					<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
						<div className="flex items-center gap-2">
							<ArrowUpCircle className="h-4 w-4 text-base-content/60" />
							<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
								Apply Update
							</h4>
							<div className="h-px flex-1 bg-base-300/50" />
						</div>

						{updateStatus.docker_available ? (
							<div className="flex items-start gap-4">
								<Box className="mt-0.5 h-5 w-5 shrink-0 text-base-content/50" />
								<div className="min-w-0 flex-1">
									<h5 className="font-bold text-sm">Docker Deployment Detected</h5>
									<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
										To update, pull the latest image and restart your container. Refer to your
										deployment configuration for the exact commands.
									</p>
								</div>
							</div>
						) : (
							<div className="flex items-start justify-between gap-4">
								<div className="min-w-0 flex-1">
									<h5 className="font-bold text-sm">Install Update</h5>
									<p className="mt-1 break-words text-[11px] text-base-content/50 leading-relaxed">
										The application will download and apply the update, then restart automatically.
									</p>
								</div>
								<button
									type="button"
									className="btn btn-primary btn-sm shrink-0"
									onClick={() => applyUpdate.mutate()}
									disabled={applyUpdate.isPending}
								>
									{applyUpdate.isPending ? (
										<LoadingSpinner size="sm" />
									) : (
										<ArrowUpCircle className="h-4 w-4" />
									)}
									{applyUpdate.isPending ? "Applying..." : "Apply Update"}
								</button>
							</div>
						)}

						{updateStatus.release_url && (
							<a
								href={updateStatus.release_url}
								target="_blank"
								rel="noreferrer"
								className="btn btn-ghost btn-sm border-base-300 bg-base-100 hover:bg-base-200"
							>
								<ExternalLink className="h-3 w-3" />
								View Release Notes
							</a>
						)}
					</div>
				)}

				{/* Refresh */}
				<div className="flex justify-end">
					<button
						type="button"
						className="btn btn-ghost btn-sm border-base-300 bg-base-100 hover:bg-base-200"
						onClick={() => refetch()}
					>
						<RefreshCw className="h-3 w-3" />
						Check for Updates
					</button>
				</div>
			</div>
		</div>
	);
}
