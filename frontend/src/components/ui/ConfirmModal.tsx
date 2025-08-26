import { AlertTriangle, CheckCircle, Info, X, XCircle } from "lucide-react";
import { useEffect, useRef } from "react";

export interface ConfirmModalProps {
	isOpen: boolean;
	title: string;
	message: string;
	type?: "info" | "warning" | "error" | "success";
	confirmText?: string;
	cancelText?: string;
	confirmButtonClass?: string;
	onConfirm: () => void;
	onCancel: () => void;
}

const typeConfig = {
	info: {
		icon: Info,
		iconClass: "text-info",
		alertClass: "alert-info",
	},
	warning: {
		icon: AlertTriangle,
		iconClass: "text-warning",
		alertClass: "alert-warning",
	},
	error: {
		icon: XCircle,
		iconClass: "text-error",
		alertClass: "alert-error",
	},
	success: {
		icon: CheckCircle,
		iconClass: "text-success",
		alertClass: "alert-success",
	},
};

export function ConfirmModal({
	isOpen,
	title,
	message,
	type = "warning",
	confirmText = "Confirm",
	cancelText = "Cancel",
	confirmButtonClass = "btn-primary",
	onConfirm,
	onCancel,
}: ConfirmModalProps) {
	const modalRef = useRef<HTMLDialogElement>(null);
	const confirmButtonRef = useRef<HTMLButtonElement>(null);

	useEffect(() => {
		const modal = modalRef.current;
		if (modal) {
			if (isOpen) {
				modal.showModal();
				// Focus the confirm button for accessibility
				setTimeout(() => {
					confirmButtonRef.current?.focus();
				}, 0);
			} else {
				modal.close();
			}
		}
	}, [isOpen]);

	useEffect(() => {
		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.key === "Escape" && isOpen) {
				onCancel();
			}
		};

		if (isOpen) {
			document.addEventListener("keydown", handleKeyDown);
		}

		return () => {
			document.removeEventListener("keydown", handleKeyDown);
		};
	}, [isOpen, onCancel]);

	const config = typeConfig[type];
	const Icon = config.icon;

	return (
		<dialog ref={modalRef} className="modal" onClose={onCancel}>
			<div className="modal-box">
				{/* Header with Close Button */}
				<div className="mb-4 flex items-center justify-between">
					<h3 className="font-bold text-lg">{title}</h3>
					<button
						type="button"
						className="btn btn-ghost btn-sm"
						onClick={onCancel}
						aria-label="Close modal"
					>
						<X className="h-4 w-4" />
					</button>
				</div>

				{/* Content */}
				<div className={`alert ${config.alertClass} mb-6`}>
					<Icon className={`h-6 w-6 ${config.iconClass}`} />
					<div>
						<p>{message}</p>
					</div>
				</div>

				{/* Actions */}
				<div className="modal-action">
					<button type="button" className="btn btn-ghost" onClick={onCancel}>
						{cancelText}
					</button>
					<button
						ref={confirmButtonRef}
						type="button"
						className={`btn ${confirmButtonClass}`}
						onClick={onConfirm}
					>
						{confirmText}
					</button>
				</div>
			</div>

			{/* Backdrop */}
			<button
				type="button"
				className="modal-backdrop"
				onClick={onCancel}
				aria-label="Close modal"
			/>
		</dialog>
	);
}
