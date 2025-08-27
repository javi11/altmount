import { Edit, GripVertical, Plus, Power, PowerOff, Trash2, Wifi, WifiOff } from "lucide-react";
import { useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useProviders } from "../../hooks/useProviders";
import type { ConfigResponse, ProviderConfig } from "../../types/config";
import { ProviderModal } from "./ProviderModal";

interface ProvidersConfigSectionProps {
	config: ConfigResponse;
}

export function ProvidersConfigSection({ config }: ProvidersConfigSectionProps) {
	const [isModalOpen, setIsModalOpen] = useState(false);
	const [editingProvider, setEditingProvider] = useState<ProviderConfig | null>(null);
	const [modalMode, setModalMode] = useState<"create" | "edit">("create");
	const [draggedProvider, setDraggedProvider] = useState<string | null>(null);
	const [dragOverProvider, setDragOverProvider] = useState<string | null>(null);

	const { deleteProvider, updateProvider, reorderProviders } = useProviders();
	const { confirmDelete } = useConfirm();
	const { showToast } = useToast();

	const handleCreate = () => {
		setEditingProvider(null);
		setModalMode("create");
		setIsModalOpen(true);
	};

	const handleEdit = (provider: ProviderConfig) => {
		setEditingProvider(provider);
		setModalMode("edit");
		setIsModalOpen(true);
	};

	const handleDelete = async (providerId: string) => {
		const confirmed = await confirmDelete("provider");
		if (confirmed) {
			try {
				await deleteProvider.mutateAsync(providerId);
			} catch (error) {
				console.error("Failed to delete provider:", error);
				showToast({
					type: "error",
					title: "Delete Failed",
					message: "Failed to delete provider. Please try again.",
				});
			}
		}
	};

	const handleToggleEnabled = async (provider: ProviderConfig) => {
		try {
			await updateProvider.mutateAsync({
				id: provider.id,
				data: { enabled: !provider.enabled },
			});
		} catch (error) {
			console.error("Failed to toggle provider:", error);
			showToast({
				type: "error",
				title: "Update Failed",
				message: "Failed to update provider. Please try again.",
			});
		}
	};

	const handleModalSuccess = () => {
		setIsModalOpen(false);
		setEditingProvider(null);
	};

	const handleDragStart = (e: React.DragEvent, providerId: string) => {
		setDraggedProvider(providerId);
		e.dataTransfer.effectAllowed = "move";
		e.dataTransfer.setData("text/plain", providerId);
	};

	const handleDragOver = (e: React.DragEvent, providerId: string) => {
		e.preventDefault();
		e.dataTransfer.dropEffect = "move";
		setDragOverProvider(providerId);
	};

	const handleDragLeave = (e: React.DragEvent) => {
		// Only clear drag over if leaving the provider card
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		const x = e.clientX;
		const y = e.clientY;

		if (x < rect.left || x > rect.right || y < rect.top || y > rect.bottom) {
			setDragOverProvider(null);
		}
	};

	const handleDrop = async (e: React.DragEvent, targetProviderId: string) => {
		e.preventDefault();
		const draggedProviderId = e.dataTransfer.getData("text/plain");

		setDraggedProvider(null);
		setDragOverProvider(null);

		if (!draggedProviderId || draggedProviderId === targetProviderId) {
			return;
		}

		// Find current positions
		const draggedIndex = config.providers.findIndex((p) => p.id === draggedProviderId);
		const targetIndex = config.providers.findIndex((p) => p.id === targetProviderId);

		if (draggedIndex === -1 || targetIndex === -1) {
			return;
		}

		// Create new order
		const reorderedProviders = [...config.providers];
		const [draggedProvider] = reorderedProviders.splice(draggedIndex, 1);
		reorderedProviders.splice(targetIndex, 0, draggedProvider);

		// Send reorder request
		try {
			await reorderProviders.mutateAsync({
				provider_ids: reorderedProviders.map((p) => p.id),
			});
		} catch (error) {
			console.error("Failed to reorder providers:", error);
			showToast({
				type: "error",
				title: "Reorder Failed",
				message: "Failed to reorder providers. Please try again.",
			});
		}
	};

	const handleDragEnd = () => {
		setDraggedProvider(null);
		setDragOverProvider(null);
	};

	return (
		<div className="space-y-6">
			{/* Header */}
			<div className="flex items-center justify-between">
				<div>
					<h3 className="font-semibold text-lg">NNTP Providers</h3>
					<p className="text-base-content/70 text-sm">
						Manage Usenet provider connections for downloading
					</p>
					{config.providers.length > 1 && (
						<p className="mt-1 text-base-content/50 text-xs">
							<GripVertical className="mr-1 inline h-3 w-3" />
							Drag to reorder • Higher priority providers are used first
						</p>
					)}
				</div>
				<button type="button" className="btn btn-primary btn-sm" onClick={handleCreate}>
					<Plus className="h-4 w-4" />
					Add Provider
				</button>
			</div>

			{/* Providers List */}
			{config.providers.length === 0 ? (
				<div className="rounded-lg bg-base-200 py-12 text-center">
					<Wifi className="mx-auto mb-4 h-12 w-12 text-base-content/30" />
					<h4 className="mb-2 font-medium text-lg">No Providers Configured</h4>
					<p className="mb-4 text-base-content/70">
						Add NNTP providers to enable downloading from Usenet
					</p>
					<button type="button" className="btn btn-primary" onClick={handleCreate}>
						<Plus className="h-4 w-4" />
						Add First Provider
					</button>
				</div>
			) : (
				<div className="grid gap-4">
					{config.providers.map((provider, index) => (
						// biome-ignore lint/a11y/useSemanticElements: <a button can not be a child of a button>
						<div
							key={provider.id}
							draggable
							role="button"
							tabIndex={0}
							aria-label={`Reorder provider ${provider.host}:${provider.port}@${provider.username}`}
							onDragStart={(e) => handleDragStart(e, provider.id)}
							onDragOver={(e) => handleDragOver(e, provider.id)}
							onDragLeave={handleDragLeave}
							onDrop={(e) => handleDrop(e, provider.id)}
							onDragEnd={handleDragEnd}
							className={`card cursor-move border-2 bg-base-100 transition-all duration-200 ${
								provider.enabled
									? provider.is_backup_provider
										? "border-warning/20"
										: "border-success/20"
									: "border-base-300"
							} ${draggedProvider === provider.id ? "scale-95 opacity-50" : ""} ${
								dragOverProvider === provider.id ? "border-primary border-dashed bg-primary/5" : ""
							}`}
						>
							<div className="card-body p-4">
								<div className="flex items-center justify-between">
									<div className="flex items-center space-x-3">
										<div className="flex items-center space-x-2">
											<GripVertical className="h-4 w-4 text-base-content/40" />
											<div
												className={`h-3 w-3 rounded-full ${
													provider.enabled ? "bg-success" : "bg-base-300"
												}`}
											/>
											<div className="text-base-content/50 text-xs">#{index + 1}</div>
										</div>
										<div>
											<h4 className="font-semibold">
												{provider.host}:{provider.port}@{provider.username}
											</h4>
										</div>
									</div>

									<div className="flex items-center space-x-2">
										{/* Status Badge */}
										<div
											className={`badge ${provider.enabled ? "badge-success" : "badge-neutral"}`}
										>
											{provider.enabled ? "Enabled" : "Disabled"}
										</div>

										{/* Backup Provider Badge */}
										{provider.is_backup_provider && (
											<div className="badge badge-warning badge-sm">Backup</div>
										)}

										{/* Actions */}
										<div className="join">
											<button
												type="button"
												className={`btn btn-sm join-item ${
													provider.enabled ? "btn-warning" : "btn-success"
												}`}
												onClick={() => handleToggleEnabled(provider)}
												title={provider.enabled ? "Disable" : "Enable"}
											>
												{provider.enabled ? (
													<PowerOff className="h-4 w-4" />
												) : (
													<Power className="h-4 w-4" />
												)}
											</button>
											<button
												type="button"
												className="btn btn-sm btn-outline join-item"
												onClick={() => handleEdit(provider)}
												title="Edit"
											>
												<Edit className="h-4 w-4" />
											</button>
											<button
												type="button"
												className="btn btn-sm btn-error join-item"
												onClick={() => handleDelete(provider.id)}
												title="Delete"
											>
												<Trash2 className="h-4 w-4" />
											</button>
										</div>
									</div>
								</div>

								{/* Provider Details */}
								<div className="mt-3 border-base-300 border-t pt-3">
									<div className="grid grid-cols-2 gap-4 text-sm md:grid-cols-5">
										<div>
											<span className="text-base-content/60">Username:</span>
											<div className="font-mono">{provider.username}</div>
										</div>
										<div>
											<span className="text-base-content/60">Max Connections:</span>
											<div className="font-mono">{provider.max_connections}</div>
										</div>
										<div>
											<span className="text-base-content/60">TLS:</span>
											<div className="flex items-center space-x-1">
												{provider.tls ? (
													<>
														<Wifi className="h-4 w-4 text-success" />
														<span>Enabled</span>
													</>
												) : (
													<>
														<WifiOff className="h-4 w-4 text-warning" />
														<span>Disabled</span>
													</>
												)}
											</div>
										</div>
										<div>
											<span className="text-base-content/60">Password:</span>
											<div className="flex items-center space-x-1">
												{provider.password_set ? (
													<span className="badge badge-success badge-sm">Set</span>
												) : (
													<span className="badge badge-error badge-sm">Not Set</span>
												)}
											</div>
										</div>
										<div>
											<span className="text-base-content/60">Type:</span>
											<div className="flex items-center space-x-1">
												{provider.is_backup_provider ? (
													<span className="badge badge-warning badge-sm">Backup</span>
												) : (
													<span className="badge badge-primary badge-sm">Primary</span>
												)}
											</div>
										</div>
									</div>
								</div>
							</div>
						</div>
					))}
				</div>
			)}

			{/* Provider Modal */}
			{isModalOpen && (
				<ProviderModal
					mode={modalMode}
					provider={editingProvider}
					onSuccess={handleModalSuccess}
					onCancel={() => setIsModalOpen(false)}
				/>
			)}
		</div>
	);
}
