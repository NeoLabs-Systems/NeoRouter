'use strict';

// The page a pool invite link opens. The link belongs to the host's server,
// but the person invited must join from their OWN NeoRouter. This page says
// so, shows which pool it is, and sends them on: they type their NeoRouter's
// address and land in its join dialog with the invite filled in. Link
// previews (messengers) show the pool's name too.
const pools = require('../services/pools/pool_service');
const { publicOrigin } = require('../utils/public_origin');

const GITHUB_URL = 'https://github.com/NeoLabs-Systems/NeoRouter';

const TEXT = {
  en: {
    title: (pool) => `Invite to the pool "${pool}"`,
    preview: (host) => `${host} invites you to share AI capacity between your NeoRouters.`,
    heading: 'You are invited to a pool',
    lead: (host, pool) => `${host} invites you to “${pool}”: NeoRouters that share AI capacity with each other. Nobody hands out keys; each request runs on the server of whoever shares it.`,
    members: (count) => (count === 1 ? '1 member so far' : `${count} members so far`),
    step: 'Join from your own NeoRouter. Enter its address and you will land in its join dialog with this invite filled in.',
    label: 'Address of your NeoRouter',
    placeholder: 'https://router.example.com',
    button: 'Continue',
    bad: 'Enter the address of your NeoRouter, for example https://router.example.com',
    manual: 'Or copy this page’s link and paste it in your NeoRouter under Network → Pools → Join with a link.',
    none: 'No NeoRouter yet?',
    noneLink: 'What is NeoRouter?',
    invalidTitle: 'This invite is no longer valid',
    invalid: 'It was used already, revoked or has expired. Ask for a new link.',
    once: 'The link works once and expires after 7 days.',
  },
  de: {
    title: (pool) => `Einladung in den Pool „${pool}“`,
    preview: (host) => `${host} lädt dich ein, KI-Kapazität zwischen euren NeoRoutern zu teilen.`,
    heading: 'Du bist in einen Pool eingeladen',
    lead: (host, pool) => `${host} lädt dich in „${pool}“ ein: NeoRouter, die KI-Kapazität miteinander teilen. Niemand gibt Schlüssel heraus; jede Anfrage läuft auf dem Server dessen, der teilt.`,
    members: (count) => (count === 1 ? 'Bisher 1 Mitglied' : `Bisher ${count} Mitglieder`),
    step: 'Tritt von deinem eigenen NeoRouter aus bei. Gib seine Adresse ein, dann landest du dort im Beitrittsdialog mit dieser Einladung.',
    label: 'Adresse deines NeoRouter',
    placeholder: 'https://router.example.com',
    button: 'Weiter',
    bad: 'Gib die Adresse deines NeoRouter ein, zum Beispiel https://router.example.com',
    manual: 'Oder kopiere den Link dieser Seite und füge ihn in deinem NeoRouter unter Netzwerk → Pools → Mit Link beitreten ein.',
    none: 'Noch kein NeoRouter?',
    noneLink: 'Was ist NeoRouter?',
    invalidTitle: 'Diese Einladung gilt nicht mehr',
    invalid: 'Sie wurde schon benutzt, widerrufen oder ist abgelaufen. Bitte um einen neuen Link.',
    once: 'Der Link funktioniert einmal und läuft nach 7 Tagen ab.',
  },
};

const escape = (value) => String(value).replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]);

function language(req) {
  if (req.query.lang === 'de' || req.query.lang === 'en') return req.query.lang;
  return /^de\b/i.test(String(req.get('accept-language') || '')) ? 'de' : 'en';
}

