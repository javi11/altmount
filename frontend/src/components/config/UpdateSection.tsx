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

	const dockerMode = updateStatus?.docker_available ?? false;
	const binaryMode = !dockerMode && (updateStatus?.binary_update_available ?? false);
	const updateUnavailable = updateStatus !== undefined && !dockerMode && !binaryMode;

	const handleApplyUpdate = async (force = false) => {
		const actionTitle = force ? "Force Reinstall" : "Apply Update";
		const baseAction = dockerMode
			? `pull the ${channel} image and restart the container`
			: `download the ${channel} binary and restart`;
		const actionMessage = force
			? `This will force-${baseAction}, even if the version hasn't changed. Continue?`
			: `This will ${baseAction}. The service will be briefly unavailable. Continue?`;

		const confirmed = await confirmAction(actionTitle, actionMessage, {
			type: force ? "error" : "warning",
			confirmText: force ? "Force Reinstall" : "Update Now",
			confirmButtonClass: force ? "btn-error" : "btn-warning",
		});
		if (!confirmed) return;

		try {
			await applyUpdate.mutateAsync({ channel, force });
			showToast({
				type: "success",
				title: force ? "Reinstall started" : "Update started",
				message: dockerMode
					? "Pulling image. The container will restart automatically."
					: "Downloading binary. The service will restart automatically.",
			});
		} catch (err) {
			showToast({
				type: "error",
				title: "Operation failed",
				message: err instanceof Error ? err.message : "Failed to apply update",
			});
		}
	};

	const updateAvailable = updateStatus?.update_available ?? false;

	/** Taller tap targets below md (touch-friendly ~48px min height) */
	const updateActionBtnLayout = "max-md:min-h-12 max-md:py-2.5 max-md:leading-snug";

	return (
		<div className="min-w-0 space-y-6 overflow-hidden rounded-2xl border-2 border-base-300/80 bg-base-200/60 p-6">
			<div className="flex items-center gap-2">
				<ArrowUpCircle className="h-4 w-4 text-base-content/60" />
				<h4 className="font-bold text-base-content/40 text-xs uppercase tracking-widest">
					Updates
				</h4>
				<div className="h-px flex-1 bg-base-300/50" />
			</div>

			{/* Version info */}
			{updateStatus && (
				<div className="flex min-w-0 flex-wrap gap-3">
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

			{/* Row 1: channel · Row 2: check + apply/reinstall */}
			<div className="flex min-w-0 flex-col gap-4">
				<fieldset className="fieldset min-w-0">
					<legend className="fieldset-legend font-semibold text-xs">Update Channel</legend>
					<div className="join w-full min-w-0">
						<button
							type="button"
							className={`btn btn-sm join-item min-w-0 flex-1 gap-1 px-2 sm:px-3 ${channel === "latest" ? "btn-primary" : "btn-ghost border-base-300"}`}
							onClick={() => {
								setChannel("latest");
								setCheckEnabled(false);
							}}
						>
							<CheckCircle className="h-3 w-3 shrink-0" />
							<span className="truncate">Latest (stable)</span>
						</button>
						<button
							type="button"
							className={`btn btn-sm join-item min-w-0 flex-1 gap-1 px-2 sm:px-3 ${channel === "dev" ? "btn-primary" : "btn-ghost border-base-300"}`}
							onClick={() => {
								setChannel("dev");
								setCheckEnabled(false);
							}}
						>
							<Zap className="h-3 w-3 shrink-0" />
							<span className="truncate">Dev (rolling)</span>
						</button>
					</div>
					<p className="label mt-1 min-w-0 max-w-full whitespace-normal break-words text-[11px] text-base-content/50">
						{channel === "latest"
							? "Stable releases tagged as vX.Y.Z"
							: "Rolling builds from the main branch — may be unstable"}
					</p>
				</fieldset>

				<div className="grid min-w-0 grid-cols-2 gap-2">
					<button
						type="button"
						className={`btn btn-sm btn-ghost min-w-0 border-base-300 bg-base-100 hover:bg-base-200 ${updateActionBtnLayout}`}
						onClick={handleCheckForUpdates}
						disabled={isChecking}
					>
						{isChecking ? <LoadingSpinner size="sm" /> : <RefreshCw className="h-3 w-3" />}
						Check for Updates
					</button>

					{updateAvailable ? (
						<button
							type="button"
							className={`btn btn-sm btn-warning min-w-0 ${updateActionBtnLayout}`}
							onClick={() => handleApplyUpdate(false)}
							disabled={applyUpdate.isPending || updateUnavailable}
						>
							{applyUpdate.isPending ? (
								<LoadingSpinner size="sm" />
							) : (
								<ArrowUpCircle className="h-3 w-3" />
							)}
							Update Now
						</button>
					) : (
						<button
							type="button"
							className={`btn btn-sm btn-ghost min-w-0 border-base-300 bg-base-100 hover:bg-base-200 ${updateActionBtnLayout}`}
							onClick={() => handleApplyUpdate(true)}
							disabled={applyUpdate.isPending || updateUnavailable || isChecking}
						>
							{applyUpdate.isPending ? (
								<LoadingSpinner size="sm" />
							) : (
								<RefreshCw className="h-3 w-3" />
							)}
							Force Reinstall
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

					{updateUnavailable && (
						<div className="alert alert-warning">
							<AlertTriangle className="h-5 w-5 shrink-0" />
							<div>
								<div className="font-semibold">Auto-update unavailable</div>
								<div className="text-sm">
									For Docker installs, mount <code className="font-mono">/var/run/docker.sock</code>{" "}
									into the container to enable one-click updates. For standalone binaries, ensure
									the executable file is writable by this process.
								</div>
							</div>
						</div>
					)}
					{binaryMode && (
						<div className="alert alert-info">
							<ArrowUpCircle className="h-5 w-5 shrink-0" />
							<div className="text-sm">
								Running as standalone binary — updates download the new binary and restart.
							</div>
						</div>
					)}
				</>
			)}
		</div>
	);
}
