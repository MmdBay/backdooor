package app

import "testing"

func TestRefuseUpstreamTrustRejectsAminKey(t *testing.T) {
	saved := ReleasePublicKey
	t.Cleanup(func() { ReleasePublicKey = saved })

	ReleasePublicKey = UpstreamReleasePublicKey
	if err := RefuseUpstreamTrust(); err == nil {
		t.Fatal("accepted the AminMGMT ReleasePublicKey")
	}

	ReleasePublicKey = ""
	if err := RefuseUpstreamTrust(); err == nil {
		t.Fatal("accepted an empty ReleasePublicKey")
	}

	ReleasePublicKey = saved
	if RepoOwner == UpstreamRepoOwner {
		t.Fatal("RepoOwner still points at AminMGMT")
	}
	if err := RefuseUpstreamTrust(); err != nil {
		t.Fatalf("MmdBay identity with fork key should be accepted: %v", err)
	}
}

func TestShowcaseURLIsEmpty(t *testing.T) {
	if ShowcaseURL != "" {
		t.Fatalf("ShowcaseURL must be empty on this fork, got %q", ShowcaseURL)
	}
}
