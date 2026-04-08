package engine

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/middleware/apikey"
	"github.com/openloadbalancer/olb/internal/middleware/basic"
	"github.com/openloadbalancer/olb/internal/middleware/botdetection"
	"github.com/openloadbalancer/olb/internal/middleware/cache"
	"github.com/openloadbalancer/olb/internal/middleware/coalesce"
	"github.com/openloadbalancer/olb/internal/middleware/csp"
	"github.com/openloadbalancer/olb/internal/middleware/csrf"
	"github.com/openloadbalancer/olb/internal/middleware/forcessl"
	"github.com/openloadbalancer/olb/internal/middleware/hmac"
	"github.com/openloadbalancer/olb/internal/middleware/jwt"
	mwlogging "github.com/openloadbalancer/olb/internal/middleware/logging"
	mwmetrics "github.com/openloadbalancer/olb/internal/middleware/metrics"
	"github.com/openloadbalancer/olb/internal/middleware/oauth2"
	"github.com/openloadbalancer/olb/internal/middleware/realip"
	"github.com/openloadbalancer/olb/internal/middleware/requestid"
	"github.com/openloadbalancer/olb/internal/middleware/rewrite"
	"github.com/openloadbalancer/olb/internal/middleware/secureheaders"
	"github.com/openloadbalancer/olb/internal/middleware/trace"
	"github.com/openloadbalancer/olb/internal/middleware/transformer"
	"github.com/openloadbalancer/olb/internal/middleware/validator"
	"github.com/openloadbalancer/olb/internal/waf"
)

