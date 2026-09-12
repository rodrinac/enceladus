// Command enceladus-api is the Go replacement for src/main.py.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/redis/go-redis/v9"

	"github.com/rodrinac/enceladus/go/internal/api"
	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/email"
	"github.com/rodrinac/enceladus/go/internal/jobstatus"
	"github.com/rodrinac/enceladus/go/internal/reports"
	"github.com/rodrinac/enceladus/go/internal/rreport"
	"github.com/rodrinac/enceladus/go/internal/settings"
	"github.com/rodrinac/enceladus/go/internal/store"
)

func redisAddr(st settings.Settings) string {
	host := st.RedisHost
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s:%d", host, st.RedisPort)
}

func main() {
	st := settings.FromEnvironment().WithCORS()

	cfg, err := config.Load(filepath.Join(st.SourceRoot, "config.yml"))
	if err != nil {
		slog.Error("falha ao carregar configuração", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(st.SESRegion))
	if err != nil {
		slog.Error("falha ao configurar AWS", "error", err)
		os.Exit(1)
	}
	sender := &email.SES{
		Client:           ses.NewFromConfig(awsConfig),
		ConfigurationSet: st.SESConfigurationSet,
		SenderAddress:    st.SESSender,
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr(st),
		Password: st.RedisPassword,
		DB:       st.RedisDB,
	})
	dataStore := store.NewRedisStore(redisClient)
	registry := jobstatus.NewRegistry()
	runner := &rreport.Runner{SourceRoot: st.SourceRoot, RscriptsDir: st.RscriptsDir}
	worker := reports.NewWorker(cfg, st.ReportsDir, dataStore, runner, sender.Send)
	server := api.NewServer(cfg, st, dataStore, registry, worker)

	addr := st.Bind
	if addr == "" {
		addr = "0.0.0.0:8000"
	}
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("servidor iniciado", "endereco", "http://"+addr)
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("falha no servidor HTTP", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		slog.Info("encerrando servidor")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}
}
