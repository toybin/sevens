#!/usr/bin/env bash
# Generate a type/function definition map of the sevens codebase,
# organized by package → file. Output is markdown.
set -euo pipefail

ROOT="${1:-$(cd "$(dirname "$0")/.." && pwd)}"

echo "# API Map"
echo ""
echo "Auto-generated from source. Do not edit."
echo ""
echo '```'
echo "Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "Commit:    $(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
echo '```'
echo ""

current_pkg=""

grep -rn "^type \|^func " --include="*.go" "$ROOT" \
  | grep -v "_test\.go:" \
  | sed "s|^${ROOT}/||" \
  | sort -t: -k1,1 -k2,2n \
  | while IFS=: read -r file line decl; do
    # Derive package from directory
    pkg=$(dirname "$file")

    if [ "$pkg" != "$current_pkg" ]; then
      current_pkg="$pkg"
      echo "## \`$pkg\`"
      echo ""
      prev_file=""
    fi

    base=$(basename "$file")
    if [ "$base" != "$prev_file" ]; then
      prev_file="$base"
      echo "### $base"
      echo ""
    fi

    # Clean up the declaration: strip trailing brace, trim
    clean=$(echo "$decl" | sed 's/ {$//' | sed 's/[[:space:]]*$//')
    echo "- \`$clean\`"
  done

echo ""
