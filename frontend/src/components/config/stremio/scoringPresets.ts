import type { ScoringPreset, StreamScoringConfig, TrashCustomFormat } from "../../../types/config";

export const TRASH_RECOMMENDED_FORMATS: TrashCustomFormat[] = [
	{
		id: "remux_4k",
		name: "4K UHD Remux (Disc)",
		category: "source",
		pattern:
			"\\b(2160p|4k)\\b.*\\b(remux|bdremux|uhd\\.remux)\\b|\\b(remux|bdremux|uhd\\.remux)\\b.*\\b(2160p|4k)\\b",
		patternType: "regex",
		score: 500,
		enabled: true,
		isCustom: false,
	},
	{
		id: "remux_1080p",
		name: "1080p Remux (Disc)",
		category: "source",
		pattern: "\\b1080p\\b.*\\b(remux|bdremux)\\b|\\b(remux|bdremux)\\b.*\\b1080p\\b",
		patternType: "regex",
		score: 350,
		enabled: true,
		isCustom: false,
	},
	{
		id: "dv_hdr",
		name: "Dolby Vision (P7/P8)",
		category: "hdr",
		pattern: "\\b(dv|dovi|dolby[ ._-]?vision)\\b",
		patternType: "regex",
		score: 350,
		enabled: true,
		isCustom: false,
	},
	{
		id: "hdr10_plus",
		name: "HDR10+ / HDR10",
		category: "hdr",
		pattern: "\\b(hdr10\\+|hdr10|hdr)\\b",
		patternType: "regex",
		score: 200,
		enabled: true,
		isCustom: false,
	},
	{
		id: "lossless_atmos",
		name: "Lossless Atmos / TrueHD",
		category: "audio",
		pattern: "\\b(truehd[ ._-]?atmos|truehd|atmos)\\b",
		patternType: "regex",
		score: 250,
		enabled: true,
		isCustom: false,
	},
	{
		id: "dts_hd_ma",
		name: "DTS-HD MA / DTS:X",
		category: "audio",
		pattern: "\\b(dts[ ._-]hd([ ._-]ma)?|dts[ ._-]?x)\\b",
		patternType: "regex",
		score: 200,
		enabled: true,
		isCustom: false,
	},
	{
		id: "webdl_4k",
		name: "4K WEB-DL / WEBRip",
		category: "source",
		pattern: "\\b(2160p|4k)\\b.*\\b(web[ ._-]?dl|webrip)\\b",
		patternType: "regex",
		score: 180,
		enabled: true,
		isCustom: false,
	},
	{
		id: "tier1_groups",
		name: "Tier 1 High-Quality Release Groups",
		category: "release_group",
		pattern: "-(FLUX|FraMeSToR|EPSiLON|DON|playBD|CtrlHD|ZQ|TayTO|BHDStudio|SURCODE)\\b",
		patternType: "regex",
		score: 150,
		enabled: true,
		isCustom: false,
	},
	{
		id: "webdl_1080p",
		name: "1080p WEB-DL",
		category: "source",
		pattern: "\\b1080p\\b.*\\b(web[ ._-]?dl|webrip)\\b",
		patternType: "regex",
		score: 120,
		enabled: true,
		isCustom: false,
	},
	{
		id: "aac_stereo_demote",
		name: "Low Bitrate Stereo Audio",
		category: "audio",
		pattern: "\\b(aac[ ._-]?2\\.0|stereo|mp3)\\b",
		patternType: "regex",
		score: -100,
		enabled: true,
		isCustom: false,
	},
	{
		id: "cam_ts_discard",
		name: "CAM / TeleSync / Screener",
		category: "source",
		pattern: "\\b(cam|camrip|telesync|ts|hdcam|hdts|screener|scr|dvdscr)\\b",
		patternType: "regex",
		score: -2000,
		enabled: true,
		isCustom: false,
	},
];

