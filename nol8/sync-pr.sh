#!/usr/bin/env bash
# sync-pr.sh opens a pull request that moves this fork to upstream's newest
# release. The "Upstream sync" job in .github/workflows/nol8-upstream.yml runs
# it every month and on demand; see "Updating to a new upstream release" in
# README.md.
#
# It compares upstream's newest release vX.Y.Z with nol8/UPSTREAM. When upstream
# is ahead and this repository has no branch sync/vX.Y.Z, it runs
# nol8/sync.sh vX.Y.Z from a clean checkout of master, pushes the branch it
# builds and opens a pull request into master. When the nol8 changes conflict
# with the new release, it pushes nothing and fails, naming the files; a person
# then runs nol8/sync.sh.
#
# Environment:
#   GH_TOKEN            a token that may push branches, including changes to
#                       workflows, and open pull requests in this repository
#   GITHUB_REPOSITORY   this repository, owner/name
#   GITHUB_SERVER_URL   default https://github.com
#   NOL8_UPSTREAM_URL   default https://github.com/quic-go/quic-go.git
#   GITHUB_STEP_SUMMARY, GITHUB_RUN_ID   set by GitHub Actions; optional
#
# Exit status: 0 when master is on upstream's newest release, when branch
# sync/vX.Y.Z already has a pull request (open or closed), or when this run
# opened one; 1 otherwise.
set -euo pipefail

upstream_url=${NOL8_UPSTREAM_URL:-https://github.com/quic-go/quic-go.git}
upstream_web=https://github.com/quic-go/quic-go
server=${GITHUB_SERVER_URL:-https://github.com}
release_re='^v[0-9]+\.[0-9]+\.[0-9]+$'

# summary adds Markdown to the job summary when there is one.
summary() {
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
		printf '%s\n' "$@" >>"$GITHUB_STEP_SUMMARY"
	fi
}

die() {
	echo "nol8/sync-pr.sh: $*" >&2
	summary "nol8/sync-pr.sh: $*"
	exit 1
}

# git_gh runs git with GH_TOKEN, served by gh, as its only credential.
git_gh() {
	git -c credential.helper= -c 'credential.helper=!gh auth git-credential' "$@"
}

# newest prints the highest of the vX.Y.Z versions on stdin.
newest() {
	sort -t. -k1.2,1n -k2,2n -k3,3n | tail -n 1
}

[ $# -eq 0 ] || die "usage: nol8/sync-pr.sh"
[ -n "${GH_TOKEN:-}" ] || die "GH_TOKEN is empty; in GitHub Actions, add the token as the repository secret NOL8_SYNC_TOKEN (see README.md)"
repo=${GITHUB_REPOSITORY:-}
[[ $repo =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "GITHUB_REPOSITORY must be owner/name"
for tool in git gh; do
	command -v "$tool" >/dev/null || die "$tool isn't on PATH"
done
fork_url=$server/$repo.git
cd "$(git rev-parse --show-toplevel)"

# Fail every run, not only a run that finds a release, while the token is
# missing, expired or revoked.
gh api "repos/$repo" --silent || die "GitHub refused GH_TOKEN; it may have expired. Replace it (see README.md)"

current=$(tr -d '[:space:]' <nol8/UPSTREAM) || die "can't read nol8/UPSTREAM"
[[ $current =~ $release_re ]] || die "nol8/UPSTREAM holds '$current', not an upstream release vX.Y.Z"
tags=$(git ls-remote --tags --refs "$upstream_url") || die "can't list upstream's tags"
latest=$(awk '{ sub("^refs/tags/", "", $2); print $2 }' <<<"$tags" | grep -E "$release_re" | newest || true)
[ -n "$latest" ] || die "upstream has no release vX.Y.Z"
if [ "$(printf '%s\n%s\n' "$current" "$latest" | newest)" = "$current" ]; then
	echo "master is on upstream $current, and upstream's newest release is $latest; nothing to do."
	summary "master is on upstream \`$current\`, upstream's newest release; nothing to do."
	exit 0
fi

branch=sync/$latest
found=0
git_gh ls-remote --exit-code --heads "$fork_url" "refs/heads/$branch" >/dev/null || found=$?
case $found in
0)
	# A pull request from the branch, open or closed, means the sync is in
	# hand or was skipped. A branch without one is left from a failed run or
	# pushed by hand: report it rather than stop syncing without a word.
	prs=$(gh pr list --repo "$repo" --head "$branch" --state all --json number --jq length) ||
		die "can't list the pull requests from $branch"
	[ "$prs" != 0 ] ||
		die "branch $branch exists but has no pull request; open one from it, or delete it so the next run starts over"
	echo "Branch $branch already has a pull request, so the sync to $latest is in hand; nothing to do."
	summary "Branch \`$branch\` already has a pull request, so the sync to upstream \`$latest\` is in hand; nothing to do."
	exit 0
	;;
2) ;;
*) die "can't list the branches of $fork_url" ;;
esac

log=$(mktemp "${TMPDIR:-/tmp}/nol8-sync-pr.XXXXXX")
body=$(mktemp "${TMPDIR:-/tmp}/nol8-sync-pr.XXXXXX")
trap 'rm -f "$log" "$body"' EXIT

status=0
nol8/sync.sh "$latest" >"$log" 2>&1 || status=$?
cat "$log"
case $status in
0) ;;
2)
	summary "## The nol8 changes conflict with upstream $latest" "" \
		"No pull request was opened. Finish the sync by hand, as in \"Updating to a new upstream release\" in README.md:" "" \
		"1. In a clean checkout of \`master\`, run \`nol8/sync.sh $latest\`." \
		"2. Resolve the conflicts in the worktree it prints, stage the result and run \`nol8/sync.sh --finish <worktree>\`." \
		"3. Push \`$branch\` with \`--no-follow-tags\` and open a pull request into \`master\`." "" \
		'```' "$(cat "$log")" '```'
	die "the nol8 changes conflict with upstream $latest; finish the sync by hand (see README.md)"
	;;
