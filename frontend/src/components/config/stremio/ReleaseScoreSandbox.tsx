import {
	AlertTriangle,
	ArrowRight,
	CheckCircle,
	Film,
	Filter,
	FlaskConical,
	Search,
	Tv,
	XCircle,
	Zap,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { apiClient } from "../../../api/client";
import type { ScoredReleaseItem, StreamScoringConfig } from "../../../types/config";
import { LoadingSpinner } from "../../ui/LoadingSpinner";
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
		label: "1080p Remux (DTS-HD MA)",
		title: "Infidelity.in.Suburbia.2017.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-EPSiLON",
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

const SAMPLE_SEARCHES = [
	{ label: "Infidelity in Suburbia", query: "Infidelity in Suburbia", type: "movie" as const },
	{ label: "Gladiator II", query: "Gladiator II", type: "movie" as const },
	{ label: "Arcane S02E01", query: "Arcane", type: "series" as const, season: 2, episode: 1 },
	{ label: "Dune: Part Two", query: "Dune Part Two", type: "movie" as const },
];

function formatBytes(bytes: number, decimals = 2): string {
	if (!bytes || bytes <= 0) return "0 B";
	const k = 1024;
	const dm = decimals < 0 ? 0 : decimals;
	const sizes = ["B", "KB", "MB", "GB", "TB", "PB"];
	const i = Math.floor(Math.log(bytes) / Math.log(k));
	return `${Number.parseFloat((bytes / k ** i).toFixed(dm))} ${sizes[i]}`;
}

function formatDate(dateStr: string): string {
	if (!dateStr) return "";
	try {
		const d = new Date(dateStr);
		return d.toLocaleDateString(undefined, {
			year: "numeric",
			month: "short",
			day: "numeric",
		});
	} catch {
		return dateStr;
	}
}

export function ReleaseScoreSandbox({ scoringConfig }: ReleaseScoreSandboxProps) {
	const [activeTab, setActiveTab] = useState<"search" | "single">("search");

	// Single release sandbox state
	const [testTitle, setTestTitle] = useState(SAMPLE_RELEASES[0].title);

	// Media search inspector state
	const [searchType, setSearchType] = useState<"movie" | "series">("movie");
	const [searchQuery, setSearchQuery] = useState("Infidelity in Suburbia");
	const [seasonNum, setSeasonNum] = useState<number>(1);
	const [episodeNum, setEpisodeNum] = useState<number>(1);
	const [isSearching, setIsSearching] = useState(false);
	const [searchError, setSearchError] = useState<string | null>(null);
	const [searchResults, setSearchResults] = useState<ScoredReleaseItem[] | null>(null);
	const [activeFilterTab, setActiveFilterTab] = useState<"all" | "active" | "discarded">("all");
	const [searchFilterText, setSearchFilterText] = useState("");
	// Monotonic request counter so responses from stale searches are ignored
	// when a newer search has already been issued.
	const searchSeqRef = useRef(0);

	const evaluation = useMemo(() => {
		return evaluateRelease(testTitle, scoringConfig);
	}, [testTitle, scoringConfig]);

	const handleExecuteSearch = async () => {
		const q = searchQuery.trim();
		if (!q) return;

		const seq = ++searchSeqRef.current;
		setIsSearching(true);
		setSearchError(null);
		try {
			const res = await apiClient.inspectStremioSearch({
				query: q,
				type: searchType,
				season: searchType === "series" ? seasonNum : undefined,
				episode: searchType === "series" ? episodeNum : undefined,
				timeout_ms: 6000,
				scoring: scoringConfig, // evaluate against live/draft rules
			});
			if (seq !== searchSeqRef.current) return;
			setSearchResults(res.releases || []);
			setActiveFilterTab("all");
		} catch (err) {
			if (seq !== searchSeqRef.current) return;
			setSearchError(err instanceof Error ? err.message : "Failed to execute media search");
			setSearchResults(null);
		} finally {
			if (seq === searchSeqRef.current) {
				setIsSearching(false);
			}
		}
	};

	const handleInspectInSandbox = (title: string) => {
		setTestTitle(title);
		setActiveTab("single");
	};

	// Filtered search releases
	const filteredReleases = useMemo(() => {
		if (!searchResults) return [];
		return searchResults.filter((item) => {
			if (activeFilterTab === "active" && item.excluded) return false;
			if (activeFilterTab === "discarded" && !item.excluded) return false;
			if (searchFilterText) {
				const term = searchFilterText.toLowerCase();
				return (
					item.title.toLowerCase().includes(term) ||
					item.indexer?.toLowerCase().includes(term) ||
					item.exclude_reason?.toLowerCase().includes(term) ||
					item.matched_formats.some((f) => f.toLowerCase().includes(term))
				);
			}
			return true;
		});
	}, [searchResults, activeFilterTab, searchFilterText]);

	const activeCount = searchResults?.filter((r) => !r.excluded).length ?? 0;
	const discardedCount = searchResults?.filter((r) => r.excluded).length ?? 0;

	return (
		<div className="min-w-0 overflow-hidden rounded-2xl border-2 border-primary/30 bg-primary/5 p-6 shadow-sm">
			{/* Section Header with Tabs */}
			<div className="flex flex-wrap items-center justify-between gap-4 border-primary/20 border-b pb-4">
				<div className="flex items-center gap-2.5">
					<div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-content shadow-sm">
						{activeTab === "search" ? (
							<Search className="h-4 w-4" />
						) : (
							<FlaskConical className="h-4 w-4" />
						)}
					</div>
					<div>
						<h3 className="font-bold text-base tracking-tight">
							Release Scoring & Media Search Inspector
						</h3>
						<p className="text-base-content/60 text-xs">
							Query live indexers or test individual release titles against your TRaSH scoring rules
						</p>
					</div>
				</div>

				{/* Tab Selector */}
				<div className="flex items-center gap-1 rounded-xl border border-base-300 bg-base-100 p-1 shadow-inner">
					<button
						type="button"
						onClick={() => setActiveTab("search")}
						className={`btn btn-xs gap-1.5 rounded-lg font-medium transition-all ${
							activeTab === "search"
								? "btn-primary shadow-xs"
								: "btn-ghost text-base-content/70 hover:text-base-content"
						}`}
					>
						<Search className="h-3 w-3" />
						<span>Live Media Search</span>
					</button>

					<button
						type="button"
						onClick={() => setActiveTab("single")}
						className={`btn btn-xs gap-1.5 rounded-lg font-medium transition-all ${
							activeTab === "single"
								? "btn-primary shadow-xs"
								: "btn-ghost text-base-content/70 hover:text-base-content"
						}`}
					>
						<FlaskConical className="h-3 w-3" />
						<span>Single Title Sandbox</span>
					</button>
				</div>
			</div>

			{/* TAB 1: Live Media Search & Ranking */}
			{activeTab === "search" && (
				<div className="mt-5 space-y-5">
					{/* Search Controls */}
					<div className="space-y-3 rounded-xl border border-base-300 bg-base-100/90 p-4 shadow-sm">
						<div className="flex flex-wrap items-center justify-between gap-2">
							<div className="flex items-center gap-1.5">
								<span className="font-semibold text-base-content/70 text-xs">Media Type:</span>
								<div className="join">
									<button
										type="button"
										onClick={() => setSearchType("movie")}
										className={`btn btn-xs join-item gap-1 ${
											searchType === "movie" ? "btn-primary" : "btn-ghost bg-base-200/60"
										}`}
									>
										<Film className="h-3 w-3" />
										<span>Movie</span>
									</button>
									<button
										type="button"
										onClick={() => setSearchType("series")}
										className={`btn btn-xs join-item gap-1 ${
											searchType === "series" ? "btn-primary" : "btn-ghost bg-base-200/60"
										}`}
									>
										<Tv className="h-3 w-3" />
										<span>TV Series</span>
									</button>
								</div>
							</div>

							{/* Quick Sample Search Chips */}
							<div className="flex flex-wrap items-center gap-1.5">
								<span className="text-[11px] text-base-content/50">Quick Tests:</span>
								{SAMPLE_SEARCHES.map((sample) => (
									<button
										key={sample.label}
										type="button"
										className="btn btn-ghost btn-xs rounded-md border border-base-300/80 bg-base-100/60 text-[10px] hover:bg-primary/20"
										onClick={() => {
											setSearchQuery(sample.query);
											setSearchType(sample.type);
											if (sample.season) setSeasonNum(sample.season);
											if (sample.episode) setEpisodeNum(sample.episode);
										}}
									>
										{sample.label}
									</button>
								))}
							</div>
						</div>

						{/* Search Inputs Row */}
						<div className="flex flex-wrap items-center gap-2">
							<div className="relative min-w-48 flex-1">
								<input
									type="text"
									className="input input-bordered input-sm w-full pr-8 font-mono text-xs shadow-inner"
									placeholder={
										searchType === "movie"
											? "Movie title (e.g. Infidelity in Suburbia) or IMDb ID (tt...)"
											: "Series title (e.g. Arcane) or IMDb ID (tt...)"
									}
									value={searchQuery}
									onChange={(e) => setSearchQuery(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter") {
											e.preventDefault();
											handleExecuteSearch();
										}
									}}
								/>
								{searchQuery && (
									<button
										type="button"
										onClick={() => setSearchQuery("")}
										className="absolute top-2 right-2 text-base-content/40 hover:text-base-content"
									>
										<XCircle className="h-3.5 w-3.5" />
									</button>
								)}
							</div>

							{searchType === "series" && (
								<div className="flex items-center gap-1.5">
									<div className="flex items-center gap-1">
										<span className="text-[11px] text-base-content/60">S:</span>
										<input
											type="number"
											min="1"
											className="input input-bordered input-sm w-14 text-center font-mono text-xs"
											value={seasonNum}
											onChange={(e) => setSeasonNum(Number.parseInt(e.target.value, 10) || 1)}
										/>
									</div>
									<div className="flex items-center gap-1">
										<span className="text-[11px] text-base-content/60">E:</span>
										<input
											type="number"
											min="1"
											className="input input-bordered input-sm w-14 text-center font-mono text-xs"
											value={episodeNum}
											onChange={(e) => setEpisodeNum(Number.parseInt(e.target.value, 10) || 1)}
										/>
									</div>
								</div>
							)}

							<button
								type="button"
								onClick={handleExecuteSearch}
								disabled={isSearching || !searchQuery.trim()}
								className="btn btn-primary btn-sm gap-1.5 shadow-sm"
							>
								{isSearching ? <LoadingSpinner size="sm" /> : <Search className="h-3.5 w-3.5" />}
								<span>Search & Rank Releases</span>
							</button>
						</div>
					</div>

					{/* Error Alert */}
					{searchError && (
						<div className="alert alert-error text-xs shadow-md">
							<AlertTriangle className="h-4 w-4 shrink-0" />
							<div>
								<span className="font-bold">Search Error: </span>
								<span>{searchError}</span>
							</div>
						</div>
					)}

					{/* Search Results Display */}
					{searchResults !== null && (
						<div className="space-y-4">
							{/* Results Summary Bar */}
							<div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-base-300 bg-base-100/90 px-4 py-3 shadow-sm">
								<div className="flex items-center gap-2">
									<span className="font-bold text-base-content/80 text-xs uppercase tracking-wider">
										Found {searchResults.length} Releases
									</span>
									<span className="badge badge-success badge-sm font-semibold">
										{activeCount} Active Streams
									</span>
									{discardedCount > 0 && (
										<span className="badge badge-error badge-sm font-semibold">
											{discardedCount} Discarded
										</span>
									)}
								</div>

								{/* Filter Tabs */}
								<div className="flex items-center gap-1">
									<div className="join">
										<button
											type="button"
											onClick={() => setActiveFilterTab("all")}
											className={`btn btn-xs join-item ${
												activeFilterTab === "all" ? "btn-neutral" : "btn-ghost bg-base-200/50"
											}`}
										>
											All ({searchResults.length})
										</button>
										<button
											type="button"
											onClick={() => setActiveFilterTab("active")}
											className={`btn btn-xs join-item ${
												activeFilterTab === "active" ? "btn-success" : "btn-ghost bg-base-200/50"
											}`}
										>
											✅ Active ({activeCount})
										</button>
										<button
											type="button"
											onClick={() => setActiveFilterTab("discarded")}
											className={`btn btn-xs join-item ${
												activeFilterTab === "discarded" ? "btn-error" : "btn-ghost bg-base-200/50"
											}`}
										>
											🚫 Discarded ({discardedCount})
										</button>
									</div>

									{/* Filter in results search */}
									<div className="relative ml-2 w-32 md:w-44">
										<input
											type="text"
											className="input input-bordered input-xs w-full pl-7 font-mono text-[11px]"
											placeholder="Filter..."
											value={searchFilterText}
											onChange={(e) => setSearchFilterText(e.target.value)}
										/>
										<Filter className="absolute top-1.5 left-2 h-3 w-3 text-base-content/40" />
									</div>
								</div>
							</div>

							{/* Releases List */}
							{filteredReleases.length === 0 ? (
								<div className="rounded-xl border border-base-300 bg-base-100/90 p-8 text-center text-base-content/50 text-xs">
									No releases matching the current filter.
								</div>
							) : (
								<div className="space-y-2.5">
									{filteredReleases.map((release, idx) => (
										<div
											key={`${release.guid || release.download_url || idx}-${release.title}`}
											className={`group relative rounded-xl border p-3.5 transition-all ${
												release.excluded
													? "border-error/30 bg-error/5 hover:border-error/60"
													: release.score >= 500
														? "border-success/30 bg-success/5 hover:border-success/60"
														: "border-base-300 bg-base-100/90 hover:border-primary/50"
											}`}
										>
											<div className="flex flex-wrap items-start justify-between gap-3">
												{/* Left: Title & Metadata */}
												<div className="min-w-0 flex-1 space-y-1.5">
													<div className="flex items-center gap-2">
														<span className="font-bold font-mono text-base-content/40 text-xs">
															#{idx + 1}
														</span>
														<p
															className={`break-all font-mono font-semibold text-xs ${
																release.excluded
																	? "text-base-content/60 line-through"
																	: "text-base-content"
															}`}
															title={release.title}
														>
															{release.title}
														</p>
													</div>

													{/* Details Row: Indexer, Size, Date, Formats, Languages */}
													<div className="flex flex-wrap items-center gap-1.5 text-[11px]">
														{release.source === "library" ? (
															<span className="badge badge-primary badge-xs font-semibold text-[10px]">
																⚡ Local Library (Instant)
															</span>
														) : (
															release.indexer && (
																<span className="badge badge-ghost badge-xs font-medium font-mono text-base-content/70">
																	{release.indexer}
																	{release.source && ` [${release.source}]`}
																</span>
															)
														)}

														{release.size > 0 && (
															<span className="badge badge-ghost badge-xs font-mono text-base-content/60">
																{formatBytes(release.size)}
															</span>
														)}

														{release.publish_date && (
															<span className="text-[10px] text-base-content/50">
																{formatDate(release.publish_date)}
															</span>
														)}

														{/* Matched Custom Formats */}
														{release.matched_formats.map((fmt) => (
															<span
																key={fmt}
																className="badge badge-success badge-xs font-mono text-[10px]"
															>
																{fmt}
															</span>
														))}

														{/* Matched Languages */}
														{release.matched_languages.map((lang) => (
															<span
																key={lang}
																className="badge badge-info badge-xs font-mono text-[10px]"
															>
																{lang}
															</span>
														))}
													</div>

													{/* Discard Error Box */}
													{release.excluded && (
														<div className="mt-1.5 flex items-center gap-1.5 rounded-lg border border-error/20 bg-error/10 px-2.5 py-1 text-error text-xs">
															<XCircle className="h-3.5 w-3.5 shrink-0" />
															<span className="font-semibold">
																🚫 Discarded / Blacklisted:{" "}
																<span className="font-normal">
																	{release.exclude_reason || "Matched exclusion rule"}
																</span>
															</span>
														</div>
													)}
												</div>

												{/* Right: Score & Quick Action */}
												<div className="flex flex-col items-end gap-1.5">
													<div className="flex items-center gap-1.5">
														<span
															className={`badge px-2.5 py-1 font-bold font-mono text-xs shadow-xs ${
																release.excluded
																	? "badge-error text-error-content"
																	: release.score > 0
																		? "badge-success text-success-content"
																		: release.score < 0
																			? "badge-warning text-warning-content"
																			: "badge-neutral"
															}`}
														>
															{release.excluded
																? "Excluded"
																: `${release.score > 0 ? `+${release.score}` : release.score} pts`}
														</span>
													</div>

													{/* Drill-down action */}
													<button
														type="button"
														onClick={() => handleInspectInSandbox(release.title)}
														className="btn btn-ghost btn-xs gap-1 text-[11px] text-primary opacity-0 transition-opacity group-hover:opacity-100"
														title="Inspect release title breakdown in sandbox"
													>
														<span>Inspect Title</span>
														<ArrowRight className="h-3 w-3" />
													</button>
												</div>
											</div>
										</div>
									))}
								</div>
							)}
						</div>
					)}
				</div>
			)}

			{/* TAB 2: Single Title Inspector */}
			{activeTab === "single" && (
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
									{evaluation.verdict === "top_pick" && (
										<CheckCircle className="mr-1 h-3.5 w-3.5" />
									)}
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
			)}
		</div>
	);
}
