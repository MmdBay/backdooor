package manage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/backpack/backpack/internal/app"
)

// FetchVerifiedBinary downloads the latest GitHub release for this
// architecture, verifies SHA256SUMS + Ed25519 signature with the fork's
// ReleasePublicKey, extracts the backpack binary to a temporary file, and
// returns its path. cleanup removes the temp binary (and should always run).
//
// Used by fleet Install so the panel host — which already carries the trust
// key — verifies before any bytes are pushed to a managed server. That replaces
// remote curl|bash of unsigned install.sh from main.
func FetchVerifiedBinary(logf func(string)) (binPath string, cleanup func(), err error) {
	if logf == nil {
		logf = func(string) {}
	}
	noop := func() {}
	if err := app.RefuseUpstreamTrust(); err != nil {
		return "", noop, err
	}
	tag, err := latestTag()
	if err != nil {
		return "", noop, err
	}
	dir, err := os.MkdirTemp("", "backpack-verified-*")
	if err != nil {
		return "", noop, err
	}
	cleanupDir := func() { _ = os.RemoveAll(dir) }

	archive, err := downloadAsset(tag, dir, logf)
	if err != nil {
		cleanupDir()
		return "", noop, err
	}
	sums, want, cerr := fetchChecksums(tag, filepath.Base(archive))
	if cerr != nil {
		cleanupDir()
		return "", noop, fmt.Errorf("could not fetch the checksum list for %s: %w", tag, cerr)
	}
	if want == "" {
		cleanupDir()
		return "", noop, fmt.Errorf("release %s publishes no checksum for %s", tag, filepath.Base(archive))
	}
	if serr := verifyChecksumSignature(tag, sums); serr != nil {
		cleanupDir()
		return "", noop, serr
	}
	if verr := verifyChecksum(archive, want); verr != nil {
		cleanupDir()
		return "", noop, fmt.Errorf("the downloaded release failed verification: %w", verr)
	}
	logf("Signature and checksum verified for " + tag + ".")

	dest := filepath.Join(dir, "backpack")
	if err := extractBinaryTo(archive, dest); err != nil {
		cleanupDir()
		return "", noop, err
	}
	_ = os.Remove(archive)
	return dest, cleanupDir, nil
}
