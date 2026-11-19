#!/usr/bin/env bash
# The release post-processing, one step per subcommand. The release
# workflow (.github/workflows/release.yml) and scripts/release-dry-run.sh
# both call it, so the dry run checks exactly what the release does. Run
# it from the repository root of a checkout that may be modified.
#
#   check-semver <semver>  exits non-zero unless semver is a v0/v1 version
#   modules                prints every released module directory ("." for
#                          the root), each after the siblings it requires
#   strip                  drops examples from go.work and removes tests,
#                          examples/, docs/ and tests/
#   tidy                   go mod tidy in every released module
#   pin <semver>           every released module requires its released
#                          siblings at <semver> and has no replace left
#   verify                 go build and go vet in every released module
#   tags <semver>          prints the tags of the release, one per line
#
# The released modules are derived from the go.mod files: the root and
# every module below it outside examples/, docs/ and tests/. A released
# module may require released siblings (the root, clock/clockwork, ...);
# in the repository it reaches them through local replace directives.
# Needs go and jq.
set -euo pipefail

root_module="github.com/kartaladev/doger"

# the module path has no /vN suffix, so only v0 and v1 versions are valid;
# pre-releases such as v0.2.0-rc.1 are allowed
check_semver() {
	local re='^v[01]\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
	if ! [[ "${1:-}" =~ $re ]]; then
		echo "semver '${1:-}' must be a v0 or v1 version such as v0.1.0 or v0.2.0-rc.1 (module path $root_module has no major-version suffix)" >&2
		return 1
	fi
}

# module_dirs prints the directory of every released module, unordered:
# every go.mod outside the unreleased trees
module_dirs() {
	find . \( -path ./examples -o -path ./docs -o -path ./tests -o -name node_modules -o -path "./.*" \) -prune \
		-o -name go.mod -type f -print |
		sed 's|^\./||; s|/\{0,1\}go\.mod$||; s|^$|.|' | sort
}

# module_path prints the module path declared by directory $1.
module_path() {
	(cd "$1" && go mod edit -json) | jq -r '.Module.Path'
}

# dir_paths prints "<dir>\t<module path>" for every released module.
dir_paths() {
	local dir
	while IFS= read -r dir; do
		printf '%s\t%s\n' "$dir" "$(module_path "$dir")"
	done < <(module_dirs)
}

# sibling_requirements prints the released module paths that directory $1
# requires, given the "<dir>\t<path>" rows $2.
sibling_requirements() {
	(cd "$1" && go mod edit -json) |
		jq -r --arg paths "$(cut -f2 <<<"$2")" '
			($paths | split("\n")) as $released
			| .Require // [] | .[].Path | select(. as $p | $released | index($p))'
}

# modules prints the released directories in dependency order: each one
# after every released sibling it requires. A cycle is an error.
modules() {
	local rows dir path dep done_paths="" progress remaining
	rows="$(dir_paths)"
	remaining="$(cut -f1 <<<"$rows")"
	while [ -n "$remaining" ]; do
		progress=0
		local next=""
		while IFS= read -r dir; do
			path="$(awk -F'\t' -v d="$dir" '$1==d{print $2}' <<<"$rows")"
			local ready=1
			while IFS= read -r dep; do
				[ -n "$dep" ] || continue
				[ "$dep" != "$path" ] || continue
				grep -qxF "$dep" <<<"$done_paths" || ready=0
			done < <(sibling_requirements "$dir" "$rows")
			if [ "$ready" -eq 1 ]; then
				echo "$dir"
				done_paths+="$path"$'\n'
				progress=1
			else
				next+="$dir"$'\n'
			fi
		done <<<"$remaining"
		remaining="${next%$'\n'}"
		if [ "$progress" -eq 0 ]; then
			echo "release-steps.sh: a requirement cycle among: $(tr '\n' ' ' <<<"$remaining")" >&2
			return 1
		fi
	done
}

