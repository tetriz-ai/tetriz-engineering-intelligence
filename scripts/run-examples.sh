#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
#
# Runs every example and compares its output with its README. CI and contributors use this
# script. For each examples/<name>/README.md it takes the first ```sh block (the command) and the
# last ```text block (the expected standard output), runs the command from the repository root,
# and diffs the two. It also fails when the index lists a folder that does not exist, or misses
# one that does, and when the score-held-out-prs corpus is out of date.
set -euo pipefail
cd "$(dirname "$0")/.."

go vet ./examples/...
go run ./examples/score-held-out-prs/gencorpus -check

dirs="$(find examples -mindepth 1 -maxdepth 1 -type d | sort)"
indexed="$(grep -oE '\]\(([a-z0-9-]+)/\)' examples/README.md | sed -E 's/\]\(([a-z0-9-]+)\/\)/\1/' | sort -u)"

for d in $dirs; do
	name="${d#examples/}"
	if ! printf '%s\n' "$indexed" | grep -qx "$name"; then
		echo "example $name is not listed in examples/README.md" >&2
		exit 1
	fi
done
for name in $indexed; do
	if [ ! -d "examples/$name" ]; then
		echo "examples/README.md lists $name, which does not exist" >&2
		exit 1
	fi
done

extract_block() { # <file> <lang> <first|last>
	awk -v lang="$2" -v which="$3" '
		$0 == "```"lang { inblk=1; buf=""; next }
		inblk && $0 == "```" { inblk=0; last=buf; if (which=="first" && !seen) { print buf; seen=1 } next }
		inblk { buf = buf $0 "\n" }
		END { if (which=="last") printf "%s", last }
	' "$1"
}

fail=0
for d in $dirs; do
	name="${d#examples/}"
	readme="$d/README.md"
	cmd="$(extract_block "$readme" sh first)"
	want="$(extract_block "$readme" text last)"
	if [ -z "$cmd" ] || [ -z "$want" ]; then
		echo "FAIL $name: README needs an sh block and a text block" >&2
		fail=1
		continue
	fi
	got="$(eval "$cmd" 2>/dev/null)"
	if [ "$got" != "$want" ]; then
		echo "FAIL $name: output does not match its README" >&2
		diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") >&2 || true
		fail=1
	else
		echo "ok   $name"
	fi
done
exit "$fail"
