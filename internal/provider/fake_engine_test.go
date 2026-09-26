package provider

// An in-memory fake of the subset of the Orch8 REST API the provider uses.
// Shapes follow orch8-api/src/{sequences,triggers,cron,api_keys,queue_routing,
// queue_dispatch,rollback,credentials}.rs.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeEngine struct {
	t        *testing.T
	mu       sync.Mutex
	apiKey   string
	seqs     map[string]map[string]any
	triggers map[string]map[string]any
	crons    map[string]map[string]any
	keys     map[string]map[string]any
	rules    map[string]map[string]any
	dispatch map[string]map[string]any // tenant/queue
	secrets  map[string]any            // dispatch secret state (write-only)
	policies map[string]map[string]any // tenant/sequence
	creds    map[string]map[string]any
	creds2   map[string]string // credential values (write-only)
	inst     map[string]map[string]any
	counter  int
	calls    []string
}

func newFakeEngine(t *testing.T) (*fakeEngine, *httptest.Server) {
	f := &fakeEngine{
		t: t, apiKey: "test-key",
		seqs: map[string]map[string]any{}, triggers: map[string]map[string]any{}, crons: map[string]map[string]any{},
		keys: map[string]map[string]any{}, rules: map[string]map[string]any{}, dispatch: map[string]map[string]any{},
		secrets: map[string]any{}, policies: map[string]map[string]any{}, creds: map[string]map[string]any{},
		creds2: map[string]string{}, inst: map[string]map[string]any{},
	}
	mux := http.NewServeMux()
	p := "/api/v1"
	mux.HandleFunc("POST "+p+"/sequences", f.createSequence)
	mux.HandleFunc("GET "+p+"/sequences/by-name", f.sequenceByName)
	mux.HandleFunc("GET "+p+"/sequences/{id}", f.get(f.seqs, "id"))
	mux.HandleFunc("DELETE "+p+"/sequences/{id}", f.del(f.seqs, "id"))

	mux.HandleFunc("POST "+p+"/triggers", f.createTrigger)
	mux.HandleFunc("GET "+p+"/triggers/{slug}", f.get(f.triggers, "slug"))
	mux.HandleFunc("DELETE "+p+"/triggers/{slug}", f.del(f.triggers, "slug"))
	mux.HandleFunc("PATCH "+p+"/triggers/{slug}/target", f.retarget)

	mux.HandleFunc("POST "+p+"/cron", f.createCron)
	mux.HandleFunc("GET "+p+"/cron/{id}", f.get(f.crons, "id"))
	mux.HandleFunc("PUT "+p+"/cron/{id}", f.updateCron)
	mux.HandleFunc("DELETE "+p+"/cron/{id}", f.del(f.crons, "id"))

	mux.HandleFunc("POST "+p+"/api-keys", f.createKey)
	mux.HandleFunc("GET "+p+"/api-keys", f.listKeys)
	mux.HandleFunc("DELETE "+p+"/api-keys/{id}", f.revokeKey)

	mux.HandleFunc("POST "+p+"/routing-rules", f.createRule)
	mux.HandleFunc("GET "+p+"/routing-rules/{id}", f.get(f.rules, "id"))
	mux.HandleFunc("DELETE "+p+"/routing-rules/{id}", f.del(f.rules, "id"))

	mux.HandleFunc("POST "+p+"/queues/dispatch", f.setDispatch)
	mux.HandleFunc("GET "+p+"/queues/dispatch", f.listDispatch)
	mux.HandleFunc("DELETE "+p+"/queues/dispatch/{tenant}/{queue}", f.deleteDispatch)

	mux.HandleFunc("POST "+p+"/rollback-policies", f.upsertPolicy)
	mux.HandleFunc("GET "+p+"/rollback-policies/{name}", f.getPolicy)
	mux.HandleFunc("DELETE "+p+"/rollback-policies/{name}", f.deletePolicy)

	mux.HandleFunc("POST "+p+"/credentials", f.createCred)
	mux.HandleFunc("GET "+p+"/credentials/{id}", f.get(f.creds, "id"))
	mux.HandleFunc("PATCH "+p+"/credentials/{id}", f.patchCred)
	mux.HandleFunc("DELETE "+p+"/credentials/{id}", f.del(f.creds, "id"))

	mux.HandleFunc("GET "+p+"/instances/{id}", f.get(f.inst, "id"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != f.apiKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeEngine) nextID() string {
	f.counter++
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", f.counter)
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request) map[string]any {
	m := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (f *fakeEngine) get(store map[string]map[string]any, key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		v, ok := store[r.PathValue(key)]
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, 200, v)
	}
}

func (f *fakeEngine) del(store map[string]map[string]any, key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := store[r.PathValue(key)]; !ok {
			notFound(w)
			return
		}
		delete(store, r.PathValue(key))
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeEngine) createSequence(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	for _, k := range []string{"id", "tenant_id", "namespace", "name", "version", "blocks", "created_at"} {
		if _, ok := b[k]; !ok {
			writeJSON(w, 400, map[string]string{"error": "missing field " + k})
			return
		}
	}
	id := b["id"].(string)
	if _, ok := b["status"]; !ok {
		b["status"] = "production"
	}
	b["deprecated"] = false
	b["schema_version"] = 1
	f.seqs[id] = b
	writeJSON(w, 201, map[string]any{"id": id, "warnings": []string{"example warning"}})
}

func (f *fakeEngine) sequenceByName(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	var best map[string]any
	for _, s := range f.seqs {
		if s["tenant_id"] != q.Get("tenant_id") || s["namespace"] != q.Get("namespace") || s["name"] != q.Get("name") {
			continue
		}
		v := int(s["version"].(float64))
		if want := q.Get("version"); want != "" {
			if strconv.Itoa(v) == want {
				best = s
			}
			continue
		}
		if best == nil || v > int(best["version"].(float64)) {
			best = s
		}
	}
	if best == nil {
		notFound(w)
		return
	}
	writeJSON(w, 200, best)
}

func (f *fakeEngine) createTrigger(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	slug, _ := b["slug"].(string)
	if _, dup := f.triggers[slug]; dup {
		writeJSON(w, 409, map[string]string{"error": "exists"})
		return
	}
	if b["namespace"] == nil {
		b["namespace"] = "default"
	}
	if b["trigger_type"] == nil {
		b["trigger_type"] = "webhook"
	}
	if b["secret"] != nil {
		b["secret"] = "[REDACTED]"
	}
	if _, ok := b["config"]; !ok {
		b["config"] = nil
	}
	b["enabled"] = true
	b["created_at"], b["updated_at"] = now(), now()
	f.triggers[slug] = b
	writeJSON(w, 201, b)
}

func (f *fakeEngine) retarget(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.triggers[r.PathValue("slug")]
	if !ok {
		notFound(w)
		return
	}
	b := decode(r)
	if b["expected_updated_at"] != t["updated_at"] {
		writeJSON(w, 409, map[string]string{"error": "changed"})
		return
	}
	s, ok := f.seqs[fmt.Sprint(b["sequence_id"])]
	if !ok {
		notFound(w)
		return
	}
	t["sequence_name"], t["namespace"], t["version"] = s["name"], s["namespace"], s["version"]
	t["updated_at"] = now()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeEngine) createCron(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	if _, ok := f.seqs[fmt.Sprint(b["sequence_id"])]; !ok {
		notFound(w)
		return
	}
	id := f.nextID()
	b["id"] = id
	if _, ok := b["metadata"]; !ok {
		b["metadata"] = nil
	}
	b["next_fire_at"] = now()
	b["last_triggered_at"] = nil
	b["skipped_fires"] = 0
	b["created_at"], b["updated_at"] = now(), now()
	f.crons[id] = b
	writeJSON(w, 201, map[string]any{"id": id, "next_fire_at": b["next_fire_at"]})
}

func (f *fakeEngine) updateCron(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.crons[r.PathValue("id")]
	if !ok {
		notFound(w)
		return
	}
	for k, v := range decode(r) {
		c[k] = v
	}
	c["updated_at"] = now()
	writeJSON(w, 200, c)
}

func (f *fakeEngine) createKey(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	caps, _ := b["capabilities"].([]any)
	if len(caps) == 0 {
		caps = []any{"operator"}
	}
	id := "key_" + strconv.Itoa(f.counter+1)
	f.counter++
	k := map[string]any{"id": id, "tenant_id": b["tenant_id"], "name": b["name"], "capabilities": caps,
		"created_at": now(), "expires_at": b["expires_at"], "last_used_at": nil, "revoked": false}
	f.keys[id] = k
	out := map[string]any{"secret": "o8_secret_" + id}
	for kk, v := range k {
		if kk != "revoked" && kk != "last_used_at" {
			out[kk] = v
		}
	}
	writeJSON(w, 201, out)
}

func (f *fakeEngine) listKeys(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []map[string]any{}
	for _, k := range f.keys {
		if k["tenant_id"] == r.URL.Query().Get("tenant_id") {
			out = append(out, k)
		}
	}
	writeJSON(w, 200, out)
}

func (f *fakeEngine) revokeKey(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.keys[r.PathValue("id")]
	if !ok {
		notFound(w)
		return
	}
	k["revoked"] = true
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeEngine) createRule(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	b["id"] = f.nextID()
	if _, ok := b["match_queue"]; !ok {
		b["match_queue"] = nil
	}
	b["created_at"], b["updated_at"] = now(), now()
	f.rules[b["id"].(string)] = b
	writeJSON(w, 201, b)
}

func (f *fakeEngine) setDispatch(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	key := fmt.Sprint(b["tenant_id"], "/", b["queue_name"])
	if s, ok := b["secret"]; ok {
		f.secrets[key] = s // nil means cleared
	}
	delete(b, "secret")
	if b["mode"] == "push" && b["push_url"] == nil {
		writeJSON(w, 400, map[string]string{"error": "push mode requires a push_url"})
		return
	}
	prev, ok := f.dispatch[key]
	b["created_at"] = now()
	if ok {
		b["created_at"] = prev["created_at"]
	}
	b["updated_at"] = now()
	f.dispatch[key] = b
	writeJSON(w, 200, b)
}

func (f *fakeEngine) listDispatch(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []map[string]any{}
	for _, d := range f.dispatch {
		if d["tenant_id"] == r.URL.Query().Get("tenant_id") {
			out = append(out, d)
		}
	}
	writeJSON(w, 200, out)
}

func (f *fakeEngine) deleteDispatch(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.dispatch, r.PathValue("tenant")+"/"+r.PathValue("queue"))
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeEngine) upsertPolicy(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	key := fmt.Sprint(b["tenant_id"], "/", b["sequence_name"])
	if b["cooldown_secs"] == nil {
		b["cooldown_secs"] = 3600
	}
	if b["confirmation_window_secs"] == nil {
		b["confirmation_window_secs"] = 60
	}
	if _, ok := b["webhook_url"]; !ok {
		b["webhook_url"] = nil
	}
	b["enabled"] = true
	if prev, ok := f.policies[key]; ok {
		b["id"], b["created_at"] = prev["id"], prev["created_at"]
	} else {
		f.counter++
		b["id"], b["created_at"] = f.counter, now()
	}
	b["updated_at"] = now()
	f.policies[key] = b
	writeJSON(w, 201, b)
}

func (f *fakeEngine) getPolicy(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.policies[r.URL.Query().Get("tenant_id")+"/"+r.PathValue("name")]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, 200, p)
}

func (f *fakeEngine) deletePolicy(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.policies, r.URL.Query().Get("tenant_id")+"/"+r.PathValue("name"))
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeEngine) createCred(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := decode(r)
	id := fmt.Sprint(b["id"])
	f.creds2[id] = fmt.Sprint(b["value"])
	delete(b, "value")
	b["has_refresh_token"] = b["refresh_token"] != nil
	delete(b, "refresh_token")
	if b["kind"] == nil {
		b["kind"] = "api_key"
	}
	b["enabled"] = true
	b["created_at"], b["updated_at"] = now(), now()
	f.creds[id] = b
	writeJSON(w, 201, b)
}

func (f *fakeEngine) patchCred(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.creds[r.PathValue("id")]
	if !ok {
		notFound(w)
		return
	}
	for k, v := range decode(r) {
		switch k {
		case "value":
			f.creds2[r.PathValue("id")] = fmt.Sprint(v)
		case "refresh_token":
			c["has_refresh_token"] = true
		default:
			c[k] = v
		}
	}
	c["updated_at"] = now()
	writeJSON(w, 200, c)
}
