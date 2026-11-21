// Package config is the tunnel configuration file's schema: the TOML tables
// [server], [client], [direct] and [l3], their keys, and the few rules that
// belong to the schema itself (which transports exist, what a fallback chain
// may name). Defaults and the checks that need the machine are applied by the
// engine (package cmd); docs/config-reference.md is generated from here.
//
// File map:
//
//	config.go       — TransportType + ServerConfig/ClientConfig/Config
//	l3.go           — [l3] direct / paqet schema
//	legacydirect.go — removed [direct] recognition + ErrDirectRemoved
//	naive.go        — optional Naive/Xray helper tables
//	fallback.go     — reverse transport set + fallback chain validation
package config
