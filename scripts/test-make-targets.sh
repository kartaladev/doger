#!/usr/bin/env bash
# Checks that "make test-all" and "make lint-all" fail, naming the module,
# when any module has a failing test or a lint issue (release-integrity
# scenarios "A failure in any module fails make test-all" and "An issue in
# any module fails make lint-all").
#
# It works on a temporary copy of the repository: the working tree is never
# modified. A lint row whose linter is not enabled in .golangci.yml is
# reported as SKIP, not as a failure.
#
# Usage: scripts/test-make-targets.sh [test|lint|discovery|vuln]...
#        (default: all four)
# LINT_FLAGS is passed to make lint-all (e.g. --allow-parallel-runners).
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0
fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}
pass() {
	echo "ok:   $*"
}

# fresh_copy prints the path of a new copy of the repository.
fresh_copy() {
	local dst
	dst="$(mktemp -d "$work/copy.XXXXXX")"
	rsync -a --exclude .git --exclude .claude --exclude node_modules \
		--exclude docs "$repo/" "$dst/"
	echo "$dst"
}

# package_name prints the Go package name of module directory $2 in copy $1.
package_name() {
	(cd "$1/$2" && go list -f '{{.Name}}' .)
}

check_test_all() {
	local copy out modules row pkg status
	local -a rows=("." "./http/chi" "./scheduler/gocron" "./clock/clockwork")
	copy="$(fresh_copy)"

	for row in "${rows[@]}"; do
		pkg="$(package_name "$copy" "$row")"
		printf 'package %s\n\nimport "testing"\n\nfunc TestPlantedFailure(t *testing.T) {\n\tt.Fatal("boom")\n}\n' \
			"$pkg" >"$copy/$row/zz_planted_failure_test.go"
	done

	out="$work/test-all.log"
	status=0
	(cd "$copy" && make test-all) >"$out" 2>&1 || status=$?
	modules="$(cd "$copy" && make --no-print-directory modules | wc -l | tr -d ' ')"

	if [ "$status" -ne 0 ]; then
		pass "make test-all exits non-zero ($status) with planted failures"
	else
		fail "make test-all exited 0 with planted failures"
	fi
	for row in "${rows[@]}"; do
		if grep -qxF "FAIL module $row" "$out"; then
			pass "make test-all names $row"
		else
			fail "make test-all does not name $row as failing"
		fi
	done
	if grep -q 'boom' "$out"; then
		pass "make test-all shows the failure message"
	else
		fail "make test-all output lacks the failure message \"boom\""
	fi
	local ran
	ran="$(grep -c '^==> ' "$out" || true)"
	if [ "$ran" -eq "$modules" ]; then
		pass "make test-all ran all $modules modules"
	else
		fail "make test-all ran $ran of $modules modules"
	fi
	local unexpected
	unexpected="$(grep '^FAIL module ' "$out" | sed 's/^FAIL module //' |
		grep -vxF -e . -e ./http/chi -e ./scheduler/gocron -e ./clock/clockwork || true)"
	if [ -z "$unexpected" ]; then
		pass "make test-all names no other module"
	else
		fail "make test-all names unplanted modules: $unexpected"
	fi
	[ "$failures" -eq 0 ] || sed 's/^/    | /' "$out" | tail -40
}

# linter_enabled reports whether .golangci.yml enables linter $1.
linter_enabled() {
	awk '/^linters:/{l=1} l&&/^  enable:/{e=1;next} e&&/^  [a-z]/{exit} e' "$repo/.golangci.yml" |
		grep -Eq "^[[:space:]]*-[[:space:]]+$1[[:space:]]*(#.*)?$"
}

