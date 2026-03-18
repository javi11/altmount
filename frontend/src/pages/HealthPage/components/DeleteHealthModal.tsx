import { Info, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";

export interface DeleteHealthOptions {
	deleteMeta: boolean;
	deleteSymlink: boolean;
}

interface DeleteHealthModalProps {
	show: boolean;
	itemCount: number;
	isPending: boolean;
	onClose: () => void;
	onConfirm: (options: DeleteHealthOptions) => void;
}

export function DeleteHealthModal({
	show,
	itemCount,
	isPending,
	onClose,
	onConfirm,
}: DeleteHealthModalProps) {
	const [deleteMeta, setDeleteMeta] = useState(false);
	const [deleteSymlink, setDeleteSymlink] = useState(false);

	// Reset checkbox state when modal opens
	useEffect(() => {
		if (show) {
			setDeleteMeta(false);
			setDeleteSymlink(false);
		}
	}, [show]);

	if (!show) {
		return null;
	}

	const hasExtraOptions = deleteMeta || deleteSymlink;
	const isSingle = itemCount === 1;

	const getConfirmText = () => {
		if (isPending) return "Deleting...";
		if (deleteMeta && deleteSymlink) return isSingle ? "Delete All" : "Delete All Selected";
		if (deleteMeta) return isSingle ? "Delete Record & Meta" : "Delete Selected & Meta";
		if (deleteSymlink) return isSingle ? "Delete Record & File" : "Delete Selected & Files";
		return isSingle ? "Delete Record" : "Delete Selected";
	};

	return (
		<div className="modal modal-open">
			<div className="modal-box">
				<div className="mb-4 flex items-center justify-between">
					<h3 className="font-bold text-lg">Delete Health {isSingle ? "Record" : "Records"}</h3>
					<button type="button" className="btn btn-sm btn-circle btn-ghost" onClick={onClose}>
						✕
					</button>
				</div>

				<div className="space-y-4">
					<p className="text-base-content/80">
						{isSingle
							? "Are you sure you want to delete this health record?"
							: `Are you sure you want to delete ${itemCount} health records?`}
					</p>

					<fieldset className="fieldset">
						<legend className="fieldset-legend">Additional Cleanup</legend>
						<label className="label cursor-pointer">
							<span className="label-text">Delete metadata files</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={deleteMeta}
								onChange={(e) => setDeleteMeta(e.target.checked)}
							/>
						</label>
						<p className="label text-base-content/70 text-sm">
							Removes .meta and .id sidecar files associated with this record
						</p>

						<label className="label mt-2 cursor-pointer">
							<span className="label-text">Delete library file</span>
							<input
								type="checkbox"
								className="checkbox"
								checked={deleteSymlink}
								onChange={(e) => setDeleteSymlink(e.target.checked)}
							/>
						</label>
						<p className="label text-base-content/70 text-sm">
							Removes the symlink/file from the library directory
						</p>
					</fieldset>

					<div className={`alert ${hasExtraOptions ? "alert-warning" : "alert-info"}`}>
						{hasExtraOptions ? (
							<TriangleAlert className="h-6 w-6 shrink-0" aria-hidden="true" />
						) : (
							<Info className="h-6 w-6 shrink-0" aria-hidden="true" />
						)}
						<div className="text-sm">
							{hasExtraOptions ? (
								<>
									<div className="font-bold">This will permanently delete:</div>
									<ul className="mt-1 list-inside list-disc">
										<li>
											{itemCount} health database {isSingle ? "record" : "records"}
										</li>
										{deleteMeta && <li>Associated metadata files</li>}
										{deleteSymlink && (
											<li>Library files (empty parent directories will be cleaned up)</li>
										)}
									</ul>
								</>
							) : (
								<span>
									Only the database {isSingle ? "record" : "records"} will be removed. Files will
									remain intact.
								</span>
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
						className={`btn ${hasExtraOptions ? "btn-error" : "btn-warning"}`}
						onClick={() => onConfirm({ deleteMeta, deleteSymlink })}
						disabled={isPending}
					>
						{isPending && <span className="loading loading-spinner loading-sm" />}
						{getConfirmText()}
					</button>
				</div>
			</div>
		</div>
	);
}
