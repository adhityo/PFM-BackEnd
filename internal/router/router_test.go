package router

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"pfm-backend/internal/config"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/security"
	"pfm-backend/internal/platform/storage"
	"pfm-backend/internal/repository"
	"pfm-backend/internal/service"
	"pfm-backend/internal/testutil"
)

// fakeDocs is an in-memory DocumentStorage.
type fakeDocs struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (f *fakeDocs) Bucket() string { return "test-bucket" }
func (f *fakeDocs) Put(_ context.Context, key string, r io.Reader, _ int64, _, _ string) error {
	b, err := io.ReadAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[key] = b
	return err
}
func (f *fakeDocs) PresignGet(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "https://s3.example.test/test-bucket/" + key + "?X-Amz-Signature=test", nil
}
func (f *fakeDocs) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objs, key)
	return nil
}
func (f *fakeDocs) Ping(context.Context) error { return nil }

type env struct {
	t    *testing.T
	app  *fiber.App
	pool *pgxpool.Pool // pfm_core
	// The other databases (PRD §7.1).
	secPool, logPool, docPool *pgxpool.Pool
	docs                      *fakeDocs
}

// ship moves audit events from the outboxes into pfm_log, as the worker does.
func (e *env) ship() {
	e.t.Helper()
	s := audit.NewShipper(database.NewTxManager(e.logPool), map[string]*database.TxManager{
		"core": database.NewTxManager(e.pool), "security": database.NewTxManager(e.secPool),
		"document": database.NewTxManager(e.docPool)})
	if _, err := s.ShipAll(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

const adminPassword = "Admin-Password-123"

func setup(t *testing.T) *env {
	t.Helper()
	pools := testutil.NewPools(t)
	ctx := context.Background()
	tx := database.NewTxManager(pools.Security)
	if err := service.SyncCatalog(ctx, tx, repository.NewActionRepository(tx), repository.NewRoleRepository(tx), Catalog()); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewConfigurationRepository(database.NewTxManager(pools.Core)).Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	hasher := security.NewPasswordHasher(security.Argon2Params{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32})

	// Bootstrap admin, as `migrate seed-admin` does.
	roles := repository.NewRoleRepository(tx)
	users := repository.NewUserRepository(tx)
	adminRole, err := roles.UpsertByCode(ctx, "ADMIN", "ADMINISTRATOR", "system")
	if err != nil {
		t.Fatal(err)
	}
	_, keyHash, _ := security.NewOpaqueToken()
	uid, err := users.Create(ctx, model.NewUser{UUID: uuid.New(), Username: "admin", Email: "admin@example.test",
		Status: model.StatusActive, RoleID: adminRole.ID, ActivationKeyHash: keyHash, ActivationExpires: time.Now(), Actor: "system"})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := hasher.Hash(adminPassword)
	if err := users.SetPassword(ctx, uid, h, "system"); err != nil {
		t.Fatal(err)
	}

	images, err := storage.NewLocalImageStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = images.Close() })
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	docs := &fakeDocs{objs: map[string][]byte{}}
	cfg := config.Config{AppEnv: "test", JWTAccessTTL: 15 * time.Minute, JWTRefreshTTL: time.Hour,
		ActivationKeyTTL: time.Hour, LoginMaxAttempts: 5, LoginLockFor: 15 * time.Minute,
		RequestTimeout: 10 * time.Second, RateLimitPerMinute: 10000, S3PresignTTL: 5 * time.Minute,
		FrontendBaseURL: "https://pfm.test", PasswordResetTTL: 30 * time.Minute}
	app := New(Deps{Cfg: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Pools: pools, Images: images, Docs: docs,
		Signer: security.NewJWTSigner(priv, "pfm", "pfm-api", cfg.JWTAccessTTL), Hasher: hasher})
	return &env{t: t, app: app, pool: pools.Core, secPool: pools.Security, logPool: pools.Log, docPool: pools.Document, docs: docs}
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
	raw    []byte
}

