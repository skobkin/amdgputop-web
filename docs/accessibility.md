# Accessibility & Lighthouse Baseline

This project maintains a repeatable accessibility audit and Lighthouse baseline so regressions can be caught early.

## Accessibility

- Tool: [`pa11y`](https://pa11y.org/) (uses headless Chromium via Puppeteer)
- Command:
  ```bash
  cd web
  npm run build
  npx pa11y "file://$(pwd)/../internal/httpserver/assets/index.html" --reporter json
  ```
- Result (2025-03-11): `[]` — no violations detected against the default WCAG2AA ruleset.

Store updated reports under `docs/` when material changes land in the frontend.

## Lighthouse

- Tool: [`@lhci/cli`](https://github.com/GoogleChrome/lighthouse-ci)
- Command (from repo root):
  ```bash
  npm run build --prefix web
  npx @lhci/cli collect \
    --staticDistDir=internal/httpserver/assets \
    --numberOfRuns=1
  ```
- Scores (Performance/Accessibility/Best-Practices/SEO): `1.00 / 0.92 / 0.96 / 0.90`

Artifacts are generated under `.lighthouseci/` (ignored by Git). Attach them to release notes or store externally if long-term archiving is needed.
