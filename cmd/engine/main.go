// GoPilotKit — Enterprise Intelligence Runtime
//
// A single Go binary implementing all three agentic protocols:
// AG-UI (agent ↔ user), MCP (agent ↔ tools), A2A (agent ↔ agent),
// with autonomous scaffold-based self-improvement, persistent threads,
// and analytics — deployable anywhere.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xsaurabhx/GoPilotKit/internal/config"
	"github.com/0xsaurabhx/GoPilotKit/internal/protocol/agui"
	"github.com/0xsaurabhx/GoPilotKit/internal/runtime"
	"github.com/0xsaurabhx/GoPilotKit/internal/store/analytics"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var cfgPath string

	root := &cobra.Command{
		Use:   "gopilotkit",
		Short: "GoPilotKit — Enterprise Intelligence Runtime",
		Long: `GoPilotKit is a single-binary enterprise intelligence platform implementing
AG-UI, MCP, and A2A protocols with autonomous self-improvement, persistent
threads, and analytics. Configure everything via intelligence.yaml.`,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "Path to configuration file (default: intelligence.yaml or $GOPILOTKIT_CONFIG)")

	// --- run command ---
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start the GoPilotKit runtime",
		Long:  "Load configuration, initialize all subsystems, and start the HTTP/gRPC server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.FindConfigPath(cfgPath)
			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			return runServer(cfg)
		},
	}

	// --- validate command ---
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		Long:  "Parse and validate the configuration file without starting the server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.FindConfigPath(cfgPath)
			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("config validation FAILED: %w", err)
			}
			fmt.Printf("✓ Config OK: %s (profile: %s)\n", path, cfg.Profile)
			fmt.Printf("  Server:    port=%d transport=%v\n", cfg.Server.Port, cfg.Server.Transport)
			fmt.Printf("  Storage:   driver=%s\n", cfg.Storage.Driver)
			fmt.Printf("  Providers: %d configured\n", len(cfg.Providers))
			fmt.Printf("  Agents:    %d configured\n", len(cfg.Agents))
			fmt.Printf("  Tools:     %d configured\n", len(cfg.Tools))
			if cfg.Learning.Enabled {
				fmt.Printf("  Learning:  enabled (containers=%d)\n", len(cfg.Learning.Containers))
			}
			if cfg.Analytics.Enabled {
				fmt.Printf("  Analytics: enabled (dashboard=%v)\n", cfg.Analytics.Dashboard)
			}
			return nil
		},
	}

	// --- version command ---
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print GoPilotKit version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("GoPilotKit %s\n", version)
		},
	}

	root.AddCommand(runCmd, validateCmd, versionCmd)
	return root
}

func runServer(cfg *config.IntelligenceConfig) error {
	fmt.Println("GoPilotKit starting...")
	fmt.Printf("  Profile:    %s\n", cfg.Profile)
	fmt.Printf("  Port:       %d\n", cfg.Server.Port)
	fmt.Printf("  Transports: %v\n", cfg.Server.Transport)
	fmt.Printf("  Storage:    %s (%s)\n", cfg.Storage.Driver, cfg.Storage.ConnectionURI)
	fmt.Printf("  Providers:  %d\n", len(cfg.Providers))
	fmt.Printf("  Agents:     %d\n", len(cfg.Agents))
	fmt.Printf("  Tools:      %d\n", len(cfg.Tools))
	fmt.Println()

	// Initialize the runtime engine — this connects providers, store, and tools.
	engine, err := runtime.NewEngine(cfg)
	if err != nil {
		return fmt.Errorf("initializing runtime engine: %w", err)
	}
	defer engine.Close()

	// Create the AG-UI coordinator with the real engine handler.
	coord := agui.NewCoordinator()
	for _, agent := range cfg.Agents {
		coord.RegisterHandler(agent.Name, engine.HandleRun)
		log.Printf("registered agent: %s (provider=%s)", agent.Name, agent.Provider)
	}

	// Initialize analytics store if enabled.
	var analyticsStore *analytics.Store
	if cfg.Analytics.Enabled {
		analyticsStore = analytics.NewStore(analytics.Config{
			Enabled:       true,
			OTLPEndpoint:  cfg.Telemetry.Endpoint,
			RetentionDays: 30,
			MaxEvents:     100000,
		})
		log.Printf("analytics store initialized (retention=30d, max_events=100000)")
	}

	// Create the AG-UI HTTP handler backed by the coordinator.
	var aguiHandler *agui.Handler
	if analyticsStore != nil {
		aguiHandler = agui.NewHandlerWithAnalytics(func(req agui.RunRequest, stream *agui.RunStream) {
			coord.ExecuteRun(context.Background(), req, stream)
		}, analyticsStore)
	} else {
		aguiHandler = agui.NewHandler(func(req agui.RunRequest, stream *agui.RunStream) {
			coord.ExecuteRun(context.Background(), req, stream)
		})
	}

	// Set up HTTP server.
	mux := http.NewServeMux()
	mux.Handle("/", aguiHandler)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	fmt.Printf("  AG-UI:      http://localhost%s/run\n", addr)
	fmt.Printf("  WebSocket:  ws://localhost%s/ws\n", addr)
	fmt.Printf("  Health:     http://localhost%s/health\n", addr)
	fmt.Printf("  Runs:       http://localhost%s/runs\n", addr)
	if analyticsStore != nil {
		fmt.Printf("  Dashboard:  http://localhost%s/dashboard\n", addr)
		fmt.Printf("  Events:     http://localhost%s/events\n", addr)
		fmt.Printf("  Analytics:  http://localhost%s/analytics\n", addr)
	}
	fmt.Println()

	// Start gRPC server if grpc transport is enabled.
	var grpcSrv *agui.GRPCServer
	for _, t := range cfg.Server.Transport {
		if t == "grpc" {
			grpcAddr := fmt.Sprintf(":%d", cfg.Server.Port+1)
			grpcSrv = agui.NewGRPCServer(func(req agui.RunRequest, stream *agui.RunStream) {
				coord.ExecuteRun(context.Background(), req, stream)
			}, aguiHandler)
			if err := grpcSrv.Start(grpcAddr); err != nil {
				return fmt.Errorf("starting gRPC server: %w", err)
			}
			fmt.Printf("  gRPC:       localhost%s\n", grpcAddr)
			break
		}
	}

	fmt.Println()
	fmt.Println("Press Ctrl+C to stop.")

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		fmt.Println("\nShutting down...")
		if grpcSrv != nil {
			grpcSrv.Stop()
		}
		server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}
