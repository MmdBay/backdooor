// Package manage is the management work every front end shares — the menu,
// the panel, the Telegram bot, the CLI and the fleet RPC all call it: the
// setup wizards, editing a tunnel, share links, the panel's API adapters,
// update and rollback, migration, benchmarks.
//
// It sits on layers of its own (docs/adr/0003): core and spec (the tunnel
// listing, systemd, the transport vocabulary), host (this machine), tunnelspec
// (a tunnel's configuration and applying a change safely), backup, and health
// (whether the tunnels work). Their names are re-exported here unchanged, so a
// caller sees one package.
//
// Hot entry files (commenting Waves C–D):
//
//	update.go / updatecheck.go / updaterelay.go — signed release install
//	showcase.go                               — developer ad box (phone-home)
//	health/ + health_alias.go                  — TunnelHealth + watchdog
//	migrate.go                                 — post-update config fixes
//	setup.go / reversesetup / directsetup      — wizards + transport picker
//	webapi.go / directwebapi.go                — panel form → same Apply path
//	conntest.go (+ menu/web/board siblings)    — transport survival matrix
//	benchmark.go                               — path latency/jitter/loss
//	menu.go                                    — Manage Tunnels TUI screens
//
// Layers (ADR 0003): core → host/spec/tunnelspec/backup → health → wizards here.
package manage