func (r resp) data() map[string]any {
	d, _ := r.body["data"].(map[string]any)
	return d
}

func (r resp) errCode() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (e *env) do(method, path, token string, body any, headers ...string) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		default:
			buf, _ := json.Marshal(b)
			rd = bytes.NewReader(buf)
		}
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return e.send(req)
}

func (e *env) send(req *http.Request) resp {
	e.t.Helper()
	res, err := e.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	if err != nil {
		e.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, header: res.Header, raw: raw}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (e *env) expect(r resp, status int, what string) resp {
	e.t.Helper()
	if r.status != status {
		e.t.Fatalf("%s: status %d, want %d; body=%s", what, r.status, status, r.raw)
	}
	return r
}

func (e *env) login(user, pass string) (access, refresh string) {
	e.t.Helper()
	r := e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"login": user, "password": pass}), 200, "login "+user)
	return r.data()["access_token"].(string), r.data()["refresh_token"].(string)
}

func (e *env) upload(path, token, purpose, filename string, content []byte) resp {
	e.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("purpose", purpose)
	fw, _ := w.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = w.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	return e.send(req)
}

func TestHealthAndBasics(t *testing.T) {
	e := setup(t)
	e.expect(e.do("GET", "/healthz", "", nil), 200, "healthz")
	r := e.expect(e.do("GET", "/readyz", "", nil), 200, "readyz")
	if r.body["ready"] != true {
		t.Fatalf("readyz: %s", r.raw)
	}
	r = e.expect(e.do("GET", "/api/v1/nope", "", nil), 404, "unknown route")
	if r.errCode() != "NOT_FOUND" || r.header.Get("X-Request-Id") == "" {
		t.Fatalf("404 envelope: %s", r.raw)
	}
	if r.header.Get("X-Frame-Options") != "DENY" || r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers missing: %v", r.header)
	}
	e.expect(e.do("GET", "/api/v1/users", "", nil), 401, "no token")
	e.expect(e.do("GET", "/api/v1/users", "garbage", nil), 401, "bad token")
}

func TestLoginLockoutAndGenericErrors(t *testing.T) {
	e := setup(t)
	unknown := e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"login": "ghost", "password": "x"}), 401, "unknown user")
	wrong := e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"login": "admin", "password": "x"}), 401, "wrong password")
	if unknown.body["error"].(map[string]any)["message"] != wrong.body["error"].(map[string]any)["message"] {
		t.Fatal("login errors must not reveal whether the user exists")
	}
	for i := 0; i < 4; i++ {
		e.do("POST", "/api/v1/auth/login", "", map[string]string{"login": "admin", "password": "x"})
	}
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"login": "admin", "password": adminPassword}), 401, "locked account")
	var n int
	e.ship()
	_ = e.logPool.QueryRow(context.Background(), "SELECT count(*) FROM activity_log WHERE log_action LIKE 'auth.login%' AND source = 'security'").Scan(&n)
	if n < 7 {
		t.Fatalf("expected login attempts to be audited, got %d", n)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	e := setup(t)
	access, refresh := e.login("admin", adminPassword)
	me := e.expect(e.do("GET", "/api/v1/me", access, nil), 200, "me")
	if perms := me.data()["permissions"].([]any); me.data()["is_admin"] != true || len(perms) != len(Catalog()) {
		t.Fatalf("admin must receive the whole catalog: is_admin=%v, %d of %d", me.data()["is_admin"], len(perms), len(Catalog()))
	}

	r := e.expect(e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh}), 200, "refresh")
	refresh2 := r.data()["refresh_token"].(string)
	if refresh2 == refresh {
		t.Fatal("refresh token not rotated")
	}
	// Reusing the old token revokes the whole family, including refresh2.
	e.expect(e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh}), 401, "reuse old token")
	e.expect(e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh2}), 401, "family revoked")

	_, refresh3 := e.login("admin", adminPassword)
	e.expect(e.do("POST", "/api/v1/auth/logout", "", map[string]string{"refresh_token": refresh3}), 204, "logout")
	e.expect(e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh3}), 401, "after logout")
}

