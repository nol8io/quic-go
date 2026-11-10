#!/usr/bin/env bash
# sync.sh moves the nol8 fork of quic-go to another upstream release.
#
#   nol8/sync.sh <upstream-tag>        build branch sync/<upstream-tag>
#   nol8/sync.sh --finish <worktree>   finish a sync after resolving conflicts
#   nol8/sync.sh rename [<dir>]        rename the module path in a checkout
#
# Run it from a clean checkout of master. master is the upstream release named
# in nol8/UPSTREAM plus the nol8 changes. sync.sh takes those changes (master
# compared with that release after the module rename), merges them into the
# new release after the same rename, following files upstream renamed, and
# commits the result as one commit on top of master, on branch
# sync/<upstream-tag>. Open a pull request from that branch.
# See "Updating to a new upstream release" in README.md.
#
# Exit status: 0 on success; 2 when the merge stops on conflicts for a person
# to resolve, and only then; any other non-zero status on failure.
set -euo pipefail
# The pathspecs below use glob and exclude magic, which these switch off.
unset GIT_LITERAL_PATHSPECS GIT_GLOB_PATHSPECS GIT_NOGLOB_PATHSPECS GIT_ICASE_PATHSPECS

upstream_url=${NOL8_UPSTREAM_URL:-https://github.com/quic-go/quic-go.git}

# The module path outside URLs, so links to upstream issues and docs keep
# pointing upstream; a glob's **/ prefix still matches.
module_re='(?<![\w.])(?<![\w.:/]/)github\.com/quic-go/quic-go(?![\w.-])'

# The upstream releases a sync fetches, under a name unique to this run.
sync_refs=refs/nol8-sync/$$

# sync_tmp is do_sync's temporary directory. It is global because the EXIT trap
# runs after do_sync's local variables are gone.
sync_tmp=

die() {
	echo "nol8/sync.sh: $*" >&2
	exit 1
}

usage() {
	echo "usage: nol8/sync.sh <upstream-tag>" >&2
	echo "       nol8/sync.sh --finish <worktree>" >&2
	echo "       nol8/sync.sh rename [<dir>]" >&2
	exit 1
}

# for_each_file runs a command on the NUL-separated file names on stdin, from
# directory $1, at most once, and not at all for an empty list.
for_each_file() {
	local dir=$1 f files=()
	shift
	while IFS= read -r -d '' f; do
		files+=("$f")
	done
	if [ ${#files[@]} -gt 0 ]; then
		(cd "$dir" && "$@" "${files[@]}")
	fi
}

# rename_module rewrites the module path in Go files, go.mod files, the lint
# configuration and the build scripts that name packages, then gofmts the
# changed Go files, whose imports may sort differently.
rename_module() {
	local dir=$1 flags
	# git doesn't see edits to files marked assume-unchanged, which
	# core.ignoreStat sets on checkout, or skip-worktree, which a sparse
	# checkout sets on the files it leaves out. ls-files -v tags those h, s,
	# m and S; a list, not a range, so the locale can't widen it.
	flags=$(git -C "$dir" ls-files -v) || die "git ls-files failed in $dir"
	if grep -q '^[hmsS] ' <<<"$flags"; then
		die "$dir has files marked assume-unchanged or skip-worktree (core.ignoreStat or a sparse checkout); use a full checkout without them"
	fi
	# perl -i only warns about a file it can't open, so check first.
	git -C "$dir" ls-files -z -- ':(glob)**/*.go' ':(glob)**/go.mod' \
		':(glob)**/modules.txt' ':(glob)**/.golangci.yml' \
		.github/workflows/unit.yml interop/Dockerfile .clusterfuzzlite/build.sh |
		for_each_file "$dir" perl -pi -e "BEGIN { for (@ARGV) { -f or die \"nol8/sync.sh: \$_ is missing\\n\" } }" \
			-e "s{$module_re}{github.com/nol8io/quic-go}g" -- ||
		die "renaming the module path in $dir failed"
	git -C "$dir" diff --no-color --no-ext-diff -z --name-only -- ':(glob)**/*.go' |
		for_each_file "$dir" gofmt -w -- ||
		die "gofmt failed in $dir"
}

# leftovers lists remaining references to the old module path in tracked files
# outside Markdown and nol8/, for a person to check: a new upstream file may
# need the new path.
leftovers() {
	local dir=$1
	git -C "$dir" ls-files -z -- . ':!:*.md' ':!:nol8/' |
		for_each_file "$dir" perl -ne "print \"\$ARGV:\$.: \$_\" if m{$module_re}; close ARGV if eof" --
}

# upstream_ref resolves an upstream release to a ref: a tag name, or a full ref.
upstream_ref() {
	case $1 in
	refs/*) echo "$1" ;;
	*) echo "refs/tags/$1" ;;
	esac
}

# sync_branch is the branch a sync to an upstream release creates.
sync_branch() {
	local name=${1#refs/tags/}
	echo "sync/${name#refs/heads/}"
}

drop_fetched() {
	git update-ref -d "$sync_refs/base" 2>/dev/null || true
	git update-ref -d "$sync_refs/target" 2>/dev/null || true
}

# on_exit removes do_sync's worktrees, temporary directory and fetched refs. A
# failure never exits 2, which means "conflicts to resolve".
on_exit() {
	local status=$?
	if [ -n "$sync_tmp" ]; then
		git worktree remove --force "$sync_tmp/base" 2>/dev/null || true
		git worktree remove --force "$sync_tmp/sync" 2>/dev/null || true
		rm -rf "$sync_tmp"
	fi
	drop_fetched
	if [ "$status" -eq 2 ]; then
		exit 1
	fi
}

# commit_tree runs git commit-tree, which needs user.name and user.email.
commit_tree() {
	git commit-tree "$@" || die "git commit-tree failed"
}

do_sync() {
	local target=$1 base head branch status base_commit changes renamed wt unmerged line
	cd "$(git rev-parse --show-toplevel)"
	[ "$(git symbolic-ref -q --short HEAD || true)" = master ] || die "check out master first"
	status=$(git status --porcelain --untracked-files=normal) || die "git status failed"
	[ -z "$status" ] || die "the checkout has uncommitted changes"
	[ -f nol8/UPSTREAM ] || die "nol8/UPSTREAM is missing"
	base=$(tr -d '[:space:]' <nol8/UPSTREAM)
	head=$(git rev-parse HEAD)
	branch=$(sync_branch "$target")
	git check-ref-format --branch "$branch" >/dev/null 2>&1 || die "bad upstream release name: $target"
	if git show-ref --verify --quiet "refs/heads/$branch"; then
		die "branch $branch already exists"
	fi

	# On any failure from here on, remove everything this run created; the
	# conflict exit below disarms this.
	trap on_exit EXIT
	# --no-tags keeps upstream tags out of this clone, so no later push can
	# publish them.
	git fetch --quiet --no-tags "$upstream_url" \
		"+$(upstream_ref "$base"):$sync_refs/base" \
		"+$(upstream_ref "$target"):$sync_refs/target"
	sync_tmp=$(mktemp -d "${TMPDIR:-/tmp}/nol8-sync.XXXXXX")
	wt=$sync_tmp/sync

	# The nol8 changes, as one commit on top of the renamed base release.
	git worktree add --quiet --detach "$sync_tmp/base" "$sync_refs/base^{commit}"
	rename_module "$sync_tmp/base"
	git -C "$sync_tmp/base" add -A
	base_commit=$(commit_tree "$(git -C "$sync_tmp/base" write-tree)" -m "upstream $base, renamed")
	git worktree remove --force "$sync_tmp/base"
	changes=$(commit_tree "$head^{tree}" -p "$base_commit" -m "nol8 changes from master")

	# The target release, renamed, as the worktree's HEAD.
	git worktree add --quiet --detach "$wt" "$sync_refs/target^{commit}"
	rename_module "$wt"
	git -C "$wt" add -A
	renamed=$(cd "$wt" && commit_tree "$(git write-tree)" -p HEAD -m "upstream $target, renamed")
	git -C "$wt" reset --quiet --soft "$renamed"
	printf '%s\n%s\n%s\n%s\n' "$target" "$head" "$(git rev-parse "$sync_refs/target^{commit}")" "$PWD" \
		>"$(git -C "$wt" rev-parse --absolute-git-dir)/nol8-sync"

	# Merge the nol8 changes into it: a three-way merge that follows renamed
	# files and stops on a renamed directory, whatever the user's rename and
	# rerere settings.
	if ! git -C "$wt" -c merge.renames=true -c merge.directoryRenames=conflict \
		-c merge.renameLimit=7000 -c rerere.enabled=false \
		cherry-pick --no-commit "$changes" >"$sync_tmp/merge.log" 2>&1; then
		unmerged=$(git -C "$wt" diff --no-color --name-only --diff-filter=U) || die "git diff failed"
		if [ -z "$unmerged" ]; then
			cat "$sync_tmp/merge.log" >&2
			die "merging the nol8 changes into $target failed"
		fi
		trap - EXIT
		drop_fetched
		echo
		echo "The nol8 changes conflict with $target in:"
		while IFS= read -r line; do
			echo "  $line"
		done <<<"$unmerged"
		grep '^CONFLICT' "$sync_tmp/merge.log" || true
		rm -f "$sync_tmp/merge.log"
		echo
		echo "Resolve them in $wt and stage the result (git add or git rm), then run:"
		printf '  nol8/sync.sh --finish %q\n' "$wt"
		echo "To give up instead, run:"
		printf '  git worktree remove --force %q && rmdir %q\n' "$wt" "$sync_tmp"
		exit 2
	fi
	rm -f "$sync_tmp/merge.log"
	do_finish "$wt"
}

do_finish() {
	local wt state target head upstream top tree commit branch left line found unmerged untracked
	[ -d "$1" ] || die "$1 doesn't exist"
	wt=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || die "$1 is not a worktree that sync.sh created"
	state=$(git -C "$wt" rev-parse --absolute-git-dir)/nol8-sync
	[ -f "$state" ] || die "$1 is not a worktree that sync.sh created"
	target=$(sed -n 1p "$state")
	head=$(sed -n 2p "$state")
	upstream=$(sed -n 3p "$state")
	top=$(sed -n 4p "$state")
	branch=$(sync_branch "$target")
	# Work from the checkout the sync started in, never from inside the
	# worktree this removes.
	cd "$top" || die "the checkout $top is gone"

	unmerged=$(git -C "$wt" diff --no-color --name-only --diff-filter=U) || die "git diff failed"
	[ -z "$unmerged" ] || die "conflicts remain in $wt; resolve them and stage the result first"
	untracked=$(git -C "$wt" ls-files --others --exclude-standard) || die "git ls-files failed"
	if [ -n "$untracked" ]; then
		die "untracked files in $wt; stage them with git add or delete them first:
$untracked"
	fi
	# Commit only what the person staged: an unstaged change, such as a file
	# a tmp cleaner removed, is refused rather than committed. The refresh
	# marks files whose timestamps changed but not their content as unchanged;
	# with -q it leaves real changes for diff-files to report, and fails only
	# on an error, such as another git holding the index lock. nol8/UPSTREAM is
	# left out: --finish writes it below, and a run stopped there leaves it
	# rewritten.
	git -C "$wt" update-index -q --refresh >/dev/null ||
		die "git update-index --refresh failed in $wt; is another git using it?"
	found=0
	git -C "$wt" diff-files --quiet -- . ':(exclude)nol8/UPSTREAM' || found=$?
	case $found in
	0) ;;
	1) die "unstaged changes in $wt; stage the result with git add or git rm first:
$(git -C "$wt" diff-files --name-status -- . ':(exclude)nol8/UPSTREAM')" ;;
	*) die "git diff-files failed in $wt" ;;
	esac
	if git show-ref --verify --quiet "refs/heads/$branch"; then
		die "branch $branch already exists"
	fi

	# A new regular file, whatever the person left there: the unstaged check
	# above skips this one.
	rm -f "$wt/nol8/UPSTREAM"
	printf '%s\n' "$target" >"$wt/nol8/UPSTREAM"
	git -C "$wt" add -- nol8/UPSTREAM || die "staging nol8/UPSTREAM failed in $wt; is another git using it?"
	found=0
	git -C "$wt" grep --cached -q -E '^(<<<<<<<|[|]{7}|>>>>>>>)( |$)' || found=$?
	case $found in
	0) die "conflict markers remain in $wt" ;;
	1) ;;
	*) die "git grep failed in $wt" ;;
	esac
	left=$(leftovers "$wt")
	tree=$(git -C "$wt" write-tree)
	if [ "$tree" = "$(git rev-parse "$head^{tree}")" ]; then
		git worktree remove --force "$wt"
		rmdir "$(dirname "$wt")" 2>/dev/null || true
		echo "master is already upstream $target plus the nol8 changes; nothing to do."
		return
	fi

	commit=$(commit_tree "$tree" -p "$head" -m "nol8: sync to upstream ${target#refs/tags/}" \
		-m "Upstream ${target#refs/tags/} ($upstream) with the module rename,
plus the nol8 changes from master ($head).
Built by nol8/sync.sh.")
	git branch "$branch" "$commit"
	git worktree remove --force "$wt"
	rmdir "$(dirname "$wt")" 2>/dev/null || true

	echo "Created branch $branch ($commit), one commit on top of master."
	git diff --no-color --shortstat "$head" "$commit"
	if [ -n "$left" ]; then
		echo
		echo "These files still name github.com/quic-go/quic-go outside a URL."
		echo "Check each, and fix any that need the new path on the branch:"
		while IFS= read -r line; do
			echo "  $line"
		done <<<"$left"
	fi
	echo
	echo "Next: run the checks, push $branch, open a pull request into master, and"
	echo "after the squash merge tag the next version on master (see Versions in README.md)."
}

[ $# -ge 1 ] || usage
case $1 in
--finish)
	[ $# -eq 2 ] || usage
	do_finish "$2"
	;;
rename)
	[ $# -le 2 ] || usage
	rename_module "${2:-$(git rev-parse --show-toplevel)}"
	;;
-*) usage ;;
*)
	[ $# -eq 1 ] || usage
	do_sync "$1"
	;;
esac
