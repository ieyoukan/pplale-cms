// Command server runs the pplale-cms HTTP server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ieyoukan/pplale-cms/internal/auth"
	"github.com/ieyoukan/pplale-cms/internal/config"
	"github.com/ieyoukan/pplale-cms/internal/ghapp"
	"github.com/ieyoukan/pplale-cms/internal/httpapi"
	"github.com/ieyoukan/pplale-cms/internal/publish"
	"github.com/ieyoukan/pplale-cms/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var db store.Store
	if cfg.DatabaseURL == "" {
		logger.Warn("DATABASE_URL is unset, falling back to in-memory storage (development only)")
		db = store.NewMemory()
	} else {
		pg, err := store.OpenPostgres(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pg.Close()
		db = pg
	}

	if cfg.BootstrapAdminID != "" {
		bootstrapAdmin, err := db.GetUser(ctx, cfg.BootstrapAdminID)
		if errors.Is(err, store.ErrNotFound) {
			if err := db.UpsertUser(ctx, store.User{
				DiscordID: cfg.BootstrapAdminID,
				Role:      store.RoleAdmin,
				AddedBy:   "bootstrap",
			}); err != nil {
				return err
			}
			logger.Info("bootstrap admin created", "discord_id", cfg.BootstrapAdminID)
		} else if err != nil {
			return err
		} else if bootstrapAdmin.AddedBy == "bootstrap" && strings.TrimSpace(bootstrapAdmin.DisplayName) == "bootstrap admin" {
			// Older versions stored this placeholder as if it were a real Discord
			// name. Remove it once; the next login fills in the actual name.
			bootstrapAdmin.DisplayName = ""
			if err := db.UpsertUser(ctx, bootstrapAdmin); err != nil {
				return err
			}
			logger.Info("legacy bootstrap display name removed", "discord_id", cfg.BootstrapAdminID)
		}
	}

	privateKey, err := ghapp.ParsePrivateKey(cfg.GitHubPrivateKey)
	if err != nil {
		return err
	}
	api := ghapp.NewAPI(cfg.GitHubAPIBase)
	repo := &ghapp.Client{
		Owner:      cfg.GitHubOwner,
		Repo:       cfg.GitHubRepo,
		BaseBranch: cfg.GitHubBaseBranch,
		API:        api,
		Tokens: &ghapp.TokenSource{
			AppID:          cfg.GitHubAppID,
			InstallationID: cfg.GitHubInstallationID,
			PrivateKey:     privateKey,
			API:            api,
		},
	}

	signer, err := auth.NewStateSigner(cfg.SessionKey)
	if err != nil {
		return err
	}

	deps := httpapi.Deps{
		Store:    db,
		Sessions: &auth.Sessions{Store: db, TTL: cfg.SessionTTL, Secure: cfg.CookieSecure},
		Discord: &auth.Discord{
			ClientID:     cfg.DiscordClientID,
			ClientSecret: cfg.DiscordClientSecret,
			RedirectURI:  cfg.RedirectURI(),
		},
		StateSigner:   signer,
		Publisher:     &publish.Publisher{Repo: repo},
		CardReader:    repo,
		WebhookSecret: cfg.GitHubWebhookSecret,
		Logger:        logger,
		SubmitLimit:   cfg.SubmitPerHour,
		DevSkipAuth:   cfg.DevSkipAuth,
		DevUser:       auth.DiscordUser{ID: cfg.DevUserDiscordID, Username: cfg.DevUserName},
	}
	if info, err := os.Stat(cfg.StaticDir); err == nil && info.IsDir() {
		deps.StaticFS = os.DirFS(cfg.StaticDir)
		logger.Info("serving frontend", "dir", cfg.StaticDir)
	} else {
		logger.Warn("frontend assets not found, API only", "dir", cfg.StaticDir)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("listening", "addr", cfg.Addr, "upstream", cfg.GitHubOwner+"/"+cfg.GitHubRepo)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
