package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestBuildSequenceBody(t *testing.T) {
	m := &sequenceModel{
		Name:       types.StringValue("welcome"),
		Namespace:  types.StringValue("default"),
		Version:    types.Int64Value(2),
		Definition: jsontypes.NewNormalizedValue(`{"name":"welcome","blocks":[{"type":"step","id":"a","handler":"noop"}]}`),
	}
	body, diags := buildSequenceBody(m, "acme", "id-1", "2026-01-01T00:00:00Z")
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	want := map[string]any{"id": "id-1", "tenant_id": "acme", "namespace": "default", "name": "welcome",
		"version": int64(2), "created_at": "2026-01-01T00:00:00Z"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%s] = %#v, want %#v", k, body[k], v)
		}
	}
	if _, ok := body["blocks"]; !ok {
		t.Error("blocks dropped")
	}
}

func TestBuildSequenceBodyRejectsConflicts(t *testing.T) {
	m := &sequenceModel{
		Name: types.StringValue("welcome"), Namespace: types.StringValue("default"), Version: types.Int64Value(1),
		Definition: jsontypes.NewNormalizedValue(`{"name":"other","blocks":[]}`),
	}
	if _, diags := buildSequenceBody(m, "acme", "id", "ts"); !diags.HasError() {
		t.Fatal("expected conflict error")
	}
	m.Definition = jsontypes.NewNormalizedValue(`{"steps":[]}`)
	if _, diags := buildSequenceBody(m, "acme", "id", "ts"); !diags.HasError() {
		t.Fatal("expected missing blocks error")
	}
	m.Definition = jsontypes.NewNormalizedValue(`[1,2]`)
	if _, diags := buildSequenceBody(m, "acme", "id", "ts"); !diags.HasError() {
		t.Fatal("expected non-object error")
	}
}

func TestJSONFromServer(t *testing.T) {
	null := jsontypes.NewNormalizedNull()
	if v := jsonFromServer(json.RawMessage(`{}`), null); !v.IsNull() {
		t.Errorf("empty object with null prior should stay null, got %s", v)
	}
	if v := jsonFromServer(json.RawMessage(`null`), null); !v.IsNull() {
		t.Errorf("null should stay null")
	}
	prior := jsontypes.NewNormalizedValue(`{}`)
	if v := jsonFromServer(json.RawMessage(`{}`), prior); v.ValueString() != `{}` {
		t.Errorf("empty prior should be kept, got %s", v)
	}
	if v := jsonFromServer(json.RawMessage(`{"a":1}`), null); v.ValueString() != `{"a":1}` {
		t.Errorf("value should be taken from server, got %s", v)
	}
}

func TestSplitImportID(t *testing.T) {
	parts, err := splitImportID("acme/queue-a", 2)
	if err != nil || parts[0] != "acme" || parts[1] != "queue-a" {
		t.Fatalf("got %v %v", parts, err)
	}
	for _, bad := range []string{"acme", "/q", "acme/"} {
		if _, err := splitImportID(bad, 2); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestSameInstant(t *testing.T) {
	if !sameInstant("2030-01-01T00:00:00Z", "2030-01-01T00:00:00.000000Z") {
		t.Error("precision difference should compare equal")
	}
	if !sameInstant("2030-01-01T02:00:00+02:00", "2030-01-01T00:00:00Z") {
		t.Error("offset difference should compare equal")
	}
	if sameInstant("2030-01-01T00:00:00Z", "2030-01-01T00:00:01Z") {
		t.Error("different instants")
	}
	p := types.StringValue("2030-01-01T00:00:00Z")
	s := "2030-01-01T00:00:00.000Z"
	if keepInstant(p, &s) != p {
		t.Error("keepInstant should keep prior spelling")
	}
	if !keepInstant(p, nil).IsNull() {
		t.Error("nil server value -> null")
	}
}
