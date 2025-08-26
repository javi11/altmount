import { useToast } from "../../contexts/ToastContext";
import { ToastComponent } from "./Toast";

export function ToastContainer() {
	const { toasts, removeToast } = useToast();

	if (toasts.length === 0) {
		return null;
	}

	return (
		<div
			className="pointer-events-none fixed top-4 right-4 z-50 w-full max-w-sm space-y-2"
			aria-live="polite"
		>
			{toasts.map((toast) => (
				<div key={toast.id} className="pointer-events-auto">
					<ToastComponent toast={toast} onClose={removeToast} />
				</div>
			))}
		</div>
	);
}
