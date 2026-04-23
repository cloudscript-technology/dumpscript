package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestNoop_AllOpsNil(t *testing.T) {
	n := Noop{}
	n.RecordRun(ResultSuccess)
	n.RecordDump("postgresql", time.Second, 1024)
	n.RecordUpload("s3", time.Second, 1024)
	n.RecordRetry("upload")
	n.RecordLastSuccess()
	if err := n.Flush(context.Background()); err != nil {
		t.Errorf("Noop.Flush err = %v", err)
	}
}

func TestFactory_DisabledReturnsNoop(t *testing.T) {
	cfg := &config.Config{Prometheus: config.Prometheus{Enabled: false}}
	m := New(cfg, quietLogger())
	if _, ok := m.(Noop); !ok {
		t.Errorf("expected Noop, got %T", m)
	}
}

func TestFactory_EnabledReturnsProm(t *testing.T) {
	cfg := &config.Config{Prometheus: config.Prometheus{
		Enabled: true, JobName: "test", LogOnExit: false,
	}}
	m := New(cfg, quietLogger())
	if _, ok := m.(*Prom); !ok {
		t.Errorf("expected *Prom, got %T", m)
	}
}

func TestProm_RecordsAndGathers(t *testing.T) {
	p := NewProm(Config{JobName: "t", Instance: "host1"}, quietLogger())

	p.RecordRun(ResultSuccess)
	p.RecordRun(ResultSuccess)
	p.RecordRun(ResultFailure)
	p.RecordRun(ResultSkipped)
	p.RecordDump("postgresql", 2*time.Second, 1024*1024)
	p.RecordUpload("s3", time.Second, 1024*1024)
	p.RecordRetry("upload")
	p.RecordRetry("upload")
	p.RecordLastSuccess()

	mfs, err := p.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	found := map[string]bool{}
	for _, mf := range mfs {
		found[mf.GetName()] = true
	}
	want := []string{
		"dumpscript_runs_total",
		"dumpscript_dump_duration_seconds",
		"dumpscript_dump_bytes",
		"dumpscript_upload_duration_seconds",
		"dumpscript_upload_bytes",
		"dumpscript_storage_retries_total",
		"dumpscript_last_success_timestamp_seconds",
		"dumpscript_run_started_timestamp_seconds",
	}
	for _, name := range want {
		if !found[name] {
			t.Errorf("metric %q missing", name)
		}
	}
}

func TestProm_Flush_NoPushgateway_Noop(t *testing.T) {
	p := NewProm(Config{JobName: "t"}, quietLogger())
	if err := p.Flush(context.Background()); err != nil {
		t.Errorf("Flush (no pushgateway) err = %v", err)
	}
}

func TestProm_Flush_IdempotentFlushedOnce(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewProm(Config{
		JobName:        "test",
		PushgatewayURL: srv.URL,
		Instance:       "host1",
	}, quietLogger())
	p.RecordRun(ResultSuccess)

	if err := p.Flush(context.Background()); err != nil {
		t.Errorf("first flush err = %v", err)
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Errorf("second flush err = %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("pushgateway hit %d times, want 1 (idempotent)", got)
	}
}

func TestProm_Flush_PushgatewayError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewProm(Config{JobName: "t", PushgatewayURL: srv.URL}, quietLogger())
	p.RecordRun(ResultSuccess)
	err := p.Flush(context.Background())
	if err == nil {
		t.Fatal("expected push error")
	}
	if !strings.Contains(err.Error(), "pushgateway") {
		t.Errorf("err should mention pushgateway, got %v", err)
	}
}
