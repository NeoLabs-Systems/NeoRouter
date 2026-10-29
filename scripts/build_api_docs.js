'use strict';

// Builds the GET /api reference as static files; the Pages workflow puts
// them at /api/ next to the landing page:
//   <out>/index.md    the Markdown agents read
//   <out>/index.html  the same, rendered for people
// Usage: node scripts/build_api_docs.js [out-dir]
// NEOROUTER_DOCS_ORIGIN sets the server address the examples use.
const fs = require('node:fs');
const path = require('node:path');
const { marked } = require('marked');
const { reference } = require('../server/routes/api_docs');

const ROOT = path.join(__dirname, '..');
const out = path.resolve(process.argv[2] || path.join(ROOT, 'build', 'api-docs'));
const origin = (process.env.NEOROUTER_DOCS_ORIGIN || 'https://neorouter.example.com').replace(/\/+$/, '');

const markdown = reference(origin, { placeholder: true });

// Palette and fonts follow landing/css/styles.css.
const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>NeoRouter API</title>
  <meta name="description" content="Reference for the NeoRouter gateway API. Agents: read index.md.">
  <link rel="alternate" type="text/markdown" href="index.md">
  <link rel="icon" type="image/png" href="../assets/favicon.png">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,560&family=IBM+Plex+Mono:wght@450;500&family=Inter:wght@440;600&display=swap" rel="stylesheet">
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f4f1e8; --surface: #fdfcf8; --ink: #1c2117; --ink-2: #49503f; --ink-3: #7e8470;
      --line: rgba(28, 33, 23, 0.12); --gold: #b07d2b; --gold-muted: rgba(176, 125, 43, 0.14);
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #0e1511; --surface: #171f1a; --ink: #ecefe5; --ink-2: #aeb7a6; --ink-3: #7e8877;
        --line: rgba(224, 240, 224, 0.12); --gold: #e1b052; --gold-muted: rgba(225, 176, 82, 0.14);
      }
    }
    * { box-sizing: border-box; }
    body { margin: 0; background: var(--bg); color: var(--ink); font: 16px/1.65 Inter, system-ui, sans-serif; }
    main { max-width: 860px; margin: 0 auto; padding: 48px 16px 96px; }
    .agents { margin: 0 0 32px; padding: 10px 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); color: var(--ink-2); font-size: 14px; }
    h1, h2, h3 { font-family: Fraunces, Georgia, serif; font-weight: 560; line-height: 1.2; }
    h1 { font-size: 40px; margin: 0 0 16px; }
    h2 { font-size: 26px; margin: 48px 0 12px; padding-top: 16px; border-top: 1px solid var(--line); }
    h3 { font-size: 19px; margin: 28px 0 8px; }
    a { color: var(--gold); }
    code { font: 0.88em/1.5 "IBM Plex Mono", ui-monospace, monospace; background: var(--gold-muted); padding: 1px 5px; border-radius: 5px; overflow-wrap: anywhere; }
    pre { background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 14px 16px; overflow-x: auto; }
    pre code { background: none; padding: 0; overflow-wrap: normal; }
    .table { overflow-x: auto; }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th, td { text-align: left; vertical-align: top; padding: 8px 10px; border-bottom: 1px solid var(--line); }
    th { color: var(--ink-3); font-weight: 600; }
  </style>
</head>
<body>
  <main>
    <p class="agents">Reading this as an agent? Fetch <a href="index.md">index.md</a> for plain Markdown. A running server serves the same reference at <code>GET /api</code>.</p>
    ${marked.parse(markdown).replace(/<table>/g, '<div class="table"><table>').replace(/<\/table>/g, '</table></div>')}
  </main>
</body>
</html>
`;

fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
fs.writeFileSync(path.join(out, 'index.md'), markdown);
fs.writeFileSync(path.join(out, 'index.html'), page);
console.log(`API docs written to ${out}`);
