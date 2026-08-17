package stremio

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrashCustomFormat defines a single scoring format rule.
type TrashCustomFormat struct {
	ID          string `yaml:"id" mapstructure:"id" json:"id"`
	Name        string `yaml:"name" mapstructure:"name" json:"name"`
	Category    string `yaml:"category" mapstructure:"category" json:"category"`
	Pattern     string `yaml:"pattern" mapstructure:"pattern" json:"pattern"`
	PatternType string `yaml:"pattern_type" mapstructure:"pattern_type" json:"pattern_type"` // "regex" or "token"
	Score       int    `yaml:"score" mapstructure:"score" json:"score"`
	Enabled     bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	IsCustom    bool   `yaml:"is_custom" mapstructure:"is_custom" json:"is_custom"`
	Invert      bool   `yaml:"invert,omitempty" mapstructure:"invert" json:"invert,omitempty"`
}

// StreamScoringConfig holds the full TRaSH format scoring and filter settings.
type StreamScoringConfig struct {
	Preset                   string              `yaml:"preset" mapstructure:"preset" json:"preset"`
	CustomFormats            []TrashCustomFormat `yaml:"custom_formats" mapstructure:"custom_formats" json:"custom_formats"`
	ExcludeKeywords          []string            `yaml:"exclude_keywords" mapstructure:"exclude_keywords" json:"exclude_keywords"`
	ExcludeRegex             string              `yaml:"exclude_regex,omitempty" mapstructure:"exclude_regex" json:"exclude_regex,omitempty"`
	PreferredLanguages       []string            `yaml:"preferred_languages" mapstructure:"preferred_languages" json:"preferred_languages"`
	RequirePreferredLanguage bool                `yaml:"require_preferred_language" mapstructure:"require_preferred_language" json:"require_preferred_language"`
}

// SearchResult is the common interface for release results from any indexer.
type SearchResult struct {
	Title       string
	DownloadURL string
	Size        int64
	PublishDate time.Time
	Indexer     string
	IndexerID   string
	GUID        string
}

// ScoredRelease represents a ranked release with evaluation metadata.
type ScoredRelease struct {
	SearchResult
	Score            int      `json:"score"`
	MatchedFormats   []string `json:"matched_formats"`
	MatchedLanguages []string `json:"matched_languages"`
	Excluded         bool     `json:"excluded"`
	ExcludeReason    string   `json:"exclude_reason,omitempty"`
}

var (
	regexCache   = sync.Map{}
	languageMap  = map[string][]string{
		"English":        {"english", "eng", "\\ben\\b"},
		"French":         {"french", "vff", "vfq", "truefrench", "\\bfr\\b", "fra"},
		"Spanish":        {"spanish", "castellano", "latino", "esp", "spa"},
		"German":         {"german", "deutsch", "ger", "deu"},
		"Italian":        {"italian", "ita"},
		"Japanese":       {"japanese", "jap", "jpn"},
		"Korean":         {"korean", "kor"},
		"Chinese":        {"chinese", "mandarin", "cantonese", "chi", "zho"},
		"Portuguese":     {"portuguese", "por", "pt-br"},
		"Russian":        {"russian", "rus"},
		"Original Audio": {"original", "dual", "multi", "multisubs"},
	}
)

func getCompiledRegex(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	if val, ok := regexCache.Load(pattern); ok {
		if re, ok := val.(*regexp.Regexp); ok {
			return re, nil
		}
	}

	expr := pattern
	if !strings.HasPrefix(pattern, "(?i)") {
		expr = "(?i)" + pattern
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, re)
	return re, nil
}

