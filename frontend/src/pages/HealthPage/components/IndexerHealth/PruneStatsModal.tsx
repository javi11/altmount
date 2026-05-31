import { RefreshCw, Trash2 } from "lucide-react";
import { useState } from "react";

type PruneOption = "24h" | "7d" | "30d" | "custom";

interface PruneStatsModalProps {
	isPending: boolean;
	onClose: () => void;
	onPrune: (hours: number) => Promise<void>;
}

export function PruneStatsModal({ isPending, onClose, onPrune }: PruneStatsModalProps) {
	const [pruneOption, setPruneOption] = useState<PruneOption>("24h");
	const [customDays, setCustomDays] = useState(3);

	const handleConfirm = async () => {
		let hours = 24;
		if (pruneOption === "7d") hours = 7 * 24;
		else if (pruneOption === "30d") hours = 30 * 24;
		else if (pruneOption === "custom") hours = customDays * 24;
		await onPrune(hours);
	};

	return (
		<div
			className="modal modal-open backdrop-blur-sm"
			role="dialog"
			aria-modal="true"
			aria-labelledby="prune-modal-title"
		>
			<div className="modal-box max-w-md border border-base-300 bg-base-100 p-6 shadow-2xl sm:p-8">
				<h3
					id="prune-modal-title"
					className="flex items-center gap-2 font-bold text-base-content text-xl"
				>
					<Trash2 className="h-6 w-6 text-amber-500" aria-hidden="true" />
					Prune Statistics
				</h3>
				<p className="py-4 font-medium text-base-content/60 text-sm">
					Choose the time period of historical statistics you would like to clear.
				</p>

				<div className="space-y-3">
					{(["24h", "7d", "30d", "custom"] as const).map((opt) => {
						const isSelected = pruneOption === opt;
						return (
							<label
								key={opt}
								className={`label cursor-pointer justify-start gap-3 rounded-xl border p-4 transition-all duration-200 hover:bg-base-200 ${
									isSelected
										? "border-primary bg-primary/5 shadow-sm"
										: "border-base-200 bg-base-200/30"
								}`}
							>
								<input
									type="radio"
									name="prune_option"
									className="radio radio-primary"
									checked={isSelected}
									onChange={() => setPruneOption(opt)}
									aria-label={`Prune period: ${
										opt === "24h"
											? "24 Hours"
											: opt === "7d"
												? "7 Days"
												: opt === "30d"
													? "30 Days"
													: "Custom Days"
									}`}
								/>
								<div className="flex-1">
									<span className="font-bold text-base-content text-sm">
										{opt === "24h"
											? "Delete Last 24 Hours"
											: opt === "7d"
												? "Delete Last 7 Days"
												: opt === "30d"
													? "Delete Last 30 Days"
													: "Delete Custom Period"}
									</span>
									<p className="mt-0.5 font-medium text-[10px] text-base-content/50 sm:text-xs">
										{opt === "24h"
											? "Resets statistics from the most recent day only."
											: opt === "7d"
												? "Resets the last week of collected indexer data."
												: opt === "30d"
													? "Clears the past month of statistics."
													: "Specify a custom number of days to clear."}
									</p>
									{opt === "custom" && isSelected && (
										<div className="mt-3 flex items-center gap-3">
											<input
												type="number"
												className="input input-bordered input-sm w-24 border-base-300 bg-base-100 text-center font-bold text-base-content"
												value={customDays}
												onChange={(e) =>
													setCustomDays(Number.parseInt(e.target.value, 10) || 0)
												}
												min="1"
												aria-label="Custom prune period in days"
											/>
											<span className="font-bold text-base-content/60 text-xs">
												Days of data
											</span>
										</div>
									)}
								</div>
							</label>
						);
					})}
				</div>

				<div className="modal-action mt-6 gap-2">
					<button
						type="button"
						className="btn btn-ghost text-base-content/70 hover:text-base-content"
						onClick={onClose}
						disabled={isPending}
					>
						Cancel
					</button>
					<button
						type="button"
						className="btn btn-warning gap-2 shadow-[0_2px_12px_rgba(217,119,6,0.2)] transition-all duration-200"
						onClick={handleConfirm}
						disabled={isPending}
					>
						{isPending && <RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />}
						Prune Statistics
					</button>
				</div>
			</div>
		</div>
	);
}