export const REMUX_ENTHUSIAST_FORMATS: TrashCustomFormat[] = [
	{
		id: "remux_4k",
		name: "4K UHD Remux (Disc)",
		category: "source",
		pattern:
			"\\b(2160p|4k)\\b.*\\b(remux|bdremux|uhd\\.remux)\\b|\\b(remux|bdremux|uhd\\.remux)\\b.*\\b(2160p|4k)\\b",
		patternType: "regex",
		score: 800,
		enabled: true,
		isCustom: false,
	},
	{
		id: "remux_1080p",
		name: "1080p Remux (Disc)",
		category: "source",
		pattern: "\\b1080p\\b.*\\b(remux|bdremux)\\b|\\b(remux|bdremux)\\b.*\\b1080p\\b",
		patternType: "regex",
		score: 500,
		enabled: true,
		isCustom: false,
	},
	{
		id: "dv_hdr",
		name: "Dolby Vision (P7/P8)",
		category: "hdr",
		pattern: "\\b(dv|dovi|dolby[ ._-]?vision)\\b",
		patternType: "regex",
		score: 400,
		enabled: true,
		isCustom: false,
	},
	{
		id: "hdr10_plus",
		name: "HDR10+ / HDR10",
		category: "hdr",
		pattern: "\\b(hdr10\\+|hdr10|hdr)\\b",
		patternType: "regex",
		score: 250,
		enabled: true,
		isCustom: false,
	},
	{
		id: "lossless_atmos",
		name: "Lossless Atmos / TrueHD",
		category: "audio",
		pattern: "\\b(truehd[ ._-]?atmos|truehd|atmos)\\b",
		patternType: "regex",
		score: 350,
		enabled: true,
		isCustom: false,
	},
	{
		id: "dts_hd_ma",
		name: "DTS-HD MA / DTS:X",
		category: "audio",
		pattern: "\\b(dts[ ._-]hd([ ._-]ma)?|dts[ ._-]?x)\\b",
		patternType: "regex",
		score: 300,
		enabled: true,
		isCustom: false,
	},
	{
		id: "tier1_groups",
		name: "Tier 1 High-Quality Release Groups",
		category: "release_group",
		pattern: "-(FLUX|FraMeSToR|EPSiLON|DON|playBD|CtrlHD|ZQ|TayTO|BHDStudio|SURCODE)\\b",
		patternType: "regex",
		score: 200,
		enabled: true,
		isCustom: false,
	},
	{
		id: "webdl_demote",
		name: "Compressed WEB-DL Demotion",
		category: "source",
		pattern: "\\b(web[ ._-]?dl|webrip)\\b",
		patternType: "regex",
		score: -200,
		enabled: true,
		isCustom: false,
	},
	{
		id: "cam_ts_discard",
		name: "CAM / TeleSync / Screener",
		category: "source",
		pattern: "\\b(cam|camrip|telesync|ts|hdcam|hdts|screener|scr|dvdscr)\\b",
		patternType: "regex",
		score: -2000,
		enabled: true,
		isCustom: false,
	},
];

export const COMPATIBILITY_FORMATS: TrashCustomFormat[] = [
	{
		id: "webdl_1080p",
		name: "1080p WEB-DL (High Compatibility)",
		category: "source",
		pattern: "\\b1080p\\b.*\\b(web[ ._-]?dl|webrip)\\b",
		patternType: "regex",
		score: 400,
		enabled: true,
		isCustom: false,
	},
	{
		id: "webdl_4k",
		name: "4K WEB-DL (SDR / Standard HDR)",
		category: "source",
		pattern: "\\b(2160p|4k)\\b.*\\b(web[ ._-]?dl|webrip)\\b",
		patternType: "regex",
		score: 300,
		enabled: true,
		isCustom: false,
	},
	{
		id: "h264_avc",
		name: "H.264 / AVC (Universal Playback)",
		category: "source",
		pattern: "\\b(h[ ._-]?264|x264|avc)\\b",
		patternType: "regex",
		score: 250,
		enabled: true,
		isCustom: false,
	},
	{
		id: "eac3_ddp",
		name: "Dolby Digital Plus (E-AC-3 / DDP)",
		category: "audio",
		pattern: "\\b(e[ ._-]?ac[ ._-]?3|ddp|dd\\+)\\b",
		patternType: "regex",
		score: 200,
		enabled: true,
		isCustom: false,
	},
	{
		id: "aac_stereo",
		name: "AAC Audio",
		category: "audio",
		pattern: "\\baac\\b",
		patternType: "regex",
		score: 150,
		enabled: true,
		isCustom: false,
	},
	{
		id: "remux_demote",
		name: "Heavy High-Bitrate Remux Demotion",
		category: "source",
		pattern: "\\b(remux|bdremux)\\b",
		patternType: "regex",
		score: -100,
		enabled: true,
		isCustom: false,
	},
	{
		id: "cam_ts_discard",
		name: "CAM / TeleSync / Screener",
		category: "source",
		pattern: "\\b(cam|camrip|telesync|ts|hdcam|hdts|screener|scr|dvdscr)\\b",
		patternType: "regex",
		score: -2000,
		enabled: true,
		isCustom: false,
	},
];

