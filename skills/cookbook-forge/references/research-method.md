# Research Method — Gather, Compare, Analyze Deeply

The quality ceiling of the cookbook is set here. A book can't be more accurate,
more complete, or more insightful than the research under it. This is the step
where "thorough" is won or lost — **do not skimp.**

> The failure mode to avoid: skim three blog posts, paraphrase them, ship. That
> produces a confident, shallow, partly-wrong book. The whole point of this skill
> is to go deeper than that.

## Principles

- **Primary over secondary.** Official docs, specs, source code, the thing
  itself > tutorials and blog posts > forum hearsay. Go upstream until you hit
  the source of truth.
- **Triangulate.** Don't trust a single source for any non-trivial claim. Find
  it stated (or contradicted) in 2–3 independent places. Where sources disagree,
  that disagreement is itself something to surface in the book.
- **Verify when you can.** If the topic is something you can actually run, test,
  or reproduce, do it — a verified fact outranks any amount of citation. Real
  output beats described output.
- **Read deeply, not just widely.** A handful of sources read closely and
  cross-checked beats fifty skimmed. Read the whole doc page, not the snippet
  the search returned.
- **Capture provenance as you go.** For every fact worth keeping: the claim, the
  source (URL/title), and your confidence grade. This becomes the grounding
  file and the Sources appendix.

## Process

### 1. Scope the question

Write down what the reader must be able to do at the end. List the sub-topics
that implies. This is your research checklist and, later, your chapter list.

### 2. Map the source landscape

Find, for the topic:
- **Canonical sources** — official documentation, specification, reference
  implementation, the primary site/repo.
- **Authoritative secondary** — well-regarded books, talks, maintainer blog
  posts, standards.
- **Practitioner sources** — high-signal tutorials, real-world write-ups, issue
  threads where edge cases surface.
- **The neighbors** — competing or adjacent tools/approaches, for the
  comparison and positioning chapters.

Use web search / fetch / available research tools. Cast wide first, then go
deep on the canonical few.

### 3. Read deeply and take grounded notes

For each important source, extract claims into your grounding notes with
provenance and a confidence grade (verified / reported / inference — see
`writing-craft.md`). Note exact names, signatures, numbers, defaults, version
caveats, and **quote** anything you might reproduce verbatim. Record
contradictions between sources explicitly.

### 4. Comparative analysis — the step that adds insight

Reading summarizes; comparing *understands*. Actively build:
- **Comparison tables** across the dimensions that matter (these often become
  the positioning/comparison chapters directly).
- **The "why" behind the "what."** Don't just record that something works a
  certain way — find or reason out *why* it was designed that way. That's the
  insight a novice can't get from the docs alone.
- **Edge cases and failure modes.** Hunt for where it breaks: issue trackers,
  "gotcha" posts, the caveats buried in docs. These power the pitfalls and
  troubleshooting chapters.
- **Trade-offs.** Every real tool trades something. Name the trades. A book that
  only lists benefits reads like marketing.
- **The expert/novice gap.** What do practitioners take for granted that trips
  up beginners? These become your highest-value chapters.

### 5. Identify and close gaps

Compare your grounded notes against the chapter checklist from step 1. Where a
planned chapter has thin or no support, that's a gap: go back and research it,
or cut the chapter. **A chapter with no grounding is where fabrication creeps
in** — never paper over a gap with confident prose.

## Working with user-provided material

If the user supplies their own material (notes, docs, a corpus):
- Treat it as a **primary, high-priority source** — it's why they chose it.
- Still grade it: user-provided ≠ automatically verified. Cross-check
  non-obvious claims.
- **Ask whether to supplement with web research.** Some users want a book built
  strictly from their material; others want it enriched. Confirm before going
  wide, and respect "use only what I gave you" if that's the answer.
- Mine it for the topic-specific chapter archetypes the generic plan misses.

## How much is enough

Enough to write every planned chapter from grounded notes without ever reaching
for memory or guesswork. If you find yourself about to write a sentence you
can't trace to a source or a test, stop — that's the signal to research more,
not to write more. Depth of research, not volume of prose, is the goal.
