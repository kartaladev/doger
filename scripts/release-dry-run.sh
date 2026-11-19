#!/usr/bin/env bash
# Dry-runs a release: post-processes a copy of the repository with the
# release workflow's own steps (scripts/release-steps.sh), publishes every
# released module to a local file proxy, and checks that a clean consumer
# (GOWORK=off, GOFLAGS=-mod=mod, GOPROXY set to that proxy only) can
# "go get" each module at the version and build a program importing it.
# It also checks the released go.mod files: adapters pin the root release,
# nothing has a replace directive, test-only requirements are gone and
# runtime requirements are kept.
#
# Usage: scripts/release-dry-run.sh <semver>     e.g. v0.99.0
#
# Nothing in the working tree or the user's module cache is modified: the
# script works on a copy, and the consumers use private module caches.
# Fetching third-party modules for the proxy needs network access.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
steps="$repo/scripts/release-steps.sh"
root_module="github.com/kartaladev/doger"

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <semver>" >&2
	exit 2
fi
version="$1"
"$steps" check-semver "$version"

work="$(mktemp -d)"
cleanup() {
	# module caches are read-only
	chmod -R u+w "$work" 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

failures=0
fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}
pass() {
	echo "ok:   $*"
}

# --- post-process a copy as the release job does ---------------------------

src="$work/src"
mkdir -p "$src"
rsync -a --exclude .git --exclude .claude --exclude node_modules "$repo/" "$src/"
cd "$src"

echo "--- release steps on a copy"
"$steps" strip
"$steps" tidy
# as the release's "Vulnerabilities" step
make --no-print-directory vuln
"$steps" pin "$version"
"$steps" verify

released=". $("$steps" modules | tr '\n' ' ')"

# module_path prints the module path of released directory $1.
module_path() {
	(cd "$src/$1" && go mod edit -json | jq -r '.Module.Path')
}

# requires reports whether released directory $1 requires module $2, and
# prints the version.
requires() {
	(cd "$src/$1" && go mod edit -json | jq -er --arg p "$2" '.Require // [] | map(select(.Path == $p)) | .[0].Version')
}

# --- released go.mod files ---------------------------------------------------

echo "--- released go.mod files"
for dir in $released; do
	replaces="$(cd "$src/$dir" && go mod edit -json | jq -r '.Replace // [] | length')"
	if [ "$replaces" -eq 0 ]; then
		pass "$dir/go.mod has no replace directive"
	else
		fail "$dir/go.mod still has $replaces replace directive(s)"
	fi
	if [ "$dir" != "." ]; then
		have="$(requires "$dir" "$root_module" || true)"
		if [ "$have" = "$version" ]; then
			pass "$dir requires $root_module $version"
		else
			fail "$dir requires $root_module ${have:-(none)}, want $version"
		fi
	fi
done

# released module | dependency that must not be required
for row in \
	". github.com/stretchr/testify" \
	". go.uber.org/goleak" \
	". github.com/jonboulle/clockwork"; do
	read -r dir dep <<<"$row"
	if have="$(requires "$dir" "$dep")"; then
		fail "$dir requires test-only $dep $have"
	else
		pass "$dir does not require $dep"
	fi
done
# no adapter requires testify or goleak
for dir in $released; do
	[ "$dir" != "." ] || continue
	for dep in github.com/stretchr/testify go.uber.org/goleak; do
		if have="$(requires "$dir" "$dep")"; then
			fail "$dir requires test-only $dep $have"
		else
			pass "$dir does not require $dep"
		fi
	done
done

# released module | runtime dependency that must stay
for row in \
	". github.com/samber/go-type-to-string" \
	"scheduler/gocron github.com/go-co-op/gocron/v2" \
	"clock/clockwork github.com/jonboulle/clockwork"; do
	read -r dir dep <<<"$row"
	if [ ! -f "$src/$dir/go.mod" ] || ! grep -qw "$dir" <<<"$released"; then
		fail "$dir is not released"
	elif have="$(requires "$dir" "$dep")"; then
		pass "$dir requires runtime $dep $have"
	else
		fail "$dir does not require runtime $dep"
	fi
done

# --- publish to a file proxy -------------------------------------------------

proxy="$work/proxy"
stage="$work/stage"
info_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# publish copies released directory $1 into the proxy as a module zip.
publish() {
	local dir="$1" path at excludes nested
	path="$(module_path "$dir")"
	at="$path@$version"
	excludes=(--exclude .git)
	# a module zip leaves out nested modules
	while IFS= read -r nested; do
		excludes+=(--exclude "/${nested#./}")
	done < <(cd "$src/$dir" && find . -mindepth 2 -name go.mod -exec dirname {} \;)
	mkdir -p "$stage/$at" "$proxy/$path/@v"
	rsync -a "${excludes[@]}" "$src/$dir/" "$stage/$at/"
	(cd "$stage" && zip -qrD "$proxy/$path/@v/$version.zip" "$at")
	cp "$src/$dir/go.mod" "$proxy/$path/@v/$version.mod"
	printf '{"Version":"%s","Time":"%s"}\n' "$version" "$info_time" >"$proxy/$path/@v/$version.info"
	echo "$version" >>"$proxy/$path/@v/list"
}

echo "--- publishing to a file proxy"
for dir in $released; do
	publish "$dir"
	pass "published $(module_path "$dir")@$version"
done

# --- clean consumers ---------------------------------------------------------

# consume runs a fresh consumer of module $1 against GOPROXY $2 and GOSUMDB
# $3 with module cache $4, writing its output to $5.
consume() {
	local mod="$1" goproxy="$2" sumdb="$3" cache="$4" log="$5" dir
	dir="$(mktemp -d "$work/consumer.XXXXXX")"
	(
		cd "$dir"
		export GOWORK=off GOFLAGS=-mod=mod GOPROXY="$goproxy" GOSUMDB="$sumdb" \
			GOMODCACHE="$cache" GONOSUMDB="$root_module" GOTOOLCHAIN=local
		go mod init example.com/consumer
		go get "$mod@$version"
		printf 'package main\n\nimport _ "%s"\n\nfunc main() {}\n' "$mod" >main.go
		go build -o "$dir/consumer" .
	) >"$log" 2>&1
}

# Pass 1 fills a private cache from the release proxy and the public proxy;
# its downloads are added to the release proxy, so pass 2 runs with
# GOPROXY set to the release proxy only and GOSUMDB off.
fill_cache="$work/fill-cache"
echo "--- clean consumers"
for dir in $released; do
	mod="$(module_path "$dir")"
	consume "$mod" "file://$proxy,$(go env GOPROXY)" "$(go env GOSUMDB)" "$fill_cache" "$work/fill.log" ||
		echo "note: filling the proxy for $mod failed; pass 2 reports it"
done
if [ -d "$fill_cache/cache/download" ]; then
	rsync -a --ignore-existing "$fill_cache/cache/download/" "$proxy/"
fi

for dir in $released; do
	mod="$(module_path "$dir")"
	log="$work/consumer-${dir//\//_}.log"
	if consume "$mod" "file://$proxy" off "$work/cache-${dir//\//_}" "$log"; then
		pass "a clean consumer gets and builds $mod@$version"
	else
		fail "a clean consumer cannot get and build $mod@$version:"
		sed 's/^/    | /' "$log" | tail -15
	fi
done

echo
if [ "$failures" -gt 0 ]; then
	echo "release dry run of $version: $failures check(s) failed"
	exit 1
fi
echo "release dry run of $version passed"