func TestRequestValidation(t *testing.T) {
	e := setup(t)
	access, _ := e.login("admin", adminPassword)

	r := e.expect(e.do("POST", "/api/v1/roles", access, `{"role_code":"X","role_name":"X","status":"active","is_admin":true}`), 400, "unknown field")
	if !strings.Contains(r.raw2(), "is_admin") {
		t.Fatalf("unknown field not reported: %s", r.raw)
	}
	e.expect(e.do("POST", "/api/v1/roles", access, `{"role_code":"X","role_name":"X","status":"active"} {}`), 400, "trailing json")
	req := httptest.NewRequest("POST", "/api/v1/roles", strings.NewReader(`role_code=X`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+access)
	e.expect(e.send(req), 415, "wrong content type")

	r = e.expect(e.do("POST", "/api/v1/roles", access, map[string]string{"role_code": "bad code!", "role_name": "", "status": "maybe"}), 422, "invalid fields")
	if details := r.body["error"].(map[string]any)["details"].([]any); len(details) != 3 {
		t.Fatalf("expected 3 field errors, got %s", r.raw)
	}
	e.expect(e.do("GET", "/api/v1/roles?sort=password", access, nil), 422, "sort not whitelisted")
	e.expect(e.do("GET", "/api/v1/roles?id=1", access, nil), 422, "unknown query param")
	e.expect(e.do("GET", "/api/v1/roles?per_page=1000", access, nil), 422, "per_page too large")
	e.expect(e.do("GET", "/api/v1/roles/not-a-uuid", access, nil), 404, "malformed uuid")

	// LIKE wildcards in q are escaped: "%" must not match everything.
	r = e.expect(e.do("GET", "/api/v1/roles?q=%25", access, nil), 200, "escaped like")
	if total := r.body["meta"].(map[string]any)["total"]; total != nil && total.(float64) != 0 {
		t.Fatalf("q=%% matched %v rows", total)
	}
}

func (r resp) raw2() string { return string(r.raw) }

func TestRBACScopeAndOptimisticLocking(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)

	// Role with read-only user permission.
	role := e.expect(e.do("POST", "/api/v1/roles", admin, map[string]string{"role_code": "OPS", "role_name": "Operasi Harian", "status": "active"}), 201, "create role").data()
	roleUUID := role["uuid"].(string)
	getRole := e.expect(e.do("GET", "/api/v1/roles/"+roleUUID, admin, nil), 200, "get role")
	etag := getRole.header.Get("ETag")

	actions := e.expect(e.do("GET", "/api/v1/actions?q=app_user&per_page=100", admin, nil), 200, "list actions")
	var readUUID string
	for _, a := range actions.body["data"].([]any) {
		if a.(map[string]any)["action_code"] == "app_user:read" {
			readUUID = a.(map[string]any)["uuid"].(string)
		}
	}
	body := map[string]any{"action_uuids": []string{readUUID}}
	e.expect(e.do("PUT", "/api/v1/roles/"+roleUUID+"/actions", admin, body), 428, "missing If-Match")
	e.expect(e.do("PUT", "/api/v1/roles/"+roleUUID+"/actions", admin, body, "If-Match", `"abc"`), 412, "stale If-Match")
	// Unknown action UUID: whole replacement is rejected and nothing changes.
	bad := map[string]any{"action_uuids": []string{readUUID, uuid.NewString()}}
	e.expect(e.do("PUT", "/api/v1/roles/"+roleUUID+"/actions", admin, bad, "If-Match", etag), 422, "unknown action")
	r := e.expect(e.do("PUT", "/api/v1/roles/"+roleUUID+"/actions", admin, body, "If-Match", etag), 200, "replace actions")
	if n := len(r.data()["actions"].([]any)); n != 1 {
		t.Fatalf("role actions = %d", n)
	}

	// Create a user with that role and activate it.
	created := e.expect(e.do("POST", "/api/v1/users", admin, map[string]any{"username": "Ops.User", "email": "OPS@example.test",
		"role_uuid": roleUUID, "status": "active"}), 201, "create user").data()
	if created["user"].(map[string]any)["username"] != "ops.user" {
		t.Fatalf("username not normalised: %v", created["user"])
	}
	key := created["activation_key"].(string)
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": key, "new_password": "alllowercaseonly"}), 422, "weak password")
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": key, "new_password": "Ops.user-Strong1"}), 422, "password contains username")
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": key, "new_password": "Str0ng-Passw0rd!"}), 204, "activate")
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": key, "new_password": "Str0ng-Passw0rd!"}), 422, "activation key is single use")

	ops, _ := e.login("ops.user", "Str0ng-Passw0rd!")
	e.expect(e.do("GET", "/api/v1/users", ops, nil), 200, "granted read")
	e.expect(e.do("POST", "/api/v1/roles", ops, map[string]string{"role_code": "Y", "role_name": "Y", "status": "active"}), 403, "no create permission")
	e.expect(e.do("GET", "/api/v1/activity-logs", ops, nil), 403, "no log permission")

	// Deleting the user cuts off the existing access token immediately.
	opsUUID := created["user"].(map[string]any)["uuid"].(string)
	uEtag := e.expect(e.do("GET", "/api/v1/users/"+opsUUID, admin, nil), 200, "get user").header.Get("ETag")
	e.expect(e.do("DELETE", "/api/v1/users/"+opsUUID, admin, nil, "If-Match", uEtag), 204, "delete user")
	e.expect(e.do("GET", "/api/v1/users", ops, nil), 401, "deleted user token")

	// Role still referenced? No: user deleted, so role delete succeeds.
	rEtag := e.expect(e.do("GET", "/api/v1/roles/"+roleUUID, admin, nil), 200, "get role").header.Get("ETag")
	e.expect(e.do("DELETE", "/api/v1/roles/"+roleUUID, admin, nil, "If-Match", rEtag), 204, "delete role")

	// ADMIN role is protected.
	var adminRoleUUID string
	_ = e.secPool.QueryRow(context.Background(), "SELECT uuid::text FROM role WHERE role_code='ADMIN'").Scan(&adminRoleUUID)
	aEtag := e.expect(e.do("GET", "/api/v1/roles/"+adminRoleUUID, admin, nil), 200, "get admin role").header.Get("ETag")
	e.expect(e.do("DELETE", "/api/v1/roles/"+adminRoleUUID, admin, nil, "If-Match", aEtag), 409, "delete ADMIN role")

	e.ship()
	logs := e.expect(e.do("GET", "/api/v1/activity-logs?log_action=app_user.delete", admin, nil), 200, "activity logs")
	if logs.body["meta"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("delete not audited: %s", logs.raw)
	}
}

