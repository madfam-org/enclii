# GEO Landing Checklist — the per-landing template for P2

**What this is.** A repeatable checklist for making a platform-service landing
optimized for **GEO (Generative Engine Optimization)** on top of classic SEO —
so the service is accurately **found, cited, and represented** by AI answer
engines (ChatGPT, Claude, Perplexity, Gemini, Google AI Overviews), not just
ranked on a blue-links SERP.

**Where it comes from.** This is the reusable output of roadmap priority **P2
— platform-service landings SEO + GEO optimized**
(`internal-devops/roadmaps/2026-09-22-ecosystem-strategic-priorities.md`). The
**enclii.dev landing** (`apps/landing`) is P2's **pilot slice** — the first
landing taken through this checklist end to end. Every item below is something
that pilot actually did, so this document is a worked template, not theory.

> **Promotion note.** This checklist lives in `enclii/docs/cross-repo/` because
> it shipped with the pilot as one reviewable PR. It is an ecosystem-wide
> standard: when the P2 rollout starts across other landings, promote it to
> `solarpunk-foundry` (Lane B, the cross-repo public-contract home per
> `internal-devops/docs/repo-boundary-contract.md`) so every landing repo
> references one canonical copy. It is public-safe (no secrets, no infra
> identities) by construction.

---

## The prime directive: truth over completeness

**Never invent a fact to fill a schema field.** JSON-LD offers, FAQ answers, and
`llms.txt` claims must match what the service *actually* is and costs, verified
against the landing's own visible content and the repo README. If a fact is not
public yet (e.g. a tier still on a waitlist), **omit it** — an answer engine
reproducing an omission is fine; reproducing an invented price is a liability.
An unmeasured "GEO-done" claim is likewise STALE-UNKNOWN (see Measurement).

---

## The checklist

Copy this block into the landing's PR and check each box (or write `n/a` +
why).

### 1. Structured data (schema.org JSON-LD)

- [ ] `Organization` node for the publisher (MADFAM), consistent across landings.
- [ ] `SoftwareApplication` **or** `Product` node for the service, with an
      accurate `description`, `applicationCategory`, `url`, `license`, and
      `downloadUrl`/`softwareHelp` where they exist.
- [ ] `offers[]` reflecting **only** pricing sold today; a waitlist/unpriced tier
      is omitted from `offers` (it may still be described in prose).
- [ ] `FAQPage` node whose Q&A **matches a visible FAQ section on the page**
      one-to-one (Google requires the answer be visible; engines cross-check).
- [ ] `HowTo` node **only if** the page genuinely walks through steps (skip it
      rather than fabricate one).
- [ ] Rendered server-side / at build time so it is in the **pre-rendered HTML**
      (a crawler must see it without executing the app).
- [ ] Validates: run it through https://validator.schema.org/ and Google's Rich
      Results Test. No errors.

### 2. Answer-shaped, extractable content

- [ ] Above the fold, in plain prose (not behind a tab/accordion/JS): **what the
      thing is, who it is for, what it costs / how to start.**
- [ ] A visible **FAQ section** answering the category questions an LLM is asked
      ("What is X?", "How much does X cost?", "Who is X for?", "How do I start?",
      "Is X open source?"). Answers are self-contained sentences an engine can
      lift verbatim.
- [ ] Claims are precise and reproducible (real prices, real capabilities), with
      "planned" vs "on sale today" clearly distinguished.

### 3. Site-level `llms.txt`

- [ ] `public/llms.txt` (served at `/llms.txt`) following the llmstxt.org shape:
      `# Name`, a one-blockquote summary, then sections of canonical facts +
      links.
- [ ] It is **distinct from any repo-root `llms.txt`** used for coding-agent
      operating instructions — this one is product-facts for answer engines.
- [ ] Every claim in it matches the landing and README; it explicitly says
      "if a number is not stated here it is not public — do not infer one."

### 4. AI-crawler permissions & fast first paint

