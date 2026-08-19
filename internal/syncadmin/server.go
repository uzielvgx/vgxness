package syncadmin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/vgxness/vgxness/internal/syncpg"
)

const maxFormBytes = 4 << 10

type Reader interface {
	AdminOverview(context.Context, syncpg.AdminPage, syncpg.AdminPage) (syncpg.AdminOverview, error)
}
type handler struct {
	reader     Reader
	authority  string
	secretHash [sha256.Size]byte
	random     io.Reader
	mutex      sync.Mutex
	session    [sha256.Size]byte
	active     bool
}

func New(reader Reader, operatorSecret, authority string) (http.Handler, error) {
	if reader == nil || operatorSecret == "" || !validAuthority(authority) {
		return nil, errors.New("invalid admin configuration")
	}
	return &handler{reader: reader, authority: authority, secretHash: sha256.Sum256([]byte(operatorSecret)), random: rand.Reader}, nil
}

func NewOperatorSecret() (string, error) { return randomValue(rand.Reader) }
func randomValue(source io.Reader) (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(source, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func (h *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	securityHeaders(writer.Header())
	if request.Host != h.authority {
		htmlError(writer, http.StatusMisdirectedRequest)
		return
	}
	if request.Method == http.MethodPost && request.Header.Get("Origin") != "http://"+h.authority {
		htmlError(writer, http.StatusForbidden)
		return
	}
	switch request.URL.Path {
	case "/login":
		h.login(writer, request)
	case "/logout":
		h.logout(writer, request)
	case "/":
		h.overview(writer, request)
	default:
		htmlError(writer, http.StatusNotFound)
	}
}
func (h *handler) login(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		render(writer, http.StatusOK, loginTemplate, "")
		return
	}
	if request.Method != http.MethodPost || !formRequest(request) {
		htmlError(writer, http.StatusMethodNotAllowed)
		return
	}
	if !parseForm(writer, request) {
		return
	}
	secret, ok := singleFormValue(request, "secret")
	if !ok || !h.secretMatches(secret) {
		render(writer, http.StatusUnauthorized, loginTemplate, "Login failed")
		return
	}
	token, err := randomValue(h.random)
	if err != nil {
		htmlError(writer, http.StatusInternalServerError)
		return
	}
	h.renderOverview(writer, request, token, true)
}
func (h *handler) overview(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		http.Redirect(writer, request, "/login", http.StatusSeeOther)
		return
	}
	if request.Method != http.MethodPost || !formRequest(request) {
		htmlError(writer, http.StatusMethodNotAllowed)
		return
	}
	if !parseForm(writer, request) {
		return
	}
	token, ok := singleFormValue(request, "session")
	if !ok || !h.authorized(token) {
		htmlError(writer, http.StatusUnauthorized)
		return
	}
	h.renderOverview(writer, request, token, false)
}

type overviewView struct {
	syncpg.AdminOverview
	Session        string
	ActiveDevices  int
	RevokedDevices int
}

