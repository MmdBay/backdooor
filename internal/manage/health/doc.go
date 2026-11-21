// Package health is whether the tunnels are working: one tunnel's state as the
// panel, the bot and the CLI show it, the watchdog that restarts a tunnel that
// has stopped carrying traffic, the monitor's own heartbeat, and the full
// Health Check with the path and MTU probes behind it.
//
// It reads the engines' snapshots and the tunnel specs and acts through core;
// nothing in it edits a tunnel's configuration, and nothing below it knows it
// exists. The wizards and the panel that show its results sit above it.
//
// The names are re-declared in internal/manage exactly as they were
// (health_alias.go), so no caller changed.
//
// File map:
//
//	health.go            — TunnelHealth / AllHealth (metrics.Connected first)
//	watchdog.go          — RunWatchdog; enginectl then systemctl; flap alerts
//	heartbeat.go         — monitor-heartbeat file for “watchdog silent”
//	diagnose.go          — full Health Check checklist
//	diagnose_path.go     — path / MTU probes
//	directhealth.go / grecheck.go / pckcheck.go / throughputhealth.go — carriers
//	vocab.go             — re-exports from manage/spec
package health