check_lint_all() {
	local copy out status row linter pkg file pattern
	# module|linter|imports|body (\n-separated). golangci-lint keeps one
	# issue per line, so the planted issue sits on a line of its own, apart
	# from the "unused" report on the func line.
	local -a rows=(
		'./http/chi|errcheck|"net/http"|func plantedLint(w http.ResponseWriter) {\n\tw.Write([]byte("planted"))\n}'
		'./http/std|gosec|"fmt"; "net/http"|func plantedLint(w http.ResponseWriter, r *http.Request) {\n\t_, _ = fmt.Fprint(w, r.URL.Query().Get("q"))\n}'
		'.|errorlint||func plantedLint(err error) bool {\n\treturn err == ErrServiceNotFound\n}'
		'./http/gin|noctx|"net/http"|func plantedLint() {\n\tresp, err := http.Get("http://example.invalid/")\n\tif err == nil {\n\t\t_ = resp.Body.Close()\n\t}\n}'
	)
	copy="$(fresh_copy)"

	local -a active=()
	local entry imports body
	for entry in "${rows[@]}"; do
		IFS='|' read -r row linter imports body <<<"$entry"
		if ! linter_enabled "$linter"; then
			echo "SKIP: $linter is not enabled in .golangci.yml (row $row)"
			continue
		fi
		pkg="$(package_name "$copy" "$row")"
		{
			printf 'package %s\n\n' "$pkg"
			[ -z "$imports" ] || printf 'import (%s)\n\n' "$imports"
			printf '%b\n' "$body"
		} >"$copy/$row/zz_planted_lint.go"
		gofmt -w "$copy/$row/zz_planted_lint.go"
		active+=("$row|$linter")
	done
	[ "${#active[@]}" -gt 0 ] || return 0

	out="$work/lint-all.log"
	status=0
	(cd "$copy" && make lint-all LINT_FLAGS="${LINT_FLAGS:-}") >"$out" 2>&1 || status=$?

	if [ "$status" -ne 0 ]; then
		pass "make lint-all exits non-zero ($status) with planted issues"
	else
		fail "make lint-all exited 0 with planted issues"
	fi
	for entry in "${active[@]}"; do
		IFS='|' read -r row linter <<<"$entry"
		file="${row#./}/zz_planted_lint.go"
		file="${file#./}"
		# golangci-lint prints paths relative to the config, the repo root
		pattern="^${file//./\\.}:[0-9]+:[0-9]+: .*\\(${linter}\\)\$"
		if grep -Eq "$pattern" "$out" && grep -qxF "FAIL module $row" "$out"; then
			pass "make lint-all names $file and $linter, and fails $row"
		else
			fail "make lint-all does not report $file with $linter in $row"
		fi
	done
	[ "$failures" -eq 0 ] || grep -E 'zz_planted|^FAIL' "$out" | sed 's/^/    | /'
}

# A module list that cannot be read must fail every loop, never pass as an
# empty workspace: a go directive above go.work's makes "go list -m" fail.
check_discovery() {
	local copy out status target
	copy="$(fresh_copy)"
	(cd "$copy/http/chi" && go mod edit -go=1.99)
	for target in modules test-all vuln; do
		out="$work/discovery-$target.log"
		status=0
		(cd "$copy" && GOTOOLCHAIN=local make "$target") >"$out" 2>&1 || status=$?
		if [ "$status" -ne 0 ] && ! grep -q '^all 0 modules passed' "$out"; then
			pass "make $target fails when the module list cannot be read ($status)"
		else
			fail "make $target exits $status when the module list cannot be read: $(tail -1 "$out")"
		fi
	done
}

# Each module is scanned with its own requirements: a vulnerable version in
# one module is not hidden by a newer one elsewhere in the workspace.
check_vuln_isolation() {
	local copy out status
	copy="$(fresh_copy)"
	(
		cd "$copy/http/fiber"
		export GOWORK=off
		# fasthttp v1.51.0 brings back brotli v1.1.0 (GO-2026-6888); v1.79.0
		# dropped brotli
		go mod edit -require=github.com/valyala/fasthttp@v1.51.0
		go mod download github.com/valyala/fasthttp@v1.51.0
		go mod tidy
	) >"$work/downgrade.log" 2>&1 || {
		fail "cannot plant fasthttp v1.51.0 in http/fiber: $(tail -3 "$work/downgrade.log")"
		return
	}
	if ! grep -q 'andybalholm/brotli v1.1.0' "$copy/http/fiber/go.mod" ||
		grep -q 'andybalholm/brotli' "$copy/examples/http/fiber/go.mod"; then
		fail "the planted downgrade did not apply (http/fiber brotli v1.1.0, examples/http/fiber no brotli)"
		return
	fi
	out="$work/vuln.log"
	status=0
	(cd "$copy" && make vuln) >"$out" 2>&1 || status=$?
	if [ "$status" -ne 0 ] && grep -qxF 'FAIL module ./http/fiber' "$out" &&
		grep -q 'VULNERABLE  GO-2026-6888 .* from \./http/fiber' "$out"; then
		pass "make vuln fails on brotli v1.1.0 in ./http/fiber while examples/http/fiber has none"
	else
		fail "make vuln exits $status and does not fail ./http/fiber on GO-2026-6888"
	fi
	if grep -qxF 'FAIL module ./examples/http/fiber' "$out"; then
		fail "make vuln fails ./examples/http/fiber, which does not require brotli"
	else
		pass "make vuln passes ./examples/http/fiber (no brotli)"
	fi
}

targets=("$@")
[ "${#targets[@]}" -gt 0 ] || targets=(test lint discovery vuln)
for target in "${targets[@]}"; do
	case "$target" in
	test) check_test_all ;;
	lint) check_lint_all ;;
	discovery) check_discovery ;;
	vuln) check_vuln_isolation ;;
	*)
		echo "unknown target $target (want test, lint, discovery or vuln)" >&2
		exit 2
		;;
	esac
done

if [ "$failures" -gt 0 ]; then
	echo "$failures check(s) failed"
	exit 1
fi
echo "all make-target checks passed"
