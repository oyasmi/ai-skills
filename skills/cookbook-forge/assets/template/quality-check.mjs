#!/usr/bin/env node
/*
 * cookbook-forge — quality-check.mjs
 * Heuristic "book quality" audit beyond mechanical links. Run from the cookbook root:
 *     node quality-check.mjs
 * It reports, per chapter:
 *   - structural richness: tables / mermaid / code / callouts / images / headings
 *   - length (CJK-aware char count)
 *   - leftover placeholders / stubs / TODOs
 * And book-level: whether a Sources/信源 appendix exists.
 *
 * Exit non-zero (fail) on: leftover placeholders, or a BODY chapter with zero
 * structural elements. Thin/低密度 chapters are warnings, not failures.
 * Thresholds are deliberately loose — tune in the file if your topic warrants.
 */
import fs from 'fs';
import path from 'path';

const ROOT = process.cwd();
const manifest = JSON.parse(fs.readFileSync(path.join(ROOT, 'manifest.json'), 'utf8'));

const MIN_CHARS = 600;          // chapters shorter than this are flagged "thin"
const PLACEHOLDER_RE = /(TODO|TBD|FIXME|XXX|lorem ipsum|请替换|待补充|占位|本章正在撰写|placeholder|<主题>|<topic>|<Cookbook title>)/i;

