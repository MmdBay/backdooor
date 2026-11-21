package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/backpack/backpack/internal/metrics"

	"github.com/backpack/backpack/config"
	"github.com/backpack/backpack/internal/client"

	"github.com/backpack/backpack/internal/server"
	"github.com/backpack/backpack/internal/utils"
	"github.com/backpack/backpack/internal/utils/handlers"
	"github.com/backpack/backpack/internal/utils/network"

	"github.com/BurntSushi/toml"
)

var (
	logger = utils.NewLogger("info")
)

// tunnelNameFromPath derives a tunnel's name from its config path, which is
// how the rest of the tool identifies it.
func tunnelNameFromPath(configPath string) string {
	base := filepath.Base(configPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// startMetrics records what the tunnel carries so the CLI can show it later.
// It is best-effort: a tunnel must never fail because diagnostics could not be
// written.
//
// The function it returns stops the collector and waits for its last write.
// Every engine calls it on the way out: a reload starts the next generation's
// collector straight after, and that one takes its baseline from the file —
// read before the last write is in, the traffic since the previous tick was
// lost from the total.
func startMetrics(ctx context.Context, configPath, transport, role string) (stop func()) {
	return startMetricsWithTraffic(ctx, configPath, transport, role, nil, nil)
}

// startMetricsWithTraffic is the same, for an engine that counts its own
// traffic rather than reporting it through metrics.AddBytes.
//
// The reverse transports call AddBytes from inside their copy loops, which is
// where the bytes pass. The layer-3 engine has no copy loop — it moves whole
// packets between a device and a carrier — and already keeps exact counters of
// its own, so it hands them over to be read once per snapshot instead. Without
// this its panel card showed 0 in and 0 out for a tunnel that was carrying
// traffic, which reads as a tunnel that is up but doing nothing.
func startMetricsWithTraffic(
	ctx context.Context,
	configPath, transport, role string,
	bytesIn, bytesOut func() uint64,
) (stop func()) {
	name := tunnelNameFromPath(configPath)

	// The same three facts identify a line in a shipped log as identify a
	// snapshot on disk, and both are known exactly here and nowhere earlier —
	// so they are set together rather than derived twice. It has to happen
	// before the engine builds its logger, which is why this call comes before
	// NewServer and NewClient in every path. See utils/logident.go.
	utils.SetLogIdentity(utils.LogIdentity{Tunnel: name, Role: role, Transport: transport})

	if name == "" {
		return func() {}
	}
	c := metrics.NewCollector(filepath.Dir(configPath), name, transport, role, bytesIn, bytesOut)
	liveCollector.Store(c)
	ctx, cancel := context.WithCancel(ctx)
	written := make(chan struct{})
	go func() {
		defer close(written)
		_ = c.Write() // an immediate first reading, so the file exists right away
		c.Run(ctx.Done(), 30*time.Second)
	}()
	return func() {
		liveCollector.CompareAndSwap(c, nil)
		cancel()
		<-written
	}
}

// Run keeps one tunnel running from a configuration file, restarting it in
// place whenever the file changes. See reload.go for why the file is watched at
// all, and for the two rules that keep watching it from being a liability: a
// file that does not parse is ignored, and a file that means the same thing
// does not disturb the tunnel.
//
// Loop shape (one "generation" per pass):
//
//	load/validate → await quota → start engine + quota watcher →
//	await config change or enginectl restart → stop → settle ports → repeat
func Run(configPath string, ctx context.Context) {
	// --- First load (fatal if bad; no running tunnel to keep) ---------------
	cfg, err := loadConfig(configPath)
	if err != nil {
		logger.Fatalf("failed to load configuration: %v", err)
	}
	applyDefaults(cfg)
	if err := validateConfig(cfg); err != nil {
		logger.Fatalf("%v", err)
	}

	// Kernel tuning is process-wide; apply once, not on every reload.
	tuned := false

	// Process-lifetime control socket (enginectl). Survives generations so a
	// Status call between transports still gets an answer. See enginesocket.go.
	ctl := &engineControl{name: tunnelNameFromPath(configPath)}
	serveEngineControl(ctx, ctl)

	for {
		// --- Quota gate: stay "offline" until limit raised ------------------
		ok, waited := awaitQuota(ctx, configPath)
		if !ok {
			return
		}
		if waited {
			if next, err := loadConfig(configPath); err == nil {
				applyDefaults(next)
				if validateConfig(next) == nil {
					cfg = next
				}
			}
		}

		// Engine mutates its copy (status fields); keep cfg pristine for DeepEqual.
		running := *cfg

		applyTuning := !tuned
		tuned = true

		// --- One generation: transport + live quota watcher -----------------
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		// enginectl RestartTransport cancels this generation — same path as reload.
		ctl.setGeneration(cancel, cfg)
		runCtx = ctl.helperContext(runCtx)
		go func() {
			defer close(done)
			runEngine(&running, runCtx, configPath, applyTuning)
		}()
		go watchQuota(runCtx, configPath, cancel)

		next, why := awaitConfigChange(ctx, runCtx, configPath, cfg)
		cancel()
		<-done

		switch why {
		case wakeShutdown:
			return

		case wakeRestart:
			if quotaReached(configPath) {
				// Ended by its traffic limit; awaitQuota holds it at the top.
				continue
			}
			logger.Info("the transport was asked to restart; starting it again")
			waitForPorts(ctx, portsInUse(cfg))
			continue
		}

		logger.Info("the configuration file changed; restarting the tunnel with it")
		waitForPorts(ctx, portsInUse(cfg))
		cfg = next
	}
}

// runEngine runs one tunnel generation until ctx ends.
//
// Dispatch order is an invariant: [l3] first, then refuse legacy [direct],
// then reverse [server]/[client]. The three engines never share startup code.
func runEngine(cfg *config.Config, ctx context.Context, configPath string, applyTuning bool) {
	cleanupPck := func() {
		network.CleanupPckGuards(cfg.Server.Token, cfg.Client.Token, cfg.L3.Token)
	}
	// Recover orphaned tags even if an earlier tunnel was deleted or its token
	// was replaced. The ownership checks preserve every live PCK carrier.
	network.CleanupPckGuards()
	defer cleanupPck()

	// --- Engine A: layer-3 / paqet ([l3] table) ------------------------------
	if cfg.L3.Enabled() {
		runL3Tunnel(cfg, ctx, configPath)
		return
	}
	// --- Removed engine: [direct] stream (v2.0.0) — never fall through ------
	if cfg.Direct.Present() {
		logger.Fatalf("%v", config.ErrDirectRemoved)
		return
	}

	// --- Engine B: reverse tunnel ([server] Iran / [client] kharej) ---------
	configType := ""
	if cfg.Server.BindAddr != "" {
		configType = "server"
	} else if cfg.Client.RemoteAddr != "" {
		configType = "client"
	} else {
		logger.Fatalf("neither server nor client configuration is properly set.")
	}

	switch configType {
	case "server":
		// Apply temporary TCP optimizations at startup
		if applyTuning && !cfg.Server.SkipOptz {
			ApplyTCPTuning()
		}

		stopMetrics := startMetrics(ctx, configPath, string(cfg.Server.Transport), "server")
		defer stopMetrics()

		cfg.Server.PeerPinPath = network.PeerPinPath(configPath)
		srv := server.NewServer(&cfg.Server, ctx) // server
		reportZeroCopy(ctx)
		// Start blocks until its own context ends and everything it owns has
		// stopped. Running it in a goroutine and waiting on ctx here instead
		// meant this returned the moment the signal arrived, while the parts of
		// the generation that shut down in their own time were still holding
		// their ports — and a reload starts the next generation as soon as this
		// function returns.
		srv.Start()
		srv.Stop()
		logger.Println("shutting down server...")
	case "client":
		// Apply temporary TCP optimizations at startup
		if applyTuning && !cfg.Client.SkipOptz {
			ApplyTCPTuning()
		}

		stopMetrics := startMetrics(ctx, configPath, string(cfg.Client.Transport), "client")
		defer stopMetrics()

		cfg.Client.PeerPinPath = network.PeerPinPath(configPath)
		clnt := client.NewClient(&cfg.Client, ctx) // client
		reportZeroCopy(ctx)
		// Blocks until the generation is over; see the server case above.
		clnt.Start()
		clnt.Stop()
		logger.Println("shutting down client...")

	default:
		logger.Fatalf("neither server nor client configuration is properly set.")

	}
}

// CheckConfigFile is the load-time validation Run applies to a config file,
// without starting anything: `backpack check` asks it, so the question "would
// this start?" has one answer, the engine's. See cli.EngineCheck.
//
// The engine logs to stdout, which is right under systemd and wrong here:
// `backpack check --json` owns stdout, and a warning ahead of the JSON breaks
// whatever parses it. For the check, the log goes to stderr.
func CheckConfigFile(path string) error {
	out := logger.Out
	logger.SetOutput(os.Stderr)
	defer logger.SetOutput(out)

	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	applyDefaults(cfg)
	return validateConfig(cfg)
}

// loadConfig loads and parses the TOML configuration file.
func loadConfig(configPath string) (*config.Config, error) {
	var cfg config.Config
	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return &cfg, err
	}
	return &cfg, nil
}

// reportZeroCopy says, periodically and in the tunnel's own journal, whether
// the kernel forwarding path is being used.
//
// Enabling it and having it work are different things: it declines silently on
// a mux or websocket transport, on a rate-limited tunnel, and off Linux. An
// operator who switched it on to try it has no way to tell which happened, and
// the counters live in this process — not in the CLI that runs Health Check —
// so this is where they have to be said out loud.
//
// It is deliberately in the log rather than the panel: the point of it is to be
// pasted into a bug report alongside everything else from the same minutes.
func reportZeroCopy(ctx context.Context) {
	if !handlers.ZeroCopy() {
		return
	}
	logger.Infof("zero-copy forwarding is enabled (experimental; plain tcp transport on Linux only) — %s", handlers.RelaySummary())

	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				logger.Infof("zero-copy forwarding: %s", handlers.RelaySummary())
			}
		}
	}()
}
