#!/usr/bin/env bash
# Verifies that arch-lint.sh actually rejects a violation, rather than passing
# vacuously. It plants a forbidden import, expects failure, then removes it.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "case 1: the current tree must pass"
./scripts/arch-lint.sh

echo "case 2: a domain package importing an adapter must fail"
mkdir -p api/internal/domain
cat > api/internal/domain/violation.go <<'GO'
package domain

import _ "github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
GO

if ./scripts/arch-lint.sh; then
    rm -f api/internal/domain/violation.go
    rmdir api/internal/domain
    echo "FAIL: arch-lint.sh accepted a domain -> adapter import"
    exit 1
fi

rm -f api/internal/domain/violation.go
rmdir api/internal/domain
echo "arch-lint self-check passed"
