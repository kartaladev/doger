#!/usr/bin/env bash
# Runs govulncheck in the current module and fails when the module's code
# reaches a known vulnerability (a symbol-level finding), unless SECURITY.md
# lists the advisory under "Accepted advisories". Findings in imported
# packages or required modules that the code does not call are listed
# without failing.
#
# Usage (from a module directory): scripts/govulncheck.sh [packages]
# Needs govulncheck and jq.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
security_md="$script_dir/../SECURITY.md"

for tool in govulncheck jq; do
	command -v "$tool" >/dev/null || {
		echo "$tool is required (make tools installs govulncheck)" >&2
		exit 2
	}
done

# advisories accepted in SECURITY.md: every GO-YYYY-NNNN id in the
# "Accepted advisories" section
accepted=""
if [ -f "$security_md" ]; then
	accepted="$(awk '/^#+ .*Accepted advisories/{on=1; next} on && /^#+ /{exit} on' "$security_md" |
		grep -oE 'GO-[0-9]{4}-[0-9]+' | sort -u || true)"
fi

out="$(mktemp)"
trap 'rm -f "$out"' EXIT
govulncheck -format json "${@:-./...}" >"$out"

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
	while IFS=$'\t' read -r id _ module fixed; do
		if grep -qxF "$id" <<<"$accepted"; then
			echo "accepted  $id in $module (fixed in: $fixed; see SECURITY.md)"
		else
			echo "VULNERABLE  $id in $module is reachable (fixed in: $fixed): https://pkg.go.dev/vuln/$id"
			status=1
		fi
	done <<<"$called"
fi

# not reachable: informational only
awk -F'\t' '$2!="called"{print $1}' <<<"$rows" | sort -u | while read -r id; do
	grep -qxF "$id" <<<"$(cut -f1 <<<"$called")" && continue
	echo "not called  $id ($(awk -F'\t' -v id="$id" '$1==id{print $2; exit}' <<<"$rows" ) only)"
done

if [ "$status" -eq 0 ]; then
	echo "no reachable vulnerability outside the accepted advisories"
fi
exit "$status"