export const DEFAULT_EXCLUDE_KEYWORDS: string[] = [
	"CAM",
	"TeleSync",
	"TS",
	".sample",
	"sample",
	"Korean Dub",
	"HDCAM",
	"HDTS",
];

export const DEFAULT_PREFERRED_LANGUAGES: string[] = ["English", "French", "Original Audio"];

export const LANGUAGE_OPTIONS: { id: string; name: string; flag: string; matchTerms: string[] }[] =
	[
		{ id: "en", name: "English", flag: "🇬🇧", matchTerms: ["english", "eng", "en"] },
		{
			id: "fr",
			name: "French",
			flag: "🇫🇷",
			matchTerms: ["french", "vff", "vfq", "truefrench", "fr", "fra"],
		},
		{
			id: "es",
			name: "Spanish",
			flag: "🇪🇸",
			matchTerms: ["spanish", "castellano", "latino", "esp", "spa"],
		},
		{ id: "de", name: "German", flag: "🇩🇪", matchTerms: ["german", "deutsch", "ger", "deu"] },
		{ id: "it", name: "Italian", flag: "🇮🇹", matchTerms: ["italian", "ita"] },
		{ id: "ja", name: "Japanese", flag: "🇯🇵", matchTerms: ["japanese", "jap", "jpn"] },
		{ id: "ko", name: "Korean", flag: "🇰🇷", matchTerms: ["korean", "kor"] },
		{
			id: "zh",
			name: "Chinese",
			flag: "🇨🇳",
			matchTerms: ["chinese", "mandarin", "cantonese", "chi", "zho"],
		},
		{ id: "pt", name: "Portuguese", flag: "🇵🇹", matchTerms: ["portuguese", "por", "pt-br"] },
		{ id: "ru", name: "Russian", flag: "🇷🇺", matchTerms: ["russian", "rus"] },
		{
			id: "orig",
			name: "Original Audio",
			flag: "🌐",
			matchTerms: ["original", "dual", "multi", "multisubs"],
		},
	];

export function getDefaultFormatsForPreset(preset: ScoringPreset): TrashCustomFormat[] {
	switch (preset) {
		case "remux_enthusiast":
			return JSON.parse(JSON.stringify(REMUX_ENTHUSIAST_FORMATS));
		case "compatibility":
			return JSON.parse(JSON.stringify(COMPATIBILITY_FORMATS));
		default:
			return JSON.parse(JSON.stringify(TRASH_RECOMMENDED_FORMATS));
	}
}

export function getDefaultScoringConfig(
	preset: ScoringPreset = "trash_recommended",
): StreamScoringConfig {
	return {
		preset,
		custom_formats: getDefaultFormatsForPreset(preset),
		exclude_keywords: [...DEFAULT_EXCLUDE_KEYWORDS],
		preferred_languages: [...DEFAULT_PREFERRED_LANGUAGES],
		require_preferred_language: false,
		size_limits: {
			resolution_4k: { min_gb: 5, max_gb: 120 },
			resolution_1080p: { min_gb: 1, max_gb: 30 },
		},
	};
}