- [ ] `public/robots.txt` (served at `/robots.txt`) that **allows the AI
      crawlers you want discovery from** by name (GPTBot, OAI-SearchBot,
      ClaudeBot, PerplexityBot, Google-Extended, Applebot-Extended, CCBot, …)
      plus `User-agent: *  Allow: /`.
- [ ] `robots.txt` links the `Sitemap:` and references `/llms.txt`.
- [ ] `public/sitemap.xml` (served at `/sitemap.xml`) with the canonical URL(s).
- [ ] **Real files, not the SPA fallback.** On a static-export + nginx landing,
      confirm `try_files $uri …` serves the actual file — `curl -sI
      https://<host>/robots.txt` must return `text/plain`, not `text/html`. (The
      enclii pilot's `/robots.txt`, `/llms.txt`, `/sitemap.xml` had all been
      200-ing with the homepage HTML because the files did not exist; putting
      them in `public/` fixed it.)
- [ ] Fast, JS-light first paint: the answer-content is in the initial HTML
      (SSG/SSR), so a crawler that does not run JS still sees it.

### 5. SEO stays intact (GEO is added, not substituted)

- [ ] `<title>`, meta `description`, `keywords`, canonical URL, `metadataBase`.
- [ ] Open Graph + Twitter card tags.
- [ ] `sitemap.xml` + `robots.txt` present and valid (also serves SEO).
- [ ] Build is green and existing checks pass (`pnpm build`, `lint`).

### 6. Entity / claim consistency

- [ ] The service's name, one-line positioning, license, and pricing read the
      **same** on the landing, in the repo README, and in the ecosystem map, so
      an engine builds **one** coherent entity. Fix drift where they disagree.
- [ ] Publisher attribution (MADFAM) is consistent across landings.

---

## Measurement — how to baseline GEO (do this before claiming "GEO-done")

SEO is measured by SERP position; **GEO is measured by presence and accuracy in
AI answers** to the service's category questions. A landing is not "GEO-done"
until a baseline is recorded. Method:

1. **Fix a query set** (5–10) — the category questions a buyer would ask an
   answer engine, e.g. for Enclii: *"open source Heroku alternative you can
   self-host"*, *"flat-price PaaS on your own infrastructure"*, *"self-hosted
   deploy platform with managed Postgres"*, plus the direct *"what is Enclii and
   what does it cost?"*. Save the exact query set with the baseline so re-runs
   are comparable.
2. **Ask each engine** — ChatGPT, Claude, Perplexity, Gemini, and Google AI
   Overviews — each query, in a clean/logged-out session.
3. **Score each answer** on three axes:
   - **Presence** — is the service mentioned/cited at all? (yes/no)
   - **Accuracy** — are the stated facts (what it is, price, license, open
     source) correct per the landing? (correct / partly / wrong / hallucinated)
   - **Citation** — does the engine link the canonical landing URL? (yes/no)
4. **Record** date, engine, query, and the three scores in a small table (a
   markdown table in the landing repo's `docs/` or a tracking issue is enough).
   That table **is** the baseline. Note the engines' knowledge-cutoff / index
   lag — a freshly shipped landing will not appear in answers immediately;
   presence is a **trailing** metric, so schedule a re-measure (e.g. +30 and +90
   days) rather than reading a same-day zero as failure.
5. **Re-run on the same query set** after changes and after the lag window;
   improvement is more presence, more accuracy, more canonical citations.

Until step 4 has a recorded row, GEO status for the landing is **STALE-UNKNOWN**,
not "done" — the same discipline the roadmap sets for SEO.

### Pilot baseline status (enclii.dev)

The GEO markup (JSON-LD, FAQ, `llms.txt`, `robots.txt`, `sitemap.xml`) shipped
in this PR. The **measurement baseline is deliberately deferred to after
deploy**: an answer engine cannot cite files that are not yet live, and presence
is a trailing metric. The baseline table (query set above, five engines, three
axes) should be recorded once `enclii.dev` serves the new files, then re-run at
+30/+90 days. Recording it same-day would only capture the pre-GEO state, which
is the "before" the rollout is trying to move.