// createMiddlewareChain creates the middleware chain based on configuration.
func createMiddlewareChain(cfg *config.Config, logger *logging.Logger, registry *metrics.Registry) *middleware.Chain {
	chain := middleware.NewChain()

	// Panic Recovery (priority 1) — MUST be first to catch panics from all downstream middleware
	chain.Use(middleware.NewRecoveryMiddleware(middleware.RecoveryConfig{
		LogFunc: func(panicVal any, stack string) {
			logger.Error("panic recovered",
				logging.String("panic", fmt.Sprintf("%v", panicVal)),
				logging.String("stack", stack),
			)
		},
	}))

	// Distributed Tracing (priority 10) — extract trace context early
	if cfg.Middleware != nil && cfg.Middleware.Trace != nil && cfg.Middleware.Trace.Enabled {
		t := cfg.Middleware.Trace
		chain.Use(trace.New(trace.Config{
			Enabled:         t.Enabled,
			ServiceName:     t.ServiceName,
			ServiceVersion:  t.ServiceVersion,
			Propagators:     t.Propagators,
			SampleRate:      t.SampleRate,
			BaggageHeaders:  t.BaggageHeaders,
			ExcludePaths:    t.ExcludePaths,
			MaxBaggageItems: t.MaxBaggageItems,
			MaxBaggageSize:  t.MaxBaggageSize,
		}))
	}

	// RealIP (priority 15) — extract real client IP early for all downstream middleware
	if cfg.Middleware != nil && cfg.Middleware.RealIP != nil && cfg.Middleware.RealIP.Enabled {
		r := cfg.Middleware.RealIP
		chain.Use(realip.New(realip.Config{
			Enabled:         r.Enabled,
			Headers:         r.Headers,
			TrustedProxies:  r.TrustedProxies,
			RejectUntrusted: r.RejectUntrusted,
		}))
	}

	// Request Logging (priority 80) — log requests after Recovery
	if cfg.Middleware != nil && cfg.Middleware.Logging != nil && cfg.Middleware.Logging.Enabled {
		l := cfg.Middleware.Logging
		chain.Use(mwlogging.New(mwlogging.Config{
			Enabled:         l.Enabled,
			Format:          l.Format,
			CustomFormat:    l.CustomFormat,
			Fields:          l.Fields,
			ExcludePaths:    l.ExcludePaths,
			ExcludeStatus:   l.ExcludeStatus,
			MinDuration:     l.MinDuration,
			RequestHeaders:  l.RequestHeaders,
			ResponseHeaders: l.ResponseHeaders,
		}))
	}

	// Force SSL (priority 70) — redirect HTTP to HTTPS early
	if cfg.Middleware != nil && cfg.Middleware.ForceSSL != nil && cfg.Middleware.ForceSSL.Enabled {
		f := cfg.Middleware.ForceSSL
		chain.Use(forcessl.New(forcessl.Config{
			Enabled:      f.Enabled,
			Permanent:    f.Permanent,
			ExcludePaths: f.ExcludePaths,
			ExcludeHosts: f.ExcludeHosts,
			Port:         f.Port,
			HeaderKey:    f.HeaderKey,
			HeaderValue:  f.HeaderValue,
		}))
	}

	// Metrics (priority 85) — collect request metrics after Logging
	if cfg.Middleware != nil && cfg.Middleware.Metrics != nil && cfg.Middleware.Metrics.Enabled {
		m := cfg.Middleware.Metrics
		chain.Use(mwmetrics.New(mwmetrics.Config{
			Enabled:        m.Enabled,
			Namespace:      m.Namespace,
			Subsystem:      m.Subsystem,
			ExcludePaths:   m.ExcludePaths,
			ExcludeMethods: m.ExcludeMethods,
			EnableLatency:  m.EnableLatency,
			EnableSize:     m.EnableSize,
			EnableActive:   m.EnableActive,
			LatencyBuckets: m.LatencyBuckets,
		}))
	}

	// Always register the registry-aware metrics middleware so the Prometheus
	// /metrics endpoint (served by the admin server) has data. This is separate
	// from the config-gated v2 metrics middleware above.
	chain.Use(middleware.NewMetricsMiddleware(registry))

	// Request ID (priority 90) — generate request ID early for tracing
	if cfg.Middleware != nil && cfg.Middleware.RequestID != nil && cfg.Middleware.RequestID.Enabled {
		r := cfg.Middleware.RequestID
		chain.Use(requestid.New(requestid.Config{
			Enabled:      r.Enabled,
			Header:       r.Header,
			Generate:     r.Generate,
			Length:       r.Length,
			Response:     r.Response,
			ExcludePaths: r.ExcludePaths,
		}))
	}

	// IP Filter (priority 100)
	if cfg.Middleware != nil && cfg.Middleware.IPFilter != nil && cfg.Middleware.IPFilter.Enabled {
		ipCfg := cfg.Middleware.IPFilter
		ipFilter, err := middleware.NewIPFilterMiddleware(middleware.IPFilterConfig{
			AllowList: ipCfg.AllowList, DenyList: ipCfg.DenyList, DefaultAction: ipCfg.DefaultAction,
		})
		if err == nil {
			chain.Use(ipFilter)
		}
	}

	// Bot Detection (priority 95) — detect bots early, after IP filter
	if cfg.Middleware != nil && cfg.Middleware.BotDetection != nil && cfg.Middleware.BotDetection.Enabled {
		bd := cfg.Middleware.BotDetection

		// Convert user agent rules
		uaRules := make([]botdetection.UserAgentRule, len(bd.UserAgentRules))
		for i, rule := range bd.UserAgentRules {
			uaRules[i] = botdetection.UserAgentRule{
				Pattern: rule.Pattern,
				Action:  botdetection.Action(rule.Action),
				Name:    rule.Name,
			}
		}

		// Convert header rules
		headerRules := make([]botdetection.HeaderRule, len(bd.CustomHeaders))
		for i, rule := range bd.CustomHeaders {
			headerRules[i] = botdetection.HeaderRule{
				Header:  rule.Header,
				Pattern: rule.Pattern,
				Action:  botdetection.Action(rule.Action),
				Name:    rule.Name,
			}
		}

		chain.Use(botdetection.New(botdetection.Config{
			Enabled:              bd.Enabled,
			Action:               botdetection.Action(bd.Action),
			BlockKnownBots:       bd.BlockKnownBots,
			AllowVerified:        bd.AllowVerified,
			RequestRateThreshold: bd.RequestRateThreshold,
			JA3Fingerprints:      bd.JA3Fingerprints,
			ChallengePath:        bd.ChallengePath,
			ExcludePaths:         bd.ExcludePaths,
			UserAgentRules:       uaRules,
			CustomHeaders:        headerRules,
		}))
	}

	// Max Body Size (priority 50) — MUST run before WAF to reject oversized
	// payloads before they enter the detection pipeline (prevents DoS).
	if cfg.Middleware != nil && cfg.Middleware.MaxBodySize != nil && cfg.Middleware.MaxBodySize.Enabled {
		maxSize := cfg.Middleware.MaxBodySize.MaxSize
		if maxSize <= 0 {
			maxSize = 10 * 1024 * 1024 // 10 MB default
		}
		chain.Use(middleware.NewBodyLimitMiddleware(middleware.BodyLimitConfig{MaxSize: maxSize}))
	}

	// HTTP Cache (priority 55) — cache responses after body size check
	if cfg.Middleware != nil && cfg.Middleware.Cache != nil && cfg.Middleware.Cache.Enabled {
		c := cfg.Middleware.Cache

		var ttl time.Duration
		if c.DefaultTTL != "" {
			ttl, _ = time.ParseDuration(c.DefaultTTL)
		}
		if ttl == 0 {
			ttl = 5 * time.Minute
		}

		methods := c.Methods
		if len(methods) == 0 {
			methods = []string{"GET", "HEAD"}
		}

		statusCodes := c.StatusCodes
		if len(statusCodes) == 0 {
			statusCodes = []int{200, 203, 204, 206, 300, 301, 404, 405, 410, 414, 501}
		}

		chain.Use(cache.New(cache.Config{
			Enabled:      c.Enabled,
			TTL:          ttl,
			MaxSize:      c.MaxSize,
			MaxEntries:   c.MaxEntries,
			Methods:      methods,
			StatusCodes:  statusCodes,
			VaryHeaders:  c.VaryHeaders,
			ExcludePaths: c.ExcludePaths,
			CachePrivate: c.CachePrivate,
			CacheCookies: c.CacheCookies,
		}))
	}

	// WAF (priority 100) — 6-layer security pipeline
	if cfg.WAF != nil && cfg.WAF.Enabled {
		wafMW, err := waf.NewWAFMiddleware(waf.WAFMiddlewareConfig{
			Config:          cfg.WAF,
			MetricsRegistry: registry,
		})
		if err == nil {
			chain.Use(wafMW)
		}
	}

	// Rewrite (priority 135) — rewrite URLs before stripping prefix
	if cfg.Middleware != nil && cfg.Middleware.Rewrite != nil && cfg.Middleware.Rewrite.Enabled {
		r := cfg.Middleware.Rewrite
		rules := make([]rewrite.Rule, len(r.Rules))
		for i, rule := range r.Rules {
			rules[i] = rewrite.Rule{
				Pattern:     rule.Pattern,
				Replacement: rule.Replacement,
				Flag:        rule.Flag,
			}
		}
		rewriteMW, err := rewrite.New(rewrite.Config{
			Enabled:      r.Enabled,
			Rules:        rules,
			ExcludePaths: r.ExcludePaths,
		})
		if err == nil {
			chain.Use(rewriteMW)
		}
	}

	// Strip Prefix (priority 140) — strip path prefix early for routing
	if cfg.Middleware != nil && cfg.Middleware.StripPrefix != nil && cfg.Middleware.StripPrefix.Enabled {
		sp := cfg.Middleware.StripPrefix
		chain.Use(middleware.NewStripPrefixMiddleware(middleware.StripPrefixConfig{
			Prefix:        sp.Prefix,
			RedirectSlash: sp.RedirectSlash,
		}))
	}

	// Validator (priority 145) — validate requests before auth
	if cfg.Middleware != nil && cfg.Middleware.Validator != nil && cfg.Middleware.Validator.Enabled {
		v := cfg.Middleware.Validator
		validatorMW, err := validator.New(validator.Config{
			Enabled:          v.Enabled,
			ValidateRequest:  v.ValidateRequest,
			ValidateResponse: v.ValidateResponse,
			MaxBodySize:      v.MaxBodySize,
			ContentTypes:     v.ContentTypes,
			RequiredHeaders:  v.RequiredHeaders,
			ForbiddenHeaders: v.ForbiddenHeaders,
			QueryRules:       v.QueryRules,
			PathPatterns:     v.PathPatterns,
			ExcludePaths:     v.ExcludePaths,
			RejectOnFailure:  v.RejectOnFailure,
			LogOnly:          v.LogOnly,
		})
		if err == nil {
			chain.Use(validatorMW)
		}
	}

	// Request Coalescing (priority 160) — combine identical concurrent requests
	if cfg.Middleware != nil && cfg.Middleware.Coalesce != nil && cfg.Middleware.Coalesce.Enabled {
		c := cfg.Middleware.Coalesce
		var ttl time.Duration
		if c.TTL != "" {
			ttl, _ = time.ParseDuration(c.TTL)
		}
		if ttl == 0 {
			ttl = 100 * time.Millisecond
		}
		chain.Use(coalesce.New(coalesce.Config{
			Enabled:      c.Enabled,
			TTL:          ttl,
			MaxRequests:  c.MaxRequests,
			ExcludePaths: c.ExcludePaths,
		}))
	}

	// JWT Authentication (priority 210) — run after WAF, before Real IP
	if cfg.Middleware != nil && cfg.Middleware.JWT != nil && cfg.Middleware.JWT.Enabled {
		j := cfg.Middleware.JWT

		// Load public key from file if specified and algorithm is EdDSA
		var publicKey []byte
		if j.Algorithm == "EdDSA" && j.PublicKey != "" {
			// Check if it's a file path
			if _, err := os.Stat(j.PublicKey); err == nil {
				publicKey, err = os.ReadFile(j.PublicKey)
				if err != nil {
					logger.Warn("Failed to load JWT public key from file",
						logging.String("file", j.PublicKey),
						logging.String("error", err.Error()),
					)
				}
			} else {
				// Assume it's base64 encoded
				publicKey, err = base64.StdEncoding.DecodeString(j.PublicKey)
				if err != nil {
					logger.Warn("Failed to decode JWT public key as base64",
						logging.String("error", err.Error()),
					)
				}
			}
		}

		jwtMW, err := jwt.New(jwt.Config{
			Enabled:      j.Enabled,
			Secret:       j.Secret,
			PublicKey:    publicKey,
			Algorithm:    j.Algorithm,
			Header:       j.Header,
			Prefix:       j.Prefix,
			Required:     j.Required,
			ExcludePaths: j.ExcludePaths,
			ClaimsValidation: jwt.ClaimsValidation{
				Issuer:   j.ClaimsValidation.Issuer,
				Audience: j.ClaimsValidation.Audience,
			},
		})
		if err == nil {
			chain.Use(jwtMW)
		}
	}

	// OAuth2/OIDC (priority 212) — run after JWT, before API Key
	if cfg.Middleware != nil && cfg.Middleware.OAuth2 != nil && cfg.Middleware.OAuth2.Enabled {
		o := cfg.Middleware.OAuth2
		oauth2MW, err := oauth2.New(oauth2.Config{
			Enabled:          o.Enabled,
			IssuerURL:        o.IssuerURL,
			ClientID:         o.ClientID,
			ClientSecret:     o.ClientSecret,
			JwksURL:          o.JwksURL,
			Audience:         o.Audience,
			Scopes:           o.Scopes,
			Header:           o.Header,
			Prefix:           o.Prefix,
			ExcludePaths:     o.ExcludePaths,
			IntrospectionURL: o.IntrospectionURL,
			CacheDuration:    o.CacheDuration,
		})
		if err == nil {
			chain.Use(oauth2MW)
		}
	}

	// HMAC Signature (priority 213) — run after OAuth2, before API Key
	if cfg.Middleware != nil && cfg.Middleware.HMAC != nil && cfg.Middleware.HMAC.Enabled {
		h := cfg.Middleware.HMAC
		hmacMW, err := hmac.New(hmac.Config{
			Enabled:         h.Enabled,
			Secret:          h.Secret,
			Algorithm:       h.Algorithm,
			Header:          h.Header,
			Prefix:          h.Prefix,
			Encoding:        h.Encoding,
			UseBody:         h.UseBody,
			ExcludePaths:    h.ExcludePaths,
			TimestampHeader: h.TimestampHeader,
			MaxAge:          h.MaxAge,
		})
		if err == nil {
			chain.Use(hmacMW)
		}
	}

	// API Key (priority 215) — run after HMAC, before Basic Auth
	if cfg.Middleware != nil && cfg.Middleware.APIKey != nil && cfg.Middleware.APIKey.Enabled {
		a := cfg.Middleware.APIKey
		apiKeyMW, err := apikey.New(apikey.Config{
			Enabled:      a.Enabled,
			Keys:         a.Keys,
			Header:       a.Header,
			QueryParam:   a.QueryParam,
			ExcludePaths: a.ExcludePaths,
			Hash:         a.Hash,
		})
		if err == nil {
			chain.Use(apiKeyMW)
		}
	}

	// Basic Auth (priority 220) — run after JWT, before Real IP
	if cfg.Middleware != nil && cfg.Middleware.BasicAuth != nil && cfg.Middleware.BasicAuth.Enabled {
		b := cfg.Middleware.BasicAuth
		basicMW, err := basic.New(basic.Config{
			Enabled:      b.Enabled,
			Users:        b.Users,
			Realm:        b.Realm,
			ExcludePaths: b.ExcludePaths,
			Hash:         b.Hash,
		})
		if err == nil {
			chain.Use(basicMW)
		}
	}

	// CSRF Protection (priority 200) — run after auth middleware
	if cfg.Middleware != nil && cfg.Middleware.CSRF != nil && cfg.Middleware.CSRF.Enabled {
		c := cfg.Middleware.CSRF
		csrfMW, err := csrf.New(csrf.Config{
			Enabled:        c.Enabled,
			CookieName:     c.CookieName,
			HeaderName:     c.HeaderName,
			FieldName:      c.FieldName,
			ExcludePaths:   c.ExcludePaths,
			ExcludeMethods: c.ExcludeMethods,
			CookiePath:     c.CookiePath,
			CookieDomain:   c.CookieDomain,
			CookieMaxAge:   c.CookieMaxAge,
			CookieSecure:   c.CookieSecure,
			CookieHTTPOnly: c.CookieHTTPOnly,
			TokenLength:    c.TokenLength,
		})
		if err == nil {
			chain.Use(csrfMW)
		}
	}

	// NOTE: Real IP and Request ID v1 middleware removed — v2 versions
	// (realip.New and requestid.New) are registered earlier in the chain
	// with proper config gating.

	// Request Timeout (priority 450) — prevent hung backends from blocking clients
	if cfg.Middleware != nil && cfg.Middleware.Timeout != nil && cfg.Middleware.Timeout.Enabled {
		timeout := parseDuration(cfg.Middleware.Timeout.Timeout, 60*time.Second)
		chain.Use(middleware.NewTimeoutMiddleware(middleware.TimeoutConfig{Timeout: timeout}))
	}

	// Rate Limiter (priority 500)
	if cfg.Middleware != nil && cfg.Middleware.RateLimit != nil && cfg.Middleware.RateLimit.Enabled {
		rl := cfg.Middleware.RateLimit
		rps := rl.RequestsPerSecond
		if rps <= 0 {
			rps = 100
		}
		burst := rl.BurstSize
		if burst <= 0 {
			burst = 200
		}
		if rateLimiter, err := middleware.NewRateLimitMiddleware(middleware.RateLimitConfig{
			RequestsPerSecond: rps, BurstSize: burst,
		}); err == nil {
			chain.Use(rateLimiter)
		}
	}

	// Circuit Breaker (priority 550)
	if cfg.Middleware != nil && cfg.Middleware.CircuitBreaker != nil && cfg.Middleware.CircuitBreaker.Enabled {
		cbCfg := middleware.DefaultCircuitBreakerConfig()
		if cfg.Middleware.CircuitBreaker.ErrorThreshold > 0 {
			cbCfg.ErrorThreshold = cfg.Middleware.CircuitBreaker.ErrorThreshold
		}
		chain.Use(middleware.NewCircuitBreaker(cbCfg))
	}

	// CORS (priority 600)
	if cfg.Middleware != nil && cfg.Middleware.CORS != nil && cfg.Middleware.CORS.Enabled {
		c := cfg.Middleware.CORS
		origins := c.AllowedOrigins
		if len(origins) == 0 {
			origins = []string{"*"}
		}
		methods := c.AllowedMethods
		if len(methods) == 0 {
			methods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
		}
		chain.Use(middleware.NewCORSMiddleware(middleware.CORSConfig{
			AllowedOrigins: origins, AllowedMethods: methods, AllowedHeaders: c.AllowedHeaders,
			AllowCredentials: c.AllowCredentials, MaxAge: time.Duration(c.MaxAge) * time.Second,
		}))
	}

	// CSP (priority 610) - Content Security Policy headers
	if cfg.Middleware != nil && cfg.Middleware.CSP != nil && cfg.Middleware.CSP.Enabled {
		c := cfg.Middleware.CSP
		cspMW, err := csp.New(csp.Config{
			Enabled:         c.Enabled,
			DefaultSrc:      c.DefaultSrc,
			ScriptSrc:       c.ScriptSrc,
			StyleSrc:        c.StyleSrc,
			ImgSrc:          c.ImgSrc,
			ConnectSrc:      c.ConnectSrc,
			FontSrc:         c.FontSrc,
			ObjectSrc:       c.ObjectSrc,
			MediaSrc:        c.MediaSrc,
			FrameSrc:        c.FrameSrc,
			FrameAncestors:  c.FrameAncestors,
			FormAction:      c.FormAction,
			BaseURI:         c.BaseURI,
			UpgradeInsecure: c.UpgradeInsecure,
			BlockAllMixed:   c.BlockAllMixed,
			ReportURI:       c.ReportURI,
			ReportTo:        c.ReportTo,
			NonceScript:     c.NonceScript,
			NonceStyle:      c.NonceStyle,
			UnsafeInline:    c.UnsafeInline,
			UnsafeEval:      c.UnsafeEval,
			ExcludePaths:    c.ExcludePaths,
		})
		if err == nil {
			chain.Use(cspMW)
		}
	}

	// Headers (priority 700)
	if cfg.Middleware != nil && cfg.Middleware.Headers != nil && cfg.Middleware.Headers.Enabled {
		h := cfg.Middleware.Headers
		chain.Use(middleware.NewHeadersMiddleware(middleware.HeadersConfig{
			RequestAdd: h.RequestAdd, ResponseAdd: h.ResponseAdd,
		}))
	}

	// Compression (priority 800)
	if cfg.Middleware != nil && cfg.Middleware.Compression != nil && cfg.Middleware.Compression.Enabled {
		if comp, err := middleware.NewCompressionMiddleware(middleware.CompressionConfig{
			MinSize: cfg.Middleware.Compression.MinSize,
			Level:   cfg.Middleware.Compression.Level,
		}); err == nil {
			chain.Use(comp)
		}
	}

	// Response Transformer (priority 850) — transform responses before metrics/logging
	if cfg.Middleware != nil && cfg.Middleware.Transformer != nil && cfg.Middleware.Transformer.Enabled {
		t := cfg.Middleware.Transformer
		transformerMW, err := transformer.New(transformer.Config{
			Enabled:          t.Enabled,
			Compress:         t.Compress,
			CompressLevel:    t.CompressLevel,
			MinCompressSize:  t.MinCompressSize,
			AddHeaders:       t.AddHeaders,
			RemoveHeaders:    t.RemoveHeaders,
			RewriteBody:      t.RewriteBody,
			ExcludePaths:     t.ExcludePaths,
			ExcludeMIMETypes: t.ExcludeMIMETypes,
		})
		if err == nil {
			chain.Use(transformerMW)
		}
	}

	// Retry (priority 750)
	if cfg.Middleware != nil && cfg.Middleware.Retry != nil && cfg.Middleware.Retry.Enabled {
		retryCfg := middleware.DefaultRetryConfig()
		if cfg.Middleware.Retry.MaxRetries > 0 {
			retryCfg.MaxRetries = cfg.Middleware.Retry.MaxRetries
		}
		chain.Use(middleware.NewRetryMiddleware(retryCfg))
	}

	// NOTE: Cache v1 middleware removed — v2 version (cache.New) is registered
	// earlier in the chain with full config support.

	// Secure Headers (priority 750) — add security headers
	if cfg.Middleware != nil && cfg.Middleware.SecureHeaders != nil && cfg.Middleware.SecureHeaders.Enabled {
		s := cfg.Middleware.SecureHeaders
		var hsts *secureheaders.HSTSConfig
		if s.StrictTransportPolicy != nil {
			hsts = &secureheaders.HSTSConfig{
				MaxAge:            s.StrictTransportPolicy.MaxAge,
				IncludeSubdomains: s.StrictTransportPolicy.IncludeSubdomains,
				Preload:           s.StrictTransportPolicy.Preload,
			}
		}
		chain.Use(secureheaders.New(secureheaders.Config{
			Enabled:                       s.Enabled,
			XFrameOptions:                 s.XFrameOptions,
			XContentTypeOptions:           s.XContentTypeOptions,
			XXSSProtection:                s.XXSSProtection,
			ReferrerPolicy:                s.ReferrerPolicy,
			ContentSecurityPolicy:         s.ContentSecurityPolicy,
			StrictTransportPolicy:         hsts,
			XPermittedCrossDomainPolicies: s.XPermittedCrossDomainPolicies,
			XDownloadOptions:              s.XDownloadOptions,
			XDNSPrefetchControl:           s.XDNSPrefetchControl,
			PermissionsPolicy:             s.PermissionsPolicy,
			CrossOriginEmbedderPolicy:     s.CrossOriginEmbedderPolicy,
			CrossOriginOpenerPolicy:       s.CrossOriginOpenerPolicy,
			CrossOriginResourcePolicy:     s.CrossOriginResourcePolicy,
			CacheControl:                  s.CacheControl,
			ExcludePaths:                  s.ExcludePaths,
		}))
	}

	// NOTE: Metrics v1 middleware removed — v2 version (mwmetrics.New) is
	// registered earlier in the chain with config gating and latency buckets.

	// Access Log (priority 1000)
	chain.Use(middleware.NewAccessLogMiddleware(middleware.AccessLogConfig{Logger: logger}))

	return chain
}
