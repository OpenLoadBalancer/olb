package waf

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/waf/detection"
	"github.com/openloadbalancer/olb/internal/waf/detection/cmdi"
	"github.com/openloadbalancer/olb/internal/waf/detection/pathtraversal"
	"github.com/openloadbalancer/olb/internal/waf/detection/sqli"
	"github.com/openloadbalancer/olb/internal/waf/detection/ssrf"
	"github.com/openloadbalancer/olb/internal/waf/detection/xss"
	"github.com/openloadbalancer/olb/internal/waf/detection/xxe"
	"github.com/openloadbalancer/olb/internal/waf/ipacl"
	"github.com/openloadbalancer/olb/internal/waf/sanitizer"
)

func BenchmarkIPACLLookup(b *testing.B) {
	acl, _ := ipacl.New(ipacl.Config{
		Whitelist: []ipacl.EntryConfig{{CIDR: "10.0.0.0/8", Reason: "test"}},
		Blacklist: []ipacl.EntryConfig{
			{CIDR: "203.0.113.0/24", Reason: "test"},
			{CIDR: "198.51.100.0/24", Reason: "test"},
		},
	})
	defer acl.Stop()

	b.ResetTimer()
	for b.Loop() {
		acl.Check("192.168.1.1")
	}
}

func BenchmarkSanitizerProcess(b *testing.B) {
	s := sanitizer.New(sanitizer.DefaultConfig())
	req := httptest.NewRequest("GET", "http://example.com/api/users?page=1&sort=name&q=hello+world", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
	req.Header.Set("Accept", "application/json")

	b.ResetTimer()
	for b.Loop() {
		s.Process(req)
	}
}

func BenchmarkSQLiDetector(b *testing.B) {
	d := sqli.New()
	ctx := &detection.RequestContext{
		DecodedQuery: "id=1&name=John+Smith&page=1&sort=created_at&filter=active",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}

	b.ResetTimer()
	for b.Loop() {
		d.Detect(ctx)
	}
}

func BenchmarkSQLiDetector_Attack(b *testing.B) {
	d := sqli.New()
	ctx := &detection.RequestContext{
		DecodedQuery: "id=1' UNION SELECT username, password FROM users --",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}

	b.ResetTimer()
	for b.Loop() {
		d.Detect(ctx)
	}
}

func BenchmarkXSSDetector(b *testing.B) {
	d := xss.New()
	ctx := &detection.RequestContext{
		DecodedQuery: "q=normal+search+query&page=1&lang=en",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}

	b.ResetTimer()
	for b.Loop() {
		d.Detect(ctx)
	}
}

func BenchmarkFullPipeline_CleanRequest(b *testing.B) {
	mw, _ := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	defer mw.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw.Wrap(next)

	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest("GET", "http://example.com/api/users?page=1", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkFullPipeline_WithIPACL(b *testing.B) {
	mw, _ := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			IPACL: &config.WAFIPACLConfig{
				Enabled:   true,
				Whitelist: []config.WAFIPACLEntry{{CIDR: "10.0.0.0/8", Reason: "test"}},
				Blacklist: []config.WAFIPACLEntry{{CIDR: "203.0.113.0/24", Reason: "test"}},
			},
		},
	})
	defer mw.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw.Wrap(next)

	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest("GET", "http://example.com/api/users?page=1", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkFullPipeline_Parallel(b *testing.B) {
	mw, _ := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	defer mw.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw.Wrap(next)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "http://example.com/api/users?page=1", nil)
			req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}
	})
}

func BenchmarkCMDIDetector(b *testing.B) {
	d := cmdi.New()
	ctx := &detection.RequestContext{
		DecodedQuery: "file=test.txt&dir=documents&page=1",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	b.ResetTimer()
	for b.Loop() {
		d.Detect(ctx)
	}
}

func BenchmarkXXEDetector(b *testing.B) {
	d := xxe.New()
	ctx := &detection.RequestContext{
		Body:       []byte(`<root><name>test</name><value>123</value></root>`),
		BodyParams: make(map[string]string),
		Headers:    make(map[string][]string),
		Cookies:    make(map[string]string),
	}
	b.ResetTimer()
	for b.Loop() {
		d.Detect(ctx)
	}
}

func BenchmarkSSRFDetector(b *testing.B) {
	d := ssrf.New()
	ctx := &detection.RequestContext{
		DecodedQuery: "url=https://example.com/api/data",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	b.ResetTimer()
	for b.Loop() {
		d.Detect(ctx)
	}
}

func BenchmarkPathTraversalDetector(b *testing.B) {
	d := pathtraversal.New()
	ctx := &detection.RequestContext{
		Path:         "/api/v1/users/123/profile",
		DecodedQuery: "format=json",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	b.ResetTimer()
	for b.Loop() {
		d.Detect(ctx)
	}
}

func BenchmarkDetectionEngine_All(b *testing.B) {
	eng := detection.NewEngine(detection.Config{})
	eng.Register(sqli.New())
	eng.Register(xss.New())
	eng.Register(cmdi.New())
	eng.Register(xxe.New())
	eng.Register(ssrf.New())
	eng.Register(pathtraversal.New())

	ctx := &detection.RequestContext{
		Path:         "/api/v1/users",
		DecodedQuery: "id=1&name=John+Smith&page=1&sort=created_at",
		DecodedBody:  `{"name":"test","email":"test@example.com"}`,
		BodyParams:   map[string]string{"name": "test", "email": "test@example.com"},
		Headers:      map[string][]string{"Content-Type": {"application/json"}, "User-Agent": {"Mozilla/5.0"}},
		Cookies:      map[string]string{"session": "abc123"},
	}

	b.ResetTimer()
	for b.Loop() {
		eng.Detect(ctx)
	}
}

func BenchmarkDetectionEngine_AllParallel(b *testing.B) {
	eng := detection.NewEngine(detection.Config{})
	eng.Register(sqli.New())
	eng.Register(xss.New())
	eng.Register(cmdi.New())
	eng.Register(xxe.New())
	eng.Register(ssrf.New())
	eng.Register(pathtraversal.New())

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := &detection.RequestContext{
			Path:         "/api/v1/users",
			DecodedQuery: "id=1&name=John+Smith&page=1",
			BodyParams:   make(map[string]string),
			Headers:      map[string][]string{"User-Agent": {"Mozilla/5.0"}},
			Cookies:      make(map[string]string),
		}
		for pb.Next() {
			eng.Detect(ctx)
		}
	})
}

func BenchmarkFullPipeline_POSTWithBody(b *testing.B) {
	mw, _ := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	defer mw.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw.Wrap(next)

	body := strings.NewReader(`{"username":"admin","password":"test123","email":"user@example.com"}`)

	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest("POST", "http://example.com/api/login", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		body.Seek(0, 0)
	}
}
