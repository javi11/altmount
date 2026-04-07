import { useRegisterSW } from "virtual:pwa-register/react";

export function PWAUpdatePrompt() {
	const {
		needRefresh: [needRefresh],
		updateServiceWorker,
	} = useRegisterSW();

	if (!needRefresh) return null;

	return (
		<div className="toast toast-bottom toast-center z-50">
			<div className="alert alert-info">
				<span>A new version is available.</span>
				<button
					type="button"
					className="btn btn-sm btn-primary"
					onClick={() => updateServiceWorker(true)}
				>
					Reload
				</button>
			</div>
		</div>
	);
}
