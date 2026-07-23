#!/usr/bin/env node
/*
 * cookbook-forge — check.mjs
 * Link / anchor audit for a generated cookbook (single-language).
 * Run FROM THE COOKBOOK ROOT:  node check.mjs
 * Healthy tree prints "TOTAL ISSUES: 0". Run it before building/shipping.
 *
 * Replicates index.html's slugify() exactly and checks:
 *   - every manifest chapter has a source file, present with EXACT case
 *   - cross-page links  #/<id>   resolve to a real manifest id
 *   - in-page anchors    #<frag> resolve to an h2/h3 heading in the SAME doc
 *   - referenced image assets exist (exact case) and are local (CSP blocks remote)
 *   - raw .md links are flagged (the SPA needs #/<id>)
 */
import fs from 'fs';
import path from 'path';

const ROOT = process.cwd();
const manifest = JSON.parse(fs.readFileSync(path.join(ROOT, 'manifest.json'), 'utf8'));

// --- manifest structure validation: fail with a clear message instead of a stack trace ---
(function validateManifest(){
  const die = msg => { console.error('✗ manifest.json: ' + msg); process.exit(1); };
  if(!manifest || typeof manifest !== 'object') die('not a JSON object');
  if(!Array.isArray(manifest.parts) || manifest.parts.length === 0) die('missing or empty "parts" array');
  const seenIds = new Set();
  manifest.parts.forEach((part, pi)=>{
    if(!part || !Array.isArray(part.chapters))
      die(`parts[${pi}] has no "chapters" array`);
    part.chapters.forEach((ch, ci)=>{
      const where = `parts[${pi}].chapters[${ci}]`;
      if(!ch || typeof ch !== 'object') die(`${where} is not an object`);
      if(!ch.id || typeof ch.id !== 'string') die(`${where} missing string "id"`);
      if(/[\/\s]/.test(ch.id)) die(`${where}: id "${ch.id}" must not contain "/" or whitespace (it becomes the hash route)`);
      if(seenIds.has(ch.id)) die(`duplicate chapter id "${ch.id}" — ids must be unique`);
      seenIds.add(ch.id);
      if(!ch.file || typeof ch.file !== 'string') die(`${where} (id "${ch.id}") missing string "file"`);
    });
  });
})();

function slugify(text, seen){
  let id = String(text).trim().toLowerCase()
    .replace(/[\s_]+/gu,'-')
    .replace(/[^\p{L}\p{N}-]+/gu,'')
    .replace(/-{2,}/gu,'-').replace(/^-+|-+$/gu,'');
  if(!id) id = 'section';
  if(seen){ if(seen[id]!=null){ id = id+'-'+(++seen[id]); } else { seen[id]=0; } }
  return id;
}
function headingText(md){
  return md.replace(/!\[([^\]]*)\]\([^)]*\)/g,'$1').replace(/\[([^\]]+)\]\([^)]*\)/g,'$1').replace(/[`*~]/g,'').trim();
}
function existsExact(rel){
  const abs = path.join(ROOT, rel);
  if(!fs.existsSync(abs)) return {ok:false, reason:'missing'};
  const parts = rel.split('/'); let cur = ROOT;
  for(const seg of parts){
    const entries = fs.readdirSync(cur);
    if(!entries.includes(seg)) return {ok:false, reason:'case-mismatch', got:entries.filter(e=>e.toLowerCase()===seg.toLowerCase())};
    cur = path.join(cur, seg);
  }
  return {ok:true};
}
function parseDoc(md){
  const lines = md.split(/\r?\n/); let fence=null; const headings=[]; const links=[]; const seen={};
  for(let i=0;i<lines.length;i++){
    const ln=lines[i];
    const fm = ln.match(/^(\s*)(```+|~~~+)/);
    if(fm){ const mk=fm[2][0]; if(fence===null){ fence=mk; } else if(fence===mk){ fence=null; } continue; }
    if(fence!==null) continue;
    const hm = ln.match(/^(#{2,3})\s+(.+?)\s*#*\s*$/);
    if(hm){ headings.push({slug:slugify(headingText(hm[2]), seen)}); }
    const re=/\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g; let m;
    while((m=re.exec(ln))){ links.push({text:m[1], target:m[2], line:i+1}); }
  }
  return {links, slugSet:new Set(headings.map(h=>h.slug))};
}

