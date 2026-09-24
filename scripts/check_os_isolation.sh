#!/usr/bin/env bash
# PAT-004 guard: OS-specific branching belongs behind the shell and doctor
# interfaces. If runtime.GOOS appears anywhere else, the portability argument
# in the architecture document has quietly stopped being true.
set -euo pipefail

violations=$(grep -rn --include='*.go' 'runtime\.GOOS' . \
  | grep -v '^\./internal/shell/' \
  | grep -v '^\./internal/doctor/' \
  | grep -v '^\./scripts/' || true)

if [[ -n "$violations" ]]; then
  echo "OS-specific branching found outside internal/shell/ and internal/doctor/:"
  echo "$violations"
  echo
  echo "Move this logic behind the shell.Detector or doctor.Check interface."
  exit 1
fi

echo "OS isolation check passed."
