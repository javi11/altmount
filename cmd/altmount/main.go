package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/javi11/altmount/cmd/altmount/cmd"
)

func main() {
	if port := os.Getenv("PPROF_PORT"); port != "" {
		go func() {
			addr := ":" + port
			slog.Info("pprof enabled", "addr", addr)
			if err := http.ListenAndServe(addr, nil); err != nil {
				slog.Error("pprof server failed", "error", err)
			}
		}()
	}

	cmd.Execute()
}