func (h *handler) renderOverview(writer http.ResponseWriter, request *http.Request, token string, replace bool) {
	view, err := h.reader.AdminOverview(request.Context(), syncpg.AdminPage{Limit: 25}, syncpg.AdminPage{Limit: 50})
	if err != nil {
		htmlError(writer, http.StatusServiceUnavailable)
		return
	}
	dashboard := overviewView{AdminOverview: view, Session: token}
	for _, device := range view.Devices {
		if device.RevokedAt == nil {
			dashboard.ActiveDevices++
		} else {
			dashboard.RevokedDevices++
		}
	}
	body, err := encode(overviewTemplate, dashboard)
	if err != nil {
		htmlError(writer, http.StatusInternalServerError)
		return
	}
	provided := sha256.Sum256([]byte(token))
	h.mutex.Lock()
	if !replace && (!h.active || subtle.ConstantTimeCompare(provided[:], h.session[:]) != 1) {
		h.mutex.Unlock()
		htmlError(writer, http.StatusUnauthorized)
		return
	}
	priorSession, priorActive := h.session, h.active
	if replace {
		h.session, h.active = provided, true
	}
	writer.WriteHeader(http.StatusOK)
	written, writeErr := writer.Write(body)
	if replace && (writeErr != nil || written != len(body)) {
		h.session, h.active = priorSession, priorActive
	}
	h.mutex.Unlock()
}
func (h *handler) logout(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || !formRequest(request) {
		htmlError(writer, http.StatusMethodNotAllowed)
		return
	}
	if !parseForm(writer, request) {
		return
	}
	token, ok := singleFormValue(request, "session")
	if !ok || !validSession(token) {
		htmlError(writer, http.StatusBadRequest)
		return
	}
	provided := sha256.Sum256([]byte(token))
	h.mutex.Lock()
	if h.active && subtle.ConstantTimeCompare(provided[:], h.session[:]) == 1 {
		h.session, h.active = [sha256.Size]byte{}, false
	}
	h.mutex.Unlock()
	http.Redirect(writer, request, "/login", http.StatusSeeOther)
}
func (h *handler) secretMatches(value string) bool {
	provided := sha256.Sum256([]byte(value))
	return subtle.ConstantTimeCompare(provided[:], h.secretHash[:]) == 1
}
func (h *handler) authorized(token string) bool {
	if !validSession(token) {
		return false
	}
	provided := sha256.Sum256([]byte(token))
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.active && subtle.ConstantTimeCompare(provided[:], h.session[:]) == 1
}
func securityHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	header.Set("X-Frame-Options", "DENY")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Type", "text/html; charset=utf-8")
}
func formRequest(request *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/x-www-form-urlencoded"
}
func parseForm(writer http.ResponseWriter, request *http.Request) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxFormBytes)
	if request.ParseForm() != nil {
		htmlError(writer, http.StatusBadRequest)
		return false
	}
	return true
}
func singleFormValue(request *http.Request, name string) (string, bool) {
	values := request.PostForm[name]
	if len(request.PostForm) != 1 || len(values) != 1 {
		return "", false
	}
	return values[0], true
}
func validSession(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}
func validAuthority(value string) bool {
	host, port, splitErr := net.SplitHostPort(value)
	number, numberErr := strconv.Atoi(port)
	return splitErr == nil && (host == "127.0.0.1" || host == "::1") && numberErr == nil && number > 0 && number <= 65535 && strconv.Itoa(number) == port
}
func htmlError(writer http.ResponseWriter, status int) {
	render(writer, status, errorTemplate, struct {
		Status int
		Label  string
	}{Status: status, Label: http.StatusText(status)})
}
func render(writer http.ResponseWriter, status int, page *template.Template, value any) {
	body, err := encode(page, value)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}
func encode(page *template.Template, value any) ([]byte, error) {
	var body bytes.Buffer
	err := page.Execute(&body, value)
	return body.Bytes(), err
}

