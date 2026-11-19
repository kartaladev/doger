#!/usr/bin/env bash
# Lists the workspace modules or runs a command in each of them.
#
#   scripts/modules.sh list
#       prints the directory of every go.work use entry, relative to the
#       repository root ("." for the root, "./http/chi", ...), one per line
#
#   scripts/modules.sh run <command> [args...]
#       runs the command in every module, in go.work order. A failing module
#       does not stop the loop: every module runs, then the failing ones are
#       named and the script exits non-zero.
#
# The module list comes from `go list -m -f '{{.Dir}}'`, so it is exactly
# the go.work use entries. Paths are normalized to forward slashes, so the
# output is the same on Windows (Git Bash) as on Linux and macOS.
set -euo pipefail

root_dir() {
	go list -m -f '{{.Dir}}' github.com/kartaladev/doger
}

# prints "<relative dir>\t<absolute dir>" for every workspace module
module_rows() {
	local root dir rel
	root="$(root_dir)"
	while IFS= read -r dir; do
		[ -n "$dir" ] || continue
		dir="${dir%$'\r'}"
		if [ "$dir" = "$root" ]; then
			rel="."
		else
			rel="${dir#"$root"}"
			rel=".${rel//\\//}"
		fi
		printf '%s\t%s\n' "$rel" "$dir"
	done < <(go list -m -f '{{.Dir}}')
}

cmd_list() {
	module_rows | cut -f1
}

cmd_run() {
	if [ "$#" -eq 0 ]; then
		echo "usage: $0 run <command> [args...]" >&2
		return 2
	fi

	local rel dir total=0
	local -a failed=()
	while IFS=$'\t' read -r rel dir; do
		total=$((total + 1))
		echo "==> ${rel}: $*"
		if ! (cd "$dir" && "$@" </dev/null); then
			echo "FAIL ${rel}"
			failed+=("$rel")
		fi
	done < <(module_rows)

	echo
	if [ "${#failed[@]}" -gt 0 ]; then
		echo "${#failed[@]} of ${total} modules failed: $*"
		printf 'FAIL module %s\n' "${failed[@]}"
		return 1
	fi
	echo "all ${total} modules passed: $*"
}

case "${1:-}" in
list)
	shift
	cmd_list "$@"
	;;
run)
	shift
	cmd_run "$@"
	;;
*)
	echo "usage: $0 list | run <command> [args...]" >&2
	exit 2
	;;
esac
