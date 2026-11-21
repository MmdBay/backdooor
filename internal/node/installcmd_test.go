package node

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/backpack/backpack/internal/app"
)

// An upgrade must run signed self-update, then restart tunnels. It must never
// curl|bash install.sh from main.
func TestTheUpgradeCommandUsesSelfUpdate(t *testing.T) {
	line := upgradeCommand("/usr/local/bin/backpack")
	if strings.Contains(line, "install.sh") || strings.Contains(line, "curl") {
		t.Fatalf("upgrade still fetches an installer script: %s", line)
	}
	if !strings.Contains(line, "self-update") {
		t.Fatalf("upgrade does not run self-update: %s", line)
	}
	if !strings.Contains(line, "--restart-all") {
		t.Fatalf("upgrade does not restart tunnels after self-update: %s", line)
	}
}

// An upgrade restarts what runs the binary once self-update has replaced it.
func TestTheUpgradeCommandRestartsTheTunnelsAfterSelfUpdate(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	bin := filepath.Join(dir, "backpack")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = self-update ]; then echo updated >> " + log + "; exit 0; fi\n" +
		"if [ \"$1\" = --restart-all ]; then echo \"bin $*\" >> " + log + "; exit 0; fi\n" +
		"echo unexpected \"$@\" >> " + log + "; exit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("sh", "-c", upgradeCommand(bin)).CombinedOutput()
	if err != nil {
		t.Fatalf("the upgrade line failed: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(log)
	if string(got) != "updated\nbin --restart-all\n" {
		t.Fatalf("the upgrade did %q; want self-update, then --restart-all", got)
	}
}

// If self-update fails, nothing is restarted onto a half-installed binary.
func TestTheUpgradeCommandStopsWhenSelfUpdateFails(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	bin := filepath.Join(dir, "backpack")
	_ = os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = self-update ]; then exit 3; fi\necho \"bin $*\" >> "+log+"\n"), 0o755)

	if err := exec.Command("sh", "-c", upgradeCommand(bin)).Run(); err == nil {
		t.Fatal("a failing self-update did not fail the upgrade")
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("the tunnels were restarted after self-update failed")
	}
}

func TestRefuseUpstreamBlocksFleetWhenMiswired(t *testing.T) {
	savedKey := app.ReleasePublicKey
	t.Cleanup(func() { app.ReleasePublicKey = savedKey })
	app.ReleasePublicKey = app.UpstreamReleasePublicKey
	if err := app.RefuseUpstreamTrust(); err == nil {
		t.Fatal("RefuseUpstreamTrust accepted the AminMGMT public key")
	}
}
