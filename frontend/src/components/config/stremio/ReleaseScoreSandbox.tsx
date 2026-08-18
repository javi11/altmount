import { AlertTriangle, CheckCircle, FlaskConical, XCircle, Zap } from "lucide-react";
import { useMemo, useState } from "react";
import type { StreamScoringConfig } from "../../../types/config";
import { evaluateRelease } from "./scoringPresets";

interface ReleaseScoreSandboxProps {
	scoringConfig: StreamScoringConfig;
}

const SAMPLE_RELEASES = [
	{
		label: "4K Remux (Disc)",
		title: "Dune.Part.Two.2024.2160p.UHD.BluRay.x265.TrueHD.Atmos.7.1-FLUX",
	},
	{
		label: "Ultimate DV/HDR10+",
		title:
			"Avatar.The.Way.of.Water.2022.2160p.DV.HDR10Plus.BluRay.Remux.TrueHD.7.1.Atmos-FraMeSToR",
	},
	{
		label: "1080p WEB-DL",
		title: "Arcane.S02E01.1080p.HDR.WEB-DL.DDP5.1.Atmos.H.264-FLUX",
	},
	{
		label: "CAM / TeleSync (Discarded)",
		title: "Gladiator.II.2024.1080p.CAM.x264-NoGroup",
	},
];

export function ReleaseScoreSandbox({ scoringConfig }: ReleaseScoreSandboxProps) {
	const [testTitle, setTestTitle] = useState(SAMPLE_RELEASES[0].title);

	const evaluation = useMemo(() => {
		return evaluateRelease(testTitle, scoringConfig);
	}, [testTitle, scoringConfig]);

	return (
		<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-primary/30 bg-primary/5 p-6 shadow-sm">
			{/* Section Header */}
			<div className="flex flex-wrap items-center justify-between gap-4 border-primary/20 border-b pb-4">
				<div className="flex items-center gap-2.5">
					<div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-content shadow-sm">
						<FlaskConical className="h-4 w-4" />
					</div>
					<div>
						<h3 className="font-bold text-base tracking-tight">
							Real-Time Release Title Inspector (Scoring Sandbox)
						</h3>
						<p className="text-base-content/60 text-xs">
							Test how any release title will be scored, categorized, and prioritized by Altmount
						</p>
					</div>
				</div>

				<span className="badge badge-primary badge-sm font-semibold">Live Sandbox</span>
			</div>

			<div className="mt-5 space-y-5">
				{/* Input & Sample Buttons */}
				<div className="space-y-2">
					<div className="flex flex-wrap items-center justify-between gap-2">
						<div className="font-semibold text-base-content/70 text-xs">Test Release String:</div>
						<div className="flex flex-wrap items-center gap-1.5">
							<span className="text-[11px] text-base-content/50">Quick Samples:</span>
							{SAMPLE_RELEASES.map((sample) => (
								<button
									key={sample.label}
									type="button"
									className="btn btn-ghost btn-xs rounded-md border border-base-300/80 bg-base-100/60 text-[10px] hover:bg-primary/20"
									onClick={() => setTestTitle(sample.title)}
								>
									{sample.label}
								</button>
							))}
						</div>
					</div>

					<input
						type="text"
						className="input input-bordered w-full font-mono text-xs shadow-inner"
						placeholder="Paste any NZB or torrent release title here..."
						value={testTitle}
						onChange={(e) => setTestTitle(e.target.value)}
					/>
				</div>

				{/* Live Results Card */}
				<div className="space-y-4 rounded-xl border border-base-300 bg-base-100/90 p-4 shadow-sm">
					{/* Matched Custom Format Tags */}
					<div>
						<span className="font-semibold text-[11px] text-base-content/60 uppercase tracking-wider">
							Evaluated Custom Formats:
						</span>
						<div className="mt-2 flex min-h-7 flex-wrap items-center gap-1.5">
							{evaluation.matches.length === 0 ? (
								<span className="text-base-content/40 text-xs italic">
									No custom format rules matched this release title.
								</span>
							) : (
								evaluation.matches.map((match) => (
									<span
										key={match.format.id}
										className={`badge badge-sm gap-1.5 whitespace-nowrap px-2.5 py-1 font-mono font-semibold shadow-xs ${
											match.score > 0
												? "badge-success text-success-content"
												: match.score < 0
													? "badge-error text-error-content"
													: "badge-ghost"
										}`}
									>
										<span className="whitespace-nowrap">
											{match.score > 0 ? `+${match.score}` : match.score} pts
										</span>
										<span className="opacity-90">{match.format.name}</span>
									</span>
								))
							)}
						</div>
					</div>

					{/* Matched Languages */}
					{evaluation.matchedLanguages.length > 0 && (
						<div>
							<span className="font-semibold text-[11px] text-base-content/60 uppercase tracking-wider">
								Detected Audio Languages:
							</span>
							<div className="mt-1 flex flex-wrap items-center gap-1.5">
								{evaluation.matchedLanguages.map((lang) => (
									<span
										key={lang}
										className="badge badge-info badge-sm whitespace-nowrap font-medium"
									>
										{lang}
									</span>
								))}
							</div>
						</div>
					)}

					{/* Exclude Alert Banner if Excluded */}
					{evaluation.excluded && (
						<div className="alert alert-error py-2 text-xs">
							<XCircle className="h-4 w-4 shrink-0" />
							<div>
								<span className="font-bold">Stream Excluded / Discarded: </span>
								<span>{evaluation.excludeReason || "Matched exclusion filter"}</span>
							</div>
						</div>
					)}

					{/* Score & Verdict Footer */}
					<div className="flex flex-wrap items-center justify-between gap-3 border-base-200 border-t pt-3">
						<div className="flex items-center gap-2">
							<span className="text-base-content/60 text-xs">Computed Rank Score:</span>
							<span
								className={`font-extrabold font-mono text-sm ${
									evaluation.totalScore > 0
										? "text-success"
										: evaluation.totalScore < 0
											? "text-error"
											: "text-base-content/80"
								}`}
							>
								{evaluation.totalScore.toLocaleString()} pts
							</span>
						</div>

						<div className="flex items-center gap-2">
							<span className="text-base-content/60 text-xs">Verdict:</span>
							<span
								className={`badge px-3 py-2 font-semibold text-xs shadow-xs ${
									evaluation.verdict === "top_pick"
										? "badge-success text-success-content"
										: evaluation.verdict === "discarded"
											? "badge-error text-error-content"
											: evaluation.verdict === "low_score"
												? "badge-warning text-warning-content"
												: "badge-neutral text-neutral-content"
								}`}
							>
								{evaluation.verdict === "top_pick" && <CheckCircle className="mr-1 h-3.5 w-3.5" />}
								{evaluation.verdict === "discarded" && <XCircle className="mr-1 h-3.5 w-3.5" />}
								{evaluation.verdict === "low_score" && (
									<AlertTriangle className="mr-1 h-3.5 w-3.5" />
								)}
								{evaluation.verdict === "standard" && <Zap className="mr-1 h-3.5 w-3.5" />}
								{evaluation.verdictLabel}
							</span>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