var (
	loginTemplate = template.Must(template.New("login").Parse(pageStart + `
<main class="login-shell">
  <section class="access-panel" aria-labelledby="login-title">
    <div class="brand-lockup"><span class="brand-mark" aria-hidden="true">V</span><span>VGXNESS</span><small>cloud admin</small></div>
    <div class="access-copy">
      <p class="eyebrow">Secure console / loopback</p>
      <h1 id="login-title">Local operator access</h1>
      <p class="lede">Inspect repository health, owner history, device trust, and audit activity from this machine.</p>
      <div class="notice"><span class="status-dot" aria-hidden="true"></span><p>This console accepts credentials on the loopback interface only. Your operator secret is submitted directly to the local service and is never displayed here.</p></div>
    </div>
    {{if .}}<p class="alert" role="alert"><strong>Access denied.</strong> {{.}}</p>{{end}}
    <form class="login-form" method="post" action="/login">
      <label for="secret">Operator secret <span>Required</span></label>
      <input id="secret" name="secret" type="password" required autocomplete="current-password" autofocus>
      <button class="primary-action" type="submit">Enter control room <span aria-hidden="true">→</span></button>
    </form>
    <p class="access-foot">Read-only surface <span aria-hidden="true">·</span> no remote sign-in <span aria-hidden="true">·</span> no session cookies</p>
  </section>
</main>` + pageEnd))
	errorTemplate = template.Must(template.New("error").Parse(pageStart + `
<main class="error-shell">
  <section class="error-panel" aria-labelledby="error-title">
    <div class="brand-lockup"><span class="brand-mark" aria-hidden="true">V</span><span>VGXNESS</span><small>cloud admin</small></div>
    <p class="eyebrow">Control plane / exception</p>
    <p class="error-code" aria-label="Status {{.Status}}">{{.Status}}</p>
    <h1 id="error-title">{{.Label}}</h1>
    <p class="lede">Status {{.Status}}. The local admin service could not complete this request.</p>
    <a class="primary-action" href="/login">Return to sign in <span aria-hidden="true">→</span></a>
    <p class="access-foot">No operational data was changed.</p>
  </section>
</main>` + pageEnd))
	overviewTemplate = template.Must(template.New("overview").Funcs(template.FuncMap{
		"timestamp":   timestamp,
		"machineTime": machineTime,
	}).Parse(pageStart + `
<a class="skip-link" href="#main-content">Skip to repository overview</a>
<header class="topbar">
  <div class="brand-lockup"><span class="brand-mark" aria-hidden="true">V</span><span>VGXNESS</span><small>cloud admin</small></div>
  <div class="topbar-status"><span class="status-dot" aria-hidden="true"></span><span>Loopback</span><span class="divider" aria-hidden="true"></span><span>Read only</span></div>
  <form class="compact-form" method="post" action="/logout">
    <input type="hidden" name="session" value="{{.Session}}">
    <button class="quiet-action" type="submit">Sign out</button>
  </form>
</header>
<main id="main-content" class="dashboard-shell">
  <section class="command-header" aria-labelledby="page-title">
    <div>
      <p class="eyebrow">Cloud repository / live read model</p>
      <h1 id="page-title">Owner control plane</h1>
      <p class="lede">Operational state for the bound repository, trusted devices, and the latest authorization decisions.</p>
    </div>
    <form class="compact-form" method="post" action="/">
      <input type="hidden" name="session" value="{{.Session}}">
      <button class="primary-action" type="submit">Refresh snapshot <span aria-hidden="true">↻</span></button>
    </form>
  </section>

  <section class="metrics-grid" aria-label="Repository status">
    <article class="metric metric-health">
      <div class="metric-top"><span>Repository</span>{{if .Health.Database}}<span class="live-signal"><i aria-hidden="true"></i> Live</span>{{else}}<span class="metric-index">Unavailable</span>{{end}}</div>
      <strong>{{if .Health.Database}}Repository online{{else}}Repository unavailable{{end}}</strong>
      <p>{{if .Health.Database}}Read model responding normally{{else}}Read model requires attention{{end}}</p>
    </article>
    <article class="metric">
      <div class="metric-top"><span>Head sequence</span><span class="metric-index">01</span></div>
      <strong>{{.Health.HeadSequence}}</strong>
      <p>Latest owner event applied</p>
    </article>
	<article class="metric">
	  <div class="metric-top"><span>Active in snapshot</span><span class="metric-index">02</span></div>
	  <strong>{{.ActiveDevices}}</strong>
	  <p>Trusted identities in this page</p>
	</article>
	<article class="metric metric-revoked">
	  <div class="metric-top"><span>Revoked in snapshot</span><span class="metric-index">03</span></div>
	  <strong>{{.RevokedDevices}}</strong>
	  <p>Withdrawn identities in this page</p>
	</article>
  </section>

  <section class="identity-strip" aria-labelledby="history-label">
    <div><p class="eyebrow" id="history-label">Owner history</p><p>Immutable repository lineage</p></div>
    <code>{{.Health.HistoryID}}</code>
  </section>

  <section class="data-panel" aria-labelledby="devices-title">
    <div class="panel-heading">
      <div><p class="eyebrow">Trust registry</p><h2 id="devices-title">Owner-bound devices</h2></div>
	  <p>Current page · {{len .Devices}} identities</p>
    </div>
    <div class="table-scroll">
      <table>
        <caption>Devices issued by the current owner history, including latest observed activity.</caption>
        <thead><tr><th scope="col">Device</th><th scope="col">Identifier</th><th scope="col">Issued</th><th scope="col">Last seen</th><th scope="col">Status</th></tr></thead>
        <tbody>{{range .Devices}}
          <tr>
            <td><strong>{{.Name}}</strong><span class="cell-note">Owner credential</span></td>
            <td><code>{{.ID}}</code></td>
            <td><time datetime="{{machineTime .IssuedAt}}">{{timestamp .IssuedAt}}</time></td>
            <td>{{if .LastSeenAt}}<time datetime="{{machineTime .LastSeenAt}}">{{timestamp .LastSeenAt}}</time>{{else}}<span class="muted">Never observed</span>{{end}}</td>
            <td>{{if .RevokedAt}}<span class="pill pill-revoked"><i aria-hidden="true"></i> Revoked</span>{{else}}<span class="pill pill-active"><i aria-hidden="true"></i> Active</span>{{end}}</td>
          </tr>
        {{else}}<tr><td class="empty-state" colspan="5"><strong>No devices issued</strong><span>The trust registry is empty for this owner.</span></td></tr>{{end}}</tbody>
      </table>
    </div>
  </section>

  <section class="data-panel audit-panel" aria-labelledby="audit-title">
    <div class="panel-heading">
      <div><p class="eyebrow">Repository chronicle</p><h2 id="audit-title">Recent audit activity</h2></div>
      <p>Newest decisions first</p>
    </div>
    <div class="table-scroll">
      <table>
        <caption>Recent actions and outcomes recorded by the owner repository.</caption>
        <thead><tr><th scope="col">Time</th><th scope="col">Action</th><th scope="col">Outcome</th><th scope="col">Reason</th><th scope="col">Device</th></tr></thead>
        <tbody>{{range .AuditEvents}}
          <tr>
            <td><time datetime="{{machineTime .OccurredAt}}">{{timestamp .OccurredAt}}</time></td>
            <td><strong class="action-name">{{.Action}}</strong></td>
            <td><span class="outcome">{{.Outcome}}</span></td>
            <td>{{if .Reason}}<code>{{.Reason}}</code>{{else}}<span class="muted">—</span>{{end}}</td>
            <td>{{if .DeviceID}}<code>{{.DeviceID}}</code>{{else}}<span class="muted">System</span>{{end}}</td>
          </tr>
        {{else}}<tr><td class="empty-state" colspan="5"><strong>No audit events</strong><span>The repository chronicle has no recent entries.</span></td></tr>{{end}}</tbody>
      </table>
    </div>
  </section>
</main>
<footer class="site-footer"><span>VGXNESS / local operations</span><span>Snapshot is read only</span></footer>` + pageEnd))
)

