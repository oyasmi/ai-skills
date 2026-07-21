#!/usr/bin/env node
/*
 * cookbook-forge — build.mjs
 * Produces a self-contained index.html that opens directly from the filesystem
 * (file://) with no web server and no network:
 *   1) inlines manifest.json + every chapter's Markdown into the page, and
 *   2) inlines the vendored render libs (marked / DOMPurify / highlight.js) over
 *      their CDN <script> tags, so Markdown renders fully offline.
 * (Mermaid diagrams and the display fonts still come from a CDN when online and
 *  degrade gracefully offline — diagrams show their source, fonts fall back to
 *  the system serif.)
 *
 * Run FROM THE COOKBOOK ROOT (dir holding index.html + manifest.json + docs/):
 *     node build.mjs                 # strict: aborts if any chapter source is missing
 *     node build.mjs --allow-missing # draft mode: missing chapters show a placeholder
 *
 * Idempotent. The unbuilt index.html still works over http(s) via fetch(), so
 * you only NEED this for offline distribution — but running it is the recommended
 * final step.
 */
import fs from 'fs';
import path from 'path';

const ROOT = process.cwd();
const ALLOW_MISSING = process.argv.includes('--allow-missing');
const read = p => fs.readFileSync(path.join(ROOT, p), 'utf8');
function fail(msg){ console.error('✗ ' + msg); process.exit(1); }

// strip helper/comment keys (anything starting with "_") recursively
function clean(v){
  if(Array.isArray(v)) return v.map(clean);
  if(v && typeof v === 'object'){
    const o = {};
    for(const [k, val] of Object.entries(v)){ if(!k.startsWith('_')) o[k] = clean(val); }
    return o;
  }
  return v;
}

if(!fs.existsSync(path.join(ROOT, 'manifest.json'))) fail('manifest.json not found. Run from the cookbook root.');
if(!fs.existsSync(path.join(ROOT, 'index.html'))) fail('index.html not found. Run from the cookbook root.');

const manifest = clean(JSON.parse(read('manifest.json')));
const docs = {};
let chapterCount = 0; const missing = [];

for(const part of manifest.parts){
  for(const ch of part.chapters){
    chapterCount++;
    if(!ch.file){ missing.push(`${ch.id}: no file path in manifest`); continue; }
    const abs = path.join(ROOT, ch.file);
    if(!fs.existsSync(abs)){ missing.push(`${ch.id}: ${ch.file} not found`); continue; }
    docs[ch.id] = fs.readFileSync(abs, 'utf8');
  }
}

if(missing.length){
  console.error('✗ ' + missing.length + ' chapter source(s) missing:');
  for(const m of missing) console.error('  - ' + m);
  if(!ALLOW_MISSING){
    fail('Refusing to build a partial book. Write the missing chapters, or pass --allow-missing for a draft.');
  }
  console.warn('⚠ --allow-missing: building anyway; these chapters show the "being written" placeholder.');
}

const data = { manifest, docs };
// Escape so the JSON survives inside <script>…</script> and inline JS:
//   < >  defuse a literal </script>;  U+2028/U+2029 are line terminators that break JS strings.
const json = JSON.stringify(data).replace(new RegExp('[<>\\u2028\\u2029]', 'g'), c => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'));

let html = read('index.html');

// 1) inject the data blob
const tagRe = /(<script id="cookbook-data"[^>]*>)([\s\S]*?)(<\/script>)/;
if(!tagRe.test(html)) fail('index.html has no <script id="cookbook-data"> element. Use the cookbook-forge template index.html.');
html = html.replace(tagRe, (_, open, _mid, close) => open + json + close);

// 2) inline the vendored render libs over their <script data-vendor> tags (true offline rendering).
//    The replacement KEEPS the data-vendor attribute, so re-running build.mjs re-inlines cleanly
//    (idempotent) rather than seeing the tags as "already gone".
let inlined = 0;
html = html.replace(/<script\b[^>]*\bdata-vendor="([^"]+)"[^>]*>[\s\S]*?<\/script>/g, (full, file) => {
  const vp = path.join(ROOT, 'vendor', file);
  if(!fs.existsSync(vp)){
    console.warn('⚠ vendor/' + file + ' not found — leaving its <script> tag as-is (needs network to render).');
    return full;
  }
  inlined++;
  // inline scripts are covered by the page CSP's 'unsafe-inline'; no integrity needed
  return '<script data-vendor="' + file + '">/* vendored: ' + file + ' */\n' + fs.readFileSync(vp, 'utf8') + '\n</script>';
});

// 3) patch <title> + meta description from the manifest
const site = manifest.site || {};
const title = site.title || site.brand || '食谱';
const desc = site.tagline || site.lead || title;
const htmlEsc = s => String(s).replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
html = html.replace(/<title>[\s\S]*?<\/title>/, '<title>' + htmlEsc(title) + '</title>');
html = html.replace(/(<meta name="description" content=")[^"]*(")/, '$1' + htmlEsc(desc) + '$2');

fs.writeFileSync(path.join(ROOT, 'index.html'), html);

const bytes = Buffer.byteLength(html, 'utf8');
console.log('✓ Embedded ' + chapterCount + ' chapter(s); inlined ' + inlined + '/3 render libs into index.html');
console.log('  index.html: ' + (bytes/1024).toFixed(0) + ' KB · open it directly in a browser, or serve the folder.');
if(inlined < 3) console.log('  (some libs still load from CDN — run from a dir with vendor/*.js for full offline rendering.)');
