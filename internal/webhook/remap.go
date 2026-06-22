package webhook

import "encoding/json"

func RemapEvent(eventResource []byte) (body []byte, eventID, eventType string, err error) {
	var ev map[string]json.RawMessage
	if err = json.Unmarshal(eventResource, &ev); err != nil {
		return nil, "", "", err
	}
	_ = json.Unmarshal(ev["id"], &eventID)
	_ = json.Unmarshal(ev["type"], &eventType)
	out := map[string]json.RawMessage{}
	for _, k := range []string{"id", "type", "api_version", "created", "livemode", "correlation_id", "data"} {
		if v, ok := ev[k]; ok {
			out[k] = v
		}
	}
	body, err = json.Marshal(out)
	return body, eventID, eventType, err
}
