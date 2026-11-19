#!/usr/bin/env bash
# Checks the CI supply chain and the repository's policy files against the
# release-integrity spec. Every finding is printed as "FAIL [section] ...",
# naming the file (and line where there is one); the script exits non-zero
# when there is any.
#
# Usage:
#   scripts/check-workflows.sh [--root DIR] [section...]
#   scripts/check-workflows.sh --self-test
#
# Sections (default: all of them, in this order):
#   pinning      every "uses:" names a 40-char commit SHA and a "# v" comment
#   permissions  top-level "permissions: contents: read"; raises only on the
#                allowed jobs
#   matrix       test.yml runs make test-all on 3 OSes x {1.27.x, stable};
#                lint.yml runs make lint-all; security.yml runs make vuln
#   release      release.yml provenance (main only, release environment,
#                tidy before pinning, build/vet after, make vuln before
#                tagging, generated notes, clock/* tagged)
#   dependabot   auto-merge only for dependabot[bot] patch/minor updates;
#                a gomod entry covers every module directory
#   floors       minimum versions of vulnerable dependencies in go.mod files
#   policy       SECURITY.md, CHANGELOG.md, .github/CODEOWNERS, no
#                vercel.json, Bearer at its default log level
#
# The workflows are parsed line by line, assuming the repository's layout:
# two-space indentation, top-level keys at column 0 and jobs at column 2.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
all_sections=(pinning permissions matrix release dependabot floors policy)

failures=0
fail() {
	echo "FAIL [$section] $*"
	failures=$((failures + 1))
}

workflows() {
	local f
	for f in "$root"/.github/workflows/*.yml "$root"/.github/workflows/*.yaml; do
		[ -f "$f" ] && echo "$f"
	done
	return 0
}

rel() {
	echo "${1#"$root"/}"
}

# --- pinning -----------------------------------------------------------------

check_pinning() {
	local f n line
	while IFS= read -r f; do
		n=0
		# shellcheck disable=SC2094 # rel only prints the path, it never writes the file
		while IFS= read -r line || [ -n "$line" ]; do
			n=$((n + 1))
			[[ "$line" =~ ^[[:space:]]*(-[[:space:]]+)?uses: ]] || continue
			if ! [[ "$line" =~ uses:[[:space:]]+[^[:space:]@]+@[0-9a-f]{40}[[:space:]]+#[[:space:]]v[0-9] ]]; then
				fail "$(rel "$f"):$n: action not pinned to a commit SHA with a '# vX.Y.Z' comment: ${line#"${line%%[![:space:]]*}"}"
			fi
		done <"$f"
	done < <(workflows)
}

# --- permissions -------------------------------------------------------------

# allowed_raises prints the permissions job $2 of workflow file $1 may set
# (one "key: value" per line).
allowed_raises() {
	case "$1:$2" in
	doc.yml:deploy-docs) printf '%s\n' 'pages: write' 'id-token: write' ;;
	security.yml:codeql | security.yml:trivy-codeql) printf '%s\n' 'security-events: write' ;;
	release.yml:release) printf '%s\n' 'contents: write' ;;
	dependabot-automerge.yaml:automerge) printf '%s\n' 'contents: write' 'pull-requests: write' ;;
	esac
}

# permission_blocks prints "<job>\t<key: value>" for every permission entry
# of file $1; the job is "-" for the top-level block. A scalar such as
# "permissions: write-all" is printed as "<job>\t=<scalar>".
permission_blocks() {
	awk '
		function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]*(#.*)?$/, "", s); return s }
		/^[ \t]*(#.*)?$/ { next }
		{ match($0, /^ */); indent = RLENGTH }
		inperm && indent > permindent { print job "\t" trim($0); next }
		inperm && indent <= permindent { inperm = 0 }
		indent == 0 { injobs = ($0 ~ /^jobs:/); job = "-" }
		injobs && indent == 2 { job = trim($0); sub(/:$/, "", job) }
		$0 ~ /^ *permissions:/ {
			if ((indent == 0) || (injobs && indent == 4)) {
				v = $0; sub(/^ *permissions:/, "", v); v = trim(v)
				if (v != "") { print job "\t=" v } else { inperm = 1; permindent = indent }
			}
		}
	' "$1"
}

