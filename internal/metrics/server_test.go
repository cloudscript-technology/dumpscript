package metrics

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// TestServeMetrics_NoOpWhenAddrEmpty makes sure the binary doesn't open a
// rogue listener when METRICS_LISTEN is empty (the CronJob default).
func TestServeMetrics_NoOpWhenAddrEmpty(t *testing.T) {
	reg := prometheus.NewRegistry()
	// Expect no panic and no listener — just an early return.
	ServeMetrics("", reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestServeMetrics_ServesMetricsEndpoint binds to an ephemeral port, then
// scrapes /metrics. The exact body doesn't matter — we just confirm the
// goroutine starts the listener and the handler responds.
func TestServeMetrics_ServesMetricsEndpoint(t *testing.T) {
	// Pick a free port by listening then closing — small race but acceptable
	// for a unit test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	reg := prometheus.NewRegistry()
	cnt := prometheus.NewCounter(prometheus.CounterOpts{Name: "dumpscript_test_total"})
	reg.MustRegister(cnt)
	cnt.Inc()

	ServeMetrics(addr, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Poll briefly until the listener is up (goroutine startup race).
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	var lastErr error
	for time.Now().Before(deadline) {
		resp, lastErr = http.Get("http://" + addr + "/metrics") //nolint:noctx
		if lastErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("/metrics never came up: %v", lastErr)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "dumpscript_test_total") {
		t.Errorf("expected counter in /metrics body, got:\n%s", body)
	}
}
