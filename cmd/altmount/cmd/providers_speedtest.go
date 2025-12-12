package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/nntppool/v2"
	"github.com/javi11/nzbparser"
	"github.com/spf13/cobra"
)

var (
	testSize string
)

// providersCmd represents the providers command
var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage NNTP providers",
}

// speedtestCmd represents the speedtest command
var speedtestCmd = &cobra.Command{
	Use:   "speedtest",
	Short: "Test download speed of configured providers",
	RunE:  runSpeedTest,
}

func init() {
	rootCmd.AddCommand(providersCmd)
	providersCmd.AddCommand(speedtestCmd)

	speedtestCmd.Flags().StringVar(&testSize, "size", "100MB", "Download size (100MB, 1GB)")
}

func runSpeedTest(cmd *cobra.Command, args []string) error {
	// Load config
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate size
	validSizes := map[string]bool{
		"100MB": true,
		"1GB":   true,
	}
	if !validSizes[testSize] {
		return fmt.Errorf("invalid size: %s. Allowed: 100MB, 1GB", testSize)
	}

	fmt.Println("Preparing speed test (Target: " + testSize + ")...")

	// Download NZB
	nzbURL := fmt.Sprintf("https://sabnzbd.org/tests/test_download_%s.nzb", testSize)
	fmt.Println("Fetching test NZB from " + nzbURL + "...")

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, nzbURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download test NZB: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download test NZB: status %s", resp.Status)
	}

	// Parse NZB
	nzbFile, err := nzbparser.Parse(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to parse NZB: %w", err)
	}

	// Collect segments
	var allSegments []segmentInfo
	for _, file := range nzbFile.Files {
		for _, seg := range file.Segments {
			allSegments = append(allSegments, segmentInfo{
				ID:     seg.ID,
				Groups: file.Groups,
				Size:   int64(seg.Bytes),
			})
		}
	}

	if len(allSegments) == 0 {
		return fmt.Errorf("no segments found in NZB")
	}

	fmt.Println(fmt.Sprintf("Found %d segments in NZB. Starting tests...", len(allSegments)))
	fmt.Println()

	// Test each provider
	for _, provider := range cfg.Providers {
		if provider.Enabled == nil || !*provider.Enabled {
			continue
		}

		fmt.Println(fmt.Sprintf("Testing provider: %s (%s:%d)...", provider.ID, provider.Host, provider.Port))

		speed, err := testProviderSpeed(cmd.Context(), provider, allSegments)
		if err != nil {
			fmt.Println(fmt.Sprintf("  ERROR: %v", err))
		} else {
			fmt.Println(fmt.Sprintf("  Speed: %.2f MB/s", speed))
		}
		fmt.Println()
	}

	return nil
}

type segmentInfo struct {
	ID     string
	Groups []string
	Size   int64
}

func testProviderSpeed(ctx context.Context, pCfg config.ProviderConfig, segments []segmentInfo) (float64, error) {
	// Create pool config
	poolCfg := nntppool.Config{
		Providers: []nntppool.UsenetProviderConfig{
			{
				Host:                           pCfg.Host,
				Port:                           pCfg.Port,
				Username:                       pCfg.Username,
				Password:                       pCfg.Password,
				TLS:                            pCfg.TLS,
				MaxConnections:                 pCfg.MaxConnections,
				InsecureSSL:                    pCfg.InsecureTLS,
				MaxConnectionIdleTimeInSeconds: 60,
				MaxConnectionTTLInSeconds:      60,
			},
		},
		// Use a basic logger that discards output
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		DelayType:      nntppool.DelayTypeFixed,
		RetryDelay:     10 * time.Millisecond,
		MinConnections: 0,
	}

	// Create pool
	pool, err := nntppool.NewConnectionPool(poolCfg)
	if err != nil {
		return 0, fmt.Errorf("failed to create pool: %w", err)
	}
	defer pool.Quit()

	// Run test for 10 seconds or until segments run out
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var totalBytes int64
	var workerWg sync.WaitGroup

	// We want enough workers to saturate the connection
	numWorkers := pCfg.MaxConnections
	if numWorkers <= 0 {
		numWorkers = 20
	}
	if numWorkers > 50 {
		numWorkers = 50 // Cap for test
	}

	// Queue segments
	segmentChan := make(chan segmentInfo, len(segments))
	for _, s := range segments {
		segmentChan <- s
	}
	close(segmentChan)

	startTime := time.Now()

	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case <-testCtx.Done():
					return
				case seg, ok := <-segmentChan:
					if !ok {
						return
					}

					// Download
					_, err := pool.Body(testCtx, seg.ID, io.Discard, seg.Groups)
					if err == nil {
						atomic.AddInt64(&totalBytes, seg.Size)
					}
				}
			}
		}()
	}

	// Wait for workers
	workerWg.Wait()

	duration := time.Since(startTime)
	if duration.Seconds() == 0 {
		return 0, nil
	}

	mb := float64(totalBytes) / 1024 / 1024
	// Use actual duration or timeout duration if it hit timeout
	// Actually time.Since(startTime) is correct because it captures the wall time elapsed.

	speed := mb / duration.Seconds()

	return speed, nil
}