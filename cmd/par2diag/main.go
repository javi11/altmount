// par2diag runs NZB-mode PAR2 planning against a real NZB and provider with
// per-fetch instrumentation, to diagnose where planning spends its time.
// Usage: par2diag <config.yaml> <file.nzb>
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"

	"github.com/javi11/altmount/internal/par2repair"
)

type loggedFetcher struct {
	inner   *par2repair.PoolFetcher
	fetches atomic.Int64
}

func (l *loggedFetcher) Fetch(ctx context.Context, id string) ([]byte, error) {
	n := l.fetches.Add(1)
	start := time.Now()
	data, err := l.inner.Fetch(ctx, id)
	fmt.Fprintf(os.Stderr, "[fetch %4d] %-50s %8db err=%v (%.2fs)\n",
		n, id, len(data), err, time.Since(start).Seconds())
	return data, err
}

func (l *loggedFetcher) StatIDs(ctx context.Context, ids []string, onResult func(done int)) (map[string]bool, error) {
	start := time.Now()
	missing, err := l.inner.StatIDs(ctx, ids, onResult)
	fmt.Fprintf(os.Stderr, "[stat] %d ids, %d missing, err=%v (%.1fs)\n", len(ids), len(missing), err, time.Since(start).Seconds())
	return missing, err
}

func main() {
	cfgTxt, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	providers := regexp.MustCompile(`(?m)^providers:`).Split(string(cfgTxt), 2)[1]
	find := func(re string) string {
		m := regexp.MustCompile(re).FindStringSubmatch(providers)
		if m == nil {
			panic(re)
		}
		return m[1]
	}
	host := find(`host:\s*(\S+)`)
	user := find(`username:\s*(\S+)`)
	pass := find(`password:\s*(\S+)`)

	ctx := context.Background()
	client, err := nntppool.NewClient(ctx, []nntppool.Provider{{
		Host:        host + ":563",
		TLSConfig:   &tls.Config{ServerName: host},
		Auth:        nntppool.Auth{Username: user, Password: pass},
		Connections: 30,
	}})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	f, err := os.Open(os.Args[2])
	if err != nil {
		panic(err)
	}
	n, err := nzbparser.Parse(f)
	f.Close()
	if err != nil {
		panic(err)
	}

	fetch := &loggedFetcher{inner: par2repair.NewPoolFetcher(
		func() (par2repair.BodyClient, error) { return client, nil }, nil)}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	progress := func(stage par2repair.Stage, done, total int) {
		fmt.Fprintf(os.Stderr, "[progress] %s %d/%d\n", stage, done, total)
	}

	start := time.Now()
	res, err := par2repair.ResolveFromNzb(ctx, n, nil, fetch,
		par2repair.Caps{MaxRepairRatio: 1, MaxMemoryBytes: 512 << 20}, log, progress)
	fmt.Fprintf(os.Stderr, "\n=== resolve done in %.1fs: err=%v\n", time.Since(start).Seconds(), err)
	if res != nil {
		fmt.Fprintf(os.Stderr, "plan: missing=%d recovery=%d spares=%d\n",
			len(res.Plan.Missing), len(res.Plan.Recovery), len(res.Plan.SpareRecovery))
	}
}
