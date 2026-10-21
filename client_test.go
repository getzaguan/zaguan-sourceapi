package sourceapi

import "testing"

// TestNewAcceptsBareAddresses checks that a person who types an address
// without the scheme does not get a cryptic error.
func TestNewAcceptsBareAddresses(t *testing.T) {
	for _, addr := range []string{
		"192.168.1.50:8081",
		"http://192.168.1.50:8081",
		"127.0.0.1:8081",
		"hub.local",
		"hub.local:8081/",
	} {
		c, err := New(Config{BaseURL: addr, Token: "t"})
		if err != nil {
			t.Errorf("%s: %v", addr, err)
			continue
		}
		if c.base == "" {
			t.Errorf("%s: the address is empty", addr)
		}
	}
}

// TestNewRejectsBadAddresses checks the addresses that cannot work.
func TestNewRejectsBadAddresses(t *testing.T) {
	cases := map[string]Config{
		"no address":   {Token: "t"},
		"no token":     {BaseURL: "http://127.0.0.1:8081"},
		"another way":  {BaseURL: "ftp://127.0.0.1:8081", Token: "t"},
		"only a slash": {BaseURL: "http://", Token: "t"},
	}
	for name, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: the address was accepted", name)
		}
	}
}
