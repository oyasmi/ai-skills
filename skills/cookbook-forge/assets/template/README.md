# 《书名》

一本由 **cookbook-forge** 生成的主题食谱。单页、自包含、无需构建工具链。

## 阅读

- **本地直接打开（无需服务器）：** 先运行一次 `node build.mjs`，然后用任意浏览器直接打开
  `index.html`（双击 / `file://`）。构建会把全部内容**以及渲染引擎（marked / DOMPurify /
  highlight.js）**一并内联进 HTML，所以没有任何会被浏览器拦截的 `fetch()`，断网也能读正文。
  > 说明：Mermaid 图示与标题用的展示字体仍走 CDN（可选）；离线时图示降级为显示源码、字体回退到
  > 系统衬线体，正文与代码不受影响。需要图示也离线时，自行把 mermaid 一并 vendoring 即可。
- **走 Web 服务器（nginx / Apache / GitHub Pages）：** 把本目录当静态文件托管，入口是
  `index.html`。构建与否都能跑——未内联时它会回退到抓取 `manifest.json` 与 `docs/` 文件。
- **全文搜索：** 左侧栏顶部的搜索框检索全书（标题 + 正文；多个词用空格分隔，按 AND 匹配）。
  点结果跳转到该章并高亮所有命中处，`Esc` 清除高亮；快捷键 `/` 或 `Cmd/Ctrl+K` 聚焦搜索框。

## 编辑

1. 章节正文在 `docs/*.md`（GitHub 也能直接渲染）。
2. 结构、标题、封面信息在 `manifest.json`。
3. 图片放 `assets/images/`（CSP 只允许本地图片，远程图请先下载到本地）。
4. `node check.mjs` 校验链接/锚点/图片（期望 `TOTAL ISSUES: 0`）。
5. `node quality-check.mjs` 审书籍质量（结构密度、占位符、信源附录）。
6. `node build.mjs` 重新内联，供离线使用（缺章节会**直接报错中止**；草稿用 `--allow-missing`）。

## 部署到 GitHub Pages

把本目录推到仓库并开启 Pages（根目录）。自带的 `.nojekyll` 让 Jekyll 不隐藏 `_` 开头的路径。
应用是 hash 路由，`…/#/p1-01` 这类深链无需服务器重写即可工作。

## 文件一览

| 文件 | 作用 |
|---|---|
| `index.html` | 整个阅读器（主题 + 引擎）。打开或托管它。 |
| `manifest.json` | 书的结构 + 封面信息。 |
| `docs/*.md` | 章节源文件。 |
| `vendor/*.js` | 离线渲染库，`build.mjs` 会内联它们。 |
| `assets/images/` | 本地图片。 |
| `build.mjs` | 把内容 + 渲染库内联进 `index.html`（供 `file://` 离线用）。 |
| `check.mjs` | 链接 / 锚点 / 图片审计。 |
| `quality-check.mjs` | 书籍质量审计（结构、占位符、信源）。 |
| `.nojekyll` | GitHub Pages：原样提供文件。 |
