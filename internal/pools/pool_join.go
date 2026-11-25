package pools

// The page a pool invite link opens (routes/pool_join.js). The link belongs to the
// host's server, but the person invited must join from their OWN NeoRouter. This
// page says so, shows which pool it is, and sends them on: they type their
// NeoRouter's address and land in its join dialog with the invite filled in. Link
// previews (messengers) show the pool's name too.
//
// The markup is part of the invite experience, so it is kept byte for byte
// identical to the Node page (internal/pools/testdata holds the Node output).

import (
	"fmt"
	"net/http"
	"strings"
)

// githubURL is where the page sends someone who has no NeoRouter yet.
const githubURL = "https://github.com/NeoLabs-Systems/NeoRouter"

// joinText is one language's words on the invite page.
type joinText struct {
	Title        func(pool string) string
	Preview      func(host string) string
	Heading      string
	Lead         func(host, pool string) string
	Members      func(count int) string
	Step         string
	Label        string
	Placeholder  string
	Button       string
	Bad          string
	Manual       string
	None         string
	NoneLink     string
	InvalidTitle string
	Invalid      string
	Once         string
}

var joinTexts = map[string]joinText{
	"en": {
		Title:   func(pool string) string { return `Invite to the pool "` + pool + `"` },
		Preview: func(host string) string { return host + " invites you to share AI capacity between your NeoRouters." },
		Heading: "You are invited to a pool",
		Lead: func(host, pool string) string {
			return host + " invites you to “" + pool + "”: NeoRouters that share AI capacity with each other. Nobody hands out keys; each request runs on the server of whoever shares it."
		},
		Members: func(count int) string {
			if count == 1 {
				return "1 member so far"
			}
			return fmt.Sprintf("%d members so far", count)
		},
		Step:         "Join from your own NeoRouter. Enter its address and you will land in its join dialog with this invite filled in.",
		Label:        "Address of your NeoRouter",
		Placeholder:  "https://router.example.com",
		Button:       "Continue",
		Bad:          "Enter the address of your NeoRouter, for example https://router.example.com",
		Manual:       "Or copy this page’s link and paste it in your NeoRouter under Network → Pools → Join with a link.",
		None:         "No NeoRouter yet?",
		NoneLink:     "What is NeoRouter?",
		InvalidTitle: "This invite is no longer valid",
		Invalid:      "It was used already, revoked or has expired. Ask for a new link.",
		Once:         "The link works once and expires after 7 days.",
	},
	"de": {
		Title: func(pool string) string { return `Einladung in den Pool „` + pool + `“` },
		Preview: func(host string) string {
			return host + " lädt dich ein, KI-Kapazität zwischen euren NeoRoutern zu teilen."
		},
		Heading: "Du bist in einen Pool eingeladen",
		Lead: func(host, pool string) string {
			return host + " lädt dich in „" + pool + "“ ein: NeoRouter, die KI-Kapazität miteinander teilen. Niemand gibt Schlüssel heraus; jede Anfrage läuft auf dem Server dessen, der teilt."
		},
		Members: func(count int) string {
			if count == 1 {
				return "Bisher 1 Mitglied"
			}
			return fmt.Sprintf("Bisher %d Mitglieder", count)
		},
		Step:         "Tritt von deinem eigenen NeoRouter aus bei. Gib seine Adresse ein, dann landest du dort im Beitrittsdialog mit dieser Einladung.",
		Label:        "Adresse deines NeoRouter",
		Placeholder:  "https://router.example.com",
		Button:       "Weiter",
		Bad:          "Gib die Adresse deines NeoRouter ein, zum Beispiel https://router.example.com",
		Manual:       "Oder kopiere den Link dieser Seite und füge ihn in deinem NeoRouter unter Netzwerk → Pools → Mit Link beitreten ein.",
		None:         "Noch kein NeoRouter?",
		NoneLink:     "Was ist NeoRouter?",
		InvalidTitle: "Diese Einladung gilt nicht mehr",
		Invalid:      "Sie wurde schon benutzt, widerrufen oder ist abgelaufen. Bitte um einen neuen Link.",
		Once:         "Der Link funktioniert einmal und läuft nach 7 Tagen ab.",
	},
}

