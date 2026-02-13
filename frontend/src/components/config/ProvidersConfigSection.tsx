import {
	CheckCircle2,
	Edit,
	ExternalLink,
	Gauge,
	GripVertical,
	Lock,
	Plus,
	Power,
	PowerOff,
	ShieldCheck,
	Trash2,
	Unlock,
	User,
	Wifi,
	XCircle,
} from "lucide-react";
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
	const [togglingProviderId, setTogglingProviderId] = useState<string | null>(null);
	const [deletingProviderId, setDeletingProviderId] = useState<string | null>(null);
	const [testingSpeedProviderId, setTestingSpeedProviderId] = useState<string | null>(null);

	const { deleteProvider, updateProvider, reorderProviders, testProviderSpeed } = useProviders();
	const isReordering = reorderProviders.isPending;
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

	const handleSpeedTest = async (provider: ProviderConfig) => {
		setTestingSpeedProviderId(provider.id);
		showToast({
			type: "info",
			title: "Speed Test Started",
			message: `Testing speed for ${provider.host}... This may take a few seconds.`,
			duration: 5000,
		});

		try {
			await testProviderSpeed.mutateAsync(provider.id);
			showToast({
				type: "success",
				title: "Speed Test Completed",
				message: `Speed test for ${provider.host} completed. Results are updated on the card.`,
				duration: 5000,
			});
		} catch (error) {
			console.error("Failed to test speed:", error);
			showToast({
				type: "error",
				title: "Speed Test Failed",
				message: error instanceof Error ? error.message : "Failed to test speed",
			});
		} finally {
			setTestingSpeedProviderId(null);
		}
	};

	const handleDelete = async (providerId: string) => {
		const confirmed = await confirmDelete("provider");
		if (confirmed) {
			setDeletingProviderId(providerId);
			try {
				await deleteProvider.mutateAsync(providerId);
			} catch (error) {
				console.error("Failed to delete provider:", error);
				showToast({
					type: "error",
					title: "Delete Failed",
					message: "Failed to delete provider. Please try again.",
				});
			} finally {
				setDeletingProviderId(null);
			}
		}
	};

	const handleToggleEnabled = async (provider: ProviderConfig) => {
		setTogglingProviderId(provider.id);
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
		} finally {
			setTogglingProviderId(null);
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
			<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
				<div>
					<h3 className="font-semibold text-lg">NNTP Providers</h3>
					<p className="text-base-content/70 text-xs sm:text-sm">
						Manage Usenet provider connections
					</p>
					{config.providers.length > 1 && (
						<p className="mt-1 text-[10px] text-base-content/50 sm:text-xs">
							<GripVertical className="mr-1 inline h-3 w-3" />
							Drag to reorder
						</p>
					)}
				</div>
				<button
					type="button"
					className="btn btn-primary btn-sm w-full sm:w-auto"
					onClick={handleCreate}
				>
					<Plus className="h-4 w-4" />
					Add Provider
				</button>
			</div>

			{/* Providers List */}
			{config.providers.length === 0 ? (
				<div className="rounded-lg bg-base-200 py-12 text-center">
					<Wifi className="mx-auto mb-4 h-12 w-12 text-base-content/30" />
					<h4 className="mb-2 font-medium text-lg">No Providers Configured</h4>
					<button
						type="button"
						className="btn btn-primary mx-auto w-full max-w-xs sm:w-auto"
						onClick={handleCreate}
					>
						<Plus className="h-4 w-4" />
						Add First Provider
					</button>
				</div>
			) : (
				<div className="relative">
					{/* Loading overlay */}
					{isReordering && (
						<div className="absolute inset-0 z-10 flex items-center justify-center rounded-lg bg-base-300/50 backdrop-blur-sm">
							<div className="flex flex-col items-center gap-2 p-4 text-center">
								<div className="loading loading-spinner loading-lg text-primary" />
								<p className="font-medium text-sm">Reordering providers...</p>
							</div>
						</div>
					)}

					<div className="grid gap-4">
						{config.providers.map((provider, index) => (
							// biome-ignore lint/a11y/useSemanticElements: <a button can not be a child of a button>
							<div
								key={provider.id}
								draggable={!isReordering}
								role="button"
								tabIndex={0}
								aria-label={`Reorder provider ${provider.host}:${provider.port}@${provider.username}`}
								onDragStart={(e) => !isReordering && handleDragStart(e, provider.id)}
								onDragOver={(e) => !isReordering && handleDragOver(e, provider.id)}
								onDragLeave={!isReordering ? handleDragLeave : undefined}
								onDrop={(e) => !isReordering && handleDrop(e, provider.id)}
								onDragEnd={!isReordering ? handleDragEnd : undefined}
								className={`card border-2 bg-base-100 transition-all duration-200 ${
									isReordering ? "cursor-not-allowed opacity-60" : "cursor-move"
								} ${
									provider.enabled
										? provider.is_backup_provider
											? "border-warning/20"
											: "border-success/20"
										: "border-base-300"
								} ${draggedProvider === provider.id ? "scale-95 opacity-50" : ""} ${
									dragOverProvider === provider.id
										? "border-primary border-dashed bg-primary/5"
										: ""
								}`}
							>
								<div className="card-body p-3 sm:p-4">
									<div className="flex flex-col gap-4">
										{/* Provider Info Header */}
										<div className="flex items-start justify-between gap-2">
											<div className="flex min-w-0 items-center space-x-2">
												<GripVertical className="h-4 w-4 shrink-0 text-base-content/40" />
												<div className="min-w-0">
													<div className="mb-1 flex items-center gap-2">
														<div
															className={`h-2.5 w-2.5 shrink-0 rounded-full ${
																provider.enabled ? "bg-success" : "bg-base-300"
															}`}
														/>
														<span className="font-mono text-base-content/50 text-xs">
															#{index + 1}
														</span>
														{provider.is_backup_provider && (
															<span className="badge badge-warning badge-outline badge-xs px-1 font-bold text-[8px] uppercase">
																Backup
															</span>
														)}
													</div>
													<h4 className="flex items-center gap-1.5 break-all font-bold text-sm leading-tight sm:text-base">
														{provider.host}
														<span className="hidden font-normal text-[10px] text-base-content/40 sm:inline">
															({provider.port})
														</span>
													</h4>
													<div className="mt-1 flex flex-col gap-1">
														<div className="flex w-fit items-center gap-1.5 rounded-md border border-primary/10 bg-primary/5 px-1.5 py-0.5 font-mono text-[10px] text-primary sm:text-xs">
															<ExternalLink className="h-3 w-3" />
															{provider.tls ? "nntps" : "nntp"}://{provider.host}:{provider.port}
														</div>
														<div className="flex items-center gap-1.5 font-mono text-[10px] text-base-content/60 sm:text-xs">
															<User className="h-3 w-3" />
															{provider.username}
														</div>
													</div>
												</div>
											</div>

											<div className="flex shrink-0 flex-col items-end gap-2">
												<div className="flex flex-wrap justify-end gap-1">
													<div
														className={`badge badge-xs gap-1 font-bold ${provider.enabled ? "badge-success" : "badge-neutral"}`}
													>
														{provider.enabled ? (
															<>
																<CheckCircle2 className="h-2.5 w-2.5" />
																OK
															</>
														) : (
															"DISABLED"
														)}
													</div>
												</div>
												<div className="flex flex-wrap justify-end gap-1">
													<button
														type="button"
														className={`btn btn-xs sm:btn-sm ${
															provider.enabled ? "btn-warning" : "btn-success"
														}`}
														onClick={(e) => {
															e.stopPropagation();
															handleToggleEnabled(provider);
														}}
														disabled={togglingProviderId === provider.id}
													>
														{togglingProviderId === provider.id ? (
															<span className="loading loading-spinner loading-xs" />
														) : provider.enabled ? (
															<PowerOff className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
														) : (
															<Power className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
														)}
													</button>
													<button
														type="button"
														className="btn btn-xs sm:btn-sm btn-info"
														onClick={(e) => {
															e.stopPropagation();
															handleSpeedTest(provider);
														}}
														disabled={testingSpeedProviderId === provider.id || !provider.enabled}
													>
														{testingSpeedProviderId === provider.id ? (
															<span className="loading loading-spinner loading-xs" />
														) : (
															<Gauge className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
														)}
													</button>
													<button
														type="button"
														className="btn btn-xs sm:btn-sm btn-outline"
														onClick={(e) => {
															e.stopPropagation();
															handleEdit(provider);
														}}
													>
														<Edit className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
													</button>
													<button
														type="button"
														className="btn btn-xs sm:btn-sm btn-error"
														onClick={(e) => {
															e.stopPropagation();
															handleDelete(provider.id);
														}}
														disabled={deletingProviderId === provider.id}
													>
														{deletingProviderId === provider.id ? (
															<span className="loading loading-spinner loading-xs" />
														) : (
															<Trash2 className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
														)}
													</button>
												</div>
											</div>
										</div>

										{/* Details Grid */}
										<div className="grid grid-cols-2 xs:grid-cols-3 gap-3 border-base-200 border-t pt-3 text-[10px] sm:text-xs md:grid-cols-6">
											<div>
												<span className="mb-1 block text-base-content/50 uppercase">Max Conn</span>
												<div className="font-bold font-mono">{provider.max_connections}</div>
											</div>
											<div>
												<span className="mb-1 block text-base-content/50 uppercase">Pipeline</span>
												<div className="font-bold font-mono">{provider.inflight_requests || 3}</div>
											</div>
											<div>
												<span className="mb-1 block text-base-content/50 uppercase">Security</span>
												<div
													className={`flex items-center gap-1 font-bold ${provider.tls ? "text-success" : "text-warning"}`}
												>
													{provider.tls ? (
														<ShieldCheck className="h-3.5 w-3.5" />
													) : (
														<Unlock className="h-3.5 w-3.5" />
													)}
													{provider.tls ? "TLS" : "PLAIN"}
												</div>
											</div>
											<div>
												<span className="mb-1 block text-base-content/50 uppercase">Auth</span>
												<div
													className={`flex items-center gap-1 font-bold ${provider.password_set ? "text-success" : "text-error"}`}
												>
													{provider.password_set ? (
														<Lock className="h-3.5 w-3.5" />
													) : (
														<XCircle className="h-3.5 w-3.5" />
													)}
													{provider.password_set ? "OK" : "MISSING"}
												</div>
											</div>
											<div>
												<span className="mb-1 block text-base-content/50 uppercase">Top Speed</span>
												<div className="truncate font-bold font-mono text-success">
													{provider.last_speed_test_mbps?.toFixed(1) || "0"} MB/s
												</div>
											</div>
											<div>
												<span className="mb-1 block text-base-content/50 uppercase">Port</span>
												<div className="font-bold font-mono text-base-content/70">
													{provider.port}
												</div>
											</div>
										</div>
									</div>
								</div>
							</div>
						))}
					</div>
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
