#!/bin/bash
# Post etch diff results as a GitHub PR comment.
# Usage: scripts/pr-comment.sh
#
# Requires:
#   GITHUB_TOKEN - GitHub API token with PR comment permissions
#   GITHUB_REPOSITORY - owner/repo (set automatically in GitHub Actions)
#   PR_NUMBER - the pull request number
#
# Typically used in a GitHub Actions workflow:
#
#   - name: Run etch test
#     run: etch test --ci --snap-dir .etch/snapshots 2>&1 | tee /tmp/etch-output.txt || true
#
#   - name: Comment on PR
#     if: github.event_name == 'pull_request'
#     env:
#       GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
#       PR_NUMBER: ${{ github.event.pull_request.number }}
#     run: bash scripts/pr-comment.sh

set -e

if [ -z "$GITHUB_TOKEN" ] || [ -z "$GITHUB_REPOSITORY" ] || [ -z "$PR_NUMBER" ]; then
  echo "Missing required env vars: GITHUB_TOKEN, GITHUB_REPOSITORY, PR_NUMBER"
  exit 1
fi

# run etch diff and capture output
DIFF_OUTPUT=$(etch diff --ci --snap-dir .etch/snapshots 2>&1 || true)

if [ -z "$DIFF_OUTPUT" ] || echo "$DIFF_OUTPUT" | grep -q "No pending diffs"; then
  BODY="## Etch API Snapshot Test\n\n:white_check_mark: No API changes detected."
else
  # escape for JSON
  ESCAPED=$(echo "$DIFF_OUTPUT" | sed 's/\\/\\\\/g' | sed 's/"/\\"/g' | sed ':a;N;$!ba;s/\n/\\n/g')
  BODY="## Etch API Snapshot Test\n\n:warning: API changes detected:\n\n\`\`\`\n${ESCAPED}\n\`\`\`\n\nRun \`etch approve\` to accept these changes."
fi

# post the comment
curl -s -X POST \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.v3+json" \
  "https://api.github.com/repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments" \
  -d "{\"body\": \"$BODY\"}"

echo "PR comment posted."