function countChars(md){
  // strip code fences + html tags + markdown punctuation, then count non-space chars (CJK-aware)
  const noCode = md.replace(/```[\s\S]*?```/g, '').replace(/`[^`]*`/g, '');
  const noTags = noCode.replace(/<[^>]+>/g, '');
  const text = noTags.replace(/[#>*_\-|!\[\]()]/g, '');
  return [...text.replace(/\s+/g, '')].length;
}
function analyze(md){
  const fences = (md.match(/^```/gm) || []).length;
  const mermaid = (md.match(/^```mermaid/gm) || []).length;
  return {
    headings: (md.match(/^#{2,3}\s/gm) || []).length,
    tables: (md.match(/^\s*\|.+\|\s*$/gm) || []).length >= 2 ? (md.match(/\n\s*\|[-:\s|]+\|\s*\n/g) || []).length : 0,
    code: Math.max(0, Math.floor(fences / 2) - mermaid),
    mermaid,
    callouts: (md.match(/<div class="callout/g) || []).length,
    images: (md.match(/!\[[^\]]*\]\([^)]+\)/g) || []).length,
    chars: countChars(md),
  };
}

const chapters = [];
for(const part of manifest.parts) for(const ch of part.chapters) chapters.push({...ch, partId: part.id});

let placeholders = 0, zeroStruct = 0, thin = 0, hasSources = false;
const partStats = {};           // partId -> {chars, chapters}
let prefaceMd = null;           // captured content of the preface chapter, if any
console.log('=== COOKBOOK QUALITY AUDIT ===\n');
console.log('chapter            chars  H  tbl code mmd call img   notes');
console.log('------------------ -----  -- --- ---- --- ---- ---   -----');

for(const ch of chapters){
  const isAppendix = /append|appendix|附录/i.test(ch.partId) || /^[A-Z附]/.test(ch.num||'');
  if(/source|信源|参考|引用|reference/i.test((ch.title||'')+ch.id)) hasSources = true;
  if(!ch.file || !fs.existsSync(path.join(ROOT, ch.file))){
    console.log((ch.id+'').padEnd(18)+'  (no source file)'); continue;
  }
  const md = fs.readFileSync(path.join(ROOT, ch.file), 'utf8');
  const a = analyze(md);
  partStats[ch.partId] = partStats[ch.partId] || { chars: 0, chapters: 0 };
  partStats[ch.partId].chars += a.chars;
  partStats[ch.partId].chapters += 1;
  if(/preface|前言|^序$/i.test(ch.id) || /preface|前言|^序/i.test(ch.title||'') || (ch.num||'') === '序') prefaceMd = md;
  const struct = a.tables + a.code + a.mermaid + a.callouts + a.images;
  const notes = [];
  if(PLACEHOLDER_RE.test(md)){ notes.push('PLACEHOLDER'); placeholders++; }
  if(struct === 0 && !isAppendix){ notes.push('NO-STRUCTURE'); zeroStruct++; }
  if(a.chars < MIN_CHARS && !isAppendix){ notes.push('thin'); thin++; }
  const row = [
    (ch.id+'').padEnd(18),
    String(a.chars).padStart(5),
    String(a.headings).padStart(2),
    String(a.tables).padStart(3),
    String(a.code).padStart(4),
    String(a.mermaid).padStart(3),
    String(a.callouts).padStart(4),
    String(a.images).padStart(3),
    ' ' + notes.join(', '),
  ].join(' ');
  console.log(row);
}

console.log('\n-- book-level --');
console.log('  Sources/信源 appendix present: ' + (hasSources ? 'yes' : 'NO (add one)'));

// -- part balance: is the recipes/How part the substantial core of the cookbook? --
// Mark the recipes part in manifest with "role":"recipes" to enable the check.
const totalChars = Object.values(partStats).reduce((s, p) => s + p.chars, 0) || 1;
let recipesId = null, biggestId = null, biggestChars = -1, balanceWarn = 0;
console.log('\n-- part balance (chars share) --');
for(const part of manifest.parts){
  const st = partStats[part.id]; if(!st) continue;
  const share = st.chars / totalChars * 100;
  if(part.role === 'recipes') recipesId = part.id;
  if(st.chars > biggestChars){ biggestChars = st.chars; biggestId = part.id; }
  console.log('  ' + (part.label || part.id).padEnd(22).slice(0, 22) + ' ' +
    String(st.chapters).padStart(2) + ' ch  ' + share.toFixed(0).padStart(3) + '%' +
    (part.role === 'recipes' ? '  <- 配方部' : ''));
}
if(recipesId){
  const share = partStats[recipesId].chars / totalChars * 100;
  if(recipesId !== biggestId || share < 25){
    balanceWarn = 1;
    console.log('  WARN: 配方部占比 ' + share.toFixed(0) + '%（应为最大部且 >=25%）—— cookbook 的手把手配方不该是少数派。');
  }
} else {
  console.log('  (info) 在 manifest 里给「配方/实战」部加 "role":"recipes" 即可启用配比检查。');
}

// -- preface honesty: does the book own its limits? a book claiming zero weakness is suspect. --
let honestyWarn = 0;
console.log('\n-- preface honesty --');
if(prefaceMd !== null){
  if(!/未核实|待核实|存疑|薄弱|局限|不确定|尚未|unverified/i.test(prefaceMd)){
    honestyWarn = 1;
    console.log('  WARN: 前言里找不到任何「未核实/薄弱/局限」类诚实标记 —— 一本自称毫无短板的书最可疑。');
    console.log('        补一段可信度声明：(a) 信源基底(哪些 doc/哪个 commit) (b) 已知薄弱章节 (c) 明确未核实条目 (d) 时效窗口。');
  } else {
    console.log('  ok（前言含诚实标记）');
  }
} else {
  console.log('  (info) 未识别到前言章节（id/标题含「序/前言/preface」）—— 跳过诚实声明检查。');
}

console.log('\n-- summary --');
console.log('  placeholders left: ' + placeholders + '  | body chapters with no structure: ' + zeroStruct + '  | thin chapters (warn): ' + thin);
console.log('  warnings: balance ' + balanceWarn + '  | preface-honesty ' + honestyWarn + '  (non-blocking — review, don\'t ignore)');

const failures = placeholders + zeroStruct + (hasSources ? 0 : 1);
if(failures > 0){
  console.log('\n=== QUALITY: ' + failures + ' blocking issue(s) (placeholders / no-structure / missing sources) ===');
  process.exit(1);
}
console.log('\n=== QUALITY: OK' + (thin ? ' (' + thin + ' thin chapter(s) — review depth)' : '') + ' ===');
