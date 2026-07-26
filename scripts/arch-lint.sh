#!/usr/bin/env bash
# Enforces the clean-architecture dependency rule:
#   internal/domain  imports no other internal package and no third-party
#                     package -- standard library only.
#   internal/usecase imports internal/domain and the standard library only.
# Anything under internal/adapter, internal/testsupport and cmd may import
# whatever it needs, including third-party infrastructure libraries -- that
# is the whole point of the rule: infrastructure lives only where it can be
# swapped out.
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
                *)
                    # Standard-library import paths have no dot in their
                    # first segment ("net/http", "errors"); third-party ones
                    # do ("github.com/...", "golang.org/...").
                    case "${imp%%/*}" in
                        *.*)
                            echo "domain must not import third-party packages: $pkg -> $imp"
                            violations=$((violations + 1))
                            ;;
                    esac
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
                *)
                    case "${imp%%/*}" in
                        *.*)
                            echo "usecase must not import third-party packages: $pkg -> $imp"
                            violations=$((violations + 1))
                            ;;
                    esac
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
