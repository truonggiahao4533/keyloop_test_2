// Command api runs the Intelligent Inventory Dashboard HTTP service.
//
// Usage:
//
//	api            serve HTTP
//	api migrate    apply database migrations and exit
//	api token -role manager [-sub alice] [-ttl 24h]
//	               print a demo JWT signed with JWT_SECRET
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpadapter "github.com/haotruong/inventory-dashboard/internal/adapter/http"
	"github.com/haotruong/inventory-dashboard/internal/adapter/postgres"
	"github.com/haotruong/inventory-dashboard/internal/app"
	"github.com/haotruong/inventory-dashboard/internal/domain"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

func main() {
	slog.SetDefault(platform.NewLogger())
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = migrate()
	case "token":
		err = token(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func migrate() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{URL: url, MaxConns: 2})
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}
	slog.Info("migrations applied")
	return nil
}

func token(args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	role := fs.String("role", "manager", "viewer | manager | ingest")
	sub := fs.String("sub", "", "subject (default demo-<role>)")
	ttl := fs.Duration("ttl", 24*time.Hour, "token lifetime")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := platform.ParseRole(*role)
	if err != nil {
		return err
	}
	if *sub == "" {
		*sub = "demo-" + *role
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return errors.New("JWT_SECRET is required")
	}
	tok, err := platform.NewAuthenticator(secret).Mint(*sub, r, *ttl)
	if err != nil {
		return err
	}
	fmt.Println(tok)
	return nil
}

func serve() error {
	cfg, err := platform.LoadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{URL: cfg.DatabaseURL, MaxConns: cfg.DBMaxConns})
	if err != nil {
		return err
	}
	defer pool.Close()
	repos := postgres.NewRepos(pool, cfg.DBTimeout)
	svc := app.New(repos.Vehicles, repos.Actions, repos.Settings, domain.SystemClock{})

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: httpadapter.NewRouter(httpadapter.Deps{
			DB:     pool,
			App:    svc,
			Auth:   platform.NewAuthenticator(cfg.JWTSecret),
			Logger: slog.Default(),
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", srv.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