check_permissions() {
	local f base top job entry allowed
	while IFS= read -r f; do
		base="$(basename "$f")"
		top="$(permission_blocks "$f" | awk -F'\t' '$1=="-"{print $2}')"
		if [ "$top" != "contents: read" ]; then
			top="$(printf '%s' "$top" | tr '\n' ',' | sed 's/,/, /g')"
			fail "$(rel "$f"): top-level permissions must be exactly 'contents: read' (found: ${top:-none})"
		fi
		while IFS=$'\t' read -r job entry; do
			[ "$job" != "-" ] || continue
			allowed="$(allowed_raises "$base" "$job")"
			if [ "$entry" = "contents: read" ]; then
				continue
			fi
			if ! grep -qxF -- "$entry" <<<"$allowed"; then
				fail "$(rel "$f"): job '$job' may not raise permission '$entry'"
			fi
		done < <(permission_blocks "$f")
	done < <(workflows)
}

# --- matrix ------------------------------------------------------------------

# matrix_values prints the values of matrix key $2 in file $1, in flow
# ("os: [a, b]") or block ("os:" then "- a") style.
matrix_values() {
	awk -v key="$2" '
		function clean(s) { gsub(/["'\'' \t]/, "", s); return s }
		{ match($0, /^ */); indent = RLENGTH }
		inlist && $0 ~ /^ *- / && indent > keyindent { v = $0; sub(/^ *- */, "", v); sub(/[ \t]*#.*$/, "", v); print clean(v); next }
		inlist && $0 !~ /^[ \t]*$/ { inlist = 0 }
		$0 ~ "^ *" key ":" {
			v = $0; sub("^ *" key ":", "", v); sub(/[ \t]*#.*$/, "", v)
			if (v ~ /\[/) { gsub(/[\[\]]/, "", v); n = split(v, a, ","); for (i = 1; i <= n; i++) print clean(a[i]) }
			else if (clean(v) == "") { inlist = 1; keyindent = indent }
		}
	' "$1"
}

check_matrix() {
	local test="$root/.github/workflows/test.yml" lint="$root/.github/workflows/lint.yml"
	local security="$root/.github/workflows/security.yml" want values
	if [ ! -f "$test" ]; then
		fail ".github/workflows/test.yml is missing"
	else
		grep -Eq '^[[:space:]]+run:[[:space:]]+make test-all[[:space:]]*$' "$test" ||
			fail ".github/workflows/test.yml: no step runs 'make test-all'"
		values="$(matrix_values "$test" os)"
		for want in ubuntu-latest macos-latest windows-latest; do
			grep -qxF "$want" <<<"$values" || fail ".github/workflows/test.yml: matrix 'os' lacks $want"
		done
		values="$(matrix_values "$test" go)"
		for want in 1.27.x stable; do
			grep -qxF "$want" <<<"$values" || fail ".github/workflows/test.yml: matrix 'go' lacks $want"
		done
	fi
	if [ ! -f "$lint" ] || ! grep -Eq '^[[:space:]]+run:[[:space:]]+make lint-all[[:space:]]*$' "$lint"; then
		fail ".github/workflows/lint.yml: no step runs 'make lint-all'"
	fi
	if [ -f "$lint" ]; then
		local mk ci
		mk="$(awk -F' *:= *' '$1=="GOLANGCI_LINT_VERSION"{print $2}' "$root/Makefile")"
		ci="$(awk '/^[[:space:]]+version:[[:space:]]+v[0-9]/{print $2; exit}' "$lint")"
		[ -n "$mk" ] && [ "$mk" = "$ci" ] ||
			fail ".github/workflows/lint.yml: golangci-lint version '${ci:-none}' differs from the Makefile's '${mk:-none}'"
	fi
	if [ ! -f "$security" ] || ! grep -Eq '^[[:space:]]+run:[[:space:]]+make vuln[[:space:]]*$' "$security"; then
		fail ".github/workflows/security.yml: no step runs 'make vuln'"
	fi
}

# --- release -----------------------------------------------------------------

# first_line prints the first line number of file $2 matching ERE $1, or 0.
first_line() {
	local n
	n="$(grep -nE -- "$1" "$2" | head -1 | cut -d: -f1)"
	echo "${n:-0}"
}

check_release() {
	local f="$root/.github/workflows/release.yml"
	if [ ! -f "$f" ]; then
		fail ".github/workflows/release.yml is missing"
		return
	fi
	local r=".github/workflows/release.yml"
	grep -Eq "^    if:.*github\.ref == 'refs/heads/main'" "$f" ||
		fail "$r: the release job lacks the condition github.ref == 'refs/heads/main'"
	grep -Eq '^    environment:[[:space:]]+release[[:space:]]*$' "$f" ||
		fail "$r: the release job does not declare 'environment: release'"
	grep -Eq '^[[:space:]]+generate_release_notes:[[:space:]]+true' "$f" ||
		fail "$r: the release action is not called with generate_release_notes: true"

	local tidy pin build vet vuln tags
	tidy="$(first_line 'go mod tidy' "$f")"
	pin="$(first_line '^[[:space:]]+- name: Pin adapters' "$f")"
	build="$(first_line 'go build \./\.\.\.' "$f")"
	vet="$(first_line 'go vet \./\.\.\.' "$f")"
	vuln="$(first_line '^[[:space:]]+run:[[:space:]]+make vuln' "$f")"
	tags="$(first_line '^[[:space:]]+- name: Create tags' "$f")"
	[ "$pin" -gt 0 ] || fail "$r: no 'Pin adapters' step"
	[ "$tags" -gt 0 ] || fail "$r: no 'Create tags' step"
	{ [ "$tidy" -gt 0 ] && [ "$tidy" -lt "$pin" ]; } ||
		fail "$r: no 'go mod tidy' step before the adapters are pinned"
	grep -B8 'go mod tidy' "$f" | grep -Eq 'for dir in .*(http|scheduler|clock)' ||
		fail "$r: 'go mod tidy' does not run in every released module"
	{ [ "$build" -gt "$pin" ] && [ "$vet" -gt "$pin" ]; } ||
		fail "$r: no 'go build ./...' and 'go vet ./...' after pinning"
	{ [ "$vuln" -gt 0 ] && [ "$vuln" -lt "$tags" ]; } ||
		fail "$r: no 'make vuln' step before 'Create tags'"
	local loops
	loops="$(grep -cE 'for dir in http/\*/ scheduler/\*/ clock/\*/' "$f" || true)"
	[ "$loops" -ge 3 ] ||
		fail "$r: tidy, pin and tag loops must all cover http/*/ scheduler/*/ clock/*/ (found $loops)"
}

# --- dependabot --------------------------------------------------------------

# gomod_dirs prints the directories of every gomod entry of dependabot.yml.
gomod_dirs() {
	awk '
		function clean(s) { sub(/[ \t]*#.*$/, "", s); gsub(/["'\'' \t]/, "", s); return s }
		/^  - package-ecosystem:/ { gomod = ($0 ~ /gomod/); indirs = 0; next }
		!gomod { next }
		/^    directory:/ { v = $0; sub(/^    directory:/, "", v); print clean(v); next }
		/^    directories:/ { indirs = 1; next }
		indirs && /^      - / { v = $0; sub(/^      - /, "", v); print clean(v); next }
		/^    [a-z]/ { indirs = 0 }
	' "$1"
}

check_dependabot() {
	local cfg="$root/.github/dependabot.yml" wf="$root/.github/workflows/dependabot-automerge.yaml"
	local w=".github/workflows/dependabot-automerge.yaml"
	if [ ! -f "$wf" ]; then
		fail "$w is missing"
	else
		grep -Eq "^    if:.*github\.event\.pull_request\.user\.login == 'dependabot\[bot\]'" "$wf" ||
			fail "$w: the automerge job is not gated on github.event.pull_request.user.login == 'dependabot[bot]'"
		local cond
		cond="$(grep -E '^[[:space:]]+if:.*update-type' "$wf" || true)"
		{ grep -q "version-update:semver-patch" <<<"$cond" && grep -q "version-update:semver-minor" <<<"$cond"; } ||
			fail "$w: the merge step is not limited to semver patch and minor updates"
		grep -q 'semver-major' <<<"$cond" &&
			fail "$w: the merge step must not enable auto-merge for semver-major updates"
		grep -E '^[[:space:]]+run:.*gh pr merge' "$wf" >/dev/null ||
			fail "$w: no step enables auto-merge"
		local merges guarded
		merges="$(grep -cE '^[[:space:]]+run:.*gh pr merge' "$wf" || true)"
		guarded="$(grep -B3 -E '^[[:space:]]+run:.*gh pr merge' "$wf" | grep -cE 'if:.*update-type' || true)"
		[ "$merges" -eq "$guarded" ] ||
			fail "$w: every 'gh pr merge' step must be gated on the update type"
	fi

	if [ ! -f "$cfg" ]; then
		fail ".github/dependabot.yml is missing"
		return
	fi
	local dirs mod d entry covered
	dirs="$(gomod_dirs "$cfg")"
	while IFS= read -r mod; do
		d="/${mod#./}"
		[ "$mod" != "." ] || d="/"
		covered=0
		while IFS= read -r entry; do
			[ -n "$entry" ] || continue
			if [ "$entry" = "$d" ]; then
				covered=1
			elif [[ "$entry" == */\*\* ]] && [[ "$d" == "${entry%/\*\*}"/* ]]; then
				covered=1
			fi
		done <<<"$dirs"
		[ "$covered" -eq 1 ] || fail ".github/dependabot.yml: no gomod entry covers module directory $d"
	done < <(cd "$root" && scripts/modules.sh list)
}

# --- floors ------------------------------------------------------------------

# version_ge reports whether semver $1 >= $2.
version_ge() {
	[ "$(printf '%s\n%s\n' "${1#v}" "${2#v}" | sort -V | head -1)" = "${2#v}" ]
}

check_floors() {
	local row mod dep min have
	for row in \
		"http/fiber github.com/andybalholm/brotli v1.2.7" \
		"examples/http/fiber github.com/andybalholm/brotli v1.2.7" \
		"http/echo golang.org/x/net v0.62.0" \
		"http/gin golang.org/x/net v0.62.0"; do
		read -r mod dep min <<<"$row"
		[ -f "$root/$mod/go.mod" ] || {
			fail "$mod/go.mod is missing"
			continue
		}
		have="$(awk -v dep="$dep" '{ for (i = 1; i < NF; i++) if ($i == dep) { print $(i + 1); exit } }' "$root/$mod/go.mod")"
		if [ -z "$have" ]; then
			fail "$mod/go.mod does not require $dep (want >= $min)"
		elif ! version_ge "$have" "$min"; then
			fail "$mod/go.mod requires $dep $have, below the floor $min"
		fi
	done
}

# --- policy ------------------------------------------------------------------

check_policy() {
	local f="$root/SECURITY.md"
	if [ ! -f "$f" ]; then
		fail "SECURITY.md is missing"
	else
		grep -q 'Reporting a vulnerability' "$f" || fail "SECURITY.md lacks a 'Reporting a vulnerability' section"
		grep -q 'Accepted advisories' "$f" || fail "SECURITY.md lacks an 'Accepted advisories' section"
		local id
		for id in GO-2026-6875 GO-2026-6872; do
			sed -n '/Accepted advisories/,$p' "$f" | grep -q "$id" ||
				fail "SECURITY.md does not list $id under 'Accepted advisories'"
		done
	fi

	f="$root/CHANGELOG.md"
	if [ ! -f "$f" ]; then
		fail "CHANGELOG.md is missing"
	else
		local unreleased
		unreleased="$(awk '/^## /{ if (on) exit; if ($0 ~ /Unreleased/) on = 1 } on' "$f")"
		if [ -z "$unreleased" ]; then
			fail "CHANGELOG.md has no 'Unreleased' section"
		else
			grep -q 'BREAKING' <<<"$unreleased" || fail "CHANGELOG.md: the 'Unreleased' section lists no BREAKING entry"
			grep -qi 'migration' <<<"$unreleased" || fail "CHANGELOG.md: the 'Unreleased' section gives no migration"
		fi
	fi

	f="$root/.github/CODEOWNERS"
	if [ ! -f "$f" ]; then
		fail ".github/CODEOWNERS is missing"
	else
		grep -Eq '^\*[[:space:]]+@[^[:space:]]+' "$f" || fail ".github/CODEOWNERS does not assign '*' to an owner"
	fi

	[ ! -e "$root/vercel.json" ] || fail "vercel.json must not exist (docs deploy through GitHub Pages)"

	f="$root/bearer.yml"
	if [ -f "$f" ]; then
		local level
		level="$(awk '/^log-level:/{print $2}' "$f")"
		[ -z "$level" ] || [ "$level" = "info" ] ||
			fail "bearer.yml: log-level is '$level', not Bearer's default 'info'"
	fi
}

# --- self-test ---------------------------------------------------------------

# expect runs the script on fixture $1 for section $2 and checks that it
# exits with status class $3 (ok|fail) and, for fail, prints ERE $4.
expect() {
	local dir="$1" sec="$2" want="$3" pattern="${4:-}" out status=0
	out="$("$script_dir/check-workflows.sh" --root "$dir" "$sec" 2>&1)" || status=$?
	if [ "$want" = ok ] && [ "$status" -eq 0 ]; then
		echo "ok:   self-test $sec passes on a compliant fixture"
	elif [ "$want" = fail ] && [ "$status" -ne 0 ] && grep -Eq -- "$pattern" <<<"$out"; then
		echo "ok:   self-test $sec fails with: $(grep -E -- "$pattern" <<<"$out" | head -1)"
	else
		echo "FAIL self-test $sec: want $want ${pattern:+matching \"$pattern\"}, got status $status:"
		while IFS= read -r line; do echo "    | $line"; done <<<"$out"
		self_failures=$((self_failures + 1))
	fi
}

self_test() {
	local tmp sha
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' RETURN
	self_failures=0
	sha="0123456789abcdef0123456789abcdef01234567"

	mkdir -p "$tmp/good/.github/workflows" "$tmp/bad/.github/workflows"
	printf 'name: T\npermissions:\n  contents: read\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@%s # v7.0.1\n' "$sha" \
		>"$tmp/good/.github/workflows/test.yml"
	expect "$tmp/good" pinning ok
	expect "$tmp/good" permissions ok

	# scenario "A tag-pinned action fails the check"
	printf 'name: T\npermissions:\n  contents: read\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v7\n' \
		>"$tmp/bad/.github/workflows/test.yml"
	expect "$tmp/bad" pinning fail '\.github/workflows/test\.yml:8: action not pinned'

	# missing top-level permissions, and a raise on a job that may not
	printf 'name: T\njobs:\n  t:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: write\n    steps: []\n' \
		>"$tmp/bad/.github/workflows/test.yml"
	expect "$tmp/bad" permissions fail "test\.yml: top-level permissions must be exactly 'contents: read'"
	expect "$tmp/bad" permissions fail "test\.yml: job 't' may not raise permission 'contents: write'"

	# a broad top-level grant
	printf 'name: S\npermissions:\n  contents: read\n  security-events: write\njobs: {}\n' \
		>"$tmp/bad/.github/workflows/test.yml"
	expect "$tmp/bad" permissions fail "top-level permissions must be exactly 'contents: read'"

	# policy: vercel.json present and nothing else
	touch "$tmp/bad/vercel.json"
	expect "$tmp/bad" policy fail 'vercel\.json must not exist'
	expect "$tmp/bad" policy fail 'SECURITY\.md is missing'

	if [ "$self_failures" -gt 0 ]; then
		echo "$self_failures self-test check(s) failed"
		return 1
	fi
	echo "self-test passed"
}

# --- main --------------------------------------------------------------------

sections=()
while [ "$#" -gt 0 ]; do
	case "$1" in
	--self-test)
		self_test
		exit $?
		;;
	--root)
		root="$(cd "$2" && pwd)"
		shift 2
		;;
	-h | --help)
		sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'
		exit 0
		;;
	*)
		sections+=("$1")
		shift
		;;
	esac
done
[ "${#sections[@]}" -gt 0 ] || sections=("${all_sections[@]}")

for section in "${sections[@]}"; do
	before=$failures
	case "$section" in
	pinning | permissions | matrix | release | dependabot | floors | policy) "check_$section" ;;
	*)
		echo "unknown section '$section' (want: ${all_sections[*]})" >&2
		exit 2
		;;
	esac
	[ "$failures" -gt "$before" ] || echo "ok:   $section"
done

if [ "$failures" -gt 0 ]; then
	echo "$failures finding(s)"
	exit 1
fi
echo "all checks passed: ${sections[*]}"
