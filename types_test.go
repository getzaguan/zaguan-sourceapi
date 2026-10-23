package sourceapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEntityCommandsKeepTheExplicitEmptyList checks the contract the hub
// uses to distinguish "the source did not declare commands" from "the entity
// is read-only".
func TestEntityCommandsKeepTheExplicitEmptyList(t *testing.T) {
	entity := Entity{UniqueID: "e1", DeviceUniqueID: "d1", Domain: "light", Commands: []string{}}
	b, err := json.Marshal(entity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"commands":[]`) {
		t.Fatalf("an explicit empty list was omitted: %s", b)
	}
	var decoded Entity
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Commands == nil {
		t.Fatal("the empty list did not survive the round trip")
	}
}