function page({ lang, origin, invite }) {
  const t = TEXT[lang];
  const valid = Boolean(invite);
  const title = valid ? t.title(invite.pool) : t.invalidTitle;
  const description = valid ? t.preview(invite.host) : t.invalid;
  const icon = `${origin}/app/icons/Icon-512.png`;
  return `<!DOCTYPE html>
<html lang="${lang}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>${escape(title)} · NeoRouter</title>
<meta name="description" content="${escape(description)}">
<meta name="theme-color" content="#0e1511" media="(prefers-color-scheme: dark)">
<meta name="theme-color" content="#f4f1e8" media="(prefers-color-scheme: light)">
<meta property="og:site_name" content="NeoRouter">
<meta property="og:title" content="${escape(title)}">
<meta property="og:description" content="${escape(description)}">
<meta property="og:type" content="website">
<meta property="og:image" content="${escape(icon)}">
<meta name="twitter:card" content="summary">
<link rel="icon" type="image/png" href="/assets/favicon.png">
<style>
:root{--bg:#0e1511;--card:#141c17;--line:#24302a;--text:#e8e6de;--soft:#a9b0a8;--muted:#78827a;--accent:#e0b85c;--ink:#1b1a12;--bad:#e08a6c}
@media (prefers-color-scheme: light){:root{--bg:#f4f1e8;--card:#fffdf7;--line:#e2ddcf;--text:#1d221e;--soft:#4e5650;--muted:#7a817b;--accent:#b8892a;--ink:#fffdf7;--bad:#b4502f}}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px 16px;background:var(--bg);color:var(--text);font:15px/1.55 Inter,system-ui,-apple-system,Segoe UI,sans-serif}
main{width:100%;max-width:520px;background:var(--card);border:1px solid var(--line);border-radius:20px;padding:28px}
.brand{display:flex;align-items:center;gap:10px;color:var(--soft);font-size:13px;letter-spacing:.4px;margin-bottom:22px}
.brand img{width:28px;height:28px;border-radius:8px}
h1{margin:0 0 10px;font-size:24px;line-height:1.25;letter-spacing:-.3px}
p{margin:0 0 14px;color:var(--soft)}
.pool{display:flex;align-items:center;gap:12px;padding:14px;border:1px solid var(--line);border-radius:14px;margin:18px 0}
.pool .mark{width:40px;height:40px;border-radius:12px;display:flex;align-items:center;justify-content:center;background:rgba(224,184,92,.14);color:var(--accent);flex-shrink:0}
.pool strong{display:block;color:var(--text)}
.pool span{color:var(--muted);font-size:13px}
label{display:block;font-weight:600;color:var(--text);margin:18px 0 8px}
.row{display:flex;gap:8px;flex-wrap:wrap}
input{flex:1 1 220px;min-width:0;height:46px;padding:0 14px;border-radius:12px;border:1px solid var(--line);background:var(--bg);color:var(--text);font:inherit}
input:focus{outline:2px solid var(--accent);outline-offset:1px}
button{height:46px;padding:0 20px;border:none;border-radius:12px;background:var(--accent);color:var(--ink);font:inherit;font-weight:600;cursor:pointer}
.error{display:none;color:var(--bad);font-size:13px;margin-top:8px}
.small{font-size:13px;color:var(--muted);margin-top:18px}
a{color:var(--accent)}
</style>
</head>
<body>
<main>
<div class="brand"><img src="/assets/favicon.png" alt=""><span>NeoRouter</span></div>
${valid ? `<h1>${escape(t.heading)}</h1>
<p>${escape(t.lead(invite.host, invite.pool))}</p>
<div class="pool"><div class="mark" aria-hidden="true"><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="3"/><circle cx="5" cy="6" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="12" cy="20" r="2"/><path d="M7 7l3 3M17 7l-3 3M12 15v3"/></svg></div><div><strong>${escape(invite.pool)}</strong><span>${escape(t.members(invite.members))}</span></div></div>
<p>${escape(t.step)}</p>
<form id="go" novalidate>
<label for="address">${escape(t.label)}</label>
<div class="row"><input id="address" name="address" type="url" inputmode="url" autocomplete="url" placeholder="${escape(t.placeholder)}" required><button type="submit">${escape(t.button)}</button></div>
<div class="error" id="error" role="alert">${escape(t.bad)}</div>
</form>
<p class="small">${escape(t.manual)}<br>${escape(t.once)}</p>
<p class="small">${escape(t.none)} <a href="${GITHUB_URL}" target="_blank" rel="noopener">${escape(t.noneLink)}</a></p>
<script>
(function(){
  var input=document.getElementById('address'),error=document.getElementById('error');
  try{var saved=localStorage.getItem('neorouter.ownAddress');if(saved)input.value=saved;}catch(e){}
  document.getElementById('go').addEventListener('submit',function(event){
    event.preventDefault();
    var value=input.value.trim();
    if(value&&!/^https?:\\/\\//i.test(value))value='https://'+value;
    var origin;
    try{origin=new URL(value).origin;}catch(e){origin=null;}
    if(!origin){error.style.display='block';input.focus();return;}
    try{localStorage.setItem('neorouter.ownAddress',origin);}catch(e){}
    location.href=origin+'/app/?join-pool='+encodeURIComponent(location.href);
  });
})();
</script>` : `<h1>${escape(t.invalidTitle)}</h1>
<p>${escape(t.invalid)}</p>
<p class="small">${escape(t.none)} <a href="${GITHUB_URL}" target="_blank" rel="noopener">${escape(t.noneLink)}</a></p>`}
</main>
</body>
</html>`;
}

function poolJoinPage(req, res) {
  const lang = language(req);
  const origin = publicOrigin(req);
  let invite = null;
  try {
    const preview = pools.handlePreview({ token: String(req.query.pool || '') });
    invite = { pool: preview.pool.name, host: preview.host.name, members: preview.members.length };
  } catch {
    invite = null;
  }
  res.set('Cache-Control', 'no-store');
  res.set('Referrer-Policy', 'no-referrer');
  res.type('html').send(page({ lang, origin, invite }));
}

module.exports = { poolJoinPage };