func timestamp(value time.Time) string   { return value.UTC().Format("2006-01-02 15:04 UTC") }
func machineTime(value time.Time) string { return value.UTC().Format(time.RFC3339) }

const pageStart = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"><title>VGXNESS cloud admin</title><style>
:root{color-scheme:dark;--ink:#070b0c;--panel:#0d1415;--panel-raised:#111b1c;--line:#263537;--line-bright:#3c5051;--text:#edf7f2;--muted:#91a69f;--lime:#c7ff4a;--cyan:#4ce7e7;--danger:#ff8d6b;--mono:ui-monospace,SFMono-Regular,Consolas,"Liberation Mono",monospace;--sans:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
*{box-sizing:border-box}
html{background:var(--ink);scroll-behavior:smooth}
body{margin:0;min-width:18rem;background:var(--ink);color:var(--text);font:400 1rem/1.55 var(--sans);letter-spacing:.005em}
button,input,a{font:inherit}
button,a{touch-action:manipulation}
a{color:inherit}
:focus-visible{outline:3px solid var(--cyan);outline-offset:4px}
.skip-link{position:fixed;z-index:10;top:1rem;left:1rem;padding:.7rem 1rem;background:var(--lime);color:var(--ink);font-weight:800;transform:translateY(-200%)}
.skip-link:focus{transform:translateY(0)}
.brand-lockup{display:flex;align-items:center;gap:.75rem;font:800 .82rem/1 var(--mono);letter-spacing:.16em;text-transform:uppercase;white-space:nowrap}
.brand-lockup small{padding-left:.75rem;border-left:1px solid var(--line-bright);color:var(--muted);font-size:.67rem;font-weight:600;letter-spacing:.11em}
.brand-mark{display:grid;width:2rem;height:2rem;place-items:center;border:1px solid var(--lime);color:var(--lime);font-size:1rem;letter-spacing:0}
.eyebrow{margin:0 0 .65rem;color:var(--cyan);font:700 .7rem/1.3 var(--mono);letter-spacing:.14em;text-transform:uppercase}
h1,h2,p{margin-top:0}
h1{max-width:16ch;margin-bottom:.85rem;font-size:clamp(2.25rem,6vw,4.8rem);font-weight:760;line-height:.95;letter-spacing:-.055em}
h2{margin-bottom:0;font-size:clamp(1.45rem,3vw,2rem);line-height:1.05;letter-spacing:-.035em}
.lede{max-width:44rem;margin-bottom:0;color:#b5c7c1;font-size:clamp(1rem,1.7vw,1.15rem)}
.status-dot{display:inline-block;width:.5rem;height:.5rem;border-radius:50%;background:var(--lime);box-shadow:0 0 0 4px rgba(199,255,74,.1)}
.topbar{min-height:5rem;display:grid;grid-template-columns:1fr auto 1fr;align-items:center;padding:1rem clamp(1rem,4vw,4rem);border-bottom:1px solid var(--line);background:#091011}
.topbar-status{display:flex;align-items:center;gap:.65rem;color:var(--muted);font:700 .67rem/1 var(--mono);letter-spacing:.11em;text-transform:uppercase}
.divider{width:1px;height:1rem;background:var(--line-bright)}
.compact-form{justify-self:end;margin:0}
input{width:100%;border:1px solid var(--line-bright);border-radius:0;background:#070d0e;color:var(--text)}
input:hover{border-color:#627877}
input:focus{border-color:var(--cyan)}
button,.primary-action,.quiet-action{display:inline-flex;min-height:2.75rem;align-items:center;justify-content:space-between;gap:1.5rem;border:0;border-radius:0;cursor:pointer;font:800 .72rem/1 var(--mono);letter-spacing:.08em;text-decoration:none;text-transform:uppercase}
.primary-action{padding:.85rem 1.1rem;background:var(--lime);color:#09100c}
.primary-action:hover{background:#dcff8e}
.quiet-action{padding:.75rem 0;border-bottom:1px solid var(--line-bright);background:transparent;color:var(--text)}
.quiet-action:hover{border-color:var(--lime);color:var(--lime)}
.dashboard-shell{width:min(100% - 2rem,94rem);margin:0 auto;padding:clamp(2.5rem,6vw,6rem) 0 4rem}
.command-header{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:end;gap:2rem;margin-bottom:clamp(2.5rem,6vw,5rem)}
.command-header h1{max-width:none}
.metrics-grid{display:grid;grid-template-columns:1.35fr repeat(3,1fr);border-top:1px solid var(--line);border-left:1px solid var(--line);margin-bottom:1rem}
.metric{min-height:12rem;padding:1.25rem;border-right:1px solid var(--line);border-bottom:1px solid var(--line);background:var(--panel)}
.metric-health{background:#101917}
.metric-revoked strong{color:var(--danger)}
.metric-top{display:flex;justify-content:space-between;gap:1rem;color:var(--muted);font:700 .68rem/1.3 var(--mono);letter-spacing:.09em;text-transform:uppercase}
.metric-index{color:#526461}
.metric strong{display:block;margin:2.2rem 0 .35rem;color:var(--lime);font-size:clamp(2.2rem,4vw,3.6rem);line-height:1;letter-spacing:-.055em}
.metric-health strong{font-size:clamp(1.5rem,2.5vw,2.35rem);letter-spacing:-.04em}
.metric p{margin:0;color:var(--muted);font-size:.8rem}
.live-signal{display:flex;align-items:center;gap:.5rem;color:var(--lime)}
.live-signal i,.pill i{width:.42rem;height:.42rem;border-radius:50%;background:currentColor}
.identity-strip{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:2rem;margin-bottom:3.5rem;padding:1rem 1.25rem;border:1px solid var(--line);background:#090f10}
.identity-strip p{margin:0;color:var(--muted);font-size:.78rem}
.identity-strip .eyebrow{margin-bottom:.25rem;color:var(--muted)}
.identity-strip code{justify-self:end;color:var(--cyan);text-align:right}
.data-panel{margin-top:1rem;border:1px solid var(--line);background:var(--panel)}
.audit-panel{margin-top:2rem}
.panel-heading{display:flex;align-items:end;justify-content:space-between;gap:1.5rem;padding:1.5rem;border-bottom:1px solid var(--line)}
.panel-heading .eyebrow{margin-bottom:.45rem}
.panel-heading>p{margin:0;color:var(--muted);font:600 .68rem/1 var(--mono);letter-spacing:.08em;text-transform:uppercase}
.table-scroll{overflow-x:auto}
table{width:100%;border-collapse:collapse;white-space:nowrap}
caption{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
th,td{padding:1rem 1.5rem;border-bottom:1px solid #1d292a;text-align:left;vertical-align:middle}
th{background:#0a1011;color:#78918a;font:700 .65rem/1 var(--mono);letter-spacing:.1em;text-transform:uppercase}
tbody tr:last-child td{border-bottom:0}
tbody tr:hover{background:#111a1b}
td{color:#c7d6d1;font-size:.84rem}
td strong{color:var(--text)}
code,time{font:500 .75rem/1.5 var(--mono)}
code{overflow-wrap:anywhere}
.cell-note{display:block;margin-top:.18rem;color:#667c76;font:600 .62rem/1.4 var(--mono);text-transform:uppercase}
.pill,.outcome{display:inline-flex;align-items:center;gap:.5rem;padding:.35rem .55rem;border:1px solid currentColor;font:800 .62rem/1 var(--mono);letter-spacing:.06em;text-transform:uppercase}
.pill-active,.outcome{color:var(--lime);background:rgba(199,255,74,.04)}
.pill-revoked{color:var(--danger);background:rgba(255,141,107,.04)}
.action-name{font-family:var(--mono);font-size:.78rem}
.muted{color:#6f8580}
.empty-state{text-align:center;padding:3rem;color:var(--muted)}
.empty-state strong,.empty-state span{display:block}
.empty-state strong{margin-bottom:.35rem;color:var(--text);font-size:1rem}
.site-footer{display:flex;justify-content:space-between;gap:1rem;width:min(100% - 2rem,94rem);margin:auto;padding:1.25rem 0 2rem;border-top:1px solid var(--line);color:#657a74;font:600 .63rem/1.4 var(--mono);letter-spacing:.09em;text-transform:uppercase}
.login-shell,.error-shell{min-height:100vh;display:grid;place-items:center;padding:clamp(1rem,5vw,4rem)}
.access-panel,.error-panel{position:relative;width:min(100%,68rem);padding:clamp(1.5rem,5vw,4.5rem);border:1px solid var(--line);background:var(--panel)}
.access-panel:before,.error-panel:before{content:"";position:absolute;top:-1px;left:-1px;width:7rem;height:3px;background:var(--lime)}
.access-panel{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(18rem,.85fr);gap:clamp(2.5rem,7vw,7rem);align-items:end}
.access-panel>.brand-lockup{grid-column:1/-1;margin-bottom:clamp(2rem,6vw,5rem)}
.access-copy h1{font-size:clamp(2.5rem,6vw,5.2rem)}
.notice{display:flex;gap:1rem;max-width:36rem;margin-top:2rem;padding:1rem 0;border-top:1px solid var(--line);border-bottom:1px solid var(--line)}
.notice .status-dot{flex:none;margin-top:.45rem}
.notice p{margin:0;color:var(--muted);font-size:.78rem}
.login-form{align-self:end;padding:1.5rem;border:1px solid var(--line);background:#091011}
.login-form label{display:flex;justify-content:space-between;margin-bottom:.65rem;font:700 .7rem/1 var(--mono);letter-spacing:.07em;text-transform:uppercase}
.login-form label span{color:var(--muted);font-size:.6rem}
.login-form input{height:3.25rem;margin-bottom:1rem;padding:.75rem 1rem;font-family:var(--mono)}
.login-form .primary-action{width:100%}
.alert{grid-column:2;margin:0 0 -2rem;padding:.8rem 1rem;border-left:3px solid var(--danger);background:rgba(255,141,107,.07);color:#ffc1ae;font-size:.82rem}
.access-foot{grid-column:1/-1;margin:2.5rem 0 0;color:#687c77;font:600 .62rem/1.5 var(--mono);letter-spacing:.08em;text-transform:uppercase}
.error-panel{max-width:50rem}
.error-panel>.brand-lockup{margin-bottom:clamp(3rem,8vw,7rem)}
.error-code{float:right;margin:0;color:#334442;font:800 clamp(5rem,18vw,12rem)/.75 var(--mono);letter-spacing:-.1em}
.error-panel h1{font-size:clamp(2.8rem,7vw,5.5rem)}
.error-panel .lede{max-width:35rem;margin-bottom:2rem}
.error-panel .access-foot{margin-top:3rem}
@media (max-width: 70rem){.metrics-grid{grid-template-columns:repeat(2,1fr)}.topbar{grid-template-columns:1fr auto}.topbar-status{display:none}}
@media (max-width: 48rem){.topbar{padding:.85rem 1rem}.brand-lockup small{display:none}.command-header{grid-template-columns:1fr;align-items:start}.command-header .compact-form{justify-self:start}.metrics-grid{grid-template-columns:1fr 1fr}.metric{min-height:10rem}.identity-strip{grid-template-columns:1fr;gap:.6rem}.identity-strip code{justify-self:start;text-align:left}.panel-heading{align-items:start;flex-direction:column}.access-panel{grid-template-columns:1fr}.access-panel>.brand-lockup,.alert,.access-foot{grid-column:1}.alert{margin:0}.access-copy h1{font-size:3rem}.error-code{float:none;margin-bottom:2rem}}
@media (max-width: 32rem){.dashboard-shell,.site-footer{width:min(100% - 1rem,94rem)}.metrics-grid{grid-template-columns:1fr}.metric{min-height:9rem}.metric strong{margin-top:1.5rem}.topbar .brand-mark{display:none}.topbar .brand-lockup{font-size:.72rem}.panel-heading,th,td{padding:1rem}.site-footer{display:block}.site-footer span{display:block;margin-bottom:.5rem}}
@media (prefers-reduced-motion: reduce){html{scroll-behavior:auto}}
</style></head><body>`
const pageEnd = `</body></html>`
