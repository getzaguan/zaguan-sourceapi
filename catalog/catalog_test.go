package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultCatalog(t *testing.T) {
	c := Default()
	for _, domain := range []string{
		DomainSwitch, DomainLight, DomainSensor, DomainBinarySensor,
		DomainEvent, DomainCamera, DomainAlarmControlPanel,
	} {
		if _, ok := c.Lookup(domain); !ok {
			t.Errorf("domain %q is not registered", domain)
		}
	}
	if _, ok := c.Lookup("does_not_exist"); ok {
		t.Error("unknown domain reported as registered")
	}
	if err := c.Register(EntityType{Domain: DomainSwitch}); err == nil {
		t.Error("duplicate registration succeeded")
	}
	if err := c.Register(EntityType{}); err == nil {
		t.Error("empty domain registration succeeded")
	}
	if got := len(c.Domains()); got < 30 {
		t.Errorf("got %d domains, want the common ones", got)
	}
}

// TestCatalogJSON checks the shape the catalog serializes with, because
// api/openapi.yaml declares the EntityType schema in snake_case.
func TestCatalogJSON(t *testing.T) {
	types := Default().Types()
	if len(types) == 0 {
		t.Fatal("the catalog is empty")
	}
	if len(types) < 30 {
		t.Fatalf("the catalog has %d domains, want the common ones", len(types))
	}
	for i := 1; i < len(types); i++ {
		if types[i-1].Domain >= types[i].Domain {
			t.Fatalf("the types are not sorted: %q before %q", types[i-1].Domain, types[i].Domain)
		}
	}
	b, err := json.Marshal(types)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"domain":`,
		`"description":`,
		`"device_classes":`,
		`"capabilities":`,
		`"commands":`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	for _, unwanted := range []string{`"Domain"`, `"Description"`, `"DeviceClasses"`, `"Commands"`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the Go field name %s leaked into JSON: %s", unwanted, got)
		}
	}
}
