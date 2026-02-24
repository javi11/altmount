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
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/version"
)

const (
	dockerSocketPath = "/var/run/docker.sock"
	ghRepoOwner      = "javi11"
	ghRepoName       = "altmount"
	ghAPIBase        = "https://api.github.com"
	dockerImageBase  = "ghcr.io/javi11/altmount"
)

// isDockerAvailable checks whether the Docker socket is accessible.
func isDockerAvailable() bool {
	_, err := os.Stat(dockerSocketPath)
	return err == nil
}

// githubReleaseResponse is a minimal subset of the GitHub releases API response.
type githubReleaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// githubCommitResponse is a minimal subset of the GitHub commits API response.
type githubCommitResponse struct {
	SHA string `json:"sha"`
}

func fetchLatestGitHubRelease(ctx context.Context) (tag, url string, err error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", ghAPIBase, ghRepoOwner, ghRepoName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "altmount/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var release githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}

	return release.TagName, release.HTMLURL, nil
}

func fetchLatestGitHubCommit(ctx context.Context) (sha string, err error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/commits/main", ghAPIBase, ghRepoOwner, ghRepoName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "altmount/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API returned status %d", resp.StatusCode)
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

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	resp := UpdateStatusResponse{
		CurrentVersion:  version.Version,
		GitCommit:       version.GitCommit,
		Channel:         channel,
		DockerAvailable: isDockerAvailable(),
	}

	switch channel {
	case UpdateChannelLatest:
		tag, releaseURL, err := fetchLatestGitHubRelease(ctx)
		if err != nil {
			slog.WarnContext(c.Context(), "Failed to fetch latest GitHub release", "error", err)
			return c.Status(200).JSON(fiber.Map{"success": true, "data": resp})
		}

		resp.LatestVersion = tag
		resp.ReleaseURL = releaseURL

		// Compare: both may start with "v", normalise for comparison
		current := strings.TrimPrefix(version.Version, "v")
		latest := strings.TrimPrefix(tag, "v")
		resp.UpdateAvailable = current != latest && version.Version != "dev"

	case UpdateChannelDev:
		sha, err := fetchLatestGitHubCommit(ctx)
		if err != nil {
			slog.WarnContext(c.Context(), "Failed to fetch latest GitHub commit", "error", err)
			return c.Status(200).JSON(fiber.Map{"success": true, "data": resp})
		}

		resp.LatestVersion = sha[:min(len(sha), 7)] // short SHA
		resp.ReleaseURL = fmt.Sprintf("https://github.com/%s/%s/commits/main", ghRepoOwner, ghRepoName)

		// An update is available if the current commit differs from the latest
		currentSHA := strings.TrimPrefix(version.GitCommit, "v")
		resp.UpdateAvailable = currentSHA != "unknown" && !strings.HasPrefix(sha, currentSHA) && !strings.HasPrefix(currentSHA, sha[:min(len(sha), 7)])
	}

	return c.Status(200).JSON(fiber.Map{"success": true, "data": resp})
}

// handleApplyUpdate handles POST /api/system/update/apply
func (s *Server) handleApplyUpdate(c *fiber.Ctx) error {
	if !isDockerAvailable() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "SERVICE_UNAVAILABLE",
				"message": "Docker socket not available",
				"details": "Mount /var/run/docker.sock into the container to enable auto-update",
			},
		})
	}

	var req UpdateApplyRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	if req.Channel != UpdateChannelLatest && req.Channel != UpdateChannelDev {
		req.Channel = UpdateChannelLatest
	}

	image := fmt.Sprintf("%s:%s", dockerImageBase, string(req.Channel))

	slog.InfoContext(c.Context(), "Update requested", "image", image)

	// Respond immediately; perform pull + restart in background
	result := c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": UpdateApplyResponse{
			Status:  "pulling",
			Message: fmt.Sprintf("Pulling %s in the background. The container will restart automatically.", image),
		},
	})

	go performUpdate(image)

	return result
}

// performUpdate pulls the new Docker image and then signals PID 1 to restart.
func performUpdate(image string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	slog.InfoContext(ctx, "Pulling Docker image", "image", image)

	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		slog.ErrorContext(ctx, "Failed to pull Docker image", "image", image, "error", err)
		return
	}

	slog.InfoContext(ctx, "Docker image pulled successfully, signalling container restart", "image", image)

	// Give response time to flush
	time.Sleep(200 * time.Millisecond)

	// Send SIGTERM to PID 1 (the container's init process).
	// Docker will restart the container because of restart: unless-stopped.
	if err := syscall.Kill(1, syscall.SIGTERM); err != nil {
		slog.ErrorContext(ctx, "Failed to send SIGTERM to PID 1", "error", err)
	}
}