// joinInvite is what the page says about one invite.
type joinInvite struct {
	Pool    string
	Host    string
	Members int
}

// htmlEscape is the page's escape(), the same five characters.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

func escapeHTML(value string) string { return htmlEscaper.Replace(value) }

// joinLanguage picks the page's language from ?lang=, else Accept-Language.
func joinLanguage(lang, acceptLanguage string) string {
	if lang == "de" || lang == "en" {
		return lang
	}
	lower := strings.ToLower(strings.TrimSpace(acceptLanguage))
	if strings.HasPrefix(lower, "de") && (len(lower) == 2 || !isWordByte(lower[2])) {
		return "de"
	}
	return "en"
}

// isWordByte reports whether a byte continues a word (for /^de\b/i).
func isWordByte(value byte) bool {
	return value == '_' || (value >= '0' && value <= '9') || (value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z')
}

// joinPageCSS is the page's stylesheet, unchanged from the Node page.
const joinPageCSS = `:root{--bg:#0e1511;--card:#141c17;--line:#24302a;--text:#e8e6de;--soft:#a9b0a8;--muted:#78827a;--accent:#e0b85c;--ink:#1b1a12;--bad:#e08a6c}
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
a{color:var(--accent)}`

// joinPageScript is the page's script, unchanged from the Node page.
const joinPageScript = `<script>
(function(){
  var input=document.getElementById('address'),error=document.getElementById('error');
  try{var saved=localStorage.getItem('neorouter.ownAddress');if(saved)input.value=saved;}catch(e){}
  document.getElementById('go').addEventListener('submit',function(event){
    event.preventDefault();
    var value=input.value.trim();
    if(value&&!/^https?:\/\//i.test(value))value='https://'+value;
    var origin;
    try{origin=new URL(value).origin;}catch(e){origin=null;}
    if(!origin){error.style.display='block';input.focus();return;}
    try{localStorage.setItem('neorouter.ownAddress',origin);}catch(e){}
    location.href=origin+'/app/?join-pool='+encodeURIComponent(location.href);
  });
})();
</script>`

// renderJoinPage is the whole page for one language, origin and invite.
func renderJoinPage(lang, origin string, invite *joinInvite) string {
	text := joinTexts[lang]
	valid := invite != nil
	title := text.InvalidTitle
	description := text.Invalid
	if valid {
		title = text.Title(invite.Pool)
		description = text.Preview(invite.Host)
	}
	icon := origin + "/app/icons/Icon-512.png"
	escapedTitle := escapeHTML(title)
	escapedDescription := escapeHTML(description)

	var page strings.Builder
	page.WriteString("<!DOCTYPE html>\n<html lang=\"")
	page.WriteString(lang)
	page.WriteString("\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<meta name=\"robots\" content=\"noindex\">\n<title>")
	page.WriteString(escapedTitle)
	page.WriteString(" · NeoRouter</title>\n<meta name=\"description\" content=\"")
	page.WriteString(escapedDescription)
	page.WriteString("\">\n<meta name=\"theme-color\" content=\"#0e1511\" media=\"(prefers-color-scheme: dark)\">\n<meta name=\"theme-color\" content=\"#f4f1e8\" media=\"(prefers-color-scheme: light)\">\n<meta property=\"og:site_name\" content=\"NeoRouter\">\n<meta property=\"og:title\" content=\"")
	page.WriteString(escapedTitle)
	page.WriteString("\">\n<meta property=\"og:description\" content=\"")
	page.WriteString(escapedDescription)
	page.WriteString("\">\n<meta property=\"og:type\" content=\"website\">\n<meta property=\"og:image\" content=\"")
	page.WriteString(escapeHTML(icon))
	page.WriteString("\">\n<meta name=\"twitter:card\" content=\"summary\">\n<link rel=\"icon\" type=\"image/png\" href=\"/assets/favicon.png\">\n<style>\n")
	page.WriteString(joinPageCSS)
	page.WriteString("\n</style>\n</head>\n<body>\n<main>\n<div class=\"brand\"><img src=\"/assets/favicon.png\" alt=\"\"><span>NeoRouter</span></div>\n")
	if valid {
		page.WriteString("<h1>")
		page.WriteString(escapeHTML(text.Heading))
		page.WriteString("</h1>\n<p>")
		page.WriteString(escapeHTML(text.Lead(invite.Host, invite.Pool)))
		page.WriteString("</p>\n<div class=\"pool\"><div class=\"mark\" aria-hidden=\"true\"><svg width=\"20\" height=\"20\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"1.8\"><circle cx=\"12\" cy=\"12\" r=\"3\"/><circle cx=\"5\" cy=\"6\" r=\"2\"/><circle cx=\"19\" cy=\"6\" r=\"2\"/><circle cx=\"12\" cy=\"20\" r=\"2\"/><path d=\"M7 7l3 3M17 7l-3 3M12 15v3\"/></svg></div><div><strong>")
		page.WriteString(escapeHTML(invite.Pool))
		page.WriteString("</strong><span>")
		page.WriteString(escapeHTML(text.Members(invite.Members)))
		page.WriteString("</span></div></div>\n<p>")
		page.WriteString(escapeHTML(text.Step))
		page.WriteString("</p>\n<form id=\"go\" novalidate>\n<label for=\"address\">")
		page.WriteString(escapeHTML(text.Label))
		page.WriteString("</label>\n<div class=\"row\"><input id=\"address\" name=\"address\" type=\"url\" inputmode=\"url\" autocomplete=\"url\" placeholder=\"")
		page.WriteString(escapeHTML(text.Placeholder))
		page.WriteString("\" required><button type=\"submit\">")
		page.WriteString(escapeHTML(text.Button))
		page.WriteString("</button></div>\n<div class=\"error\" id=\"error\" role=\"alert\">")
		page.WriteString(escapeHTML(text.Bad))
		page.WriteString("</div>\n</form>\n<p class=\"small\">")
		page.WriteString(escapeHTML(text.Manual))
		page.WriteString("<br>")
		page.WriteString(escapeHTML(text.Once))
		page.WriteString("</p>\n<p class=\"small\">")
		page.WriteString(escapeHTML(text.None))
		page.WriteString(" <a href=\"")
		page.WriteString(githubURL)
		page.WriteString("\" target=\"_blank\" rel=\"noopener\">")
		page.WriteString(escapeHTML(text.NoneLink))
		page.WriteString("</a></p>\n")
		page.WriteString(joinPageScript)
	} else {
		page.WriteString("<h1>")
		page.WriteString(escapeHTML(text.InvalidTitle))
		page.WriteString("</h1>\n<p>")
		page.WriteString(escapeHTML(text.Invalid))
		page.WriteString("</p>\n<p class=\"small\">")
		page.WriteString(escapeHTML(text.None))
		page.WriteString(" <a href=\"")
		page.WriteString(githubURL)
		page.WriteString("\" target=\"_blank\" rel=\"noopener\">")
		page.WriteString(escapeHTML(text.NoneLink))
		page.WriteString("</a></p>")
	}
	page.WriteString("\n</main>\n</body>\n</html>")
	return page.String()
}

// PoolJoinPage answers /pool/join: the invite page for one token.
func (s *Service) PoolJoinPage(w http.ResponseWriter, r *http.Request) {
	lang := joinLanguage(r.URL.Query().Get("lang"), r.Header.Get("Accept-Language"))
	origin := s.publicOrigin(r)
	var invite *joinInvite
	if answer, err := s.handlePreview(r.Context(), r.URL.Query().Get("pool")); err == nil {
		invite = &joinInvite{Pool: answer.Pool.Name, Host: answer.Host.Name, Members: len(answer.Members)}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(renderJoinPage(lang, origin, invite)))
}

// JoinRoute registers the invite page.
func (s *Service) JoinRoute(mux *http.ServeMux) {
	mux.HandleFunc("GET /pool/join", s.PoolJoinPage)
}
