import {
	AlertTriangle,
	ArrowUpCircle,
	CheckCircle,
	ExternalLink,
	RefreshCw,
	Zap,
} from "lucide-react";
import { useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useApplyUpdate, useUpdateStatus } from "../../hooks/useUpdate";
import type { UpdateChannel } from "../../types/update";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function UpdateSection() {
	const [channel, setChannel] = useState<UpdateChannel>("latest");
	const [checkEnabled, setCheckEnabled] = useState(false);

	const { confirmAction } = useConfirm();
	const { showToast } = useToast();

	const {
		data: updateStatus,
		isLoading: isChecking,
		refetch,
	} = useUpdateStatus(channel, checkEnabled);

	const applyUpdate = useApplyUpdate();

	const handleCheckForUpdates = () => {
		setCheckEnabled(true);
		refetch();
	};

	const handleApplyUpdate = async () => {
		const confirmed = await confirmAction(
			"Apply Update",
			`This will pull the latest ${channel} image and restart the container. The service will be briefly unavailable. Continue?`,
			{ type: "warning", confirmText: "Update Now", confirmButtonClass: "btn-warning" },
		);
		if (!confirmed) return;

		try {
			await applyUpdate.mutateAsync(channel);
			showToast({
				type: "success",
				title: "Update started",
				message: "Pulling new image. The container will restart automatically.",
			});
		} catch (err) {
			showToast({
				type: "error",
				title: "Update failed",
				message: err instanceof Error ? err.message : "Failed to apply update",
			});
		}
	};

	const dockerUnavailable = updateStatus && !updateStatus.docker_available;
	const updateAvailable = updateStatus?.update_available ?? false;

	return (
		<div className="space-y-6 rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
			<div className="flex items-center gap-2">
				<ArrowUpCircle className="h-4 w-4 text-base-content/60" />
				<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
					Updates
				</h4>
				<div className="h-px flex-1 bg-base-300/50" />
			</div>

			{/* Version info */}
			{updateStatus && (
				<div className="flex flex-wrap gap-3">
					<div className="rounded-lg border border-base-300 bg-base-100 px-3 py-2">
						<span className="text-[10px] text-base-content/50 uppercase tracking-wider">
							Current
						</span>
						<p className="font-mono font-semibold text-sm">{updateStatus.current_version}</p>
					</div>
					{updateStatus.git_commit && updateStatus.git_commit !== "unknown" && (
						<div className="rounded-lg border border-base-300 bg-base-100 px-3 py-2">
							<span className="text-[10px] text-base-content/50 uppercase tracking-wider">
								Commit
							</span>
							<p className="font-mono text-sm">{updateStatus.git_commit}</p>
						</div>
					)}
					{updateStatus.latest_version && (
						<div className="rounded-lg border border-base-300 bg-base-100 px-3 py-2">
							<span className="text-[10px] text-base-content/50 uppercase tracking-wider">
								Latest
							</span>
							<p className="font-mono font-semibold text-sm">{updateStatus.latest_version}</p>
						</div>
					)}
				</div>
			)}

			{/* Channel selector */}
			<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
				<fieldset className="fieldset">
					<legend className="fieldset-legend font-semibold text-xs">Update Channel</legend>
					<div className="join">
						<button
							type="button"
							className={`btn btn-sm join-item ${channel === "latest" ? "btn-primary" : "btn-ghost border-base-300"}`}
							onClick={() => {
								setChannel("latest");
								setCheckEnabled(false);
							}}
						>
							<CheckCircle className="h-3 w-3" />
							Latest (stable)
						</button>
						<button
							type="button"
							className={`btn btn-sm join-item ${channel === "dev" ? "btn-primary" : "btn-ghost border-base-300"}`}
							onClick={() => {
								setChannel("dev");
								setCheckEnabled(false);
							}}
						>
							<Zap className="h-3 w-3" />
							Dev (rolling)
						</button>
					</div>
					<p className="label mt-1 text-[11px] text-base-content/50">
						{channel === "latest"
							? "Stable releases tagged as vX.Y.Z"
							: "Rolling builds from the main branch — may be unstable"}
					</p>
				</fieldset>

				<div className="flex gap-2 self-start sm:self-auto">
					<button
						type="button"
						className="btn btn-sm btn-ghost border-base-300 bg-base-100 hover:bg-base-200"
						onClick={handleCheckForUpdates}
						disabled={isChecking}
					>
						{isChecking ? <LoadingSpinner size="sm" /> : <RefreshCw className="h-3 w-3" />}
						Check for Updates
					</button>

					{updateAvailable && (
						<button
							type="button"
							className="btn btn-sm btn-warning"
							onClick={handleApplyUpdate}
							disabled={applyUpdate.isPending || dockerUnavailable}
						>
							{applyUpdate.isPending ? (
								<LoadingSpinner size="sm" />
							) : (
								<ArrowUpCircle className="h-3 w-3" />
							)}
							Update Now
						</button>
					)}
				</div>
			</div>

			{/* Status messages */}
			{updateStatus && !isChecking && (
				<>
					{updateAvailable ? (
						<div className="alert alert-warning">
							<ArrowUpCircle className="h-5 w-5 shrink-0" />
							<div>
								<div className="font-semibold">Update available</div>
								<div className="text-sm">
									{updateStatus.latest_version} is ready to install.{" "}
									{updateStatus.release_url && (
										<a
											href={updateStatus.release_url}
											target="_blank"
											rel="noopener noreferrer"
											className="inline-flex items-center gap-1 underline"
										>
											Release notes <ExternalLink className="h-3 w-3" />
										</a>
									)}
								</div>
							</div>
						</div>
					) : updateStatus.latest_version ? (
						<div className="alert alert-success">
							<CheckCircle className="h-5 w-5 shrink-0" />
							<div className="text-sm">You are running the latest version.</div>
						</div>
					) : null}

					{dockerUnavailable && (
						<div className="alert alert-warning">
							<AlertTriangle className="h-5 w-5 shrink-0" />
							<div>
								<div className="font-semibold">Auto-update unavailable</div>
								<div className="text-sm">
									Mount <code className="font-mono">/var/run/docker.sock</code> into the container
									to enable one-click updates.
								</div>
							</div>
						</div>
					)}
				</>
			)}
		</div>
	);
}