*) die "nol8/sync.sh $latest failed" ;;
esac
if ! git show-ref --verify --quiet "refs/heads/$branch"; then
	summary "master already holds upstream \`$latest\` plus the nol8 changes; nothing to do."
	exit 0
fi

cat >"$body" <<EOF
Upstream quic-go [$latest]($upstream_web/releases/tag/$latest), with the module rename and the nol8 changes, built by \`nol8/sync.sh\`${GITHUB_RUN_ID:+ in [this workflow run]($server/$repo/actions/runs/$GITHUB_RUN_ID)}. \`nol8/UPSTREAM\` moves from \`$current\` to \`$latest\`.

Before merging:

- Read upstream's [release notes]($upstream_web/releases/tag/$latest) and [changes since $current]($upstream_web/compare/$current...$latest), above all those to the code that change 1 in README.md touches.
- If the output below lists files that still name \`github.com/quic-go/quic-go\` outside a URL, fix those that need the new path on this branch.
- Keep README.md's list of changes true to the branch.
- CI must pass. "Upstream advisories" checks \`$latest\`.

After the squash merge, tag the merge commit \`$latest-nol8.1\` and push only that tag (see "Versions" in README.md).

<details><summary>nol8/sync.sh output</summary>

\`\`\`
$(cat "$log")
\`\`\`

</details>
EOF

# --no-follow-tags keeps every local tag out of the fork.
git_gh push --quiet --no-follow-tags "$fork_url" "refs/heads/$branch:refs/heads/$branch" ||
	die "pushing $branch failed; the token needs write access to contents, pull requests and workflows"
# GitHub can briefly refuse a pull request from a branch just pushed.
url=
for attempt in 1 2 3; do
	if url=$(gh pr create --repo "$repo" --base master --head "$branch" \
		--title "nol8: sync to upstream $latest" --body-file "$body"); then
		break
	fi
	url=
	if [ "$attempt" -lt 3 ]; then
		sleep $((attempt * 5))
	fi
done
if [ -z "$url" ]; then
	# Remove the branch, so that the next run tries again.
	git_gh push --quiet --no-follow-tags "$fork_url" --delete "refs/heads/$branch" ||
		echo "nol8/sync-pr.sh: removing $branch failed too; delete it so the next run can try again" >&2
	die "opening the pull request for $branch failed"
fi
echo "Opened $url"
summary "Opened $url: the sync to upstream \`$latest\`."