func TestPrivilegeEscalationBlocked(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	role := e.expect(e.do("POST", "/api/v1/roles", admin, map[string]string{"role_code": "HR", "role_name": "HR", "status": "active"}), 201, "role").data()
	etag := e.expect(e.do("GET", "/api/v1/roles/"+role["uuid"].(string), admin, nil), 200, "get").header.Get("ETag")
	all := e.expect(e.do("GET", "/api/v1/actions?q=app_user&per_page=100", admin, nil), 200, "actions")
	var ids []string
	for _, a := range all.body["data"].([]any) {
		ids = append(ids, a.(map[string]any)["uuid"].(string))
	}
	e.expect(e.do("PUT", "/api/v1/roles/"+role["uuid"].(string)+"/actions", admin, map[string]any{"action_uuids": ids}, "If-Match", etag), 200, "grant")
	u := e.expect(e.do("POST", "/api/v1/users", admin, map[string]any{"username": "hrd", "email": "hr@example.test",
		"role_uuid": role["uuid"], "status": "active"}), 201, "user").data()
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": u["activation_key"].(string), "new_password": "Hr-Passw0rd-123"}), 204, "activate")
	hr, _ := e.login("hrd", "Hr-Passw0rd-123")

	var adminRoleUUID, adminUserUUID string
	_ = e.secPool.QueryRow(context.Background(), "SELECT uuid::text FROM role WHERE role_code='ADMIN'").Scan(&adminRoleUUID)
	_ = e.secPool.QueryRow(context.Background(), "SELECT uuid::text FROM app_user WHERE username='admin'").Scan(&adminUserUUID)

	e.expect(e.do("POST", "/api/v1/users", hr, map[string]any{"username": "evil", "email": "evil@example.test",
		"role_uuid": adminRoleUUID, "status": "active"}), 403, "assign ADMIN role")
	adminEtag := e.expect(e.do("GET", "/api/v1/users/"+adminUserUUID, hr, nil), 200, "read admin").header.Get("ETag")
	e.expect(e.do("POST", "/api/v1/users/"+adminUserUUID+"/reset-password", hr, nil, "If-Match", adminEtag), 403, "reset ADMIN password")
	e.expect(e.do("DELETE", "/api/v1/users/"+adminUserUUID, hr, nil, "If-Match", adminEtag), 403, "delete ADMIN")
}

