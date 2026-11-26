package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
	"github.com/fxamacker/cbor/v2"
)

type mailbox struct {
	mu   sync.Mutex
	sent []servicemail.Message
}

func (m *mailbox) Send(_ context.Context, _ string, message servicemail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, message)
	return nil
}

func (h *harness) postForm(target string, values url.Values) response {
	h.t.Helper()
	request := httptest.NewRequest("POST", target, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return response{status: recorder.Code, body: recorder.Body.Bytes(), header: recorder.Header()}
}

func TestPasswordResetByMail(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()

	options := h.do("GET", "/api/v1/auth/options", nil, "")
	if path(t, options.object(t), "passwordReset") != false {
		t.Fatalf("without mail the reset is off: %s", options.body)
	}

	box := &mailbox{}
	h.service.Mailer = box
	h.service.Mail = servicemail.Config{Enabled: true, PublicURL: "https://router.example.test", TokenTTLHours: 1}
	if forgot := h.do("POST", "/api/v1/auth/password/forgot", map[string]any{"account": "nobody"}, ""); forgot.status != http.StatusOK {
		t.Fatalf("forgot: %d %s", forgot.status, forgot.body)
	}
	if forgot := h.do("POST", "/api/v1/auth/password/forgot", map[string]any{"account": "admin@example.test"}, ""); forgot.status != http.StatusOK {
		t.Fatalf("forgot: %d %s", forgot.status, forgot.body)
	}
	h.service.Wait()
	if len(box.sent) != 1 {
		t.Fatalf("mails sent: %d", len(box.sent))
	}
	link, err := url.Parse(box.sent[0].ActionURL)
	if err != nil || link.Host != "router.example.test" {
		t.Fatalf("link %q: %v", box.sent[0].ActionURL, err)
	}
	token := link.Query().Get("token")

	page := h.do("GET", link.RequestURI(), nil, "")
	if page.status != http.StatusOK || page.header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("reset page: %d %v", page.status, page.header)
	}
	submit := func(password string) response {
		return h.postForm(link.Path, url.Values{"token": {token}, "password": {password}, "confirmPassword": {password}})
	}
	if weak := submit("aaaaaaaaaaaa"); weak.status != http.StatusBadRequest {
		t.Fatalf("weak password: %d", weak.status)
	}
	if done := submit("violet-Harbor-9-lantern"); done.status != http.StatusOK {
		t.Fatalf("reset: %d %s", done.status, done.body)
	}
	if me := h.do("GET", "/api/v1/auth/me", nil, h.token); me.status != http.StatusUnauthorized {
		t.Fatalf("a reset signs every session out: %d", me.status)
	}
	if again := submit("another-Harbor-9-lantern"); again.status != http.StatusBadRequest {
		t.Fatalf("the link works once: %d", again.status)
	}
	login := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": "violet-Harbor-9-lantern"}, "")
	if login.status != http.StatusOK {
		t.Fatalf("new password: %d %s", login.status, login.body)
	}
}

func TestQRSignIn(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()

	created := h.do("POST", "/api/v1/auth/qr-login/challenge", map[string]any{"requestMetadata": map[string]any{"deviceLabel": "Kitchen screen"}}, "")
	if created.status != http.StatusOK {
		t.Fatalf("challenge: %d %s", created.status, created.body)
	}
	challenge := created.object(t)
	id, poll := challenge["challengeId"].(string), challenge["pollToken"].(string)
	payload, err := url.Parse(challenge["qrPayload"].(string))
	if err != nil || payload.Path != "/app/" || payload.Query().Get("challenge") != id {
		t.Fatalf("payload %v: %v", challenge["qrPayload"], err)
	}
	secret := payload.Query().Get("secret")
	scan := map[string]any{"challengeId": id, "secret": secret}
	claim := func() response {
		return h.do("POST", "/api/v1/auth/qr-login/challenge/"+id+"/claim", map[string]any{"token": poll}, "")
	}

	if early := claim(); early.status != http.StatusConflict || early.code(t) != "QR_LOGIN_ERROR" {
		t.Fatalf("claim before approval: %d %s", early.status, early.body)
	}
	if anonymous := h.do("POST", "/api/v1/account/qr-login/approve", scan, ""); anonymous.status != http.StatusUnauthorized {
		t.Fatalf("approving needs a session: %d", anonymous.status)
	}
	resolved := h.do("POST", "/api/v1/account/qr-login/resolve", scan, h.token)
	if resolved.status != http.StatusOK || path(t, resolved.object(t), "requestedDevice", "label") != "Kitchen screen" {
		t.Fatalf("resolve: %d %s", resolved.status, resolved.body)
	}
	approved := h.do("POST", "/api/v1/account/qr-login/approve", scan, h.token)
	if approved.status != http.StatusOK || path(t, approved.object(t), "approval", "userId") != h.adminID {
		t.Fatalf("approve: %d %s", approved.status, approved.body)
	}
	claimed := claim()
	if claimed.status != http.StatusOK || path(t, claimed.object(t), "user", "id") != h.adminID {
		t.Fatalf("claim: %d %s", claimed.status, claimed.body)
	}
	token := path(t, claimed.object(t), "session", "token").(string)
	if me := h.do("GET", "/api/v1/auth/me", nil, token); me.status != http.StatusOK {
		t.Fatalf("the claimed session: %d", me.status)
	}
	if again := claim(); again.status != http.StatusConflict {
		t.Fatalf("a claim works once: %d", again.status)
	}
}

