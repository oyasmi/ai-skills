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

### 中文行文五条具体技法

The book this skill produces is in Chinese, so add a layer of Chinese-specific
prose technique on top of the general voice rules above. These five techniques
are distilled from a 29-chapter, 140k-character Chinese cookbook and are
battle-tested; they are written in Chinese because they teach Chinese craft and
the examples must be Chinese to be useful.

**① 口语化但不随便。** 像跟同事在白板前讲解，不是写政府报告或论文摘要。
- 好："先把三件事讲透"、"就这二十几行，几乎把关键点都碰到了"
- 坏："本章将阐述三个要点"、"以下将详细介绍相关概念"

**② 善用破折号（——）做插入语和转折。** 中文破折号比逗号重、比句号轻，制造出"话说到一半突然插一句"的节奏感，适合拆解长论证。
- 例："这就意味着——社区那些靠提示词苦苦撑着的编排纪律，现在能用代码一次性焊死。"
- 例："但你要是拆开它们的引擎盖，会发现一个共同的、有点尴尬的事实——**这些系统都在用提示词「祈祷」式地编排。**"

**③ 反问和设问增强参与感。** 不要一直"它是这样的、它是那样的"地平铺直叙；用反问让读者主动思考，用设问制造"先问后答"的节奏。
- 例："毛病出在这：让同一个模型来评自己的产物，它会有很强的「确认偏误」。"
- 例："为什么不把整个代码库丢给一个 agent？两个原因：……"
- 例："什么时候只用 Subagent、不上 Workflow？当你只需要**派一个分身去干一件相对独立的活**。"

**④ 四字成语和典故点缀。** 恰到好处地用几个，增添文化韵味和精炼感；但不要堆砌，一段用一个就够了。
- 自然的用法："深入浅出"、"见招拆招"、"杀鸡用牛刀"、"经纬交织，方成流水线"
- 过度的用法：每句话都嵌一个成语 → 读起来像古文翻译

**⑤ 数据说话，拒绝模糊。** 能给具体数字就不用"多个"、"显著提升"、"大量"这类空话。
- 好："3 项 × 2 阶段 = 6 agent，`total_tokens=158982`，`duration_ms=26743`"
- 好："产出 26 条发现 → 综合去重为 16 个问题"
- 坏："多个 agent 并行运行"、"显著减少了 token 消耗"

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

**Common mistake — missing blank lines (WRONG):**

```html
<!-- WRONG: no blank lines, Markdown will not render inside the div -->
<div class="callout tip">
**This bold will not render.** Neither will `this code`.
</div>
```

**Callout discipline:**
- At most 1–2 callouts per section (a wall of highlights has no highlights).
- 2–4 sentences of content per callout.
- Open with a bolded lead phrase as a scannable hook.
- Never nest callouts.
- `tip` for positive guidance, `warn` for footguns, `info` for neutral asides
  and cross-references.

**Tables** — the best tool for comparison across shared dimensions. Reach for a
table whenever you're about to write "A does X, while B does Y, and C does Z."
Wide tables scroll horizontally inside the column automatically.

Four table types that recur across cookbook chapters — copy these templates:

*Parameter / API reference table:*

| 参数 | 必填 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | 是 | string | — | 工作流标识 |
| `model` | 否 | string | 继承 | 覆盖该 agent 的模型 |

*Cross-comparison matrix:*

| 维度 | 方案甲 | 方案乙 | 方案丙 |
|------|--------|--------|--------|
| 控制流 | 确定性 | 概率性 | 混合 |
| 状态 | 无状态 | 有状态 | 无状态 |
| 成本 | 低 | 高 | 中 |

*Scenario → recommendation decision table (core shape of an appendix cheat-sheet):*

| 场景 | 推荐模式 | 关键设计 | 章节 |
|------|----------|----------|------|
| 多文件审查 | Pipeline + 对抗验证 | 逐文件流过，不设屏障 | 第 10 章 |
| 方案探索 | 评委面板 | N 候选 × M 评委 | 第 14 章 |

*Concept quick-reference (for chapter openers or glossaries):*

| 概念 | 一句话解释 |
|------|-----------|
| Pipeline | 各项独立流过各阶段，阶段间无屏障 |
| Parallel | 所有任务并发执行，屏障等全部完成 |

**Table discipline:**
- First column short and scannable; bold the key term.
- Left-align content (do not center).
- Keep tables to 4–8 rows — split or move to an appendix beyond that.

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

Three additional patterns that come up constantly:

*Side-by-side comparison (for "approach A vs approach B"):*

```
​```mermaid
flowchart LR
    subgraph A["方案 A · 两段 parallel（屏障）"]
        direction TB
        a1["步骤 1"] --> a2["步骤 2（等全部完成）"]
    end
    subgraph B["方案 B · pipeline（无屏障）"]
        direction TB
        b1["步骤 1"] --> b2["步骤 2（逐项流过）"]
    end
​```
```

*Sequence diagram (component interactions, protocols):*

```
​```mermaid
sequenceDiagram
    participant U as 用户
    participant S as 系统
    participant E as 外部服务
    U->>S: 发起请求
    S->>E: 委托处理
    E-->>S: 返回结果
    S-->>U: 交付产出
    Note over S: 内部处理逻辑
​```
```

*Layered architecture (system hierarchies, mechanism relationships):*

```
​```mermaid
flowchart TD
    subgraph L1["编排层"]
        A["组件 A"]
        B["组件 B"]
    end
    subgraph L2["逻辑层"]
        C["组件 C"]
    end
    subgraph L3["外部连接"]
        D["外部服务"]
    end
    A --> C
    B --> C
    C --> D
    style L1 fill:#eef
    style L2 fill:#efe
    style L3 fill:#fee
​```
```

**Mermaid discipline:**
- One diagram, one concept. Don't cram everything into one picture.
- Keep palette consistent: `fill:#eef` (blue), `fill:#efe` (green), `fill:#fee` (red).
- Node labels must read sensibly out of context.
- Every diagram needs a sentence of prose before or after to explain it.
- More than 8–10 nodes? Split into two diagrams.

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

### Per-chapter visual density checklist

- [ ] At least one mermaid diagram.
- [ ] At least one table (reference-style chapters should have several).
- [ ] 1–3 callouts — no more.
- [ ] Code blocks present whenever the topic touches code or configuration.
- [ ] No run of plain prose longer than ~500 characters without a visual break.
- [ ] Every diagram and table has a short prose explanation beside it.

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
