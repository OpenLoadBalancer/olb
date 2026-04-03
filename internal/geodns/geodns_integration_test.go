package geodns

import (
	"net/http"
	"net/url"
	"testing"
)

func TestGeoDNS_Route(t *testing.T) {
	g := New(Config{
		Enabled:     true,
		DefaultPool: "default",
		Rules: []GeoRule{
			{ID: "us", Country: "US", Pool: "us-pool", Fallback: "default"},
			{ID: "eu", Country: "EU", Pool: "eu-pool"},
		},
	})

	// Test with US IP
	testURL, _ := url.Parse("/")
	req := &http.Request{
		RemoteAddr: "3.0.0.1:12345",
		URL:        testURL,
		Header:     make(http.Header),
	}

	pool, loc, err := g.Route(req)
	if err != nil {
		t.Errorf("Route() error = %v", err)
	}
	if pool != "us-pool" {
		t.Errorf("expected pool 'us-pool', got '%s'", pool)
	}
	if loc == nil {
		t.Error("expected location to be set")
	}
}

func TestGeoDNS_RouteWithXForwardedFor(t *testing.T) {
	g := New(Config{
		Enabled:     true,
		DefaultPool: "default",
		Rules: []GeoRule{
			{ID: "us", Country: "US", Pool: "us-pool"},
		},
	})

	testURL, _ := url.Parse("/")
	req := &http.Request{
		RemoteAddr: "10.0.0.1:12345",
		URL:        testURL,
		Header:     http.Header{"X-Forwarded-For": []string{"3.0.0.1, 10.0.0.1"}},
	}

	pool, _, err := g.Route(req)
	if err != nil {
		t.Errorf("Route() error = %v", err)
	}
	if pool != "us-pool" {
		t.Errorf("expected pool 'us-pool', got '%s'", pool)
	}
}

func TestGeoDNS_AddGeoData(t *testing.T) {
	g := New(Config{DefaultPool: "default"})

	err := g.AddGeoData("192.168.1.0/24", &Location{
		Country:   "TEST",
		Region:    "TEST-REGION",
		City:      "TestCity",
		Latitude:  10.0,
		Longitude: 20.0,
	})
	if err != nil {
		t.Errorf("AddGeoData() error = %v", err)
	}

	// Test invalid CIDR
	err = g.AddGeoData("invalid", &Location{})
	if err == nil {
		t.Error("AddGeoData() should fail with invalid CIDR")
	}
}

func TestGeoDNS_Middleware(t *testing.T) {
	g := New(Config{
		Enabled:     true,
		DefaultPool: "default",
	})

	// Create a test handler
	handler := g.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if geo headers were added
		if r.Header.Get("X-Geo-Country") == "" {
			t.Error("X-Geo-Country header should be set")
		}
	}))

	testURL, _ := url.Parse("/")
	req := &http.Request{
		RemoteAddr: "127.0.0.1:12345",
		URL:        testURL,
		Header:     make(http.Header),
	}

	// This will call the handler which checks the headers
	handler.ServeHTTP(nil, req)
}

func TestGeoDNS_guessLocationFromIP(t *testing.T) {
	g := New(Config{DefaultPool: "default"})

	tests := []struct {
		name        string
		ip          string
		wantCountry string
		wantPrivate bool
	}{
		{"private-10.x", "10.0.0.1", "PRIVATE", true},
		{"private-192.168.x", "192.168.1.1", "PRIVATE", true},
		{"loopback", "127.0.0.1", "PRIVATE", true}, // 127.x is classified as private
		{"public-3.x", "3.0.0.1", "US", false},
		{"public-5.x", "5.0.0.1", "EU", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := g.guessLocationFromIP(tt.ip)
			if loc == nil {
				t.Fatal("guessLocationFromIP returned nil")
			}
			if loc.Country != tt.wantCountry {
				t.Errorf("expected country '%s', got '%s'", tt.wantCountry, loc.Country)
			}
		})
	}
}
