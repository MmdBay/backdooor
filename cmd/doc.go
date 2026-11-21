// Package cmd is engine mode: one tunnel, run from one configuration file
// (`backpack -c <file>`), which is what every tunnel's systemd unit executes.
//
// File map:
//
//	cmd.go          — Run loop, runEngine dispatch, metrics start
//	reload.go       — poll config; refuse bad edits; settle ports
//	quota.go        — traffic-limit offline without stopping systemd
//	l3.go           — layer-3 / paqet entry (isolated from reverse)
//	defaults.go     — applyDefaults + validateConfig (+ machine checks)
//	validate.go     — checkEngine (engine-buildability)
//	enginesocket.go — enginectl Status / RestartTransport
//	optimization.go — one-shot sysctl/rlimit from optimize.EngineStartupTuning
//
// It loads the file, fills in the defaults (applyDefaults), refuses what cannot
// run (validateConfig — the same answer `backpack check` gives; see
// CheckConfigFile), and hands the configuration to the engine the file names:
// the layer-3 tunnel for [l3], refuse [direct], the reverse tunnel for
// [server]/[client]. It then watches the file and restarts the tunnel in place
// when it changes (reload.go), keeping the running tunnel when the new file is
// unusable.
package cmd
