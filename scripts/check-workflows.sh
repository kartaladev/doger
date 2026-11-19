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
#   safety       no pull_request_target trigger and no untrusted ${{ }}
#                expression (github.event.*, inputs.*, head_ref) in run:
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
#                vercel.json, Bearer at its default log level, .gitattributes
#                keeping shell scripts and the Makefile LF on every OS
#
# The workflows are parsed line by line, assuming the repository's layout:
# two-space indentation, top-level keys at column 0 and jobs at column 2.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
all_sections=(pinning safety permissions matrix release dependabot floors policy)

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

# --- safety ------------------------------------------------------------------

# run_expressions prints "<line>\t<text>" for every ${{ ... }} expression
# inside a run: script of file $1 (inline or block scalar).
run_expressions() {
	awk '
		{ match($0, /^ */); indent = RLENGTH }
		inrun && $0 !~ /^[ \t]*$/ && indent <= runindent { inrun = 0 }
		inrun && /\$\{\{/ { print NR "\t" $0; next }
		/^ *(- )?run:/ {
			runindent = indent
			v = $0; sub(/^ *(- )?run:/, "", v)
			if (v ~ /^[ \t]*[|>]/) { inrun = 1 }
			else if (v ~ /\$\{\{/) { print NR "\t" $0 }
		}
	' "$1"
}

check_safety() {
	local f n text
	while IFS= read -r f; do
		# pull_request_target runs with a write token and secrets on the
		# code of a fork's pull request
		if grep -nE '^[[:space:]]*(-[[:space:]]+)?pull_request_target' "$f" >/dev/null ||
			grep -nE '^on:.*pull_request_target' "$f" >/dev/null; then
			fail "$(rel "$f"): the pull_request_target trigger is not allowed"
		fi
		# an expression expanded into a script is a script injection when
		# it carries outside data; pass it through env: instead
		while IFS=$'\t' read -r n text; do
			if [[ "$text" =~ \$\{\{[[:space:]]*(github\.event\.|github\.head_ref|inputs\.|github\.event_name) ]]; then
				fail "$(rel "$f"):$n: untrusted expression inside run: (pass it through env:): ${text#"${text%%[![:space:]]*}"}"
			fi
		done < <(run_expressions "$f")
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
	# the checkout must not persist the write token for the steps that run
	# dependency code (tests, tidy, govulncheck)
	grep -A2 -E 'uses:[[:space:]]+actions/checkout@' "$f" | grep -Eq '^[[:space:]]+persist-credentials:[[:space:]]+false[[:space:]]*$' ||
		fail "$r: the checkout must set 'persist-credentials: false'"
	grep -Eq '^[[:space:]]+generate_release_notes:[[:space:]]+true' "$f" ||
		fail "$r: the release action is not called with generate_release_notes: true"

	# the steps run scripts/release-steps.sh, shared with the dry run
	local strip tidy pin verify vuln tags
	strip="$(first_line 'run:[[:space:]]+scripts/release-steps\.sh strip' "$f")"
	tidy="$(first_line 'run:[[:space:]]+scripts/release-steps\.sh tidy' "$f")"
	# shellcheck disable=SC2016 # matches the literal "$SEMVER" of the workflow
	pin="$(first_line 'run:[[:space:]]+scripts/release-steps\.sh pin "\$SEMVER"' "$f")"
	verify="$(first_line 'run:[[:space:]]+scripts/release-steps\.sh verify' "$f")"
	vuln="$(first_line '^[[:space:]]+run:[[:space:]]+make vuln' "$f")"
	tags="$(first_line '^[[:space:]]+- name: Create tags' "$f")"
	[ "$strip" -gt 0 ] || fail "$r: no step strips the tests (release-steps.sh strip)"
	[ "$pin" -gt 0 ] || fail "$r: no step pins the released modules (release-steps.sh pin \"\$SEMVER\")"
	[ "$tags" -gt 0 ] || fail "$r: no 'Create tags' step"
	{ [ "$tidy" -gt "$strip" ] && [ "$tidy" -lt "$pin" ]; } ||
		fail "$r: no 'go mod tidy' step (release-steps.sh tidy) between stripping and pinning"
	[ "$verify" -gt "$pin" ] ||
		fail "$r: no build and vet step (release-steps.sh verify) after pinning"
	{ [ "$vuln" -gt "$tidy" ] && [ "$vuln" -lt "$tags" ]; } ||
		fail "$r: no 'make vuln' step on the tidied tree before 'Create tags'"
	# shellcheck disable=SC2016 # matches the literal "$SEMVER" of the workflow
	sed -n "${tags},\$p" "$f" | grep -q 'scripts/release-steps\.sh tags "\$SEMVER"' ||
		fail "$r: 'Create tags' does not tag the modules listed by release-steps.sh tags"

	# what each shared step does
	local s="$root/scripts/release-steps.sh" sr="scripts/release-steps.sh"
	if [ ! -f "$s" ]; then
		fail "$sr is missing"
		return
	fi
	step_body() { awk -v fn="$1" '$0 ~ "^" fn "\\(\\) \\{" {on=1} on {print} on && /^}/ {exit}' "$s"; }
	# the released modules come from the go.mod files, in dependency order
	step_body module_dirs | grep -q -- '-name go.mod' ||
		fail "$sr: the released modules are not derived from the go.mod files (module_dirs)"
	step_body modules | grep -q 'sibling_requirements' ||
		fail "$sr: modules does not order the released modules by their sibling requirements"
	local step
	for step in tidy pin verify tags; do
		step_body "$step" | grep -q '< <(modules)' ||
			fail "$sr: $step does not run over every released module (< <(modules))"
	done
	step_body tidy | grep -q 'go mod tidy' ||
		fail "$sr: tidy does not run 'go mod tidy'"
	# every sibling requirement is pinned, and every replace goes
	# shellcheck disable=SC2016 # matches literal $dep and $1 in the script
	{ step_body pin | grep -q 'sibling_requirements' && step_body pin | grep -q -- '-require=\$dep@\$1'; } ||
		fail "$sr: pin does not pin every released sibling requirement to the release version"
	{ step_body pin | grep -q -- '-dropreplace=' && step_body pin | grep -q '\.Replace'; } ||
		fail "$sr: pin does not drop every replace directive"
	# shellcheck disable=SC2016 # matches a literal $sib_path in the script
	{ step_body verify | grep -q 'go build ' && step_body verify | grep -q 'go vet ' &&
		step_body verify | grep -q -- '-replace=\$sib_path='; } ||
		fail "$sr: verify does not build and vet every module with its released siblings resolved locally"
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
		# exact expressions: any extra "|| ..." would widen who or what merges
		local job_if="if: github.event.pull_request.user.login == 'dependabot[bot]'"
		local step_if="if: steps.metadata.outputs.update-type == 'version-update:semver-patch' || steps.metadata.outputs.update-type == 'version-update:semver-minor'"
		local job_ifs
		job_ifs="$(grep -E '^    if:' "$wf" | sed 's/^ *//; s/[[:space:]]*$//' || true)"
		[ "$job_ifs" = "$job_if" ] ||
			fail "$w: the automerge job condition must be exactly \"$job_if\" (found: ${job_ifs:-none})"
		local merges=0 n=0 line prev_if=""
		while IFS= read -r line || [ -n "$line" ]; do
			n=$((n + 1))
			if [[ "$line" =~ ^[[:space:]]+-[[:space:]] ]]; then
				prev_if="" # a new step
			fi
			if [[ "$line" =~ ^[[:space:]]{8}if: ]]; then
				prev_if="$(sed 's/^ *//; s/[[:space:]]*$//' <<<"$line")"
			fi
			if [[ "$line" =~ gh[[:space:]]+pr[[:space:]]+merge ]]; then
				merges=$((merges + 1))
				if [ "$prev_if" != "$step_if" ]; then
					fail "$w:$n: 'gh pr merge' must be gated by exactly \"$step_if\" (found: ${prev_if:-none})"
				fi
			fi
		done <"$wf"
		[ "$merges" -gt 0 ] || fail "$w: no step enables auto-merge"
	fi

	if [ ! -f "$cfg" ]; then
		fail ".github/dependabot.yml is missing"
		return
	fi
	local dirs mods mod d entry covered
	dirs="$(gomod_dirs "$cfg")"
	# fail closed: a module list that cannot be read is a finding
	if ! mods="$(cd "$root" && scripts/modules.sh list 2>&1)" || [ -z "$mods" ]; then
		fail "cannot list the workspace modules (scripts/modules.sh list): ${mods:-no output}"
		return
	fi
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
	done <<<"$mods"
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
		# advisories are accepted per (id, module): scripts/govulncheck.sh
		# reads the same rows
		local row id mod accepted
		accepted="$("$script_dir/govulncheck.sh" --accepted "$f" 2>&1)" ||
			fail "SECURITY.md: cannot read the accepted advisories: $accepted"
		for row in "GO-2026-6875 ./http/echo" "GO-2026-6872 ./http/echo"; do
			read -r id mod <<<"$row"
			grep -qxF "$id	$mod" <<<"$accepted" ||
				fail "SECURITY.md does not list $id for $mod under 'Accepted advisories'"
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

	# a CRLF checkout (core.autocrlf=true on Windows) breaks bash scripts
	# ("set -o pipefail": invalid option name) and the Makefile recipes
	f="$root/.gitattributes"
	if [ ! -f "$f" ]; then
		fail ".gitattributes is missing (shell scripts and the Makefile must stay LF)"
	else
		local pattern
		for pattern in '*.sh' 'Makefile'; do
			awk -v p="$pattern" '$1 == p { for (i = 2; i <= NF; i++) if ($i == "eol=lf") found = 1 } END { exit !found }' "$f" ||
				fail ".gitattributes does not set 'eol=lf' for $pattern"
		done
	fi
	local crlf
	crlf="$(grep -l $'\r' "$root"/scripts/*.sh "$root/Makefile" 2>/dev/null || true)"
	local crlf_rel="" c
	while IFS= read -r c; do
		[ -n "$c" ] && crlf_rel+="$(rel "$c") "
	done <<<"$crlf"
	[ -z "$crlf_rel" ] || fail "CRLF line endings in: $crlf_rel"

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

# shellcheck disable=SC2016 # the fixtures hold literal ${{ }} and $VAR text
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
	expect "$tmp/bad" policy fail '\.gitattributes is missing'

	# fixture puts content $2 into file $1 of a fresh fixture directory
	local fx
	fixture() {
		fx="$(mktemp -d "$tmp/fx.XXXXXX")"
		mkdir -p "$fx/.github/workflows" "$fx/scripts"
	}
	put() {
		mkdir -p "$(dirname "$fx/$1")"
		printf '%s\n' "$2" >"$fx/$1"
	}
	local head='name: T
on:
  pull_request:
permissions:
  contents: read
jobs:'

	# safety: an outside expression passed through env: is fine
	fixture
	put .github/workflows/test.yml "$head"'
  t:
    runs-on: ubuntu-latest
    steps:
      - env:
          TITLE: ${{ github.event.pull_request.title }}
        run: echo "$TITLE"'
	expect "$fx" safety ok
	# ... expanded into the script it is an injection
	put .github/workflows/test.yml "$head"'
  t:
    runs-on: ubuntu-latest
    steps:
      - run: |
          echo "${{ github.event.pull_request.title }}"'
	expect "$fx" safety fail 'test\.yml:11: untrusted expression inside run:'
	put .github/workflows/test.yml "$head"'
  t:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.semver }}'
	expect "$fx" safety fail 'test\.yml:10: untrusted expression inside run:'
	put .github/workflows/test.yml 'name: T
on:
  pull_request_target:
permissions:
  contents: read
jobs: {}'
	expect "$fx" safety fail 'pull_request_target trigger is not allowed'

	# matrix: a missing OS, Go version and make target
	fixture
	put .github/workflows/test.yml "$head"'
  build:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
        go: ["stable"]
    steps:
      - run: make test'
	expect "$fx" matrix fail "matrix 'os' lacks windows-latest"
	expect "$fx" matrix fail "matrix 'go' lacks 1\.27\.x"
	expect "$fx" matrix fail "no step runs 'make test-all'"

	# release: no main-only condition, environment or credential guard
	fixture
	put .github/workflows/release.yml "$head"'
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@'"$sha"' # v7.0.1
      - name: Create tags
        run: git push origin v0.1.0'
	expect "$fx" release fail "lacks the condition github\.ref == 'refs/heads/main'"
	expect "$fx" release fail "does not declare 'environment: release'"
	expect "$fx" release fail "must set 'persist-credentials: false'"
	expect "$fx" release fail "no step pins the released modules"

	# release: steps that hard-code the adapters and pin only the root
	fixture
	cp "$script_dir/../.github/workflows/release.yml" "$fx/.github/workflows/release.yml"
	put scripts/release-steps.sh 'module_dirs() {
	for dir in http/*/ scheduler/*/ clock/*/; do echo "${dir%/}"; done
}
pin() {
	while IFS= read -r dir; do
		(cd "$dir" && go mod edit -require="$root_module@$1" -dropreplace="$root_module")
	done < <(modules)
}'
	expect "$fx" release fail 'not derived from the go\.mod files'
	expect "$fx" release fail 'pin does not pin every released sibling requirement'
	expect "$fx" release fail 'pin does not drop every replace directive'

	# dependabot: a widened job or step condition, and no module list
	fixture
	put .github/workflows/dependabot-automerge.yaml "$head"'
  automerge:
    runs-on: ubuntu-latest
    if: github.event.pull_request.user.login == '"'dependabot[bot]'"' || github.actor == '"'octocat'"'
    steps:
      - name: merge
        if: steps.metadata.outputs.update-type == '"'version-update:semver-patch'"' || true
        run: gh pr merge --auto --squash "$PR_URL"'
	put .github/dependabot.yml 'version: 2
updates:
  - package-ecosystem: gomod
    directory: /'
	expect "$fx" dependabot fail 'the automerge job condition must be exactly'
	expect "$fx" dependabot fail "'gh pr merge' must be gated by exactly"
	expect "$fx" dependabot fail 'cannot list the workspace modules'

	# floors: a vulnerable version below the floor
	fixture
	put http/fiber/go.mod 'module m
require github.com/andybalholm/brotli v1.1.0'
	expect "$fx" floors fail 'http/fiber/go\.mod requires github\.com/andybalholm/brotli v1\.1\.0, below the floor v1\.2\.7'

	# policy: an advisory accepted for another module only, and CRLF
	fixture
	put SECURITY.md '## Reporting a vulnerability
## Accepted advisories
| Advisory | Module | Reason |
|---|---|---|
| GO-2026-6875 | `./http/gin` | wrong module |'
	put .gitattributes '*.sh text eol=lf'
	printf 'set -euo pipefail\r\n' >"$fx/scripts/x.sh"
	expect "$fx" policy fail 'does not list GO-2026-6875 for \./http/echo'
	expect "$fx" policy fail "\.gitattributes does not set 'eol=lf' for Makefile"
	expect "$fx" policy fail 'CRLF line endings in: .*scripts/x\.sh'

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
	pinning | safety | permissions | matrix | release | dependabot | floors | policy) "check_$section" ;;
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
