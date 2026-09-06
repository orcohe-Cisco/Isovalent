#!/usr/bin/env bash
#
# Guard against bash 4+ syntax in the scripts people run on their laptops.
#
# macOS still ships bash 3.2 (2007, GPLv2 — Apple never shipped a newer one),
# so anything using mapfile, associative arrays or ${var,,} dies on a stock Mac
# with a bare "command not found" that gives no clue what is wrong. This has
# bitten us once; the check exists so it cannot happen twice.
#
#   bash hack/check-bash32.sh            # check the default set
#   bash hack/check-bash32.sh path.sh …  # check specific files
#
set -euo pipefail

cd "$(cd "$(dirname "$0")/.." && pwd)"

if [ "$#" -gt 0 ]; then
  FILES="$*"
else
  # Skip this file: its own patterns contain the keywords it looks for.
  FILES="$(find . -name '*.sh' -type f \
    -not -path './frontend/node_modules/*' -not -path './.git/*' -not -path './backend/vendor/*' \
    | grep -v 'check-bash32.sh' | sort)"
fi

# Feature                        first available in
#   mapfile / readarray          bash 4.0
#   declare -A (assoc arrays)    bash 4.0
#   ${var,,} ${var^^}            bash 4.0
#   &>> (append both streams)    bash 4.0
#   ;;& in case                  bash 4.0
#   coproc                       bash 4.0
#   ** globstar                  bash 4.0
#   $'\u' escapes                bash 4.2
#   ${var@Q}                     bash 4.4
PATTERN='(^|[^[:alnum:]_])(mapfile|readarray|coproc)([^[:alnum:]_]|$)'
PATTERN="$PATTERN"'|declare[[:space:]]+-[A-Za-z]*A|local[[:space:]]+-[A-Za-z]*A'
PATTERN="$PATTERN"'|\$\{[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?,,'
PATTERN="$PATTERN"'|\$\{[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\^\^'
PATTERN="$PATTERN"'|\$\{[A-Za-z_][A-Za-z0-9_]*@[QEPAa]\}'
PATTERN="$PATTERN"'|&>>|;;&|shopt[[:space:]]+-s[[:space:]]+globstar'

status=0

for f in $FILES; do
  [ -f "$f" ] || continue

  # Strip comments before matching so the explanatory notes in the scripts
  # themselves do not trip the check.
  hits="$(sed 's/#.*$//' "$f" | grep -nE "$PATTERN" || true)"
  if [ -n "$hits" ]; then
    echo "FAIL $f — bash 4+ syntax:"
    echo "$hits" | sed 's/^/       /'
    status=1
  fi

  # "${arr[@]}" on a possibly-empty array is an unbound-variable error under
  # `set -u` on bash 3.2, but fine on 4.4+. Flag arrays in scripts that set -u.
  if grep -qE 'set[[:space:]]+-[a-z]*u' "$f"; then
    arrays="$(sed 's/#.*$//' "$f" | grep -nE '\[[@*]\]|\+=\(' || true)"
    if [ -n "$arrays" ]; then
      # Warning, not failure: a literal non-empty array is fine on 3.2. Read the
      # hit and confirm the array can never be empty at that point.
      echo "WARN $f — array expansion with 'set -u' (errors on bash 3.2 when empty):"
      echo "$arrays" | sed 's/^/       /'
    fi
  fi

  bash -n "$f" || status=1
done

if [ "$status" -eq 0 ]; then
  echo "OK — all scripts are bash 3.2 compatible"
fi
exit "$status"
