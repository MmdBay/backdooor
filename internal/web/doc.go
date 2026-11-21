// Package web is a tunnel's own monitor page and its usage store: host and
// tunnel counters and, with the sniffer on, per-port usage — bound to loopback
// unless the operator says otherwise, because it has no authentication.
//
// File map: monitorhttp.go (page), sniffer.go / statscache.go (usage store).
// Distinct from the admin webui panel (internal/webui).
package web
