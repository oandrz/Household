#!/usr/bin/env bash
# Enforces the clean-architecture dependency rule:
#   internal/domain  imports no other internal package
#   internal/usecase imports internal/domain only
# Anything under internal/adapter, internal/testsupport and cmd may import
# whatever it needs.
set -euo pipefail

cd "$(dirname "$0")/../api"

MODULE="github.com/andreasoentoro/hearth/api"
violations=0

# A module that does not compile must be a hard error, not a violation-free
# pass. go list tolerates breakage that the compiler rejects — it can exit 0
# and silently omit the offending package's imports — so gate on a real build
# first, and run go list outside a process substitution so set -e can see it.
go build ./... >/dev/null

imports=$(go list -f '{{$p := .ImportPath}}{{range .Imports}}{{$p}} {{.}}
{{end}}{{range .TestImports}}{{$p}} {{.}}
{{end}}{{range .XTestImports}}{{$p}} {{.}}
{{end}}' ./...)

while read -r pkg imp; do
    [ -n "$pkg" ] || continue
    case "$pkg" in
        "$MODULE/internal/domain"|"$MODULE/internal/domain"/*)
            case "$imp" in
                "$MODULE/internal/domain"|"$MODULE/internal/domain"/*) ;;
                "$MODULE"|"$MODULE"/*)
                    echo "domain must not import internal packages: $pkg -> $imp"
                    violations=$((violations + 1))
                    ;;
            esac
            ;;
        "$MODULE/internal/usecase"|"$MODULE/internal/usecase"/*)
            case "$imp" in
                "$MODULE/internal/domain"|"$MODULE/internal/domain"/*) ;;
                "$MODULE/internal/usecase"|"$MODULE/internal/usecase"/*) ;;
                "$MODULE"|"$MODULE"/*)
                    echo "usecase may import domain only: $pkg -> $imp"
                    violations=$((violations + 1))
                    ;;
            esac
            ;;
    esac
done <<< "$imports"

if [ "$violations" -gt 0 ]; then
    echo "architecture lint failed with $violations violation(s)"
    exit 1
fi

echo "architecture lint passed"
