import {
	AlertTriangle,
	Edit,
	Gauge,
	GripVertical,
	Plus,
	Power,
	PowerOff,
	RotateCcw,
	Save,
	Trash2,
	Wifi,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useConfirm } from "../../contexts/ModalContext";
import { useToast } from "../../contexts/ToastContext";
import { useProviders } from "../../hooks/useProviders";
import { formatExpirationDate } from "../../lib/utils";
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
	const [createBackup, setCreateBackup] = useState(false);
	const [draggedProvider, setDraggedProvider] = useState<string | null>(null);
	const [dragOverProvider, setDragOverProvider] = useState<string | null>(null);
	const [deletingProviderId, setDeletingProviderId] = useState<string | null>(null);

	// Touch drag state (mobile — HTML5 drag events don't fire on touch)
	const touchDragRef = useRef<{ providerId: string } | null>(null);
	const listRef = useRef<HTMLDivElement>(null);
	const [testingSpeedProviderId, setTestingSpeedProviderId] = useState<string | null>(null);

	const [formData, setFormData] = useState<ProviderConfig[]>(config.providers ?? []);
	const [hasChanges, setHasChanges] = useState(false);
	// Single user-agent shared by every provider (stored per-provider, edited globally here).
	const [globalUserAgent, setGlobalUserAgent] = useState(
		(config.providers ?? []).map((p) => p.user_agent).find(Boolean) ?? "",
	);

	const { deleteProvider, testProviderSpeed, tuneProviderPipeline, resetProviderQuota } =
		useProviders();
	const [resettingQuotaId, setResettingQuotaId] = useState<string | null>(null);
	const [pipelineTuning, setPipelineTuning] = useState<{
		current: number;
		total: number;
		host?: string;
	} | null>(null);
	const [disablingPipeline, setDisablingPipeline] = useState(false);
	const { confirmDelete } = useConfirm();
	const { showToast } = useToast();

	// Sync with config when it changes
	useEffect(() => {
		setFormData(config.providers ?? []);
		setGlobalUserAgent((config.providers ?? []).map((p) => p.user_agent).find(Boolean) ?? "");
		setHasChanges(false);
	}, [config.providers]);

	// Attach non-passive touchmove on the list container so we can call
	// preventDefault() and prevent page scroll while a touch-drag is active.
	useEffect(() => {
		const el = listRef.current;
		if (!el) return;
		const prevent = (e: TouchEvent) => {
			if (touchDragRef.current) e.preventDefault();
		};
		el.addEventListener("touchmove", prevent, { passive: false });
		return () => el.removeEventListener("touchmove", prevent);
	}, []);

	const handleTouchStart = (_e: React.TouchEvent, providerId: string) => {
		touchDragRef.current = { providerId };
		setDraggedProvider(providerId);
	};

	const handleTouchMove = (e: React.TouchEvent) => {
		if (!touchDragRef.current) return;
		const touch = e.touches[0];
		const el = document.elementFromPoint(touch.clientX, touch.clientY);
		const section = el?.closest("[data-provider-id]");
		const targetId = section?.getAttribute("data-provider-id") ?? null;
		setDragOverProvider(targetId && targetId !== touchDragRef.current.providerId ? targetId : null);
	};

	const handleTouchEnd = (e: React.TouchEvent) => {
		if (!touchDragRef.current) return;
		const { providerId } = touchDragRef.current;
		const touch = e.changedTouches[0];
		const el = document.elementFromPoint(touch.clientX, touch.clientY);
		const section = el?.closest("[data-provider-id]");
		const targetId = section?.getAttribute("data-provider-id");

		if (targetId && targetId !== providerId) {
			const draggedIndex = formData.findIndex((p) => p.id === providerId);
			const targetIndex = formData.findIndex((p) => p.id === targetId);
			if (draggedIndex !== -1 && targetIndex !== -1) {
				const reordered = [...formData];
				const [moved] = reordered.splice(draggedIndex, 1);
				reordered.splice(targetIndex, 0, moved);
				setFormData(reordered);
				setHasChanges(JSON.stringify(reordered) !== JSON.stringify(config.providers));
			}
		}

		touchDragRef.current = null;
		setDraggedProvider(null);
		setDragOverProvider(null);
	};

	const handleCreate = (backup: boolean) => {
		setEditingProvider(null);
		setModalMode("create");
		setCreateBackup(backup);
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

	const handleResetQuota = async (provider: ProviderConfig) => {
		if (!resetProviderQuota) return;
		setResettingQuotaId(provider.id);
		try {
			await resetProviderQuota.mutateAsync(provider.id);
			showToast({
				type: "success",
				title: "Quota Reset",
				message: `Download quota for ${provider.host} has been reset.`,
			});
		} catch {
			showToast({
				type: "error",
				title: "Reset Failed",
				message: "Failed to reset provider quota.",
			});
		} finally {
			setResettingQuotaId(null);
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

	const handleGlobalUserAgentChange = (value: string) => {
		setGlobalUserAgent(value);
		const newFormData = formData.map((p) => ({ ...p, user_agent: value }));
		setFormData(newFormData);
		setHasChanges(JSON.stringify(newFormData) !== JSON.stringify(config.providers));
	};

	const handleAutoTunePipeline = async () => {
		const targets = formData.filter((p) => p.enabled);
		if (targets.length === 0) {
			return;
		}

		setPipelineTuning({ current: 0, total: targets.length });
		const recommendations = new Map<string, number>();
		const skipped: string[] = [];

		for (let i = 0; i < targets.length; i++) {
			const provider = targets[i];
			setPipelineTuning({ current: i + 1, total: targets.length, host: provider.host });
			try {
				const result = await tuneProviderPipeline.mutateAsync(provider.id);
				if (result.warning) {
					skipped.push(`${provider.host}: ${result.warning}`);
				} else {
					recommendations.set(provider.id, result.recommended_inflight);
				}
			} catch (error) {
				console.error("Failed to tune pipeline:", error);
				skipped.push(`${provider.host}: test failed`);
			}
		}
		setPipelineTuning(null);

		if (recommendations.size === 0) {
			showToast({
				type: "warning",
				title: "Pipeline Unchanged",
				message: skipped.length ? skipped.join("; ") : "No providers could be tuned.",
			});
			return;
		}

		const newFormData = formData.map((p) =>
			recommendations.has(p.id)
				? { ...p, inflight_requests: recommendations.get(p.id) as number }
				: p,
		);
		setFormData(newFormData);

		if (!onUpdate) {
			setHasChanges(true);
			return;
		}
		try {
			await onUpdate("providers", newFormData);
			setHasChanges(false);
			showToast({
				type: "success",
				title: "Pipeline Tuned",
				message: `Updated ${recommendations.size} provider${recommendations.size === 1 ? "" : "s"}.${
					skipped.length ? ` Skipped: ${skipped.join("; ")}` : ""
				}`,
			});
		} catch (error) {
			console.error("Failed to save tuned pipeline:", error);
			showToast({
				type: "error",
				title: "Save Failed",
				message: "Tuned values could not be saved.",
			});
		}
	};

	const handleDisablePipelining = async () => {
		const newFormData = formData.map((p) => ({ ...p, inflight_requests: 1 }));
		setFormData(newFormData);
		if (!onUpdate) {
			setHasChanges(true);
			return;
		}
		setDisablingPipeline(true);
		try {
			await onUpdate("providers", newFormData);
			setHasChanges(false);
			showToast({
				type: "success",
				title: "Pipelining Disabled",
				message: "All providers set to pipeline depth 1.",
			});
		} catch (error) {
			console.error("Failed to disable pipelining:", error);
			showToast({ type: "error", title: "Save Failed", message: "Could not disable pipelining." });
		} finally {
			setDisablingPipeline(false);
		}
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

	const handleDrop = (e: React.DragEvent, targetProviderId: string) => {
		e.preventDefault();
		const draggedProviderId = e.dataTransfer.getData("text/plain");
		setDraggedProvider(null);
		setDragOverProvider(null);
		if (!draggedProviderId || draggedProviderId === targetProviderId) return;
		const draggedIndex = formData.findIndex((p) => p.id === draggedProviderId);
		const targetIndex = formData.findIndex((p) => p.id === targetProviderId);
		if (draggedIndex === -1 || targetIndex === -1) return;
		const reorderedProviders = [...formData];
		const [draggedProviderObj] = reorderedProviders.splice(draggedIndex, 1);
		reorderedProviders.splice(targetIndex, 0, draggedProviderObj);
		setFormData(reorderedProviders);
		setHasChanges(JSON.stringify(reorderedProviders) !== JSON.stringify(config.providers));
	};

	const handleDragEnd = () => {
		setDraggedProvider(null);
		setDragOverProvider(null);
	};

	const renderProviderCard = (provider: ProviderConfig) => {
		const index = formData.findIndex((p) => p.id === provider.id);
		const accentBar = provider.enabled
			? provider.is_backup_provider
				? "bg-warning"
				: "bg-success"
			: "bg-base-300";

		return (
			<section
				key={provider.id}
				data-provider-id={provider.id}
				draggable
				aria-label={`Provider ${provider.host}`}
				onDragStart={(e) => handleDragStart(e, provider.id)}
				onDragOver={(e) => handleDragOver(e, provider.id)}
				onDragLeave={handleDragLeave}
				onDrop={(e) => handleDrop(e, provider.id)}
				onDragEnd={handleDragEnd}
				onTouchStart={(e) => handleTouchStart(e, provider.id)}
				onTouchMove={handleTouchMove}
				onTouchEnd={handleTouchEnd}
				className={`group relative flex cursor-move flex-col rounded-2xl border-2 bg-base-100/50 transition-all duration-300 hover:shadow-md ${
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
				<div className={`absolute top-3 bottom-3 left-1.5 w-1 rounded-full ${accentBar}`} />

				<div className="flex flex-1 flex-col gap-3 p-4 pl-6">
					{/* Identity + actions */}
					<div className="flex items-center justify-between gap-2">
						<div className="flex min-w-0 items-center gap-2">
							<div className="rounded-lg bg-base-200/50 p-1.5 text-base-content/60">
								<GripVertical className="h-4 w-4" />
							</div>
							<div className="min-w-0">
								<div className="flex items-baseline gap-1.5">
									<span className="font-black font-mono text-base text-base-content/50">
										#{index + 1}
									</span>
									<h4 className="truncate font-bold text-base text-base-content tracking-tight">
										{provider.name || provider.host}
									</h4>
								</div>
								<div className="mt-1 flex items-center gap-1.5">
									<span className="font-bold text-[10px] text-base-content/70 uppercase tracking-wider">
										{provider.port}
									</span>
									<span className="text-base-content/30">•</span>
									{/* Blur clipped to a rectangle so it doesn't bleed; hover reveals. */}
									<span className="inline-flex max-w-[150px] items-center overflow-hidden rounded bg-base-300/40 align-middle">
										<span
											className="cursor-pointer truncate px-1 font-bold text-[10px] text-base-content/70 blur-[5px] transition-all hover:blur-none"
											title="Click to reveal username"
										>
											{provider.username || "anonymous"}
										</span>
									</span>
								</div>
							</div>
						</div>

						<div className="join shrink-0 rounded-xl bg-base-200/50 p-1">
							<div className="tooltip" data-tip="Enable/Disable provider">
								<button
									type="button"
									className={`btn btn-sm join-item border-none ${
										provider.enabled
											? "bg-warning/10 text-warning hover:bg-warning/20"
											: "bg-success/10 text-success hover:bg-success/20"
									}`}
									onClick={() => handleToggleEnabled(provider)}
								>
									{provider.enabled ? (
										<PowerOff className="h-4 w-4" />
									) : (
										<Power className="h-4 w-4" />
									)}
								</button>
							</div>
							<div className="tooltip" data-tip="Run speed test">
								<button
									type="button"
									className="btn btn-sm join-item border-none bg-info/10 text-info hover:bg-info/20"
									onClick={() => handleSpeedTest(provider)}
									disabled={testingSpeedProviderId === provider.id || !provider.enabled}
								>
									{testingSpeedProviderId === provider.id ? (
										<span className="loading loading-spinner loading-sm" />
									) : (
										<Gauge className="h-4 w-4" />
									)}
								</button>
							</div>
							{(provider.quota_bytes ?? 0) > 0 && (
								<div className="tooltip" data-tip="Reset download quota">
									<button
										type="button"
										className="btn btn-sm join-item border-none bg-warning/10 text-warning hover:bg-warning/20"
										onClick={() => handleResetQuota(provider)}
										disabled={resettingQuotaId === provider.id}
									>
										{resettingQuotaId === provider.id ? (
											<span className="loading loading-spinner loading-sm" />
										) : (
											<RotateCcw className="h-4 w-4" />
										)}
									</button>
								</div>
							)}
							<div className="tooltip" data-tip="Edit provider">
								<button
									type="button"
									className="btn btn-sm join-item border-none bg-base-content/5 text-base-content hover:bg-base-content/10"
									onClick={() => handleEdit(provider)}
								>
									<Edit className="h-4 w-4" />
								</button>
							</div>
							<div className="tooltip" data-tip="Remove provider">
								<button
									type="button"
									className="btn btn-sm join-item border-none bg-error/10 text-error hover:bg-error/20"
									onClick={() => handleDelete(provider.id)}
									disabled={deletingProviderId === provider.id}
								>
									{deletingProviderId === provider.id ? (
										<span className="loading loading-spinner loading-sm" />
									) : (
										<Trash2 className="h-4 w-4" />
									)}
								</button>
							</div>
						</div>
					</div>

					{/* Quick Details Grid */}
					<div className="mt-auto grid grid-cols-2 gap-x-4 gap-y-2.5 rounded-xl bg-base-200/30 p-3 text-xs">
						<div className="flex min-w-0 items-center gap-3">
							<span className="min-w-[88px] font-black text-[10px] text-base-content/50 uppercase tracking-widest">
								Max Conn
							</span>
							<input
								type="number"
								className="input input-xs input-bordered w-16 shrink-0 bg-base-100 text-center font-bold font-mono [appearance:textfield] [&::-webkit-inner-spin-button]:m-0 [&::-webkit-inner-spin-button]:[-webkit-appearance:none] [&::-webkit-outer-spin-button]:[-webkit-appearance:none]"
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
						<div className="flex min-w-0 items-center gap-3">
							<span className="min-w-[88px] font-black text-[10px] text-base-content/50 uppercase tracking-widest">
								Latency
							</span>
							<span className="font-bold font-mono text-xs">
								{provider.last_rtt_ms !== undefined ? `${provider.last_rtt_ms}ms` : "---"}
							</span>
						</div>
						<div className="flex min-w-0 items-center gap-3">
							<span className="min-w-[88px] font-black text-[10px] text-base-content/50 uppercase tracking-widest">
								Pipeline
							</span>
							<input
								type="number"
								className="input input-xs input-bordered w-16 shrink-0 bg-base-100 text-center font-bold font-mono [appearance:textfield] [&::-webkit-inner-spin-button]:m-0 [&::-webkit-inner-spin-button]:[-webkit-appearance:none] [&::-webkit-outer-spin-button]:[-webkit-appearance:none]"
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
						<div className="flex min-w-0 items-center gap-3">
							<span className="min-w-[88px] font-black text-[10px] text-base-content/50 uppercase tracking-widest">
								Last Speed
							</span>
							<span className="truncate font-bold font-mono text-xs">
								{provider.last_speed_test_mbps !== undefined
									? `${provider.last_speed_test_mbps.toFixed(1)} MB/s`
									: "---"}
							</span>
						</div>
						<div className="col-span-2 grid grid-cols-2 gap-x-4 border-base-300/40 border-t pt-2.5">
							<span className="font-black text-[10px] text-base-content/50 uppercase tracking-widest">
								Account Expiration Date
							</span>
							<div className="flex min-w-0 items-center gap-3">
								<span className="min-w-[88px] shrink-0" aria-hidden="true" />
								<span className="font-bold font-mono text-xs">
									{formatExpirationDate(provider.account_expiration_date) || "---"}
								</span>
							</div>
						</div>
					</div>
				</div>
			</section>
		);
	};

	const primaryProviders = formData.filter((p) => !p.is_backup_provider);
	const backupProviders = formData.filter((p) => p.is_backup_provider);

	const renderSection = (
		title: string,
		subtitle: string,
		providers: ProviderConfig[],
		accent: "success" | "warning",
		addLabel: string,
		onAdd: () => void,
		emptyHint: string,
	) => (
		<div className="space-y-4">
			<div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<div className="flex items-center gap-2.5">
					<span
						className={`h-5 w-1.5 rounded-full ${accent === "success" ? "bg-success" : "bg-warning"}`}
					/>
					<div>
						<div className="flex items-center gap-2">
							<h3 className="font-bold text-base-content text-lg tracking-tight">{title}</h3>
							<span className="badge badge-sm badge-ghost font-mono">{providers.length}</span>
						</div>
						<p className="text-base-content/50 text-xs">{subtitle}</p>
					</div>
				</div>
				<button
					type="button"
					className={`btn btn-sm gap-1.5 ${accent === "success" ? "btn-outline border-success/40 text-success hover:border-success hover:bg-success/10" : "btn-outline border-warning/40 text-warning hover:border-warning hover:bg-warning/10"}`}
					onClick={onAdd}
				>
					<Plus className="h-4 w-4" />
					{addLabel}
				</button>
			</div>

			{providers.length === 0 ? (
				<div className="rounded-2xl border-2 border-base-300 border-dashed bg-base-200/20 py-8 text-center">
					<Wifi className="mx-auto mb-2 h-7 w-7 text-base-content/20" />
					<p className="text-base-content/50 text-xs">{emptyHint}</p>
				</div>
			) : (
				<div className="grid gap-4 md:grid-cols-2">{providers.map(renderProviderCard)}</div>
			)}
		</div>
	);

	return (
		<div className="space-y-8">
			<div ref={listRef} className="relative space-y-8">
				{renderSection(
					"Shared Pool",
					"Used together for every download.",
					primaryProviders,
					"success",
					"Add Primary",
					() => handleCreate(false),
					"No primary providers yet. Add one to start downloading.",
				)}

				{renderSection(
					"Backup",
					"Only used when the shared pool fails.",
					backupProviders,
					"warning",
					"Add Backup",
					() => handleCreate(true),
					"No backup providers configured.",
				)}
			</div>

			{/* Global Provider User-Agent — one value applied to every provider above */}
			<div className="space-y-3 border-base-200 border-t pt-6">
				<div>
					<h3 className="font-bold text-base-content text-lg tracking-tight">
						Provider User-Agent
					</h3>
					<p className="text-base-content/50 text-xs">
						Sent to every NNTP provider above. Leave empty to disable.
					</p>
				</div>
				<input
					type="text"
					className="input input-bordered w-full font-mono text-sm"
					value={globalUserAgent}
					onChange={(e) => handleGlobalUserAgentChange(e.target.value)}
					placeholder="e.g. SABnzbd/4.5.5"
				/>
			</div>

			{/* NNTP Pipeline Optimization */}
			<div className="space-y-3 border-base-200 border-t pt-6">
				<div>
					<h3 className="font-bold text-base-content text-lg tracking-tight">NNTP Pipeline</h3>
					<p className="text-base-content/50 text-xs">
						Auto-tune the pipeline depth (requests in flight per connection) by speed-testing each
						enabled provider, or disable pipelining everywhere. Run when downloads are idle for the
						most accurate result.
					</p>
				</div>
				<div className="flex flex-wrap items-center gap-3">
					<button
						type="button"
						className="btn btn-primary gap-2"
						onClick={handleAutoTunePipeline}
						disabled={!!pipelineTuning || disablingPipeline || formData.length === 0}
					>
						{pipelineTuning ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<Gauge className="h-4 w-4" />
						)}
						{pipelineTuning
							? `Testing ${pipelineTuning.current}/${pipelineTuning.total}${pipelineTuning.host ? ` · ${pipelineTuning.host}` : ""}…`
							: "Auto-Tune Pipeline"}
					</button>
					<button
						type="button"
						className="btn btn-outline gap-2"
						onClick={handleDisablePipelining}
						disabled={!!pipelineTuning || disablingPipeline || formData.length === 0}
					>
						{disablingPipeline ? (
							<span className="loading loading-spinner loading-sm" />
						) : (
							<PowerOff className="h-4 w-4" />
						)}
						Disable Pipelining
					</button>
				</div>
			</div>

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
					defaultBackup={createBackup}
					defaultUserAgent={globalUserAgent}
					onSuccess={handleModalSuccess}
					onCancel={() => setIsModalOpen(false)}
				/>
			)}
		</div>
	);
}
