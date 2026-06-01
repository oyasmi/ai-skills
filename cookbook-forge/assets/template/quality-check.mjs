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
console.log('\n-- summary --');
console.log('  placeholders left: ' + placeholders + '  | body chapters with no structure: ' + zeroStruct + '  | thin chapters (warn): ' + thin);

const failures = placeholders + zeroStruct + (hasSources ? 0 : 1);
if(failures > 0){
  console.log('\n=== QUALITY: ' + failures + ' blocking issue(s) (placeholders / no-structure / missing sources) ===');
  process.exit(1);
}
console.log('\n=== QUALITY: OK' + (thin ? ' (' + thin + ' thin chapter(s) — review depth)' : '') + ' ===');
