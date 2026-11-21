#!/usr/bin/env bash
#
# Build a multi-arch release locally and publish it to GitHub Releases.
#
#   scripts/publish-release.sh [tag]
#
# Default tag: exact tag on HEAD, else the newest v* tag that points at HEAD.
#
# Requires: go, zig, gcc-arm-linux-gnueabi (for armv5), gh (authenticated),
# and RELEASE_SIGNING_KEY (or RELEASE_SIGNING_KEY_FILE). This fork refuses to
# publish unsigned releases — see internal/app.ReleasePublicKey.
#
# Environment:
#   GH_REPO                 GitHub repository (default: MmdBay/backpack)
#   RELEASE_SIGNING_KEY     base64 Ed25519 private key (64 bytes decoded)
#   RELEASE_SIGNING_KEY_FILE path to a local untracked file with the key
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

GH_REPO="${GH_REPO:-MmdBay/backpack}"
TAG="${1:-}"

die() {
  echo "publish-release.sh: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing $1 on PATH"
}

app_version() {
  grep -oE 'Version = "v[^"]+"' internal/app/app.go | head -1 | grep -oE 'v[0-9.]+'
}

resolve_tag() {
  if [ -n "$TAG" ]; then
    return
  fi
  if TAG="$(git describe --tags --exact-match HEAD 2>/dev/null)"; then
    return
  fi
  TAG="$(git tag --points-at HEAD 2>/dev/null | grep -E '^v' | sort -V | tail -1 || true)"
  [ -n "$TAG" ] || die "no tag argument and HEAD has no v* tag — tag first: git tag -a vX.Y.Z -m \"vX.Y.Z\""
}

load_signing_key() {
  if [ -n "${RELEASE_SIGNING_KEY:-}" ]; then
    export RELEASE_SIGNING_KEY
    return
  fi
  local keyfile="${RELEASE_SIGNING_KEY_FILE:-}"
  if [ -z "$keyfile" ] && [ -f "${HOME}/.config/backpack/release-signing-key" ]; then
    keyfile="${HOME}/.config/backpack/release-signing-key"
  fi
  if [ -n "$keyfile" ]; then
    [ -f "$keyfile" ] || die "RELEASE_SIGNING_KEY_FILE not found: $keyfile"
    RELEASE_SIGNING_KEY="$(tr -d '[:space:]' <"$keyfile")"
    export RELEASE_SIGNING_KEY
    return
  fi
  die "set RELEASE_SIGNING_KEY or RELEASE_SIGNING_KEY_FILE (see CONTRIBUTING.md — local release publish)"
}

check_prereqs() {
  need_cmd go
  need_cmd gh
  if ! command -v zig >/dev/null 2>&1; then
    cat >&2 <<'EOF'
publish-release.sh: zig is required for multi-arch release builds (libpcap + musl cross-compile).

Install from https://ziglang.org/download/ (0.17.x matches CONTRIBUTING.md) and put zig on PATH.

Examples:
  # Official tarball — unpack and add the zig binary directory to PATH
  curl -LO https://ziglang.org/download/0.17.0/zig-x86_64-linux-0.17.0.tar.xz
  tar -xf zig-x86_64-linux-0.17.0.tar.xz
  export PATH="$PWD/zig-x86_64-linux-0.17.0:$PATH"
EOF
    exit 2
  fi
  if ! command -v arm-linux-gnueabi-gcc >/dev/null 2>&1; then
    die "missing arm-linux-gnueabi-gcc on PATH (apt install gcc-arm-linux-gnueabi libc6-dev-armel-cross)"
  fi
  if ! gh auth status >/dev/null 2>&1; then
    die "gh is not authenticated — run: gh auth login"
  fi
  load_signing_key
  if [ -z "${RELEASE_SIGNING_KEY:-}" ]; then
    die "RELEASE_SIGNING_KEY is empty"
  fi
}

validate_tag_alignment() {
  [[ "$TAG" == v* ]] || die "tag must look like v1.2.3 (got: $TAG)"
  local ver
  ver="$(app_version)" || die "could not read Version from internal/app/app.go"
  [ "$TAG" = "$ver" ] || die "tag $TAG does not match app.Version $ver in internal/app/app.go — fix one before publishing"

  if ! git rev-parse "refs/tags/$TAG" >/dev/null 2>&1; then
    die "no local tag refs/tags/$TAG — create: git tag -a $TAG -m \"$TAG\""
  fi
  local tag_commit head_commit
  tag_commit="$(git rev-parse "refs/tags/$TAG^{commit}")"
  head_commit="$(git rev-parse HEAD)"
  [ "$tag_commit" = "$head_commit" ] || die "HEAD is not at tag $TAG (tag -> ${tag_commit:0:12}, HEAD -> ${head_commit:0:12})"
}

wait_for_remote_tag() {
  local remote="${1:-origin}"
  local i
  for i in $(seq 1 90); do
    if git ls-remote --exit-code "$remote" "refs/tags/$TAG" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  die "tag $TAG not on $remote after waiting — push it first: git push origin $TAG"
}

upload_release() {
  shopt -s nullglob
  local -a assets=(release/backpack_linux_*.tar.gz)
  shopt -u nullglob
  local fixed=(release/SHA256SUMS release/SHA256SUMS.sig release/SBOM.txt)
  local a
  for a in "${fixed[@]}"; do
    [ -f "$a" ] || die "missing asset: $a"
    assets+=("$a")
  done
  [ "${#assets[@]}" -ge 4 ] || die "release/ assets incomplete — did make release succeed?"

  if [ ! -f release/SHA256SUMS.sig ]; then
    die "release/SHA256SUMS.sig missing — RELEASE_SIGNING_KEY must be set for this fork"
  fi

  if gh release view "$TAG" -R "$GH_REPO" >/dev/null 2>&1; then
    echo "Release $TAG exists on $GH_REPO — uploading assets (clobber)"
    gh release upload "$TAG" -R "$GH_REPO" --clobber "${assets[@]}"
  else
    echo "Creating GitHub release $TAG on $GH_REPO"
    gh release create "$TAG" -R "$GH_REPO" \
      --title "$TAG" \
      --notes "Backpack $TAG (local multi-arch build from MmdBay/backpack fork)." \
      "${assets[@]}"
  fi
}

resolve_tag
check_prereqs
validate_tag_alignment

echo "Publishing $TAG to $GH_REPO…"
wait_for_remote_tag origin

echo "Building release assets (make release)…"
export GITHUB_REF_NAME="$TAG"
make release

upload_release

echo "Done: https://github.com/$GH_REPO/releases/tag/$TAG"