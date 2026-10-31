package engine

import (
	"encoding/base64"
	"errors"
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

// middlewareRegistrationContext holds shared dependencies for middleware registration.
type middlewareRegistrationContext struct {
	cfg      *config.Config
	logger   *logging.Logger
	registry *metrics.Registry
	chain    *middleware.Chain

	// errs collects construction failures of enabled middleware. Dropping an
	// enabled middleware from the chain would fail open (no auth, no WAF), so
	// any error here rejects the whole config.
	errs []error
}

// orDefault returns v, or def when v is empty. Config docs promise defaults
// for some auth fields (hash: sha256, JWT algorithm/header/prefix) that the middleware
// constructors do not apply themselves.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// boolDefault returns *p, or def when the key was omitted.
func boolDefault(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// headersOrDefault returns h, or the real IP middleware's default header
// list when none is configured. A real_ip block with only trusted_proxies
// used to match no header at all and change nothing.
func headersOrDefault(h []string) []string {
	if len(h) > 0 {
		return h
	}
	return realip.DefaultConfig().Headers
}

// fail records that an enabled middleware could not be built.
func (ctx *middlewareRegistrationContext) fail(name string, err error) {
	ctx.errs = append(ctx.errs, fmt.Errorf("middleware %s: %w", name, err))
}

// createMiddlewareChain creates the middleware chain based on configuration.
// It returns an error if any enabled middleware fails to build.
func createMiddlewareChain(cfg *config.Config, logger *logging.Logger, registry *metrics.Registry) (*middleware.Chain, error) {
	ctx := &middlewareRegistrationContext{
		cfg:      cfg,
		logger:   logger,
		registry: registry,
		chain:    middleware.NewChain(),
	}

	// Panic Recovery (priority 1) — MUST be first to catch panics from all downstream middleware
	ctx.chain.Use(middleware.NewRecoveryMiddleware(middleware.RecoveryConfig{
		LogFunc: func(panicVal any, stack string) {
			logger.Error("panic recovered",
				logging.String("panic", fmt.Sprintf("%v", panicVal)),
				logging.String("stack", stack),
			)
		},
	}))

	// Register all middleware in priority order.
	// Each function checks its config gate and returns silently if not enabled.
	registerTraceMiddleware(ctx)
	registerRealIPMiddleware(ctx)
	registerLoggingMiddleware(ctx)
	registerForceSSLMiddleware(ctx)
	registerMetricsMiddleware(ctx)
	registerRequestIDMiddleware(ctx)
	registerIPFilterMiddleware(ctx)
	registerBotDetectionMiddleware(ctx)
	registerMaxBodySizeMiddleware(ctx)
	registerCacheMiddleware(ctx)
	registerWAFMiddleware(ctx)
	registerRewriteMiddleware(ctx)
	registerStripPrefixMiddleware(ctx)
	registerValidatorMiddleware(ctx)
	registerCoalesceMiddleware(ctx)
	registerJWTMiddleware(ctx)
	registerOAuth2Middleware(ctx)
	registerHMACMiddleware(ctx)
	registerAPIKeyMiddleware(ctx)
	registerBasicAuthMiddleware(ctx)
	registerCSRFMiddleware(ctx)
	registerTimeoutMiddleware(ctx)
	registerRateLimitMiddleware(ctx)
	registerCircuitBreakerMiddleware(ctx)
	registerCORSMiddleware(ctx)
	registerCSPMiddleware(ctx)
	registerHeadersMiddleware(ctx)
	registerCompressionMiddleware(ctx)
	registerTransformerMiddleware(ctx)
	registerRetryMiddleware(ctx)
	registerSecureHeadersMiddleware(ctx)

	// Access Log (priority 1000) — always enabled
	ctx.chain.Use(middleware.NewAccessLogMiddleware(middleware.AccessLogConfig{Logger: logger}))

	if len(ctx.errs) > 0 {
		return nil, errors.Join(ctx.errs...)
	}
	return ctx.chain, nil
}

func registerTraceMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Trace == nil || !ctx.cfg.Middleware.Trace.Enabled {
		return
	}
	t := ctx.cfg.Middleware.Trace
	ctx.chain.Use(trace.New(trace.Config{
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

func registerRealIPMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.RealIP == nil || !ctx.cfg.Middleware.RealIP.Enabled {
		return
	}
	r := ctx.cfg.Middleware.RealIP
	ctx.chain.Use(realip.New(realip.Config{
		Enabled:         r.Enabled,
		Headers:         headersOrDefault(r.Headers),
		TrustedProxies:  r.TrustedProxies,
		RejectUntrusted: r.RejectUntrusted,
	}))
}

func registerLoggingMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Logging == nil || !ctx.cfg.Middleware.Logging.Enabled {
		return
	}
	l := ctx.cfg.Middleware.Logging
	ctx.chain.Use(mwlogging.New(mwlogging.Config{
		Enabled:         l.Enabled,
		Format:          l.Format,
		CustomFormat:    l.CustomFormat,
		Fields:          l.Fields,
		ExcludePaths:    l.ExcludePaths,
		ExcludeStatus:   l.ExcludeStatus,
		MinDuration:     l.MinDuration,
		RequestHeaders:  l.RequestHeaders,
		ResponseHeaders: l.ResponseHeaders,
		LogFunc: func(msg string) {
			ctx.logger.Info(msg)
		},
	}))
}

func registerForceSSLMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.ForceSSL == nil || !ctx.cfg.Middleware.ForceSSL.Enabled {
		return
	}
	f := ctx.cfg.Middleware.ForceSSL
	ctx.chain.Use(forcessl.New(forcessl.Config{
		Enabled:      f.Enabled,
		Permanent:    f.Permanent,
		ExcludePaths: f.ExcludePaths,
		ExcludeHosts: f.ExcludeHosts,
		Port:         f.Port,
		HeaderKey:    f.HeaderKey,
		HeaderValue:  f.HeaderValue,
	}))
}

func registerMetricsMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware != nil && ctx.cfg.Middleware.Metrics != nil && ctx.cfg.Middleware.Metrics.Enabled {
		m := ctx.cfg.Middleware.Metrics
		ctx.chain.Use(mwmetrics.New(mwmetrics.Config{
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
	ctx.chain.Use(middleware.NewMetricsMiddleware(ctx.registry))
}

func registerRequestIDMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.RequestID == nil || !ctx.cfg.Middleware.RequestID.Enabled {
		return
	}
	r := ctx.cfg.Middleware.RequestID
	ctx.chain.Use(requestid.New(requestid.Config{
		Enabled:      r.Enabled,
		Header:       r.Header,
		Generate:     boolDefault(r.Generate, true),
		Length:       r.Length,
		Response:     boolDefault(r.Response, true),
		ExcludePaths: r.ExcludePaths,
	}))
}

func registerIPFilterMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.IPFilter == nil || !ctx.cfg.Middleware.IPFilter.Enabled {
		return
	}
	ipCfg := ctx.cfg.Middleware.IPFilter
	ipFilter, err := middleware.NewIPFilterMiddleware(middleware.IPFilterConfig{
		AllowList: ipCfg.AllowList, DenyList: ipCfg.DenyList, DefaultAction: ipCfg.DefaultAction,
	})
	if err != nil {
		ctx.fail("ip_filter", err)
		return
	}
	ctx.chain.Use(ipFilter)
}

func registerBotDetectionMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.BotDetection == nil || !ctx.cfg.Middleware.BotDetection.Enabled {
		return
	}
	bd := ctx.cfg.Middleware.BotDetection

	uaRules := make([]botdetection.UserAgentRule, len(bd.UserAgentRules))
	for i, rule := range bd.UserAgentRules {
		uaRules[i] = botdetection.UserAgentRule{
			Pattern: rule.Pattern,
			Action:  botdetection.Action(rule.Action),
			Name:    rule.Name,
		}
	}

	headerRules := make([]botdetection.HeaderRule, len(bd.CustomHeaders))
	for i, rule := range bd.CustomHeaders {
		headerRules[i] = botdetection.HeaderRule{
			Header:  rule.Header,
			Pattern: rule.Pattern,
			Action:  botdetection.Action(rule.Action),
			Name:    rule.Name,
		}
	}

	ctx.chain.Use(botdetection.New(botdetection.Config{
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

func registerMaxBodySizeMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.MaxBodySize == nil || !ctx.cfg.Middleware.MaxBodySize.Enabled {
		return
	}
	maxSize := ctx.cfg.Middleware.MaxBodySize.MaxSize
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024 // 10 MB default
	}
	ctx.chain.Use(middleware.NewBodyLimitMiddleware(middleware.BodyLimitConfig{MaxSize: maxSize}))
}

func registerCacheMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Cache == nil || !ctx.cfg.Middleware.Cache.Enabled {
		return
	}
	c := ctx.cfg.Middleware.Cache

	var ttl time.Duration
	if c.DefaultTTL != "" {
		var err error
		ttl, err = time.ParseDuration(c.DefaultTTL)
		if err != nil {
			ctx.logger.Warnf("invalid cache TTL %q, using default 5m", c.DefaultTTL)
			ttl = 5 * time.Minute
		}
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

	ctx.chain.Use(cache.New(cache.Config{
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

func registerWAFMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.WAF == nil || !ctx.cfg.WAF.Enabled {
		return
	}
	if l := ctx.cfg.WAF.Logging; l != nil && l.LogBody && ctx.logger != nil {
		// WAF events carry no request body; say so instead of letting
		// operators believe bodies are being captured.
		ctx.logger.Warn("waf.logging.log_body is not implemented and has no effect")
	}
	if b := ctx.cfg.WAF.BotDetection; b != nil && b.TLSFingerprint != nil && b.TLSFingerprint.Enabled && ctx.logger != nil {
		ctx.logger.Warn("waf.bot_detection.tls_fingerprint is not implemented (JA3 is never computed) and has no effect")
	}
	wafMW, err := waf.NewWAFMiddleware(waf.WAFMiddlewareConfig{
		Config:          ctx.cfg.WAF,
		MetricsRegistry: ctx.registry,
	})
	if err != nil {
		ctx.fail("waf", err)
		return
	}
	ctx.chain.Use(wafMW)
}

func registerRewriteMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Rewrite == nil || !ctx.cfg.Middleware.Rewrite.Enabled {
		return
	}
	r := ctx.cfg.Middleware.Rewrite
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
	if err != nil {
		ctx.fail("rewrite", err)
		return
	}
	ctx.chain.Use(rewriteMW)
}

func registerStripPrefixMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.StripPrefix == nil || !ctx.cfg.Middleware.StripPrefix.Enabled {
		return
	}
	sp := ctx.cfg.Middleware.StripPrefix
	ctx.chain.Use(middleware.NewStripPrefixMiddleware(middleware.StripPrefixConfig{
		Prefix:        sp.Prefix,
		RedirectSlash: sp.RedirectSlash,
	}))
}

func registerValidatorMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Validator == nil || !ctx.cfg.Middleware.Validator.Enabled {
		return
	}
	v := ctx.cfg.Middleware.Validator
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
	if err != nil {
		ctx.fail("validator", err)
		return
	}
	ctx.chain.Use(validatorMW)
}

func registerCoalesceMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Coalesce == nil || !ctx.cfg.Middleware.Coalesce.Enabled {
		return
	}
	c := ctx.cfg.Middleware.Coalesce
	var ttl time.Duration
	if c.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(c.TTL)
		if err != nil {
			ctx.logger.Warnf("invalid coalesce TTL %q, using default 100ms", c.TTL)
			ttl = 100 * time.Millisecond
		}
	}
	if ttl == 0 {
		ttl = 100 * time.Millisecond
	}
	ctx.chain.Use(coalesce.New(coalesce.Config{
		Enabled:      c.Enabled,
		TTL:          ttl,
		MaxRequests:  c.MaxRequests,
		ExcludePaths: c.ExcludePaths,
	}))
}

func registerJWTMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.JWT == nil || !ctx.cfg.Middleware.JWT.Enabled {
		return
	}
	j := ctx.cfg.Middleware.JWT

	var publicKey []byte
	if j.Algorithm == "EdDSA" && j.PublicKey != "" {
		if _, err := os.Stat(j.PublicKey); err == nil {
			publicKey, err = os.ReadFile(j.PublicKey)
			if err != nil {
				ctx.logger.Warn("Failed to load JWT public key from file",
					logging.String("file", j.PublicKey),
					logging.String("error", err.Error()),
				)
			}
		} else {
			publicKey, err = base64.StdEncoding.DecodeString(j.PublicKey)
			if err != nil {
				ctx.logger.Warn("Failed to decode JWT public key as base64",
					logging.String("error", err.Error()),
				)
			}
		}
	}

	jwtMW, err := jwt.New(jwt.Config{
		Enabled:      j.Enabled,
		Secret:       j.Secret,
		PublicKey:    publicKey,
		Algorithm:    orDefault(j.Algorithm, "HS256"),
		Header:       orDefault(j.Header, "Authorization"),
		Prefix:       orDefault(j.Prefix, "Bearer "),
		Required:     j.Required,
		ExcludePaths: j.ExcludePaths,
		ClaimsValidation: jwt.ClaimsValidation{
			Issuer:   j.ClaimsValidation.Issuer,
			Audience: j.ClaimsValidation.Audience,
			// Secure by default, as in jwt.DefaultConfig: a token without
			// exp would otherwise be valid forever.
			RequireExpiration: j.ClaimsValidation.RequireExpiration == nil || *j.ClaimsValidation.RequireExpiration,
		},
	})
	if err != nil {
		ctx.fail("jwt", err)
		return
	}
	ctx.chain.Use(jwtMW)
}

func registerOAuth2Middleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.OAuth2 == nil || !ctx.cfg.Middleware.OAuth2.Enabled {
		return
	}
	o := ctx.cfg.Middleware.OAuth2
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
	if err != nil {
		ctx.fail("oauth2", err)
		return
	}
	ctx.chain.Use(oauth2MW)
}

func registerHMACMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.HMAC == nil || !ctx.cfg.Middleware.HMAC.Enabled {
		return
	}
	h := ctx.cfg.Middleware.HMAC
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
	if err != nil {
		ctx.fail("hmac", err)
		return
	}
	ctx.chain.Use(hmacMW)
}

func registerAPIKeyMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.APIKey == nil || !ctx.cfg.Middleware.APIKey.Enabled {
		return
	}
	a := ctx.cfg.Middleware.APIKey
	apiKeyMW, err := apikey.New(apikey.Config{
		Enabled:      a.Enabled,
		Keys:         a.Keys,
		Header:       a.Header,
		ExcludePaths: a.ExcludePaths,
		Hash:         orDefault(a.Hash, "sha256"),
	})
	if err != nil {
		ctx.fail("api_key", err)
		return
	}
	ctx.chain.Use(apiKeyMW)
}

func registerBasicAuthMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.BasicAuth == nil || !ctx.cfg.Middleware.BasicAuth.Enabled {
		return
	}
	b := ctx.cfg.Middleware.BasicAuth
	basicMW, err := basic.New(basic.Config{
		Enabled:        b.Enabled,
		Users:          b.Users,
		Realm:          b.Realm,
		ExcludePaths:   b.ExcludePaths,
		Hash:           orDefault(b.Hash, "sha256"),
		AllowPlaintext: b.AllowPlaintext,
	})
	if err != nil {
		ctx.fail("basic_auth", err)
		return
	}
	ctx.chain.Use(basicMW)
}

func registerCSRFMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.CSRF == nil || !ctx.cfg.Middleware.CSRF.Enabled {
		return
	}
	c := ctx.cfg.Middleware.CSRF
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
	if err != nil {
		ctx.fail("csrf", err)
		return
	}
	ctx.chain.Use(csrfMW)
}

func registerTimeoutMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Timeout == nil || !ctx.cfg.Middleware.Timeout.Enabled {
		return
	}
	timeout := parseDuration(ctx.cfg.Middleware.Timeout.Timeout, 60*time.Second)
	ctx.chain.Use(middleware.NewTimeoutMiddleware(middleware.TimeoutConfig{Timeout: timeout}))
}

func registerRateLimitMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.RateLimit == nil || !ctx.cfg.Middleware.RateLimit.Enabled {
		return
	}
	rl := ctx.cfg.Middleware.RateLimit
	rps := rl.RequestsPerSecond
	if rps <= 0 {
		rps = 100
	}
	burst := rl.BurstSize
	if burst <= 0 {
		burst = 200
	}
	rateLimiter, err := middleware.NewRateLimitMiddleware(middleware.RateLimitConfig{
		RequestsPerSecond: rps, BurstSize: burst,
	})
	if err != nil {
		ctx.fail("rate_limit", err)
		return
	}
	ctx.chain.Use(rateLimiter)
}

func registerCircuitBreakerMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.CircuitBreaker == nil || !ctx.cfg.Middleware.CircuitBreaker.Enabled {
		return
	}
	cbCfg, err := circuitBreakerConfigFrom(ctx.cfg.Middleware.CircuitBreaker)
	if err != nil {
		ctx.fail("circuit_breaker", err)
		return
	}
	ctx.chain.Use(middleware.NewCircuitBreaker(cbCfg))
}

// circuitBreakerConfigFrom maps the config section onto the middleware
// config. Only error_threshold used to be applied; error_rate_threshold and
// open_duration were accepted and ignored.
func circuitBreakerConfigFrom(c *config.CircuitBreakerConfig) (middleware.CircuitBreakerConfig, error) {
	cfg := middleware.DefaultCircuitBreakerConfig()
	if c.ErrorThreshold > 0 {
		cfg.ErrorThreshold = c.ErrorThreshold
	}
	if c.ErrorRateThreshold != 0 {
		if c.ErrorRateThreshold < 0 || c.ErrorRateThreshold > 1 {
			return cfg, fmt.Errorf("error_rate_threshold %v out of range (0.0-1.0)", c.ErrorRateThreshold)
		}
		cfg.ErrorRateThreshold = c.ErrorRateThreshold
	}
	if c.OpenDuration != "" {
		d, err := time.ParseDuration(c.OpenDuration)
		if err != nil || d <= 0 {
			return cfg, fmt.Errorf("invalid open_duration %q", c.OpenDuration)
		}
		cfg.OpenDuration = d
	}
	return cfg, nil
}

func registerCORSMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.CORS == nil || !ctx.cfg.Middleware.CORS.Enabled {
		return
	}
	c := ctx.cfg.Middleware.CORS
	origins := c.AllowedOrigins
	if len(origins) == 0 {
		origins = []string{"*"}
	}
	methods := c.AllowedMethods
	if len(methods) == 0 {
		methods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	corsMW, err := middleware.NewCORSMiddleware(middleware.CORSConfig{
		AllowedOrigins: origins, AllowedMethods: methods, AllowedHeaders: c.AllowedHeaders,
		AllowCredentials: c.AllowCredentials, MaxAge: time.Duration(c.MaxAge) * time.Second,
	})
	if err != nil {
		ctx.fail("cors", err)
		return
	}
	ctx.chain.Use(corsMW)
}

func registerCSPMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.CSP == nil || !ctx.cfg.Middleware.CSP.Enabled {
		return
	}
	c := ctx.cfg.Middleware.CSP
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
	if err != nil {
		ctx.fail("csp", err)
		return
	}
	ctx.chain.Use(cspMW)
}

func registerHeadersMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Headers == nil || !ctx.cfg.Middleware.Headers.Enabled {
		return
	}
	h := ctx.cfg.Middleware.Headers
	ctx.chain.Use(middleware.NewHeadersMiddleware(middleware.HeadersConfig{
		RequestAdd: h.RequestAdd, ResponseAdd: h.ResponseAdd,
	}))
}

func registerCompressionMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Compression == nil || !ctx.cfg.Middleware.Compression.Enabled {
		return
	}
	comp, err := middleware.NewCompressionMiddleware(middleware.CompressionConfig{
		MinSize: ctx.cfg.Middleware.Compression.MinSize,
		Level:   ctx.cfg.Middleware.Compression.Level,
	})
	if err != nil {
		ctx.fail("compression", err)
		return
	}
	ctx.chain.Use(comp)
}

func registerTransformerMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Transformer == nil || !ctx.cfg.Middleware.Transformer.Enabled {
		return
	}
	t := ctx.cfg.Middleware.Transformer
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
	if err != nil {
		ctx.fail("transformer", err)
		return
	}
	ctx.chain.Use(transformerMW)
}

func registerRetryMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.Retry == nil || !ctx.cfg.Middleware.Retry.Enabled {
		return
	}
	ctx.chain.Use(middleware.NewRetryMiddleware(retryConfigFrom(ctx.cfg.Middleware.Retry)))
}

// retryConfigFrom maps the config section onto the middleware config.
// retry_on used to be accepted and ignored.
func retryConfigFrom(c *config.RetryConfig) middleware.RetryConfig {
	cfg := middleware.DefaultRetryConfig()
	if c.MaxRetries > 0 {
		cfg.MaxRetries = c.MaxRetries
	}
	if len(c.RetryOn) > 0 {
		cfg.RetryOn = c.RetryOn
	}
	return cfg
}

func registerSecureHeadersMiddleware(ctx *middlewareRegistrationContext) {
	if ctx.cfg.Middleware == nil || ctx.cfg.Middleware.SecureHeaders == nil || !ctx.cfg.Middleware.SecureHeaders.Enabled {
		return
	}
	s := ctx.cfg.Middleware.SecureHeaders
	var hsts *secureheaders.HSTSConfig
	if p := s.StrictTransportPolicy; p != nil && (p.Enabled == nil || *p.Enabled) {
		hsts = &secureheaders.HSTSConfig{
			MaxAge:            s.StrictTransportPolicy.MaxAge,
			IncludeSubdomains: s.StrictTransportPolicy.IncludeSubdomains,
			Preload:           s.StrictTransportPolicy.Preload,
		}
	}
	ctx.chain.Use(secureheaders.New(secureheaders.Config{
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
