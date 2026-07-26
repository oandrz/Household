#!/usr/bin/env bash
# Enforces the clean-architecture dependency rule:
#   internal/domain  imports no other internal package
#   internal/usecase imports internal/domain only
# Anything under internal/adapter and cmd may import whatever it needs.
set -euo pipefail

cd "$(dirname "$0")/../api"

MODULE="github.com/andreasoentoro/hearth/api"
violations=0

# Emits "<package> <import>" for every internal import in the module.
while read -r pkg imp; do
    case "$pkg" in
        "$MODULE/internal/domain"*)
            case "$imp" in
                "$MODULE/internal/domain"*) ;;
                "$MODULE"/*)
                    echo "domain must not import internal packages: $pkg -> $imp"
                    violations=$((violations + 1))
                    ;;
            esac
            ;;
        "$MODULE/internal/usecase"*)
            case "$imp" in
                "$MODULE/internal/domain"*|"$MODULE/internal/usecase"*) ;;
                "$MODULE"/*)
                    echo "usecase may import domain only: $pkg -> $imp"
                    violations=$((violations + 1))
                    ;;
            esac
            ;;
    esac
done < <(go list -f '{{$p := .ImportPath}}{{range .Imports}}{{$p}} {{.}}
{{end}}' ./... | grep -v '^$')

if [ "$violations" -gt 0 ]; then
    echo "architecture lint failed with $violations violation(s)"
    exit 1
fi

echo "architecture lint passed"
