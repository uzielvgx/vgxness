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
	Session string
}

func (h *handler) renderOverview(writer http.ResponseWriter, request *http.Request, token string, replace bool) {
	view, err := h.reader.AdminOverview(request.Context(), syncpg.AdminPage{Limit: 25}, syncpg.AdminPage{Limit: 50})
	if err != nil {
		htmlError(writer, http.StatusServiceUnavailable)
		return
	}
	body, err := encode(overviewTemplate, overviewView{AdminOverview: view, Session: token})
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
	render(writer, status, errorTemplate, http.StatusText(status))
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
	loginTemplate    = template.Must(template.New("login").Parse(pageStart + `<main><h1>VGXNESS cloud admin</h1><p>Local operator access only.</p>{{if .}}<p role="alert">{{.}}</p>{{end}}<form method="post" action="/login"><label for="secret">Operator secret</label><input id="secret" name="secret" type="password" required autocomplete="current-password"><button type="submit">Sign in</button></form></main>` + pageEnd))
	errorTemplate    = template.Must(template.New("error").Parse(pageStart + `<main><h1>{{.}}</h1><p>The request could not be completed.</p></main>` + pageEnd))
	overviewTemplate = template.Must(template.New("overview").Parse(pageStart + `<header><h1>VGXNESS cloud admin</h1><form method="post" action="/logout"><input type="hidden" name="session" value="{{.Session}}"><button type="submit">Sign out</button></form></header><main><form method="post" action="/"><input type="hidden" name="session" value="{{.Session}}"><button type="submit">Refresh</button></form><section aria-labelledby="health"><h2 id="health">Repository health</h2><dl><dt>Database</dt><dd>{{if .Health.Database}}Available{{else}}Unavailable{{end}}</dd><dt>Owner history</dt><dd><code>{{.Health.HistoryID}}</code></dd><dt>Head sequence</dt><dd>{{.Health.HeadSequence}}</dd></dl></section><section aria-labelledby="devices"><h2 id="devices">Devices</h2><table><caption>Owner-bound devices</caption><thead><tr><th scope="col">Name</th><th scope="col">ID</th><th scope="col">Issued</th><th scope="col">Status</th></tr></thead><tbody>{{range .Devices}}<tr><td>{{.Name}}</td><td><code>{{.ID}}</code></td><td>{{.IssuedAt}}</td><td>{{if .RevokedAt}}Revoked{{else}}Active{{end}}</td></tr>{{else}}<tr><td colspan="4">No devices</td></tr>{{end}}</tbody></table></section><section aria-labelledby="audit"><h2 id="audit">Recent audit events</h2><table><caption>Most recent owner events</caption><thead><tr><th scope="col">Time</th><th scope="col">Action</th><th scope="col">Outcome</th><th scope="col">Reason</th></tr></thead><tbody>{{range .AuditEvents}}<tr><td>{{.OccurredAt}}</td><td>{{.Action}}</td><td>{{.Outcome}}</td><td>{{.Reason}}</td></tr>{{else}}<tr><td colspan="4">No events</td></tr>{{end}}</tbody></table></section></main>` + pageEnd))
)

const pageStart = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>VGXNESS cloud admin</title><style>body{font:16px system-ui;max-width:72rem;margin:auto;padding:2rem;color:#17202a;background:#f7f7f4}header{display:flex;justify-content:space-between;align-items:center}main{display:grid;gap:1.5rem}section,form{background:white;border:1px solid #ccd1d1;padding:1rem}label,input,button{display:block;margin:.5rem 0}input{padding:.6rem;max-width:30rem;width:90%}button{padding:.6rem 1rem}table{border-collapse:collapse;width:100%}caption{text-align:left}th,td{text-align:left;border-bottom:1px solid #ddd;padding:.5rem}code{overflow-wrap:anywhere}</style></head><body>`
const pageEnd = `</body></html>`