// authenticator is a software security key for http://example.com, the origin
// of httptest requests.
type authenticator struct {
	t       *testing.T
	key     *ecdsa.PrivateKey
	id      []byte
	owner   string // the user handle a discoverable credential returns
	counter uint32
}

const (
	userPresent    = 0x01
	userVerified   = 0x04
	attestedKey    = 0x40
	passkeyOrigin  = "http://example.com"
	passkeyRelying = "example.com"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (a *authenticator) data(flags byte) []byte {
	a.counter++
	hash := sha256.Sum256([]byte(passkeyRelying))
	return binary.BigEndian.AppendUint32(append(hash[:], flags), a.counter)
}

func (a *authenticator) clientData(kind string, options map[string]any) []byte {
	raw, err := json.Marshal(map[string]any{"type": kind, "challenge": options["challenge"], "origin": passkeyOrigin})
	if err != nil {
		a.t.Fatal(err)
	}
	return raw
}

func (a *authenticator) create(options map[string]any) map[string]any {
	point, err := a.key.PublicKey.Bytes()
	if err != nil {
		a.t.Fatal(err)
	}
	public, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: point[1:33], -3: point[33:]})
	if err != nil {
		a.t.Fatal(err)
	}
	data := append(a.data(userPresent|userVerified|attestedKey), make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.id)))
	data = append(append(data, a.id...), public...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		a.t.Fatal(err)
	}
	return map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{
		"clientDataJSON":    b64(a.clientData("webauthn.create", options)),
		"attestationObject": b64(attestation),
	}}
}

func (a *authenticator) get(options map[string]any, flags byte) map[string]any {
	client := a.clientData("webauthn.get", options)
	data := a.data(flags)
	clientHash := sha256.Sum256(client)
	digest := sha256.Sum256(append(append([]byte{}, data...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	return map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{
		"clientDataJSON": b64(client), "authenticatorData": b64(data), "signature": b64(signature),
		"userHandle": b64([]byte(a.owner)),
	}}
}

func TestPasskeys(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device := &authenticator{t: t, key: key, id: []byte("test-credential-1"), owner: h.adminID}

	if wrong := h.do("POST", "/api/v1/settings/security-keys/options", map[string]any{"currentPassword": "nope"}, h.token); wrong.code(t) != "INVALID_PASSWORD" {
		t.Fatalf("adding a key needs the password: %d %s", wrong.status, wrong.body)
	}
	start := h.do("POST", "/api/v1/settings/security-keys/options", map[string]any{"currentPassword": h.password}, h.token)
	if start.status != http.StatusOK {
		t.Fatalf("options: %d %s", start.status, start.body)
	}
	added := h.do("POST", "/api/v1/settings/security-keys", map[string]any{
		"challengeId": path(t, start.object(t), "challengeId"), "label": "Laptop",
		"response": device.create(path(t, start.object(t), "options").(map[string]any)),
	}, h.token)
	if added.status != http.StatusCreated || path(t, added.object(t), "credentials", "0", "label") != "Laptop" {
		t.Fatalf("add: %d %s", added.status, added.body)
	}

	var spent map[string]any
	signIn := func(flags byte, code string) response {
		options := h.do("POST", "/api/v1/auth/webauthn/options", map[string]any{"account": "admin"}, "")
		if options.status != http.StatusOK {
			t.Fatalf("sign-in options: %d %s", options.status, options.body)
		}
		spent = map[string]any{
			"challengeId": path(t, options.object(t), "challengeId"), "twoFactorCode": code,
			"response": device.get(path(t, options.object(t), "options").(map[string]any), flags),
		}
		return h.do("POST", "/api/v1/auth/webauthn/verify", spent, "")
	}
	signedIn := signIn(userPresent|userVerified, "")
	if signedIn.status != http.StatusOK || path(t, signedIn.object(t), "user", "id") != h.adminID {
		t.Fatalf("sign in: %d %s", signedIn.status, signedIn.body)
	}
	if replay := h.do("POST", "/api/v1/auth/webauthn/verify", spent, ""); replay.code(t) != "CHALLENGE_EXPIRED" {
		t.Fatalf("a challenge works once: %d %s", replay.status, replay.body)
	}

	// With 2FA on, a key that only saw a touch still needs the code; one that
	// verified its user does not.
	secret := h.enableTwoFactorSecret()
	if touch := signIn(userPresent, ""); touch.code(t) != "TWO_FACTOR_REQUIRED" {
		t.Fatalf("presence only: %d %s", touch.status, touch.body)
	}
	if touch := signIn(userPresent, h.code(secret)); touch.status != http.StatusOK {
		t.Fatalf("presence with code: %d %s", touch.status, touch.body)
	}
	if verified := signIn(userPresent|userVerified, ""); verified.status != http.StatusOK {
		t.Fatalf("verified user: %d %s", verified.status, verified.body)
	}

	if err := h.service.SetUserDisabled(h.context(), "someone-else", h.adminID, true); err != nil {
		t.Fatal(err)
	}
	if disabled := signIn(userPresent|userVerified, ""); disabled.code(t) != "ACCOUNT_DISABLED" {
		t.Fatalf("a disabled account: %d %s", disabled.status, disabled.body)
	}
}
