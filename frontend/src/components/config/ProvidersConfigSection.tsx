import {
	AlertTriangle,
	Edit,
	Gauge,
	GripVertical,
	Plus,
	Power,
	PowerOff,
	Save,
	Trash2,
	Wifi,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useProviders } from "../../hooks/useProviders";
import type { ConfigResponse, ProviderConfig } from "../../types/config";
import { LoadingSpinner } from "../ui/LoadingSpinner";
import { ProviderModal } from "./ProviderModal";

interface ProvidersConfigSectionProps {
	config: ConfigResponse;
	onUpdate?: (section: string, data: ProviderConfig[]) => Promise<void>;
	isUpdating?: boolean;
}

export function ProvidersConfigSection({
	config,
	onUpdate,
	isUpdating = false,
}: ProvidersConfigSectionProps) {
	const [isModalOpen, setIsModalOpen] = useState(false);
	const [editingProvider, setEditingProvider] = useState<ProviderConfig | null>(null);
	const [modalMode, setModalMode] = useState<"create" | "edit">("create");
	const [draggedProvider, setDraggedProvider] = useState<string | null>(null);
	const [dragOverProvider, setDragOverProvider] = useState<string | null>(null);
	const [deletingProviderId, setDeletingProviderId] = useState<string | null>(null);
	const [testingSpeedProviderId, setTestingSpeedProviderId] = useState<string | null>(null);

	const [formData, setFormData] = useState<ProviderConfig[]>(config.providers);
	const [hasChanges, setHasChanges] = useState(false);

	const { deleteProvider, reorderProviders, testProviderSpeed } = useProviders();
	const isReordering = reorderProviders.isPending;
	const { confirmDelete } = useConfirm();
	const { showToast } = useToast();

	// Sync with config when it changes
	useEffect(() => {
		setFormData(config.providers);
		setHasChanges(false);
	}, [config.providers]);

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

	const handleToggleEnabled = (provider: ProviderConfig) => {
		handleFieldChange(provider.id, "enabled", !provider.enabled);
	};

	const handleFieldChange = (
		providerId: string,
		field: keyof ProviderConfig,
		// biome-ignore lint/suspicious/noExplicitAny: accepts various field types
		value: any,
	) => {
		const newFormData = formData.map((p) => {
			if (p.id === providerId) {
				return { ...p, [field]: value };
			}
			return p;
		});
		setFormData(newFormData);
		setHasChanges(JSON.stringify(newFormData) !== JSON.stringify(config.providers));
	};

	const handleSave = async () => {
		if (onUpdate && hasChanges) {
			try {
				await onUpdate("providers", formData);
				setHasChanges(false);
				showToast({
					type: "success",
					title: "Configuration Saved",
					message: "NNTP providers updated successfully.",
				});
			} catch (error) {
				console.error("Failed to save providers:", error);
				showToast({
					type: "error",
					title: "Save Failed",
					message: "Failed to save NNTP providers. Please try again.",
				});
			}
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
		if (!draggedProviderId || draggedProviderId === targetProviderId) return;
		const draggedIndex = config.providers.findIndex((p) => p.id === draggedProviderId);
		const targetIndex = config.providers.findIndex((p) => p.id === targetProviderId);
		if (draggedIndex === -1 || targetIndex === -1) return;
		const reorderedProviders = [...formData];
		const [draggedProviderObj] = reorderedProviders.splice(draggedIndex, 1);
		reorderedProviders.splice(targetIndex, 0, draggedProviderObj);

		// Update local state immediately for visual responsiveness
		setFormData(reorderedProviders);
		setHasChanges(JSON.stringify(reorderedProviders) !== JSON.stringify(config.providers));

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
		<div className="space-y-8">
			{/* Header */}
			<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<h3 className="font-bold text-base-content text-xl tracking-tight">NNTP Providers</h3>
					<p className="mt-1 text-base-content/50 text-xs">Drag cards to adjust priority order.</p>
				</div>
				<button
					type="button"
					className="btn btn-primary btn-sm px-6 shadow-lg shadow-primary/20"
					onClick={handleCreate}
				>
					<Plus className="h-4 w-4" />
					Add Provider
				</button>
			</div>

			{/* Providers List */}
			{formData.length === 0 ? (
				<div className="rounded-2xl border-2 border-base-300 border-dashed bg-base-200/30 py-16 text-center">
					<Wifi className="mx-auto mb-4 h-12 w-12 text-base-content/20" />
					<h4 className="font-bold text-base-content/80 text-lg">No Providers Configured</h4>
					<p className="mb-6 text-base-content/60 text-sm">
						Add a Usenet provider to enable downloading.
					</p>
					<button type="button" className="btn btn-primary px-8" onClick={handleCreate}>
						Add First Provider
					</button>
				</div>
			) : (
				<div className="relative">
					{isReordering && (
						<div className="absolute inset-0 z-10 flex items-center justify-center rounded-2xl bg-base-300/40 backdrop-blur-[2px]">
							<div className="flex flex-col items-center gap-3">
								<LoadingSpinner size="lg" />
								<p className="font-black text-primary text-xs uppercase tracking-widest">
									Reordering...
								</p>
							</div>
						</div>
					)}

					<div className="grid gap-4">
						{formData.map((provider, index) => (
							<section
								key={provider.id}
								draggable={!isReordering}
								aria-label={`Provider ${provider.host}`}
								onDragStart={(e) => !isReordering && handleDragStart(e, provider.id)}
								onDragOver={(e) => !isReordering && handleDragOver(e, provider.id)}
								onDragLeave={!isReordering ? handleDragLeave : undefined}
								onDrop={(e) => !isReordering && handleDrop(e, provider.id)}
								onDragEnd={!isReordering ? handleDragEnd : undefined}
								className={`group relative overflow-hidden rounded-2xl border-2 bg-base-100/50 transition-all duration-300 hover:shadow-md ${
									isReordering ? "cursor-not-allowed text-base-content/80" : "cursor-move"
								} ${
									provider.enabled
										? provider.is_backup_provider
											? "border-warning/20"
											: "border-success/20"
										: "border-base-300"
								} ${draggedProvider === provider.id ? "scale-95 text-base-content/70 ring-2 ring-primary" : ""} ${
									dragOverProvider === provider.id
										? "translate-y-1 border-primary border-dashed bg-primary/5"
										: ""
								}`}
							>
								{/* Priority Indicator Line */}
								<div
									className={`absolute top-0 bottom-0 left-0 w-1.5 ${provider.enabled ? (provider.is_backup_provider ? "bg-warning" : "bg-success") : "bg-base-300"}`}
								/>

								<div className="p-5 pl-7">
									<div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
										<div className="flex min-w-0 items-center gap-4">
											<div className="rounded-lg bg-base-200/50 p-2 text-base-content/60 transition-opacity group-hover:opacity-100">
												<GripVertical className="h-4 w-4" />
											</div>
											<div className="min-w-0 flex-1">
												<div className="flex items-center gap-2">
													<span className="font-black font-mono text-base-content/50 text-xs">
														#{index + 1}
													</span>
													<h4 className="break-all font-bold text-base text-base-content tracking-tight">
														{provider.host}
													</h4>
												</div>
												<div className="mt-1 flex items-center gap-2">
													<div
														className={`h-2 w-2 rounded-full ${provider.enabled ? "bg-success shadow-[0_0_8px_rgba(34,197,94,0.5)]" : "bg-base-300"}`}
													/>
													<span className="truncate font-bold text-base-content/70 text-xs uppercase tracking-wider">
														{provider.port} • {provider.username}
													</span>
												</div>
											</div>
										</div>

										<div className="flex flex-wrap items-center gap-2">
											{provider.is_backup_provider && (
												<div className="badge badge-warning badge-sm px-3 py-2 font-black text-xs uppercase tracking-widest shadow-sm">
													Backup
												</div>
											)}

											<div className="join rounded-xl bg-base-200/50 p-0.5">
												<button
													type="button"
													className={`btn btn-sm sm:btn-sm join-item border-none ${
														provider.enabled
															? "bg-warning/10 text-warning hover:bg-warning/20"
															: "bg-success/10 text-success hover:bg-success/20"
													}`}
													onClick={() => handleToggleEnabled(provider)}
												>
													{provider.enabled ? (
														<PowerOff className="h-3.5 w-3.5" />
													) : (
														<Power className="h-3.5 w-3.5" />
													)}
												</button>
												<button
													type="button"
													className="btn btn-sm sm:btn-sm join-item border-none bg-info/10 text-info hover:bg-info/20"
													onClick={() => handleSpeedTest(provider)}
													disabled={testingSpeedProviderId === provider.id || !provider.enabled}
												>
													{testingSpeedProviderId === provider.id ? (
														<span className="loading loading-spinner loading-xs" />
													) : (
														<Gauge className="h-3.5 w-3.5" />
													)}
												</button>
												<button
													type="button"
													className="btn btn-sm sm:btn-sm join-item border-none bg-base-content/5 text-base-content hover:bg-base-content/10"
													onClick={() => handleEdit(provider)}
												>
													<Edit className="h-3.5 w-3.5" />
												</button>
												<button
													type="button"
													className="btn btn-sm sm:btn-sm join-item border-none bg-error/10 text-error hover:bg-error/20"
													onClick={() => handleDelete(provider.id)}
													disabled={deletingProviderId === provider.id}
												>
													{deletingProviderId === provider.id ? (
														<span className="loading loading-spinner loading-xs" />
													) : (
														<Trash2 className="h-3.5 w-3.5" />
													)}
												</button>
											</div>
										</div>
									</div>

									{/* Quick Details Grid */}
									<div className="mt-5 grid grid-cols-2 gap-x-6 gap-y-4 rounded-xl bg-base-200/30 p-4 text-[11px] md:grid-cols-5">
										<div className="min-w-0">
											<span className="mb-1 block font-black text-base-content/50 text-xs uppercase tracking-widest">
												Max Conn
											</span>
											<div className="flex items-center gap-2">
												<input
													type="number"
													className="input input-xs input-bordered w-full max-w-[50px] bg-base-100 font-bold font-mono"
													value={provider.max_connections}
													onChange={(e) =>
														handleFieldChange(
															provider.id,
															"max_connections",
															Number.parseInt(e.target.value, 10) || 1,
														)
													}
													min={1}
													max={100}
												/>
											</div>
										</div>
										<div className="min-w-0">
											<span className="mb-1 block font-black text-base-content/50 text-xs uppercase tracking-widest">
												Pipeline
											</span>
											<div className="flex items-center gap-2">
												<input
													type="number"
													className="input input-xs input-bordered w-full max-w-[50px] bg-base-100 font-bold font-mono"
													value={provider.inflight_requests || 10}
													onChange={(e) =>
														handleFieldChange(
															provider.id,
															"inflight_requests",
															Number.parseInt(e.target.value, 10) || 1,
														)
													}
													min={1}
													max={100}
												/>
											</div>
										</div>
										<div className="min-w-0">
											<span className="mb-1 block font-black text-base-content/50 text-xs uppercase tracking-widest">
												Role
											</span>
											<div
												className={`badge badge-xs h-6 p-1.5 font-black ${provider.is_backup_provider ? "badge-warning/20 text-warning" : "badge-success/20 text-success"}`}
											>
												{provider.is_backup_provider ? "BACKUP" : "PRIMARY"}
											</div>
										</div>
										<div className="min-w-0">
											<span className="mb-1 block font-black text-base-content/50 text-xs uppercase tracking-widest">
												Latency
											</span>
											<div className="flex h-6 items-center font-bold font-mono">
												{provider.last_rtt_ms !== undefined ? `${provider.last_rtt_ms}ms` : "---"}
											</div>
										</div>
										<div className="col-span-2 min-w-0 md:col-span-1">
											<span className="mb-1 block font-black text-base-content/50 text-xs uppercase tracking-widest">
												Last Speed
											</span>
											<div className="flex h-6 items-center truncate font-bold font-mono">
												{provider.last_speed_test_mbps !== undefined
													? `${provider.last_speed_test_mbps.toFixed(1)} MB/s`
													: "---"}
											</div>
										</div>
									</div>
								</div>
							</section>
						))}
					</div>
				</div>
			)}

			{/* Save & Validation */}
			<div className="space-y-4 border-base-200 border-t pt-6">
				{hasChanges && (
					<div className="fade-in slide-in-from-bottom-2 flex animate-in items-center justify-end gap-4">
						<div className="flex items-center gap-2 font-bold text-warning text-xs">
							<AlertTriangle className="h-4 w-4" /> Unsaved Changes
						</div>
						<button
							type="button"
							className="btn btn-primary px-10 shadow-lg shadow-primary/20"
							onClick={handleSave}
							disabled={isUpdating}
						>
							{isUpdating ? <LoadingSpinner size="sm" /> : <Save className="h-4 w-4" />}
							{isUpdating ? "Saving..." : "Save Changes"}
						</button>
					</div>
				)}
			</div>

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
