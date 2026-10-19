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

interface ProwlarrScoringSource {
	exclude_keywords?: string[];
	preferred_languages?: string[];
	custom_scores?: Record<string, number>;
}

interface LocalScoringCache {
	localCachedFormats: TrashCustomFormat[];
	savedPreset: ScoringPreset | null;
}

/** Reads the locally cached custom-format metadata and preset choice, if any. */
function readLocalScoringCache(): LocalScoringCache {
	const empty: LocalScoringCache = { localCachedFormats: [], savedPreset: null };
	if (typeof window === "undefined") {
		return empty;
	}
	try {
		const saved = localStorage.getItem("altmount_stremio_custom_formats");
		const localCachedFormats: TrashCustomFormat[] = saved ? JSON.parse(saved) : [];
		const preset = localStorage.getItem("altmount_stremio_preset") as ScoringPreset | null;
		return { localCachedFormats, savedPreset: preset || null };
	} catch {
		return empty;
	}
}

/** Derives a stable synthetic id for a custom-score pattern with no cached metadata. */
function deriveCustomFormatId(pattern: string): string {
	const hash = pattern.split("").reduce((a, b) => ((a << 5) - a + b.charCodeAt(0)) | 0, 0);
	return `custom_${Math.abs(hash)}`;
}

/**
 * Merges backend-persisted custom_scores (pattern -> score) into the base
 * custom_formats list, preferring cached metadata (name/enabled/category)
 * for patterns that already exist locally or in the base preset.
 */
function reconcileCustomScores(
	baseFormats: TrashCustomFormat[],
	customScores: Record<string, number>,
	localCachedFormats: TrashCustomFormat[],
): TrashCustomFormat[] {
	const existingPatterns = new Map<string, TrashCustomFormat>();
	for (const f of baseFormats) {
		existingPatterns.set(f.pattern, f);
	}
	for (const f of localCachedFormats) {
		existingPatterns.set(f.pattern, f);
	}

	const updatedFormats: TrashCustomFormat[] = [];
	const visitedPatterns = new Set<string>();

	for (const f of baseFormats) {
		visitedPatterns.add(f.pattern);
		const cached = existingPatterns.get(f.pattern);
		updatedFormats.push({
			...f,
			name: cached ? cached.name : f.name,
			enabled: cached ? cached.enabled : f.enabled,
			category: cached ? cached.category : f.category,
			score: f.pattern in customScores ? customScores[f.pattern] : f.score,
		});
	}

	for (const [pattern, score] of Object.entries(customScores)) {
		if (visitedPatterns.has(pattern)) continue;
		visitedPatterns.add(pattern);
		const cached = existingPatterns.get(pattern);
		updatedFormats.push(
			cached
				? { ...cached, score }
				: {
						id: deriveCustomFormatId(pattern),
						name: pattern,
						category: "custom",
						pattern,
						patternType: pattern.startsWith("\\") || pattern.includes("(") ? "regex" : "token",
						score,
						enabled: true,
						isCustom: true,
					},
		);
	}

	// Keep any locally cached custom formats that were disabled (not in customScores).
	for (const f of localCachedFormats) {
		if (f.isCustom && !visitedPatterns.has(f.pattern)) {
			visitedPatterns.add(f.pattern);
			updatedFormats.push(f);
		}
	}

	return updatedFormats;
}

export function hydrateScoringFromProwlarr(
	scoring: StreamScoringConfig | undefined,
	prowlarr: ProwlarrScoringSource | undefined,
): StreamScoringConfig {
	const baseScoring: StreamScoringConfig =
		scoring?.custom_formats && scoring.custom_formats.length > 0
			? JSON.parse(JSON.stringify(scoring))
			: getDefaultScoringConfig("trash_recommended");

	if (prowlarr?.exclude_keywords && prowlarr.exclude_keywords.length > 0) {
		baseScoring.exclude_keywords = [...prowlarr.exclude_keywords];
	}

	if (prowlarr?.preferred_languages && prowlarr.preferred_languages.length > 0) {
		baseScoring.preferred_languages = [...prowlarr.preferred_languages];
	}

	const { localCachedFormats, savedPreset } = readLocalScoringCache();

	if (prowlarr?.custom_scores && Object.keys(prowlarr.custom_scores).length > 0) {
		baseScoring.custom_formats = reconcileCustomScores(
			baseScoring.custom_formats,
			prowlarr.custom_scores,
			localCachedFormats,
		);
		baseScoring.preset = savedPreset || "custom";
	} else if (savedPreset) {
		baseScoring.preset = savedPreset;
	}

	return baseScoring;
}
