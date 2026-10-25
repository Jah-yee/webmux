#!/usr/bin/env bash
set -euo pipefail

# Start runner setup while the producer is building. Only this workflow run's
# artifact can satisfy the dependency; never reuse another commit's executable.
deadline=$((SECONDS + 300))
while (( SECONDS < deadline )); do
  artifact=$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/artifacts" \
    --jq '.artifacts[] | select(.name == "e2e-runtime" and .expired == false) | .id')
  if [[ -n "$artifact" ]]; then
    exit 0
  fi
  result=$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/jobs?per_page=100" \
    --jq '.jobs[] | select(.name == "Format, check and build") | .conclusion // ""')
  if [[ -n "$result" && "$result" != success ]]; then
    echo "E2E runtime producer finished with $result" >&2
    exit 1
  fi
  sleep 2
done
echo "Timed out waiting for this run's E2E runtime" >&2
exit 1
