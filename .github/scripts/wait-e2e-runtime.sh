#!/usr/bin/env bash
set -euo pipefail

# Start runner setup while the producer is building. Only this workflow run's
# artifact can satisfy the dependency; never reuse another commit's executable.
# Type-aware frontend lint can make the producer longer than the old 5-minute wait.
deadline=$((SECONDS + 1200))

# GitHub's API occasionally returns transient 5xx responses under load; retry
# them instead of failing the shard on a single bad response. A failed call
# still prints the error body to stdout, so only a successful attempt's output
# is passed on; otherwise it would be read as the producer's conclusion.
gh_api() {
  local attempt out
  for attempt in 1 2 3 4 5; do
    if out=$(gh api "$@"); then
      printf '%s' "$out"
      return 0
    fi
    if (( attempt < 5 )); then
      sleep 2
    fi
  done
  return 1
}

while (( SECONDS < deadline )); do
  artifact=$(gh_api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/artifacts" \
    --jq '.artifacts[] | select(.name == "e2e-runtime" and .expired == false) | .id') || artifact=""
  if [[ -n "$artifact" ]]; then
    exit 0
  fi
  result=$(gh_api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/jobs?per_page=100" \
    --jq '.jobs[] | select(.name == "Format, check and build") | .conclusion // ""') || result=""
  if [[ -n "$result" && "$result" != success ]]; then
    echo "E2E runtime producer finished with $result" >&2
    exit 1
  fi
  sleep 2
done
echo "Timed out waiting for this run's E2E runtime" >&2
exit 1