// EvaluateRelease evaluates a release title against a scoring configuration.
func EvaluateRelease(title string, cfg *StreamScoringConfig) ScoredRelease {
	res := ScoredRelease{
		SearchResult: SearchResult{
			Title: title,
		},
		MatchedFormats:   make([]string, 0),
		MatchedLanguages: make([]string, 0),
	}

	if cfg == nil {
		return res
	}

	titleLower := strings.ToLower(title)

	// 1. Blacklist Keywords Exclusion
	for _, kw := range cfg.ExcludeKeywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if strings.Contains(titleLower, strings.ToLower(kw)) {
			res.Excluded = true
			res.ExcludeReason = "Matched exclude keyword: " + kw
			return res
		}
		if re, err := getCompiledRegex(kw); err == nil && re != nil && re.MatchString(title) {
			res.Excluded = true
			res.ExcludeReason = "Matched exclude keyword regex: " + kw
			return res
		}
	}

	// 2. Custom Exclude Regex
	if cfg.ExcludeRegex != "" {
		if re, err := getCompiledRegex(cfg.ExcludeRegex); err == nil && re != nil && re.MatchString(title) {
			res.Excluded = true
			res.ExcludeReason = "Matched exclude regex: " + cfg.ExcludeRegex
			return res
		}
	}

	// 3. Audio Language Matching
	for lang, patterns := range languageMap {
		matched := false
		for _, p := range patterns {
			if strings.HasPrefix(p, "\\b") {
				if re, err := getCompiledRegex(p); err == nil && re != nil && re.MatchString(title) {
					matched = true
					break
				}
			} else if strings.Contains(titleLower, p) {
				matched = true
				break
			}
		}
		if matched {
			res.MatchedLanguages = append(res.MatchedLanguages, lang)
		}
	}

	// If preferred language is strictly required
	if cfg.RequirePreferredLanguage && len(cfg.PreferredLanguages) > 0 {
		hasPreferred := false
		for _, pref := range cfg.PreferredLanguages {
			for _, detected := range res.MatchedLanguages {
				if strings.EqualFold(pref, detected) {
					hasPreferred = true
					break
				}
			}
			if hasPreferred {
				break
			}
		}
		if !hasPreferred {
			res.Excluded = true
			res.ExcludeReason = "Does not match required preferred languages"
			return res
		}
	}

	// 4. TRaSH Custom Format Rules
	score := 0
	for _, format := range cfg.CustomFormats {
		if !format.Enabled || strings.TrimSpace(format.Pattern) == "" {
			continue
		}

		matched := false
		if format.PatternType == "token" {
			matched = strings.Contains(titleLower, strings.ToLower(format.Pattern))
		} else {
			if re, err := getCompiledRegex(format.Pattern); err == nil && re != nil {
				matched = re.MatchString(title)
			}
		}

		if format.Invert {
			matched = !matched
		}

		if matched {
			score += format.Score
			res.MatchedFormats = append(res.MatchedFormats, format.Name)

			// Drop immediately if marked as a hard discard
			if format.Score <= -1500 {
				res.Excluded = true
				res.ExcludeReason = "Discard format triggered: " + format.Name
				res.Score = score
				return res
			}
		}
	}

	res.Score = score
	return res
}

// RankAndFilterReleases evaluates a list of search results, applies indexer priority bonuses,
// drops excluded items, and sorts the remaining releases descending by total score.
func RankAndFilterReleases(releases []SearchResult, scoringCfg *StreamScoringConfig, indexerWeights map[string]int) []ScoredRelease {
	if len(releases) == 0 {
		return []ScoredRelease{}
	}

	scored := make([]ScoredRelease, 0, len(releases))
	for _, rel := range releases {
		eval := EvaluateRelease(rel.Title, scoringCfg)
		if eval.Excluded {
			continue
		}

		eval.SearchResult = rel

		// Apply indexer bonus weight
		if indexerWeights != nil {
			if bonus, ok := indexerWeights[rel.Indexer]; ok && bonus != 0 {
				eval.Score += bonus
			} else if bonus, ok := indexerWeights[rel.IndexerID]; ok && bonus != 0 {
				eval.Score += bonus
			}
		}

		scored = append(scored, eval)
	}

	// Sort descending by score; break ties by newest publish date
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].PublishDate.After(scored[j].PublishDate)
	})

	return scored
}