strip() {
	local dir
	# to be valid after cleanup
	while IFS= read -r dir; do
		go work edit -dropuse="$dir"
	done < <(find ./examples -type d)
	# remove tests to clean dependencies; example tests are documentation
	find . -type f -name '*_test.go' ! -name '*_example_test.go' -delete
	rm -rf examples/ docs/ tests/
}

# go mod tidy everywhere while the local replace directives still point at
# the siblings: with the tests gone, test-only requirements leave every
# go.mod
tidy() {
	local dir
	while IFS= read -r dir; do
		echo "go mod tidy in $dir"
		(cd "$dir" && go mod tidy)
	done < <(modules)
}

# Consumers ignore replace directives, so every released module requires
# each released sibling it uses at the version being released, and its
# replace directives go: a released go.mod has none.
pin() {
	check_semver "$1"
	local rows dir dep old
	local -a edits
	rows="$(dir_paths)"
	while IFS= read -r dir; do
		edits=()
		while IFS= read -r dep; do
			[ -n "$dep" ] && edits+=("-require=$dep@$1")
		done < <(sibling_requirements "$dir" "$rows")
		while IFS= read -r old; do
			[ -n "$old" ] || continue
			if ! grep -qxF "$old" <<<"$(cut -f2 <<<"$rows")"; then
				echo "release-steps.sh: $dir/go.mod replaces $old, which is not a released sibling; a released go.mod has no replace" >&2
				return 1
			fi
			edits+=("-dropreplace=$old")
		done < <((cd "$dir" && go mod edit -json) | jq -r '.Replace // [] | .[].Old.Path')
		if [ "${#edits[@]}" -gt 0 ]; then
			echo "pin in $dir: ${edits[*]}"
			(cd "$dir" && go mod edit "${edits[@]}")
		fi
	done < <(modules)
}

# Build and vet every post-processed module on its own (GOWORK=off). The
# release is not published yet, so each module builds with a temporary
# copy of its go.mod (-modfile) that points every released sibling back at
# this tree; the released go.mod and go.sum stay as pinned.
verify() {
	local rows dir top tmp sib_dir sib_path
	local -a replaces
	top="$(pwd)"
	rows="$(dir_paths)"
	replaces=()
	while IFS=$'\t' read -r sib_dir sib_path; do
		replaces+=("-replace=$sib_path=$top/$sib_dir")
	done <<<"$rows"
	tmp="$(mktemp -d)"
	# shellcheck disable=SC2064 # expand now: tmp is local
	trap "rm -rf '$tmp'" RETURN
	while IFS= read -r dir; do
		echo "go build and go vet in $dir"
		mkdir -p "$tmp/$dir"
		cp "$dir/go.mod" "$tmp/$dir/go.mod"
		[ ! -f "$dir/go.sum" ] || cp "$dir/go.sum" "$tmp/$dir/go.sum"
		(
			cd "$dir"
			export GOWORK=off
			go mod edit -modfile="$tmp/$dir/go.mod" "${replaces[@]}"
			go build -modfile="$tmp/$dir/go.mod" ./...
			go vet -modfile="$tmp/$dir/go.mod" ./...
		)
	done < <(modules)
}

# the root module is tagged <semver>, every other one <dir>/<semver>
tags() {
	check_semver "$1"
	local dir
	while IFS= read -r dir; do
		if [ "$dir" = . ]; then
			echo "$1"
		else
			echo "$dir/$1"
		fi
	done < <(modules)
}

cmd="${1:-}"
shift || true
case "$cmd" in
check-semver) check_semver "$@" ;;
modules) modules ;;
strip) strip ;;
tidy) tidy ;;
pin) pin "$@" ;;
verify) verify ;;
tags) tags "$@" ;;
*)
	echo "usage: $0 check-semver|modules|strip|tidy|pin|verify|tags [semver]" >&2
	exit 2
	;;
esac
