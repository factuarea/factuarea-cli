package webhook

import (
	"encoding/json"
	"testing"
)

func TestRemapEventStripsResourceFields(t *testing.T) {
	ev := []byte(`{"id":"evt_1","object":"event","type":"invoice.paid","aggregate_id":"agg_1","correlation_id":null,"api_version":null,"livemode":false,"data":{"invoice":{"id":"inv_1"}},"created":1780700258}`)
	body, id, typ, err := RemapEvent(ev)
	if err != nil {
		t.Fatal(err)
	}
	if id != "evt_1" || typ != "invoice.paid" {
		t.Fatalf("id/type: %q %q", id, typ)
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if _, ok := m["object"]; ok {
		t.Error("object no debe estar en el cuerpo webhook")
	}
	if _, ok := m["aggregate_id"]; ok {
		t.Error("aggregate_id no debe estar")
	}
	for _, k := range []string{"id", "type", "api_version", "created", "livemode", "correlation_id", "data"} {
		if _, ok := m[k]; !ok {
			t.Errorf("falta %q en el cuerpo webhook", k)
		}
	}
}
