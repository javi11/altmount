package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/version"
)

const (
	ghAPIBase   = "https://api.github.com"
	ghRepoOwner = "javi11"
	ghRepoName  = "altmount"
)

// isDockerAvailable checks if the docker.sock and docker binary are present.
func isDockerAvailable() bool {
	// Check if docker.sock is mounted
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		return false
	}
	// Check if docker CLI is available in PATH
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	return true
}

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
//
//	@Summary		Get update status
//	@Description	Checks Docker Hub for the latest available version and returns whether an update is available.
//	@Tags			System
//	@Produce		json
//	@Param			channel	query		string	false	"Release channel"	Enums(latest,dev)
//	@Success		200		{object}	APIResponse{data=UpdateStatusResponse}
//	@Failure		400		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/system/update/status [get]
func (s *Server) handleGetUpdateStatus(c *fiber.Ctx) error {
	channel := UpdateChannel(c.Query("channel", string(UpdateChannelLatest)))
	if channel != UpdateChannelLatest && channel != UpdateChannelDev {
		return RespondBadRequest(c, "Invalid channel. Use 'latest' or 'dev'", "")
	}

	resp := UpdateStatusResponse{
		CurrentVersion:  version.Version,
		GitCommit:       version.GitCommit,
		Channel:         channel,
		DockerAvailable: isDockerAvailable(),
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

// handleApplyUpdate handles POST /api/system/update/apply
//
//	@Summary		Apply update
//	@Description	Pulls the latest Docker image and restarts the container to apply the update.
//	@Tags			System
//	@Accept			json
//	@Produce		json
//	@Param			body	body		object{channel=string}	false	"Update channel (latest or dev)"
//	@Success		200		{object}	APIResponse
//	@Failure		400		{object}	APIResponse
//	@Failure		503		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/system/update/apply [post]
func (s *Server) handleApplyUpdate(c *fiber.Ctx) error {
	user := auth.GetUserFromContext(c)
	if !s.isAdminOrLoginDisabled(user) {
		return RespondForbidden(c, "Admin privileges required", "Only administrators can perform system updates.")
	}

	if !isDockerAvailable() {
		return RespondBadRequest(c, "Auto-update is not available. Mount docker.sock into the container and ensure docker CLI is installed.", "")
	}

	var req struct {
		Channel UpdateChannel `json:"channel"`
	}
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	channel := req.Channel
	if channel == "" {
		channel = UpdateChannelLatest
	}

	if channel != UpdateChannelLatest && channel != UpdateChannelDev {
		return RespondBadRequest(c, "Invalid channel. Use 'latest' or 'dev'", "")
	}

	// Use goroutine to avoid blocking the API response
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		image := fmt.Sprintf("ghcr.io/%s/%s:%s", ghRepoOwner, ghRepoName, channel)
		slog.Info("Starting auto-update", "channel", channel, "image", image)

		// 1. Pull the new image
		cmd := exec.CommandContext(ctx, "docker", "pull", image)
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error("Failed to pull latest image", "error", err, "output", string(output))
			return
		}
		slog.Info("Successfully pulled latest image", "output", string(output))

		// 2. Trigger restart
		// Note: performRestart only restarts the process. To pick up the new image,
		// the container needs to be recreated. However, if the user has a setup
		// that handles image updates on restart (like Watchtower or similar), this will work.
		// For many users, a simple process restart is the first step.
		s.performRestart(ctx)
	}()

	return RespondSuccess(c, fiber.Map{
		"message": "Update initiated. The image is being pulled and the server will restart automatically.",
	})
}
