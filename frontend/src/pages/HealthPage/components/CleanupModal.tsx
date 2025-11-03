import type { CleanupConfig } from "../types";

interface CleanupModalProps {
	show: boolean;
	config: CleanupConfig;
	isPending: boolean;
	onClose: () => void;
	onConfigChange: (config: CleanupConfig) => void;
	onConfirm: () => void;
}

export function CleanupModal({
	show,
	config,
	isPending,
	onClose,
	onConfigChange,
	onConfirm,
}: CleanupModalProps) {
	if (!show) {
		return null;
	}

	return (
		<div className="modal modal-open">
			<div className="modal-box">
				<div className="mb-4 flex items-center justify-between">
					<h3 className="font-bold text-lg">Cleanup Old Health Records</h3>
					<button type="button" className="btn btn-sm btn-circle btn-ghost" onClick={onClose}>
						✕
					</button>
				</div>

				<div className="space-y-4">
					<fieldset className="fieldset">
						<legend className="fieldset-legend">Delete Records Older Than</legend>
						<input
							type="datetime-local"
							className="input"
							value={config.older_than}
							max={new Date().toISOString().slice(0, 16)}
							onChange={(e) =>
								onConfigChange({
									...config,
									older_than: e.target.value,
								})
							}
						/>
						<p className="label text-base-content/70 text-sm">
							Records created before this date and time will be deleted
						</p>
					</fieldset>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Delete Options</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Also delete physical files</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={config.delete_files}
								onChange={(e) =>
									onConfigChange({
										...config,
										delete_files: e.target.checked,
									})
								}
							/>
						</label>
						<p className="label text-base-content/70 text-sm">
							{config.delete_files ? (
								<span className="text-error">
									⚠️ Warning: This will permanently delete the physical files from your system. This
									action cannot be undone!
								</span>
							) : (
								<span>Only database records will be removed, files will remain intact</span>
							)}
						</p>
					</fieldset>

					<div className="alert alert-info">
						<svg
							xmlns="http://www.w3.org/2000/svg"
							fill="none"
							viewBox="0 0 24 24"
							className="h-6 w-6 shrink-0 stroke-current"
							role="img"
							aria-label="Information"
						>
							<path
								strokeLinecap="round"
								strokeLinejoin="round"
								strokeWidth="2"
								d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
							/>
						</svg>
						<div className="text-sm">
							<div className="font-bold">Records to be deleted:</div>
							<div>
								Health records created before {new Date(config.older_than).toLocaleString()}
							</div>
							{config.delete_files && (
								<div className="mt-1 font-semibold text-error">
									Physical files will also be deleted!
								</div>
							)}
						</div>
					</div>
				</div>

				<div className="modal-action">
					<button type="button" className="btn btn-ghost" onClick={onClose}>
						Cancel
					</button>
					<button
						type="button"
						className={`btn ${config.delete_files ? "btn-error" : "btn-warning"}`}
						onClick={onConfirm}
						disabled={isPending}
					>
						{isPending ? (
							<>
								<span className="loading loading-spinner loading-sm" />
								Cleaning up...
							</>
						) : config.delete_files ? (
							"Delete Records & Files"
						) : (
							"Delete Records"
						)}
					</button>
				</div>
			</div>
		</div>
	);
}
