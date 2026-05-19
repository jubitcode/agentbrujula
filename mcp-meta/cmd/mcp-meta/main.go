package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	drunRuntime "drun/runtime"

	"mcp-meta/internal/a2a"
	"mcp-meta/internal/api"
	"mcp-meta/internal/cli"
	"mcp-meta/internal/config"
	"mcp-meta/internal/deploy"
	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/mcp"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/router"
	"mcp-meta/internal/runtime"
	"mcp-meta/internal/supervisor"
	"mcp-meta/internal/transport"
)

func envFloat(name string, defaultVal float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cliHandler := cli.New()

	// Check for help with subcommands
	if len(os.Args) > 1 && (os.Args[1] == "help" || os.Args[1] == "--help" || os.Args[1] == "-h") {
		// If asking about a specific command
		if len(os.Args) > 2 && cliHandler.IsSubcommand(os.Args[2]) {
			return nil // Let the CLI handle it
		}
		cliHandler.PrintHelp()
		return nil
	}

	// Try to run as subcommand
	handled, err := cliHandler.Run(os.Args[1:])
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	// Not a subcommand, run as server
	return runServer()
}

func runServer() error {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if cfg.ShowHelp {
		fmt.Print(config.Usage())
		return nil
	}

	if cfg.ShowVersion {
		fmt.Printf("mcp-meta %s\n", mcp.Version)
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	logger := setupLogger(cfg)
	reg := registry.New()

	logger.Info("loading manifests", "path", cfg.ManifestsPath)
	if err := reg.LoadManifests(cfg.ManifestsPath); err != nil {
		return fmt.Errorf("load manifests: %w", err)
	}

	if errs := reg.Validate(); len(errs) > 0 {
		for _, e := range errs {
			logger.Error("manifest validation error", "error", e)
		}
		return fmt.Errorf("manifest validation failed with %d errors", len(errs))
	}

	if cfg.ValidateOnly {
		tools := reg.AllTools()
		logger.Info("manifests validated successfully", "tools", len(tools))
		for _, tool := range tools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
		}
		return nil
	}

	sup := supervisor.New(logger)

	// Set up runtime registry with all available drun backends
	runtimeRegistry := supervisor.NewRuntimeRegistry(logger)

	// Docker backend
	if dockerBackend, err := drunRuntime.NewDockerBackend(context.Background(), ""); err != nil {
		logger.Debug("docker backend not available", "error", err)
	} else if dockerBackend.Available() {
		runtimeRegistry.RegisterDrunBackend("docker", runtime.NewDrunRuntime(dockerBackend, logger))
		logger.Info("runtime backend registered", "backend", "docker")
	} else {
		logger.Debug("docker daemon not running")
	}

	// Sandbox backend (always available, lightweight process isolation)
	sandboxBackend := drunRuntime.NewSandboxBackend("")
	if sandboxBackend.Available() {
		runtimeRegistry.RegisterDrunBackend("sandbox", runtime.NewDrunRuntime(sandboxBackend, logger))
		logger.Info("runtime backend registered", "backend", "sandbox")
	}

	// VZ backend (macOS Virtualization.framework)
	if vzBackend, err := drunRuntime.NewVZBackend(); err == nil && vzBackend.Available() {
		runtimeRegistry.RegisterDrunBackend("vz", runtime.NewDrunRuntime(vzBackend, logger))
		logger.Info("runtime backend registered", "backend", "vz")
	}

	// Firecracker backend (Linux microVMs)
	if fcBackend, err := drunRuntime.NewFirecrackerBackend(""); err == nil && fcBackend.Available() {
		runtimeRegistry.RegisterDrunBackend("firecracker", runtime.NewDrunRuntime(fcBackend, logger))
		logger.Info("runtime backend registered", "backend", "firecracker")
	}

	// Soroban read-only backend (contract queries without subprocess)
	sorobanRT := runtime.NewSorobanReadonlyRuntime(logger)
	runtimeRegistry.RegisterDrunBackend("soroban", sorobanRT)
	logger.Info("runtime backend registered", "backend", "soroban")

	sup.SetRuntimeRegistry(runtimeRegistry)

	if podsToWarm := reg.PodsToPreWarm(); len(podsToWarm) > 0 {
		logger.Info("prewarming pods", "count", len(podsToWarm))
		if err := sup.PreWarmPods(context.Background(), podsToWarm); err != nil {
			logger.Warn("some pods failed to prewarm", "error", err)
		}
	}

	rtr := router.New(reg, sup, logger)
	sup.SetInterPodHandlerWithContract(func(ctx context.Context, sourcePod string, targetTool string, args json.RawMessage, contract *jsonrpc.ContractExpectation) (json.RawMessage, error) {
		return rtr.InternalCallWithContract(ctx, sourcePod, targetTool, args, contract)
	})

	handler := mcp.NewHandler(reg, rtr, sup, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Build A2A components if any AgentPod manifests are loaded
	embeddingURL := os.Getenv("EMBEDDING_URL")
	if embeddingURL == "" {
		embeddingURL = "http://localhost:1234/v1"
	}
	agentIndex := a2a.NewAgentIndex(reg, embeddingURL)
	var a2aHandler *a2a.Handler
	// Always create task store — needed for cost tracking even without A2A agents
	taskStore := a2a.NewMemoryTaskStore(a2a.DefaultTaskStoreConfig())
	if agentIndex.HasAgents() {
		a2aHandler = a2a.NewHandler(taskStore, rtr, agentIndex, logger)
		logger.Info("A2A agent support enabled", "agents", len(reg.AllAgentPods()))
	}

	// Build marketplace deployer
	deployDataDir := filepath.Join(os.Getenv("HOME"), ".mcp-meta")
	deployStore, err := deploy.NewDeployStore(deployDataDir)
	if err != nil {
		logger.Warn("marketplace deploy store not available", "error", err)
	}
	var deployer *deploy.Deployer
	if deployStore != nil {
		deployer = deploy.NewDeployer(deployStore, reg, logger)
		logger.Info("marketplace deploy enabled", "data_dir", deployDataDir)
	}

	// Auth setup:
	//   AUTH_ENABLED=true → API key auth
	//   KEYCLOAK_URL set  → JWT auth (coexists with API keys)
	//   Neither           → dev mode, no auth
	var authenticator api.Authenticator
	var rateLimiter api.RateLimiter

	keycloakURL := os.Getenv("KEYCLOAK_URL")

	if os.Getenv("AUTH_ENABLED") == "true" || keycloakURL != "" {
		var auths []api.Authenticator

		apiAuth := api.NewAPIKeyAuthenticator("X-API-Key")
		if freeKey := os.Getenv("AUTH_FREE_KEY"); freeKey != "" {
			apiAuth.RegisterKey(freeKey, &api.Tenant{ID: "free-user", Name: "Free Tier", Tier: "free"})
			logger.Info("registered free tier API key")
		}
		if paidKey := os.Getenv("AUTH_PAID_KEY"); paidKey != "" {
			apiAuth.RegisterKey(paidKey, &api.Tenant{ID: "paid-user", Name: "Paid Tier", Tier: "paid"})
			logger.Info("registered paid tier API key")
		}
		auths = append(auths, apiAuth)

		if keycloakURL != "" {
			realm := os.Getenv("KEYCLOAK_REALM")
			if realm == "" {
				realm = "mcp-meta"
			}
			clientID := os.Getenv("KEYCLOAK_CLIENT_ID")
			if clientID == "" {
				clientID = "mcp-meta-web"
			}

			// REQUIRE_CLIENT_ID controls whether the JWT must carry a matching
			// `aud` (audience) claim. Default "true" (strict production posture).
			// Set "false" to skip audience checking and accept any token signed
			// by the configured issuer; useful for dev or when audience mismatches
			// between mcp-meta-web and other internal services.
			requireClientID := strings.ToLower(os.Getenv("REQUIRE_CLIENT_ID")) != "false"
			audience := clientID
			if !requireClientID {
				audience = ""
			}
			jwtAuth := api.NewJWTAuthenticator(keycloakURL, realm, audience)
			auths = append(auths, jwtAuth)
			logger.Info("keycloak JWT auth enabled",
				"url", keycloakURL, "realm", realm,
				"require_client_id", requireClientID)
		}

		authenticator = api.NewCompositeAuthenticator(auths...)

		// Rate limits are env-configurable. Defaults: strict production posture
		// (50/hr free, 500/hr paid). Override via RATE_LIMIT_FREE /
		// RATE_LIMIT_PAID (per-second rate; burst is 5x rate).
		freeRate := envFloat("RATE_LIMIT_FREE", 0.014) // ~50/hr
		paidRate := envFloat("RATE_LIMIT_PAID", 0.14)  // ~500/hr
		rateLimiter = api.NewTieredRateLimiter(map[string]api.TierConfig{
			"free": {Rate: freeRate, Burst: int(freeRate*5) + 1},
			"paid": {Rate: paidRate, Burst: int(paidRate*5) + 1},
		}, func(tenantID string) string {
			t := apiAuth.GetTenantByID(tenantID)
			if t != nil {
				return t.Tier
			}
			return "free"
		})
		logger.Info("auth enabled",
			"rate_free_per_sec", freeRate,
			"rate_paid_per_sec", paidRate)
	} else {
		logger.Info("auth disabled (dev mode) — set AUTH_ENABLED=true or KEYCLOAK_URL")
	}

	// Start the appropriate transport
	switch cfg.Transport {
	case config.TransportAPI:
		// API mode: HTTP REST API server
		// CORS allowed origins
		allowedOrigins := []string{"http://localhost:3002", "http://localhost:3000"}
		if extra := os.Getenv("CORS_ALLOWED_ORIGINS"); extra != "" {
			for _, o := range strings.Split(extra, ",") {
				if trimmed := strings.TrimSpace(o); trimmed != "" {
					allowedOrigins = append(allowedOrigins, trimmed)
				}
			}
		}

		apiServer := api.NewServer(reg, rtr, handler, logger, api.Config{
			Host:           cfg.Host,
			Port:           cfg.Port,
			AgentIndex:     agentIndex,
			A2AHandler:     a2aHandler,
			TaskStore:      taskStore,
			Deployer:       deployer,
			Authenticator:  authenticator,
			RateLimiter:    rateLimiter,
			AllowedOrigins: allowedOrigins,
		})

		logger.Info("mcp-meta API server started", "version", mcp.Version, "host", cfg.Host, "port", cfg.Port)
		go func() {
			if err := apiServer.Start(ctx); err != nil && err != context.Canceled {
				logger.Error("API server error", "error", err)
				cancel()
			}
		}()

		<-ctx.Done()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("API server shutdown error", "error", err)
		}
		if taskStore != nil {
			taskStore.Stop()
		}
		if err := sup.Shutdown(shutdownCtx); err != nil {
			logger.Error("supervisor shutdown error", "error", err)
		}

	default:
		// MCP transport modes (stdio, sse)
		var trans transport.Transport
		switch cfg.Transport {
		case config.TransportStdio:
			stdioTrans := transport.NewDefaultStdioTransport()
			stdioTrans.SetLogger(logger)
			trans = stdioTrans
		case config.TransportSSE:
			sseTrans := transport.NewSSETransport(cfg.Host, cfg.Port)
			sseTrans.SetLogger(logger)
			trans = sseTrans
			logger.Info("starting SSE server", "host", cfg.Host, "port", cfg.Port)
		}

		logger.Info("mcp-meta started", "version", mcp.Version, "transport", cfg.Transport)
		if err := trans.Start(ctx, handler); err != nil && err != context.Canceled {
			return fmt.Errorf("transport error: %w", err)
		}

		logger.Info("shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := trans.Shutdown(shutdownCtx); err != nil {
			logger.Error("transport shutdown error", "error", err)
		}
		if err := sup.Shutdown(shutdownCtx); err != nil {
			logger.Error("supervisor shutdown error", "error", err)
		}
	}

	logger.Info("shutdown complete")
	return nil
}

func setupLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.LogJSON {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}
