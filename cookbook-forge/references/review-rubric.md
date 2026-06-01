# Review Rubric — The Reflection & Optimization Rounds

A first draft is never the book. After assembling the full cookbook, run **at
least one and at most three** review rounds. Each round: audit against the
rubric, fix what you find, stop when a round surfaces only cosmetic nits.

> Review the book *as a reader who paid for it*, not as the author who is proud
> of it. Look for what's wrong, thin, or boring — not for reasons it's fine.

## How to run a round

1. **Mechanical pass (always first, it's cheap and automated):**
   - `node check.mjs` → expect `TOTAL ISSUES: 0` (links, anchors, images).
   - `node quality-check.mjs` → no placeholders, no zero-structure body chapters,
     sources appendix present; review any "thin" chapters for depth.
   - `node build.mjs` → builds strict (no missing chapters) and inlines render libs.
   - Open the built `index.html` and click through: cover renders, sidebar
     navigates, code/tables/diagrams display, and it works from `file://` offline.
2. **Content audit** against the dimensions below — read actual chapters, don't
   spot-check titles.
3. **Fix** everything found this round.
4. **Decide:** if the round found substantive problems, do another (up to 3). If
   it found only polish, stop.

## The rubric

### Truthfulness (highest priority — a single confident error costs trust)
- [ ] Every factual claim traces to the grounding file or a cited source.
- [ ] Evidence is graded honestly in the prose (verified vs reported vs inferred);
      no second-hand claim is laundered into fact.
- [ ] Examples labeled real vs illustrative; "real" outputs are actually real.
- [ ] No invented APIs, numbers, quotes, dates, or capabilities. Re-verify
      anything that "sounds right" but you can't point to a source for.
- [ ] Sources appendix is complete; relative dates converted to absolute.

### Completeness & depth (the "thorough, not shallow" mandate)
- [ ] Each chapter delivers on its one-line promise — reader can *do* the thing.
- [ ] No chapter is padding: skims the surface, restates the obvious, or could be
      cut with nothing lost.
- [ ] Edge cases, failure modes, and trade-offs are present, not just the happy path.
- [ ] The recipes/How part is the substantial core, with real worked examples.
- [ ] No unfilled gaps papered over with vague prose.

### Structure & flow
- [ ] The Why→What→How→Deeper arc holds; parts are in a sensible order.
- [ ] No chapter depends on a concept introduced later.
- [ ] Chapters are distinct (no two that should merge); none that should split.
- [ ] Cross-references connect related chapters where it helps the reader.

### Voice & readability
- [ ] Reads like a person wrote it — AI-tell phrasing and hype removed
      (see `writing-craft.md`).
- [ ] Leads with the point; no throat-clearing or hollow summaries.
- [ ] Has a point of view — recommends, doesn't just enumerate.
- [ ] Rhythm varies; not every section the same shape.

### Visual richness (a book, not a wall of text)
- [ ] Every chapter alternates prose with tables / lists / callouts / diagrams.
- [ ] Comparisons are tables, processes are diagrams, copyable things are code
      cards — each form used where it fits, not decoratively.
- [ ] Callouts are used with restraint (~1–3/chapter), not as highlighter spam.
- [ ] Code examples are minimal, real, language-tagged; outputs shown where useful.

### Presentation & deployment
- [ ] Cover page: brand, tagline, kicker, lead, stats, and contents all render and
      read well.
- [ ] Built `index.html` opens directly from `file://` **with network disabled** —
      prose, tables, code highlighting, and navigation all work (render libs are
      inlined). Mermaid/fonts may degrade; nothing else should.
- [ ] Folder serves correctly as static files (nginx / Pages); `.nojekyll` present.
- [ ] **Visual spot-check** (open the built page; no Playwright needed): a couple of
      content-heavy chapters look right — tables don't overflow ugly, code cards
      scroll, diagrams render, callouts are styled, the right-hand TOC tracks
      scrolling.
- [ ] Responsive: at a narrow viewport the sidebar collapses to the menu button
      and the body stays readable (resize the window or use device emulation).

## What "done" looks like

A reader new to the topic can open the book, read front to back, and come away
able to *do* the thing — trusting every claim, never bored, never lost. When a
review round can only find wording nits, the book is done. If you've done three
rounds and still find substantive problems, ship with an honest note to the user
about what remains thin rather than silently leaving it.
