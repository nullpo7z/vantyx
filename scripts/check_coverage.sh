#!/usr/bin/env bash
set -eo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 COVERAGE_FILE MIN_PERCENT" >&2
  exit 1
fi

file="$1"
min="$2"

if [[ ! -f "$file" ]]; then
  echo "coverage file not found: $file" >&2
  exit 1
fi

percent=$(go tool cover -func="$file" | awk '/^total:/ {print $3}' | tr -d '%')
if [[ -z "${percent}" ]]; then
  echo "failed to parse coverage from $file" >&2
  exit 1
fi

percent_int=${percent%.*}

if (( percent_int < min )); then
  echo "coverage ${percent}% is below required ${min}%"
  exit 1
fi

echo "coverage ${percent}% meets required ${min}%"