const chapters = [];
for(const part of manifest.parts) for(const ch of part.chapters) chapters.push(ch);
const validIds = new Set(['home', ...chapters.map(c=>c.id)]);

const f={missingFile:[], caseMismatch:[], badCrossPage:[], crossPageAnchorSuffix:[], brokenAnchor:[], missingAsset:[], remoteImage:[]};
let totLinks=0, totAnchors=0, totCross=0, docCount=0;

for(const ch of chapters){
  const rel = ch.file;
  if(!rel){ f.missingFile.push(`${ch.id}: no file path in manifest`); continue; }
  const ex = existsExact(rel);
  if(!ex.ok){ (ex.reason==='missing'?f.missingFile:f.caseMismatch).push(`${rel} (${ex.reason}${ex.got?': '+ex.got.join('|'):''})`); continue; }
  docCount++;
  const md = fs.readFileSync(path.join(ROOT, rel),'utf8');
  const {links, slugSet} = parseDoc(md);
  for(const lk of links){
    totLinks++;
    const tgt = lk.target;
    // images first (a remote image is blocked by CSP img-src 'self' data:)
    if(/^!/.test(lk.text) || /\.(png|jpg|jpeg|gif|svg|webp)$/i.test(tgt)){
      if(/^https?:/i.test(tgt)){ f.remoteImage.push(`${rel}:${lk.line} -> ${tgt} (remote image; localize into assets/images/)`); continue; }
    }
    if(/^https?:/i.test(tgt) || /^mailto:/i.test(tgt)) continue;
    if(tgt.startsWith('#/')){
      totCross++;
      const rm = tgt.match(/^#\/([^/#]+)(#.*)?$/);
      if(!rm){ f.badCrossPage.push(`${rel}:${lk.line} -> ${tgt} (malformed)`); continue; }
      if(rm[2]){ f.crossPageAnchorSuffix.push(`${rel}:${lk.line} -> ${tgt} (route() can't handle #anchor suffix)`); }
      if(!validIds.has(rm[1])){ f.badCrossPage.push(`${rel}:${lk.line} -> ${tgt} (unknown id '${rm[1]}')`); }
    } else if(tgt.startsWith('#')){
      totAnchors++;
      const raw=decodeURIComponent(tgt.slice(1));
      if(!slugSet.has(raw) && !slugSet.has(slugify(raw))){
        f.brokenAnchor.push(`${rel}:${lk.line} [${lk.text}] -> ${tgt} (no h2/h3 in same doc; slugify='${slugify(raw)}')`);
      }
    } else if(/\.(png|jpg|jpeg|gif|svg|webp)$/i.test(tgt)){
      const arel = tgt.replace(/^\.?\//,'');
      const ax = existsExact(arel);
      if(!ax.ok) f.missingAsset.push(`${rel}:${lk.line} -> ${tgt} (${ax.reason})`);
    } else if(/\.md(#|$)/i.test(tgt)){
      f.badCrossPage.push(`${rel}:${lk.line} -> ${tgt} (raw .md link; SPA needs #/<id>)`);
    }
  }
}

const n=(a)=>a.length;
console.log('=== COOKBOOK LINK/ANCHOR AUDIT ===');
console.log(`docs scanned: ${docCount} | links: ${totLinks} (cross-page ${totCross}, in-page anchors ${totAnchors})`);
const sec=(label,arr)=>{ console.log(`\n-- ${label} (${n(arr)}) --`); arr.forEach(x=>console.log('  '+x)); };
sec('missing files', f.missingFile);
sec('case mismatches [Linux/Pages-breaking]', f.caseMismatch);
sec('bad cross-page links', f.badCrossPage);
sec('cross-page #anchor suffix', f.crossPageAnchorSuffix);
sec('broken in-page anchors', f.brokenAnchor);
sec('missing image assets', f.missingAsset);
sec('remote images [CSP-blocked]', f.remoteImage);
const total=n(f.missingFile)+n(f.caseMismatch)+n(f.badCrossPage)+n(f.crossPageAnchorSuffix)+n(f.brokenAnchor)+n(f.missingAsset)+n(f.remoteImage);
console.log(`\n=== TOTAL ISSUES: ${total} ===`);
process.exit(total===0?0:1);
