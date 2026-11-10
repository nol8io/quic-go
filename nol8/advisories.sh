#!/usr/bin/env bash
# nol8/advisories.sh [vX.Y.Z]
#
# Checks an upstream quic-go release against the Go vulnerability database: by
# default the release this fork is built on, named in nol8/UPSTREAM. govulncheck
# matches advisories by module path, and upstream's name
# github.com/quic-go/quic-go, so a scan of this fork never reports them. This
# script scans a throwaway module that requires upstream at that release.
#
# It reports only upstream quic-go's own advisories. Its dependencies keep their
# module paths in this fork, so govulncheck ./... in a checkout covers those.
#
# Exit status: 0 when no advisory affects the release, 3 when one does (as with
# govulncheck), 1 on any other failure. It needs go, govulncheck and jq on PATH,
# and access to the Go module proxy and vuln.go.dev.
set -euo pipefail

die() {
	echo "nol8/advisories.sh: $*" >&2
	exit 1
}

upstream=github.com/quic-go/quic-go

case $# in
0)
	root=$(git rev-parse --show-toplevel 2>/dev/null) ||
		die "run it in a checkout of the fork, or name a release"
	version=$(cat "$root/nol8/UPSTREAM") || die "can't read nol8/UPSTREAM"
	;;
1) version=$1 ;;
*) die "usage: nol8/advisories.sh [vX.Y.Z]" ;;
esac
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "'$version' isn't an upstream release (vX.Y.Z)"

for tool in go govulncheck jq; do
	command -v "$tool" >/dev/null || die "$tool isn't on PATH"
done

tmp=$(mktemp -d) || die "mktemp failed"
trap 'rm -rf "$tmp"' EXIT
export GOWORK=off GOFLAGS=

# Run a step quietly, and show its output only if it fails.
quiet() {
	"$@" >"$tmp/log" 2>&1 || {
		cat "$tmp/log" >&2
		die "$* failed"
	}
}

cd "$tmp"
quiet go mod init nol8.invalid/advisories
# govulncheck loads packages even for a module scan, so import upstream once.
printf 'package advisories\n\nimport _ "%s"\n' "$upstream" >advisories.go
quiet go get "$upstream@$version"
quiet go mod tidy
govulncheck -scan module -format json >scan.json 2>"$tmp/log" || {
	cat "$tmp/log" >&2
	die "govulncheck failed"
}

found=$(jq -rs --arg m "$upstream" '
	(map(select(.osv)) | map({key: .osv.id, value: .osv.summary}) | from_entries) as $summary
	| map(select(.finding.trace[0].module == $m) | .finding)
	| unique_by(.osv)[]
	| "  \(.osv): \($summary[.osv] // "")\n    fixed in \(.fixed_version // "no release yet"); https://pkg.go.dev/vuln/\(.osv)"
' scan.json) || die "can't read govulncheck's output"

if [ -z "$found" ]; then
	echo "No advisory affects upstream $upstream $version."
	exit 0
fi
echo "These advisories affect upstream $upstream $version, and so this fork:"
printf '%s\n' "$found"
exit 3