func pngFile(t *testing.T) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	return buf.Bytes()
}

func TestFileUploadAndAccess(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)

	img := e.expect(e.upload("/api/v1/files/images", admin, "configuration.qr_logo", "../../logo.png", pngFile(t)), 201, "upload image").data()
	if !strings.HasSuffix(img["created_at"].(string), "Z") {
		t.Fatalf("timestamps must be UTC: %v", img["created_at"])
	}
	if img["original_name"] != "logo.png" || img["status"] != "pending" || img["mime_type"] != "image/png" {
		t.Fatalf("image metadata: %v", img)
	}
	content := e.expect(e.do("GET", "/api/v1/files/"+img["uuid"].(string)+"/content", admin, nil), 200, "image content")
	if content.header.Get("X-Content-Type-Options") != "nosniff" || content.header.Get("Content-Type") != "image/png" {
		t.Fatalf("content headers: %v", content.header)
	}
	e.expect(e.do("GET", "/api/v1/files/"+img["uuid"].(string)+"/download", admin, nil), 404, "image has no download")

	pdf := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF")
	doc := e.expect(e.upload("/api/v1/files/documents", admin, "member_document.document", "kontrak.pdf", pdf), 201, "upload pdf").data()
	if len(e.docs.objs) != 1 {
		t.Fatal("document not stored in S3")
	}
	dl := e.expect(e.do("GET", "/api/v1/files/"+doc["uuid"].(string)+"/download", admin, nil), 302, "download")
	if !strings.HasPrefix(dl.header.Get("Location"), "https://s3.example.test/test-bucket/documents/member_document-document/") {
		t.Fatalf("presigned location: %s", dl.header.Get("Location"))
	}

	evil := []byte("%PDF-1.7\n1 0 obj << /OpenAction << /S /JavaScript /JS (app.alert(1)) >> >> endobj")
	e.expect(e.upload("/api/v1/files/documents", admin, "member_document.document", "x.pdf", evil), 422, "pdf with javascript")
	e.expect(e.upload("/api/v1/files/documents", admin, "member_document.document", "x.pdf", pngFile(t)), 415, "png as document")
	e.expect(e.upload("/api/v1/files/images", admin, "member_document.document", "x.png", pngFile(t)), 422, "purpose of other category")
	e.expect(e.upload("/api/v1/files/images", admin, "configuration.qr_logo", "x.png", []byte("<svg onload=alert(1)>")), 415, "svg/html as image")

	// Pending upload can be deleted; storage object is removed too.
	e.expect(e.do("DELETE", "/api/v1/files/"+doc["uuid"].(string), admin, nil), 204, "delete pending")
	if len(e.docs.objs) != 0 {
		t.Fatal("document object not removed from S3")
	}
	e.expect(e.do("GET", "/api/v1/files/"+doc["uuid"].(string), admin, nil), 404, "deleted file")
}