export function hydrateScoringFromProwlarr(
	scoring: StreamScoringConfig | undefined,
	prowlarr:
		| {
				exclude_keywords?: string[];
				preferred_languages?: string[];
				custom_scores?: Record<string, number>;
		  }
		| undefined,
): StreamScoringConfig {
	let baseScoring: StreamScoringConfig;

	if (scoring?.custom_formats && scoring.custom_formats.length > 0) {
		baseScoring = JSON.parse(JSON.stringify(scoring));
	} else {
		baseScoring = getDefaultScoringConfig("trash_recommended");
	}

	if (prowlarr?.exclude_keywords && prowlarr.exclude_keywords.length > 0) {
		baseScoring.exclude_keywords = [...prowlarr.exclude_keywords];
	}

	if (prowlarr?.preferred_languages && prowlarr.preferred_languages.length > 0) {
		baseScoring.preferred_languages = [...prowlarr.preferred_languages];
	}

	// Try reading custom rule metadata cache from localStorage if available
	let localCachedFormats: TrashCustomFormat[] = [];
	let savedPreset: ScoringPreset | null = null;
	if (typeof window !== "undefined") {
		try {
			const saved = localStorage.getItem("altmount_stremio_custom_formats");
			if (saved) {
				localCachedFormats = JSON.parse(saved);
			}
			const preset = localStorage.getItem("altmount_stremio_preset") as ScoringPreset;
			if (preset) {
				savedPreset = preset;
			}
		} catch {
			// ignore localStorage error
		}
	}

	// If custom_scores is saved in prowlarr config from backend:
	if (prowlarr?.custom_scores && Object.keys(prowlarr.custom_scores).length > 0) {
		const customScores = prowlarr.custom_scores;
		const existingPatterns = new Map<string, TrashCustomFormat>();

		// Add base formats
		for (const f of baseScoring.custom_formats) {
			existingPatterns.set(f.pattern, f);
		}
		// Add any locally cached custom formats
		for (const f of localCachedFormats) {
			existingPatterns.set(f.pattern, f);
		}

		// Update scores for all matching patterns
		const updatedFormats: TrashCustomFormat[] = [];
		const visitedPatterns = new Set<string>();

		for (const f of baseScoring.custom_formats) {
			visitedPatterns.add(f.pattern);
			const cached = existingPatterns.get(f.pattern);
			if (f.pattern in customScores) {
				updatedFormats.push({
					...f,
					name: cached ? cached.name : f.name,
					enabled: cached ? cached.enabled : f.enabled,
					category: cached ? cached.category : f.category,
					score: customScores[f.pattern],
				});
			} else {
				updatedFormats.push({
					...f,
					name: cached ? cached.name : f.name,
					enabled: cached ? cached.enabled : f.enabled,
					category: cached ? cached.category : f.category,
				});
			}
		}

		// Add any other patterns from customScores or local cache
		for (const [pattern, score] of Object.entries(customScores)) {
			if (!visitedPatterns.has(pattern)) {
				visitedPatterns.add(pattern);
				const cached = existingPatterns.get(pattern);
				if (cached) {
					updatedFormats.push({ ...cached, score });
				} else {
					updatedFormats.push({
						id: `custom_${Math.abs(
							pattern.split("").reduce((a, b) => ((a << 5) - a + b.charCodeAt(0)) | 0, 0),
						)}`,
						name: pattern,
						category: "custom",
						pattern,
						patternType: pattern.startsWith("\\") || pattern.includes("(") ? "regex" : "token",
						score,
						enabled: true,
						isCustom: true,
					});
				}
			}
		}

		// Also add any custom formats that might have been disabled (not in customScores)
		for (const f of localCachedFormats) {
			if (f.isCustom && !visitedPatterns.has(f.pattern)) {
				visitedPatterns.add(f.pattern);
				updatedFormats.push(f);
			}
		}

		baseScoring.custom_formats = updatedFormats;
		baseScoring.preset = savedPreset || "custom";
	} else if (savedPreset) {
		baseScoring.preset = savedPreset;
	}

	return baseScoring;
}

export interface EvaluationMatch {
	format: TrashCustomFormat;
	matched: boolean;
	score: number;
}

export interface EvaluationResult {
	totalScore: number;
	matches: EvaluationMatch[];
	excluded: boolean;
	excludeReason?: string;
	matchedLanguages: string[];
	missingRequiredLanguage: boolean;
	verdict: "top_pick" | "standard" | "low_score" | "discarded";
	verdictLabel: string;
}

/**
 * Regex flags honoured on a slash-delimited pattern. Kept in lockstep with the
 * Go implementation (internal/prowlarr.slashPatternExpr), which supports only
 * Go's inline flags. Unknown letters are dropped rather than throwing, so a
 * pattern never behaves differently here than it does on the backend.
 */
const SUPPORTED_REGEX_FLAGS = "ims";

/**
 * Detects patterns a user clearly wrote as a regex. Mirrors Go's
 * isExplicitRegex (internal/prowlarr/client.go).
 *
 * Bracket, paren and brace characters are deliberately excluded: release names
 * are full of them ("[SubsPlease]", "(2020)"), and treating such a keyword as a
 * regex turns "[SubsPlease]" into a character class matching nearly every
 * title. Users who want a bracket-only regex can use the /pattern/ form.
 */
