package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/blankdots/cnpg-dashboard/internal/api"
	"github.com/blankdots/cnpg-dashboard/internal/clients"
	"github.com/blankdots/cnpg-dashboard/internal/metrics"
	"github.com/blankdots/cnpg-dashboard/internal/store"
	"github.com/blankdots/cnpg-dashboard/internal/watcher"
	"github.com/blankdots/cnpg-dashboard/internal/ws"
)

func main() {
	// Config from env (matches Helm deployment)
	kubeconfig := os.Getenv("KUBECONFIG")
	listenAddr := envOrDefault("LISTEN_ADDR", ":8080")
	tlsCertFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")
	healthAddr := envOrDefault("HEALTH_LISTEN_ADDR", ":8080")
	staticDir := envOrDefault("STATIC_DIR", "/app/static")
	metricsInterval := 90 * time.Second

	initLogging()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	s := store.New()
	client, err := clients.K8sClient(kubeconfig)
	if err != nil {
		slog.Error("k8s client", slog.Any("err", err))
		os.Exit(1)
	}

	errCh := make(chan error, 4)
	go func() {
		for err := range errCh {
			slog.Error("informer error", slog.Any("err", err))
		}
	}()

	// Cluster informer (required)
	clusterGVR := schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	go func() {
		slog.Info("starting informer", slog.String("resource", "clusters"))
		if err := clients.DynamicInformer(ctx, client, "", clusterGVR, watcher.ClusterFuncMap(s), errCh); err != nil {
			slog.Error("clusters informer", slog.Any("err", err))
		}
	}()

	// BarmanObjectStore informer (optional; CRD may be absent in newer CNPG)
	barmanGVR := schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "barmanobjectstores"}
	ok, err := clients.ResourceExists(client, barmanGVR)
	if err != nil {
		slog.Warn("check barmanobjectstores CRD", slog.Any("err", err))
	}
	if ok {
		go func() {
			slog.Info("starting informer", slog.String("resource", "barmanobjectstores"))
			_ = clients.DynamicInformer(ctx, client, "", barmanGVR, watcher.BarmanFuncMap(s), errCh)
		}()
	} else {
		slog.Info("CRD not installed, skipping informer", slog.String("resource", "barmanobjectstores"))
	}

	// ObjectStore informer (Barman Cloud plugin)
	objectStoreGVR := schema.GroupVersionResource{Group: "barmancloud.cnpg.io", Version: "v1", Resource: "objectstores"}
	go func() {
		slog.Info("starting informer", slog.String("resource", "objectstores"))
		_ = clients.DynamicInformer(ctx, client, "", objectStoreGVR, watcher.ObjectStoreFuncMap(s), errCh)
	}()

	// Metrics fetcher (background)
	go metrics.Run(ctx, client, s, metricsInterval)

	mux := http.NewServeMux()
	_, hub := api.New(s, mux)
	ws.Commands(hub, client)

	// Static files (frontend built by Docker into STATIC_DIR, or local dev)
	mux.Handle("GET /", http.FileServer(http.Dir(staticDir)))

	if tlsCertFile != "" && tlsKeyFile != "" {
		// TLS: health on HEALTH_LISTEN_ADDR (probes), main app on LISTEN_ADDR (HTTPS)
		go func() {
			healthMux := http.NewServeMux()
			healthMux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			slog.Info("HTTP health server listening (for probes)", slog.String("addr", healthAddr))
			if err := http.ListenAndServe(healthAddr, healthMux); err != nil && err != http.ErrServerClosed {
				slog.Error("health server", slog.Any("err", err))
			}
		}()
		slog.Info("HTTPS server listening", slog.String("addr", listenAddr))
		server := &http.Server{Addr: listenAddr, Handler: mux}
		go func() {
			if err := server.ListenAndServeTLS(tlsCertFile, tlsKeyFile); err != nil && err != http.ErrServerClosed {
				slog.Error("https server", slog.Any("err", err))
			}
		}()
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	} else {
		server := &http.Server{Addr: listenAddr, Handler: mux}
		go func() {
			slog.Info("HTTP server listening", slog.String("addr", listenAddr))
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("http server", slog.Any("err", err))
			}
		}()
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func initLogging() {
	level := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "debug", "DEBUG":
		level = slog.LevelDebug
	case "warn", "WARN":
		level = slog.LevelWarn
	case "error", "ERROR":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}
