package sourceapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCommandsHasATimeout checks that a hub that accepts the long poll and
// then stalls does not block the provider forever.
func TestCommandsHasATimeout(t *testing.T) {
	old := commandTimeoutMargin
	commandTimeoutMargin = 20 * time.Millisecond
	defer func() { commandTimeoutMargin = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := client.Commands(context.Background(), 10*time.Millisecond); err == nil {
		t.Fatal("the stalled long poll did not time out")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the long poll took %v to time out", elapsed)
	}
}

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