const REGEX_CONSTRUCTS = /\\b|\\[dwsDWS]|\(\?|[|*+?^$]/;

/**
 * Matches a release title against either an explicit regex pattern or a token
 * keyword. Kept in lockstep with Go's prowlarr.MatchKeywordOrPattern so the
 * preview in this UI and the backend's scoring agree on every input.
 */
export function matchKeywordOrPattern(title: string, pattern: string): boolean {
	const trimmedPattern = pattern.trim();
	if (!trimmedPattern || !title) return false;

	// 1. Explicit slash-delimited regex: /pattern/ or /pattern/flags.
	// A structurally valid slash pattern never falls through to keyword
	// matching; an invalid body simply fails to match.
	if (trimmedPattern.startsWith("/") && trimmedPattern.lastIndexOf("/") > 0) {
		const lastSlash = trimmedPattern.lastIndexOf("/");
		const raw = trimmedPattern.slice(1, lastSlash);
		const rawFlags = trimmedPattern.slice(lastSlash + 1);
		// Drop unsupported flags instead of letting RegExp throw on them, and
		// always force case-insensitivity to match the Go side.
		const flags = [
			"i",
			...new Set(rawFlags.split("").filter((f) => f !== "i" && SUPPORTED_REGEX_FLAGS.includes(f))),
		].join("");
		try {
			return new RegExp(raw, flags).test(title);
		} catch {
			return false;
		}
	}

	// 2. Explicit regex pattern (e.g. \b(cam|ts)\b)
	if (REGEX_CONSTRUCTS.test(trimmedPattern)) {
		try {
			return new RegExp(trimmedPattern, "i").test(title);
		} catch {
			// Fallback to token matching below
		}
	}

	// 3. Plain keyword / phrase: match with word/token boundary delimiters
	try {
		const cleanKw = trimmedPattern.replace(/^[._\-\s]+|[._\-\s]+$/g, "");
		if (!cleanKw) return false;

		const escaped = cleanKw.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
		const wordsPattern = escaped.split(/\s+/).join("[ ._\\-]+");

		const tokenRegex = new RegExp(`(?:^|[^a-zA-Z0-9])${wordsPattern}(?:[^a-zA-Z0-9]|$)`, "i");
		return tokenRegex.test(title);
	} catch {
		return false;
	}
}

interface ExclusionCheck {
	excluded: boolean;
	excludeReason?: string;
}

/** Checks the release title against the configured blacklist keywords/regex patterns. */
function checkExclusions(excludeKeywords: string[] | undefined, trimmed: string): ExclusionCheck {
	for (const keyword of excludeKeywords || []) {
		const kw = keyword.trim();
		if (!kw) continue;
		if (matchKeywordOrPattern(trimmed, kw)) {
			return { excluded: true, excludeReason: `Matched blacklist keyword "${keyword}"` };
		}
	}
	return { excluded: false };
}

interface FormatScoreResult {
	totalScore: number;
	matches: EvaluationMatch[];
	exclusion?: ExclusionCheck;
}

/** Scores the release title against every enabled custom format/pattern. */
function scoreFormats(
	customFormats: TrashCustomFormat[] | undefined,
	trimmed: string,
): FormatScoreResult {
	let totalScore = 0;
	const matches: EvaluationMatch[] = [];
	let exclusion: ExclusionCheck | undefined;

	for (const format of customFormats || []) {
		if (!format.enabled) continue;

		let matched = false;
		if (format.patternType === "token") {
			matched = matchKeywordOrPattern(trimmed, format.pattern);
		} else {
			try {
				matched = new RegExp(format.pattern, "i").test(trimmed);
			} catch {
				// Fallback to token matching if regex is invalid
				matched = matchKeywordOrPattern(trimmed, format.pattern);
			}
		}

		if (format.invert) {
			matched = !matched;
		}

		if (matched) {
			totalScore += format.score;
			matches.push({ format, matched: true, score: format.score });
			if (format.score <= -1500) {
				exclusion = {
					excluded: true,
					excludeReason: `${format.name} penalty (${format.score} pts)`,
				};
			}
		}
	}

	// 2. Explicit regex containing regex special tokens (\b, (, ), [, ], |, etc.)
	const hasRegexConstructs = /\\b|\\[dwsDWS]|\(\?|[()[\]{}|*+?^$]/.test(trimmedPattern);
	if (hasRegexConstructs) {
		try {
			const re = new RegExp(trimmedPattern, "i");
			return re.test(title);
		} catch {
			// Fallback to token matching below
		}
	}

	// 3. Plain keyword / phrase: match with word/token boundary delimiters
	try {
		const cleanKw = trimmedPattern.replace(/^[._\-\s]+|[._\-\s]+$/g, "");
		if (!cleanKw) return false;

		const escaped = cleanKw.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
		const wordsPattern = escaped.split(/\s+/).join("[ ._\\-]+");

		const tokenRegex = new RegExp(`(?:^|[^a-zA-Z0-9])${wordsPattern}(?:[^a-zA-Z0-9]|$)`, "i");
		return tokenRegex.test(title);
	} catch {
		return false;
	}
}

export function evaluateRelease(
	releaseTitle: string,
	config: StreamScoringConfig,
): EvaluationResult {
	const trimmed = releaseTitle.trim();
	if (!trimmed) {
		return {
			totalScore: 0,
			matches: [],
			excluded: false,
			matchedLanguages: [],
			missingRequiredLanguage: false,
			verdict: "standard",
			verdictLabel: "Awaiting Input",
		};
	}

	const titleLower = trimmed.toLowerCase();

	let { excluded, excludeReason } = checkExclusions(config.exclude_keywords, trimmed);

	const { totalScore, matches, exclusion } = scoreFormats(config.custom_formats, trimmed);
	if (!excluded && exclusion) {
		excluded = exclusion.excluded;
		excludeReason = exclusion.excludeReason;
	}

	// 2. Evaluate custom formats
	let totalScore = 0;
	const matches: EvaluationMatch[] = [];

	for (const format of config.custom_formats || []) {
		if (!format.enabled) continue;

		let matched = false;
		if (format.patternType === "token") {
			matched = matchKeywordOrPattern(trimmed, format.pattern);
		} else {
			try {
				const regex = new RegExp(format.pattern, "i");
				matched = regex.test(trimmed);
			} catch {
				// Fallback to token matching if regex is invalid
				matched = matchKeywordOrPattern(trimmed, format.pattern);
			}
		}

		if (format.invert) {
			matched = !matched;
		}

		if (matched) {
			totalScore += format.score;
			matches.push({
				format,
				matched: true,
				score: format.score,
			});
			if (format.score <= -1500) {
				excluded = true;
				excludeReason = `${format.name} penalty (${format.score} pts)`;
			}
		}
	}

	// 3. Evaluate Preferred Languages
	const matchedLanguages: string[] = [];
	for (const lang of config.preferred_languages || []) {
		const langOpt = LANGUAGE_OPTIONS.find(
			(l) => l.name.toLowerCase() === lang.toLowerCase() || l.id === lang.toLowerCase(),
		);
		const terms = langOpt ? langOpt.matchTerms : [lang.toLowerCase()];
		const found = terms.some((term) => titleLower.includes(term));
		if (found) {
			matchedLanguages.push(lang);
		}
	}

	let missingRequiredLanguage = false;
	if (
		config.require_preferred_language &&
		(config.preferred_languages || []).length > 0 &&
		matchedLanguages.length === 0
	) {
		missingRequiredLanguage = true;
		excluded = true;
		excludeReason = "Missing required audio language";
	}

	// 4. Determine Verdict
	let verdict: "top_pick" | "standard" | "low_score" | "discarded" = "standard";
	let verdictLabel = "Standard Stream";

	if (excluded) {
		verdict = "discarded";
		verdictLabel = "🚫 Discarded / Blacklisted";
	} else if (totalScore >= 800) {
		verdict = "top_pick";
		verdictLabel = "✅ High Priority (Top Stream Pick)";
	} else if (totalScore < 0) {
		verdict = "low_score";
		verdictLabel = "⚠️ Low Score (Demoted)";
	} else {
		verdict = "standard";
		verdictLabel = "⚡ Standard Priority";
	}

	return {
		totalScore,
		matches,
		excluded,
		excludeReason,
		matchedLanguages,
		missingRequiredLanguage,
		verdict,
		verdictLabel,
	};
}
