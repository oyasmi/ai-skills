# Writing Craft — Voice, Formatting, and Truthfulness

This is how a chapter earns the word "book." Three things: it tells the truth,
it reads like a person wrote it, and it uses the page well.

## 1. Truthfulness — the non-negotiable

A cookbook that's beautifully written and wrong is worse than useless. The whole
value is that the reader can trust it.

### Keep one grounding file

Before writing chapters, distill your research into a single
`grounding.md`-style notes file (kept out of the published book) that is **the
only source of factual claims.** Every assertion in the book must trace to it or
to a cited source. The discipline the reference project used:

> Never assert an API detail, number, behavior, or quote from memory. If it isn't
> in the grounding file or a source, either go verify it or don't write it.
> When unsure, write "(unverified)" rather than inventing certainty.

### Grade your evidence

Not all claims are equal. Mark the strong ones as strong and the weak ones as
weak, in the prose itself:

- **Verified / first-hand** — you ran it, tested it, or it's in primary
  documentation. State it plainly.
- **Reported / second-hand** — a third party claims it and you couldn't confirm.
  Attribute it: "X reports that…", "according to…". Never launder a claim into
  fact by dropping the attribution.
- **Inference / forward-looking** — your reasoning or a prediction. Mark it:
  "this suggests…", "as of <date>…".

When a worked example was actually run, say so and show the **real** output. When
it's illustrative only, label it. Readers forgive "illustrative"; they don't
forgive being misled.

### Cite, and make sources first-class

Keep a Sources appendix. Link claims to it where it matters. Convert relative
dates ("last month") to absolute ones. Prefer primary sources over blog
summaries; when you must use a secondary source, say it's secondary.

## 2. Voice — write like a person, not a content farm

The reference project spent a whole revision pass *removing* AI-tell phrasing.
Match that bar.

**Cut these reflexes:**
- Hollow intensifiers and hype: "powerful", "seamless", "robust", "game-changing",
  "unleash", "dive in", "delve", "in today's fast-paced world", "the world of".
- Throat-clearing: "It's important to note that…", "Needless to say…",
  "As we all know…". Just say the thing.
- The "not only… but also", "it's not X, it's Y" rhetorical seesaw on repeat.
- Summary paragraphs that restate what was just said without adding anything.
- Symmetry for its own sake — every section the same length, every list exactly
  three items, every chapter ending on an uplifting note.

**Do these instead:**
- **Lead with the point.** First sentence of a section says what it's about and
  why it matters. No windup.
- **Concrete over abstract.** "Returns `null` and skips the rest" beats "handles
  the error gracefully." Show the number, the name, the line.
- **Earn transitions.** Connect ideas because they're actually connected, not
  with "Moreover" / "Furthermore" garnish.
- **Vary rhythm.** Mix short, punchy sentences with longer ones. A one-line
  paragraph is allowed and effective.
- **Have a point of view.** Recommend. Say "prefer X" and why. A cookbook with
  no opinions is a reference manual.
- **Address the reader directly** ("you"), in the present tense.
- **Use the topic's own metaphor consistently** if the book has one, but don't
  force it into every paragraph.

A useful test: read a paragraph aloud. If it sounds like a press release or a
listicle, rewrite it.

## 3. The page — use structure to carry meaning

Dense prose is hard to learn from. The reference book alternates constantly
between prose and visual structure. Every chapter should use several of these.

### Markdown the renderer supports

Standard GitHub-flavored markdown, plus:

**Callouts** — pull a point out of the flow. Write blank-line-separated markdown
*inside* the div (the blank lines are required for the inner markdown to render):

```html
<div class="callout tip">

**Tip.** Short, high-value guidance.

</div>
```

Variants: `tip` (green, guidance), `warn` (amber, pitfalls/gotchas), `info`
(orange, neutral aside). Use them sparingly — ~1–3 per chapter. If everything is
a callout, nothing is.

**Tables** — the best tool for comparison across shared dimensions. Reach for a
table whenever you're about to write "A does X, while B does Y, and C does Z."
Wide tables scroll horizontally inside the column automatically.

**Mermaid diagrams** — a fenced code block tagged `mermaid` renders to SVG. Use
for pipelines (`flowchart`), sequences, hierarchies, state machines, decisions.
A diagram should encode a relationship that's genuinely clearer than prose — not
decorate. Keep node labels short; explain the meaning in the surrounding text
(the alt text is auto-derived from labels, so labels should be meaningful).

```
​```mermaid
flowchart TD
    A["Start"] --> B{"Decision?"}
    B -->|yes| C["Path 1"]
    B -->|no| D["Path 2"]
​```
```

**Code cards** — every fenced code block gets a language label, syntax
highlighting, and a copy button. Tag the language (` ```python `, ` ```bash `,
` ```json `). Prefer **minimal, real, runnable** examples over long ones. Show
the output too, in its own block, when it teaches something.

**Block quotes** — render with an accent rule. Good for an epigraph opening a
chapter, or to set off a key principle.

**Lists** — for steps (ordered) or parallel items (unordered). Don't use a list
where a sentence is clearer, and don't bury a real comparison in a bullet list
that wants to be a table.

### Rhythm of a strong chapter

- Open with the point or a vivid framing (a quote, a failure story, a question).
- Introduce one idea, show it concretely, then complicate it (edge cases).
- Break every few hundred words with a heading, table, diagram, or callout.
- A heading roughly every screen — the right-hand TOC is built from `##`/`###`,
  so headings double as the reader's map.
- End with something that *advances* the reader: when not to use this, what to
  read next, a summary table of the decisions — not a hollow recap.

### Headings & anchors

- Use `#` once (the chapter title). Structure the body with `##` and `###`.
- The TOC and anchor links come from `##`/`###` only. Keep them short and
  descriptive; avoid duplicate heading text within a chapter (anchors get
  de-duplicated with a numeric suffix, which makes for ugly links).

### Cross-references

- Link to another chapter with `#/<chapter-id>` (e.g. `#/p3-10`).
- Link within the same chapter to a heading with `#<slug-of-heading>`.
- Run `node check.mjs` — it catches broken anchors, bad cross-page links, raw
  `.md` links, and remote/missing images before they ship. Then run
  `node quality-check.mjs` for structural density, leftover placeholders, and the
  sources appendix.

## 4. Images

The page's CSP allows only **local** images (`img-src 'self' data:`) — a remote
`![](https://…)` will silently fail to load. So:

- Download every figure into `assets/images/` and reference it locally:
  `![alt](assets/images/foo.png)`. `check.mjs` flags remote and missing images.
- Record each image's **source and license** in the grounding notes / Sources
  appendix. Don't ship an image you don't have the right to use.
- Always write meaningful alt text — it's both accessibility and a caption.
- Prefer a diagram you author (mermaid, which needs no asset and themes itself)
  over a borrowed screenshot when the content allows it.
