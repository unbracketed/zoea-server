package process

import (
	"encoding/json"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

// marshalUIPayload simulates what rpcHandle.SendUIResponse builds.
func marshalUIPayload(resp UIResponse) (map[string]any, error) {
	payload := map[string]any{
		"type": "extension_ui_response",
		"id":   resp.ID,
	}
	if resp.Cancelled {
		payload["cancelled"] = true
	} else if resp.Confirmed != nil {
		payload["confirmed"] = *resp.Confirmed
	} else if resp.Value != nil {
		payload["value"] = resp.Value
	}
	// Round-trip through JSON to verify it's clean.
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(b, &out)
	return out, err
}

func TestUIResponseValue(t *testing.T) {
	m, err := marshalUIPayload(UIResponse{ID: "uuid-1", Value: "Allow"})
	if err != nil {
		t.Fatal(err)
	}
	if m["type"] != "extension_ui_response" {
		t.Fatalf("expected type extension_ui_response, got %v", m["type"])
	}
	if m["id"] != "uuid-1" {
		t.Fatalf("expected id uuid-1, got %v", m["id"])
	}
	if m["value"] != "Allow" {
		t.Fatalf("expected value Allow, got %v", m["value"])
	}
	if _, exists := m["confirmed"]; exists {
		t.Fatal("confirmed should not be present")
	}
	if _, exists := m["cancelled"]; exists {
		t.Fatal("cancelled should not be present")
	}
}

func TestUIResponseConfirmedTrue(t *testing.T) {
	m, err := marshalUIPayload(UIResponse{ID: "uuid-2", Confirmed: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if m["confirmed"] != true {
		t.Fatalf("expected confirmed true, got %v", m["confirmed"])
	}
	if _, exists := m["value"]; exists {
		t.Fatal("value should not be present")
	}
}

func TestUIResponseConfirmedFalse(t *testing.T) {
	m, err := marshalUIPayload(UIResponse{ID: "uuid-3", Confirmed: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if m["confirmed"] != false {
		t.Fatalf("expected confirmed false, got %v", m["confirmed"])
	}
}

func TestUIResponseCancelled(t *testing.T) {
	m, err := marshalUIPayload(UIResponse{ID: "uuid-4", Cancelled: true})
	if err != nil {
		t.Fatal(err)
	}
	if m["cancelled"] != true {
		t.Fatalf("expected cancelled true, got %v", m["cancelled"])
	}
	if _, exists := m["value"]; exists {
		t.Fatal("value should not be present")
	}
	if _, exists := m["confirmed"]; exists {
		t.Fatal("confirmed should not be present")
	}
}

func TestUIResponseCancelledTakesPrecedence(t *testing.T) {
	// If both cancelled and value are set, cancelled wins.
	m, err := marshalUIPayload(UIResponse{ID: "uuid-5", Cancelled: true, Value: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if m["cancelled"] != true {
		t.Fatalf("expected cancelled true, got %v", m["cancelled"])
	}
	if _, exists := m["value"]; exists {
		t.Fatal("value should not be present when cancelled")
	}
}

func TestUIResponseEmptyID(t *testing.T) {
	m, err := marshalUIPayload(UIResponse{ID: "", Value: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if m["id"] != "" {
		t.Fatalf("expected empty id, got %v", m["id"])
	}
}

func TestUIResponseEditorValue(t *testing.T) {
	m, err := marshalUIPayload(UIResponse{ID: "uuid-6", Value: "Line 1\nLine 2\nLine 3"})
	if err != nil {
		t.Fatal(err)
	}
	if m["value"] != "Line 1\nLine 2\nLine 3" {
		t.Fatalf("unexpected value: %v", m["value"])
	}
}
