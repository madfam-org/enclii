#!/usr/bin/env bash
# Run in the final image as its normal runner user, with a read-only Node mount.
set -euo pipefail
export PATH="/opt/smoke-node/bin:${PATH}"
test "$(id -u)" -ne 0
test -n "${EXPECTED_AGENT:?expected runner version required}"
agent_version="$(/home/runner/bin/Runner.Listener --version)"
test "${agent_version}" = "${EXPECTED_AGENT}"
gh --version

smoke_dir="$(mktemp -d)"
trap 'rm -rf "${smoke_dir}"' EXIT
# Match the version resolved by this repository's pnpm lockfile.
npm install --prefix "${smoke_dir}" --no-audit --no-fund --ignore-scripts playwright@1.58.2
export PLAYWRIGHT_BROWSERS_PATH="${smoke_dir}/browsers"
"${smoke_dir}/node_modules/.bin/playwright" install chromium
node - "${smoke_dir}" <<'JS'
const assert = require('node:assert/strict');
const { chromium } = require(`${process.argv[2]}/node_modules/playwright`);
(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    await page.setContent('<button onclick="this.textContent=\'Passed\'">Run</button>');
    await page.getByRole('button', { name: 'Run', exact: true }).click();
    assert.equal(await page.getByRole('button').textContent(), 'Passed');
    assert.ok((await page.screenshot()).length > 0);
    console.log('Candidate Chromium interaction and rendering passed');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
JS
