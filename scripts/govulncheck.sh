#!/usr/bin/env bash
# Runs govulncheck in the current module and fails when the module's code
# reaches a known vulnerability (a symbol-level finding), unless SECURITY.md
# accepts that advisory for this module under "Accepted advisories".
# Findings in imported packages or required modules that the code does not
# call are listed without failing.
#
# The module is scanned on its own (GOWORK=off), with the requirements its
# consumers get: inside the workspace, another module's newer requirement
# would hide a vulnerable one. Local replace directives keep the root
# module resolvable.
#
# Usage:
#   scripts/govulncheck.sh [packages]       from a module directory
#   scripts/govulncheck.sh --accepted FILE  prints "<id>\t<module dir>" for
#                                           every accepted advisory of FILE
# Needs govulncheck and jq.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd -P)"
repo="$(cd "$script_dir/.." && pwd -P)"

# accepted_rows prints "<id>\t<module dir>" for every row of the "Accepted
# advisories" table of SECURITY.md file $1: the first column holds the
# GO-YYYY-NNNN id, the second exactly one module directory in backticks.
accepted_rows() {
	awk -F'|' '
		/^#+ .*Accepted advisories/ { on = 1; next }
		on && /^#+ / { exit }
		on && NF >= 4 {
			if (match($2, /GO-[0-9][0-9][0-9][0-9]-[0-9]+/) == 0) next
			id = substr($2, RSTART, RLENGTH)
			mod = $3; gsub(/[` \t]/, "", mod)
			if (mod !~ /^\.(\/[A-Za-z0-9._-]+)*$/) { print "bad module cell for " id ": " $3 > "/dev/stderr"; bad = 1; next }
			print id "\t" mod
		}
		END { exit bad }
	' "$1"
}

if [ "${1:-}" = --accepted ]; then
	accepted_rows "${2:?usage: $0 --accepted FILE}"
	exit
fi

for tool in govulncheck jq; do
	command -v "$tool" >/dev/null || {
		echo "$tool is required (make govulncheck-install installs govulncheck)" >&2
		exit 2
	}
done

# this module's directory relative to the repository, as make modules
# prints it
here="$(pwd -P)"
if [ "$here" = "$repo" ]; then
	module="."
else
	module=".${here#"$repo"}"
fi

accepted=""
if [ -f "$repo/SECURITY.md" ]; then
	accepted="$(accepted_rows "$repo/SECURITY.md")"
fi

out="$(mktemp)"
trap 'rm -f "$out"' EXIT
GOWORK=off govulncheck -format json "${@:-./...}" >"$out"

# id<TAB>level<TAB>module<TAB>fixed version, one row per (id, level)
rows="$(jq -r '
	select(.finding) | .finding
	| (if (.trace[0].function // "") != "" then "called"
	   elif (.trace[0].package // "") != "" then "imported"
	   else "required" end) as $level
	| [.osv, $level, (.trace[0].module // "?"), (.fixed_version // "none")] | @tsv
' "$out" | sort -u)"

called="$(awk -F'\t' '$2=="called"' <<<"$rows")"
status=0
if [ -n "$called" ]; then
	while IFS=$'\t' read -r id _ dep fixed; do
		if grep -qxF "$id	$module" <<<"$accepted"; then
			echo "accepted  $id in $dep for $module (fixed in: $fixed; see SECURITY.md)"
		else
			echo "VULNERABLE  $id in $dep is reachable from $module (fixed in: $fixed): https://pkg.go.dev/vuln/$id"
			status=1
		fi
	done <<<"$called"
fi

# not reachable: informational only
awk -F'\t' '$2!="called"{print $1}' <<<"$rows" | sort -u | while read -r id; do
	[ -n "$id" ] || continue
	grep -qxF "$id" <<<"$(cut -f1 <<<"$called")" && continue
	echo "not called  $id ($(awk -F'\t' -v id="$id" '$1==id{print $2; exit}' <<<"$rows") only)"
done

if [ "$status" -eq 0 ]; then
	echo "no reachable vulnerability outside the accepted advisories"
fi
exit "$status"
