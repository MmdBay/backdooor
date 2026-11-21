package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/backpack/backpack/cmd"
	"github.com/backpack/backpack/internal/app"
	"github.com/backpack/backpack/internal/cli"
	"github.com/backpack/backpack/internal/localproxy"
	"github.com/backpack/backpack/internal/manage"
	"github.com/backpack/backpack/internal/menu"
	"github.com/backpack/backpack/internal/monitor"
	"github.com/backpack/backpack/internal/telegram"
	"github.com/backpack/backpack/internal/utils"
	"github.com/backpack/backpack/internal/webui"
)

var logger = utils.NewLogger("info")

// main is the process entry for every BackPack role. One binary; the argv
// shape picks the role. Order of the branches below is load-bearing:
// subcommands that own their own argv must run before flag.Parse, or the
// flag package treats "node" / "check" as unknown flags.
//
// Roles:
//
//   - Engine:   `backpack -c /etc/backpack/<name>.toml` — one tunnel (systemd)
//   - Menu:     `backpack` with no args — interactive manage CLI on the VPS
//   - Panel:    `-webui` / `-monitor` / `-proxy` — long-lived control services
//   - One-shot: `-v`, `-restart-all`, `-telegram-report`, plus cli subcommands
func main() {
	// --- Fleet far-side (`backpack node …`) — before flag.Parse -------------
	// Used to install an agent; now only `node exec` over SSH. See nodecmd.go.
	if len(os.Args) > 1 && os.Args[1] == "node" {
		runNode(os.Args[2:])
		return
	}

	// --- Non-interactive CLI subcommands (`backpack check`, …) --------------
	// Logic lives in internal/cli and returns Out/Err/Code so the surface is
	// testable without spawning a process.
	if len(os.Args) > 1 && cli.IsCommand(os.Args[1]) {
		cli.EngineCheck = cmd.CheckConfigFile
		if st, err := os.Stdout.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			cli.Color = os.Getenv("NO_COLOR") == ""
			cli.Progress = func(s string) { fmt.Print(s) }
		}
		r := cli.Run(os.Args[1:])
		if r.Out != "" {
			fmt.Print(r.Out)
		}
		if r.Err != "" {
			fmt.Fprint(os.Stderr, r.Err)
		}
		os.Exit(r.Code)
	}

	// --- Flag-selected long-lived / one-shot modes --------------------------
	configPath := flag.String("c", "", "path to a tunnel configuration file (TOML) — runs in engine mode")
	showVersion := flag.Bool("v", false, "print the version and exit")
	restartAll := flag.Bool("restart-all", false, "restart every configured tunnel and exit (used by the auto-refresh job)")
	tgReport := flag.Bool("telegram-report", false, "send a Telegram status report and exit (used by the scheduled job)")
	webPanel := flag.Bool("webui", false, "run the web panel (used by the backpack-webui service)")
	monitorMode := flag.Bool("monitor", false, "run the watchdog, Telegram bot and alerts (used by the backpack-monitor service)")
	proxyMode := flag.Bool("proxy", false, "run the built-in SOCKS5/HTTP proxy (used by the backpack-proxy service)")
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println(app.Version)
		fmt.Println(app.SourceURL)
		return
	case *restartAll:
		ok, failed := manage.RestartAll()
		fmt.Printf("restarted %d tunnels, %d failed\n", ok, failed)
		return
	case *tgReport:
		if err := telegram.SendStatusNow(); err != nil {
			logger.Errorf("telegram report failed: %v", err)
			os.Exit(1)
		}
		return
	case *webPanel:
		if err := webui.Serve(); err != nil {
			logger.Fatalf("web panel failed: %v", err)
		}
		return
	case *monitorMode:
		monitor.Run()
		return
	case *proxyMode:
		runProxy()
		return
	}

	// --- Default: interactive menu, or engine if -c was given ---------------
	if *configPath == "" {
		menu.Run()
		return
	}

	runEngine(*configPath)
}

// runProxy runs the built-in proxy until a termination signal arrives. The
// proxy is a plain loopback service; the tunnel forwards to it like any backend.
func runProxy() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go localproxy.Run(ctx)
	<-sigChan
	cancel()
	logger.Info("backpack proxy stopped")
}

// runEngine starts a single tunnel from a TOML config and blocks until a
// termination signal arrives, then shuts down gracefully.
func runEngine(configPath string) {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		defer close(done)
		cmd.Run(configPath, ctx)
	}()

	<-sigChan
	cancel()
	<-done
	logger.Info("backpack engine stopped")
}
