#!/usr/bin/env bash
# The release post-processing, one step per subcommand. The release
# workflow (.github/workflows/release.yml) and scripts/release-dry-run.sh
# both call it, so the dry run checks exactly what the release does. Run
# it from the repository root of a checkout that may be modified.
#
#   check-semver <semver>  exits non-zero unless semver is a v0/v1 version
#   modules                prints the released adapter directories
#   strip                  drops examples from go.work and removes tests,
#                          examples/, docs/ and tests/
#   tidy                   go mod tidy in every released module
#   pin <semver>           adapters require the root module at <semver>
#                          and drop their local replace
#   verify                 go build and go vet in every released module
#   tags <semver>          prints the tags of the release, one per line
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

adapters() {
	local dir
	for dir in http/*/ scheduler/*/ clock/*/; do
		[ -f "${dir}go.mod" ] && echo "${dir%/}"
	done
	return 0
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

# every released module directory, the root first
released() {
	echo .
	adapters
}

# go mod tidy everywhere while the adapters' local replace still points at
# the root: with the tests gone, test-only requirements leave every go.mod
tidy() {
	local dir
	while IFS= read -r dir; do
		echo "go mod tidy in $dir"
		(cd "$dir" && go mod tidy)
	done < <(released)
}

pin() {
	check_semver "$1"
	local dir
	# consumers ignore the adapters' local replace directives, so each
	# adapter requires the root module at the version being released, and
	# the replace goes: a released go.mod has none
	while IFS= read -r dir; do
		(cd "$dir" && go mod edit -require="$root_module@$1" -dropreplace="$root_module")
	done < <(adapters)
}

# build and vet every post-processed module on its own (GOWORK=off). The
# root release is not published yet, so an adapter builds with a temporary
# copy of its go.mod (-modfile) that points the root back at this tree; the
# released go.mod and go.sum stay as pinned.
verify() {
	local dir top tmp
	top="$(pwd)"
	tmp="$(mktemp -d)"
	# shellcheck disable=SC2064 # expand now: tmp is local
	trap "rm -rf '$tmp'" RETURN
	while IFS= read -r dir; do
		echo "go build and go vet in $dir"
		if [ "$dir" = . ]; then
			GOWORK=off go build ./...
			GOWORK=off go vet ./...
			continue
		fi
		mkdir -p "$tmp/$dir"
		cp "$dir/go.mod" "$tmp/$dir/go.mod"
		[ ! -f "$dir/go.sum" ] || cp "$dir/go.sum" "$tmp/$dir/go.sum"
		(
			cd "$dir"
			export GOWORK=off
			go mod edit -modfile="$tmp/$dir/go.mod" -replace="$root_module=$top"
			go build -modfile="$tmp/$dir/go.mod" ./...
			go vet -modfile="$tmp/$dir/go.mod" ./...
		)
	done < <(released)
}

tags() {
	check_semver "$1"
	local dir
	echo "$1"
	while IFS= read -r dir; do
		echo "$dir/$1"
	done < <(adapters)
}

cmd="${1:-}"
shift || true
case "$cmd" in
check-semver) check_semver "$@" ;;
modules) adapters ;;
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
