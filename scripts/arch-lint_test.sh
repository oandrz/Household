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

echo "case 3: a broken build must fail the lint"
mkdir -p api/internal/domain
cat > api/internal/domain/broken.go <<'GO'
package domain

stray statement here
GO

if ./scripts/arch-lint.sh; then
    rm -f api/internal/domain/broken.go
    rmdir api/internal/domain
    echo "FAIL: arch-lint.sh accepted a broken build"
    exit 1
fi

rm -f api/internal/domain/broken.go
rmdir api/internal/domain

echo "case 4: a violation in a _test.go file must be caught"
mkdir -p api/internal/domain
cat > api/internal/domain/domain.go <<'GO'
package domain
GO

cat > api/internal/domain/violation_test.go <<'GO'
package domain

import _ "github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
GO

if ./scripts/arch-lint.sh; then
    rm -f api/internal/domain/domain.go api/internal/domain/violation_test.go
    rmdir api/internal/domain
    echo "FAIL: arch-lint.sh accepted a test import violation"
    exit 1
fi

rm -f api/internal/domain/domain.go api/internal/domain/violation_test.go
rmdir api/internal/domain
echo "arch-lint self-check passed"
