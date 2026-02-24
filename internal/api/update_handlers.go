package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/version"
)

const (
	ghAPIBase   = "https://api.github.com"
	ghRepoOwner = "javi11"
	ghRepoName  = "altmount"
)

type githubReleaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type githubCommitResponse struct {
	SHA string `json:"sha"`
}

// fetchLatestGitHubRelease retrieves the latest release tag and URL from GitHub.
func fetchLatestGitHubRelease(ctx context.Context) (tag string, releaseURL string, err error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", ghAPIBase, ghRepoOwner, ghRepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub releases API returned status %d", resp.StatusCode)
	}

	var release githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}
	return release.TagName, release.HTMLURL, nil
}

// fetchLatestGitHubCommit retrieves the latest commit SHA on the main branch.
func fetchLatestGitHubCommit(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/main", ghAPIBase, ghRepoOwner, ghRepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub commits API returned status %d", resp.StatusCode)
	}

	var commit githubCommitResponse
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return "", err
	}
	return commit.SHA, nil
}

// handleGetUpdateStatus handles GET /api/system/update/status
func (s *Server) handleGetUpdateStatus(c *fiber.Ctx) error {
	channel := UpdateChannel(c.Query("channel", string(UpdateChannelLatest)))
	if channel != UpdateChannelLatest && channel != UpdateChannelDev {
		return RespondBadRequest(c, "Invalid channel. Use 'latest' or 'dev'", "")
	}

	resp := UpdateStatusResponse{
		CurrentVersion: version.Version,
		GitCommit:      version.GitCommit,
		Channel:        channel,
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	switch channel {
	case UpdateChannelLatest:
		tag, releaseURL, err := fetchLatestGitHubRelease(ctx)
		if err != nil {
			slog.WarnContext(c.Context(), "Failed to fetch latest GitHub release", "error", err)
			return RespondSuccess(c, resp)
		}
		resp.LatestVersion = tag
		resp.ReleaseURL = releaseURL
		current := strings.TrimPrefix(version.Version, "v")
		latest := strings.TrimPrefix(tag, "v")
		resp.UpdateAvailable = current != latest && version.Version != "dev"

	case UpdateChannelDev:
		sha, err := fetchLatestGitHubCommit(ctx)
		if err != nil {
			slog.WarnContext(c.Context(), "Failed to fetch latest GitHub commit", "error", err)
			return RespondSuccess(c, resp)
		}
		shortSHA := sha[:min(len(sha), 7)]
		resp.LatestVersion = shortSHA
		resp.ReleaseURL = fmt.Sprintf("https://github.com/%s/%s/commits/main", ghRepoOwner, ghRepoName)
		currentSHA := strings.TrimPrefix(version.GitCommit, "v")
		resp.UpdateAvailable = currentSHA != "unknown" &&
			!strings.HasPrefix(sha, currentSHA) &&
			!strings.HasPrefix(currentSHA, shortSHA)
	}

	return RespondSuccess(c, resp)
}
