---
name: cookbook-forge
description: >-
  Generate a deep, book-quality HTML "cookbook" (in Chinese) that teaches a topic
  from scratch: research and compare sources, plan a custom chapter structure,
  write thoroughly, and build a single-page HTML book that opens offline and
  deploys anywhere. Use when the user asks to "make/forge/build a cookbook",
  guide, handbook, primer, or field guide on a topic, or to turn a pile of
  material into a polished, navigable book. Emphasizes depth, truthfulness, and
  rich formatting (tables, diagrams, callouts, code).
---

# cookbook-forge

## 依赖安装

这个 skill 需要 Node.js 来运行随附的 `build.mjs`、`check.mjs` 和
`quality-check.mjs`；浏览器用于打开生成的 `index.html`。安装 Node.js 18 或更高版本，
并确认命令可用：

```bash
node --version
```

可以从 [nodejs.org](https://nodejs.org/) 安装，或使用系统包管理器（例如
macOS 的 `brew install node`、Debian/Ubuntu 的 `sudo apt install nodejs`）。
本 skill 不需要 `npm install`，模板中的渲染库已经随 skill 一起提供。

Turn a topic into a thorough, trustworthy, beautifully-typeset **Chinese**
cookbook: a single-page HTML book a reader can use to learn a subject **quickly
and completely**. Two things matter equally — the content must be **true and
deep**, and the presentation must be **book-quality** (narrative, structure,
typography).

The output is `index.html` (plus editable `docs/` Markdown sources). After
`build.mjs`, both the content and the render libraries (marked / DOMPurify /
highlight.js) are inlined into the file, so it opens directly from the
filesystem **offline** and also serves over nginx / GitHub Pages. (Mermaid
diagrams and display fonts still come from a CDN when online and degrade
gracefully offline.) The book is Chinese-only by design — keep all prose,
titles, and metadata in Chinese.

## When to use

User asks to build/forge/generate a cookbook, guide, handbook, primer, or field
guide on a topic — or to turn a pile of material into a polished, navigable book.

## The bundled assets

This skill ships everything needed; paths are relative to this skill's folder:

- `assets/template/` — the complete book scaffold to copy and fill:
  - `index.html` — the reader app (editorial theme + engine). Don't rewrite it.
  - `manifest.json` — book structure + cover metadata (heavily commented).
  - `docs/*.md` — chapter sources (Markdown). Replace the stubs.
  - `vendor/*.js` — the render libs build.mjs inlines for offline use. Keep them.
  - `assets/images/` — put localized figures here (CSP blocks remote images).
  - `build.mjs` — inlines content + render libs into `index.html` (strict: fails
    on missing chapters; `--allow-missing` for drafts).
  - `check.mjs` — link / anchor / image audit.
  - `quality-check.mjs` — structural-density / placeholder / sources audit.
  - `.nojekyll`, `README.md` — GitHub Pages support + deploy notes.
- `assets/grounding-template.md` — the single-source-of-truth notes file to copy
  into the working area and fill during research (fact ledger + sources).
- `references/` — read these before the matching phase:
  - `research-method.md` — gather, compare, analyze sources deeply.
  - `chapter-blueprint.md` — preset chapter palette + the planning method.
  - `writing-craft.md` — voice, formatting, and the truthfulness discipline.
  - `review-rubric.md` — the 1–3 reflection/optimization rounds.

## The process

Work through these phases in order. Use a task list to track them. **Do not rush
to HTML** — most of the value is in research and writing; the template handles
presentation.

### Phase 0 — Intake & scope

Establish, asking the user only what you can't reasonably infer:
- **The topic** and the **learning goal** ("after this book the reader can ___").
- **Material**: did the user provide sources? If yes, treat them as primary and
  **ask whether to also research the web to fill gaps** (see `research-method.md`).
  If no material, you'll research from scratch.
- **Depth/size**: default to a thorough book sized to the topic
  (see sizing in `chapter-blueprint.md`); confirm if the user implied something
  smaller or larger.

For genuine forks (e.g. supplement-with-web? scope?): if the host supports a
structured question UI (such as `AskUserQuestion`), use it; otherwise ask a
short plain-text question. Otherwise pick sensible defaults and state them.

### Phase 1 — Research (don't skimp)

Follow `references/research-method.md`. Cast wide, then go deep on canonical
sources. Triangulate every non-trivial claim. Verify by running/testing whatever
can be verified. Copy `assets/grounding-template.md` into your working area as
**`grounding.md`** (kept out of the published book) and fill its fact ledger —
each fact with source, evidence grade, and target chapter. This becomes the only
source of factual claims and seeds the Sources appendix.

The bar: enough grounded material to write every chapter without ever reaching
for memory. This is where "thorough vs shallow" is decided.

### Phase 2 — Plan the chapters

Follow `references/chapter-blueprint.md`. Start from the preset archetype palette,
but **design a structure specific to this topic** — select, rename, reorder, and
add the 1–3 topic-specific chapters no generic outline predicts. Order along the
Why → What → How → Deeper arc and by dependency. Write a one-line promise per
chapter. **Present the plan to the user for one round of confirmation** before
deep authoring — reordering is cheap now, expensive later.

### Phase 3 — Scaffold

Copy `assets/template/` to the output location (default: a new folder in the
user's working directory, e.g. `./<topic-slug>-cookbook/`). Keep `vendor/` and
`assets/images/`. Then:
- Fill in `manifest.json`: `site` block (brand, tagline, kicker, lead, stats,
  dateline) and the `parts`/`chapters` from your plan. Give each chapter a stable
  `id`, a `num`, a `title`, and a `file` path (e.g. `docs/p1-01-foo.md`). Remove
  the `_comment`/`_help` keys (build.mjs also strips them, but keep it tidy).
- Delete the template stub `docs/` files you're not using; create the empty
  chapter files your manifest references.
- Pick a **brand/metaphor** for the book if one fits the topic (the reference
  book used weaving). A good through-metaphor lifts the whole book; a forced one
  drags it. Optional — a clean descriptive title is fine.

### Phase 4 — Write, deeply

A full book is **many chapters — often 100k+ characters — and it cannot be
written in a single response.** Write **one chapter per step** (one chapter per
turn), grounded in `grounding.md`, and tick it off your task list as you go. A
chapter truncated mid-sentence, or thinned to "fit", is worse than one written
in its own turn. Never attempt to emit the whole book at once.

Write each chapter (in Chinese) into its `docs/<id>.md`, following
`references/writing-craft.md`. For every chapter:
- Teach **one idea thoroughly**: problem → concrete example → mechanism → edge
  cases → when *not* to use it.
- Pull only from the grounding file; grade evidence in the prose; label examples
  real vs illustrative.
- **Alternate prose with structure** — tables for comparisons, mermaid for
  processes, callouts for warnings/tips, code cards for anything copyable. A wall
  of paragraphs is a failure even if every word is true.
- Write in a real human voice; cut AI-tell phrasing and hype.
- Cross-link related chapters (`#/<id>`). Localize any figure into
  `assets/images/` and reference it locally (remote images are CSP-blocked).

Write the cover metadata, a strong preface, and the appendices (glossary,
sources, quick reference) too — they're part of the book.

### Phase 5 — Assemble & verify

From the cookbook root:
- `node check.mjs` → fix until `TOTAL ISSUES: 0` (links, anchors, images).
- `node quality-check.mjs` → clear placeholders/no-structure/missing-sources;
  review any "thin" chapters for depth.
- `node build.mjs` → builds strict (aborts on missing chapters) and inlines
  content + render libs.
- Open the built `index.html` in a browser (or have the user open it), ideally
  offline, and click through: cover, navigation, code/tables/diagrams.

### Phase 6 — Reflect & optimize (1–3 rounds)

Follow `references/review-rubric.md`. Run **at least one, at most three** review
rounds. Each round: audit the whole book against the rubric (truthfulness,
depth, structure, voice, visual richness, presentation), fix what you find,
re-run `check.mjs`/`build.mjs`. Stop when a round surfaces only cosmetic nits.
Review as a paying reader hunting for what's wrong — not as the proud author.

### Phase 7 — Deliver

Hand over the cookbook folder. Tell the user, concisely:
- How to read it: open `index.html` directly (works offline), or serve the folder
  (nginx / GitHub Pages — `.nojekyll` is included; it's hash-routed so deep links
  work).
- How to edit: `docs/*.md` + `manifest.json`, then `node build.mjs`.
- An honest note on anything that stayed thin or unverified, if applicable.

## Guardrails

- **Truthfulness beats everything.** A confident error destroys the book's value.
  When unsure, verify or mark "(unverified)" — never fabricate APIs, numbers,
  quotes, or capabilities.
- **Depth over volume.** More words isn't better; more *understanding per page*
  is. Cut padding ruthlessly.
- **Don't rewrite the engine.** `index.html`'s theme and JS are the proven,
  accessible presentation layer — drive it through `manifest.json` and Markdown.
  Only touch its CSS variables if the user wants a different accent color.
- **Keep the build green.** Ship only after `check.mjs` and `quality-check.mjs`
  pass, `build.mjs` succeeds (strict), and the built file opens from `file://`
  offline.
- **Chinese-only.** All prose, titles, and metadata are Chinese; don't reintroduce
  a second language.
