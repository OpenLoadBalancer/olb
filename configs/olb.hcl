# OpenLoadBalancer Full Configuration Example (HCL)
# This is a comprehensive configuration demonstrating all features.
# Functionally equivalent to olb.yaml.

version = 1

# ---------------------------------------------------------------------------
# Global settings
# ---------------------------------------------------------------------------
global {
  # Worker settings
  workers {
    count = "auto"  # "auto" = number of CPUs
  }

  # Connection limits
  limits {
    max_connections            = 10000
    max_connections_per_source = 100
    max_connections_per_backend = 1000
  }

  # Timeouts (duration strings)
  timeouts {
    read   = "30s"
    write  = "30s"
    idle   = "120s"
    header = "10s"
    drain  = "30s"
  }
}

# ---------------------------------------------------------------------------
# Admin API configuration
# ---------------------------------------------------------------------------
admin {
  enabled = true
  address = "127.0.0.1:8081"

  auth {
    type     = "basic"
    username = "admin"
    # IMPORTANT: Change this password before deploying!
    # Generate a bcrypt hash: go run -mod=mod github.com/tyler-smith/go-bcrypt-cli <password>
    password = "$2a$10$CHANGEME_REPLACE_WITH_YOUR_OWN_BCRYPT_HASH"
  }
}

# ---------------------------------------------------------------------------
# Metrics configuration
# ---------------------------------------------------------------------------
metrics {
  enabled = true
  path    = "/metrics"
}

# ---------------------------------------------------------------------------
# Listeners
# ---------------------------------------------------------------------------

# HTTP listener on port 8080
listener "http" {
  protocol = "http"
  address  = ":8080"

  # API routes with specific backend
  route "api" {
    host    = "api.example.com"
    path    = "/api/"
    methods = ["GET", "POST", "PUT", "DELETE"]
    pool    = "api-backend"
  }

  # Static file serving (direct backend)
  route "static" {
    host = "static.example.com"
    path = "/files/"
    pool = "static-backend"
  }

  # Default route for everything else
  route "default" {
    path = "/"
    pool = "web-backend"

    middleware "rate_limit" {
      requests_per_second = 100
      burst_size          = 200
    }
  }
}

# HTTPS listener on port 8443
listener "https" {
  protocol = "https"
  address  = ":8443"

  tls {
    cert_file = "/etc/olb/certs/server.crt"
    key_file  = "/etc/olb/certs/server.key"
  }

  route "secure-api" {
    path = "/"
    pool = "api-backend"
  }
}

# ---------------------------------------------------------------------------
# Backend pools with different algorithms
# ---------------------------------------------------------------------------

# Web pool - round robin for general traffic
pool "web-backend" {
  algorithm = "round_robin"

  health_check {
    type                = "http"
    path                = "/health"
    interval            = "10s"
    timeout             = "5s"
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  backend "web-1" {
    address = "10.0.1.10:8080"
    weight  = 1
  }

  backend "web-2" {
    address = "10.0.1.11:8080"
    weight  = 1
  }

  backend "web-3" {
    address = "10.0.1.12:8080"
    weight  = 1
  }
}

# API pool - weighted round robin for API servers
pool "api-backend" {
  algorithm = "weighted_round_robin"

  health_check {
    type            = "http"
    path            = "/api/health"
    interval        = "5s"
    timeout         = "3s"
    expected_status = 200
  }

  backend "api-1" {
    address = "10.0.2.10:8080"
    weight  = 3  # More capacity
  }

  backend "api-2" {
    address = "10.0.2.11:8080"
    weight  = 2
  }
}

# Static files pool - least connections for large files
pool "static-backend" {
  algorithm = "round_robin"

  health_check {
    type     = "tcp"
    interval = "10s"
    timeout  = "5s"
  }

  backend "static-1" {
    address = "10.0.3.10:8080"
  }
}

# ---------------------------------------------------------------------------
# Middleware configuration
# ---------------------------------------------------------------------------

# Request ID injection
middleware "request_id" {
  enabled = true

  config {
    header_name    = "X-Request-ID"
    trust_incoming = false
  }
}

# Real IP extraction
middleware "real_ip" {
  enabled = true

  config {
    trusted_proxies = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
  }
}

# CORS handling
middleware "cors" {
  enabled = true

  config {
    allowed_origins = ["*"]
    allowed_methods = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    allowed_headers = ["Content-Type", "Authorization", "X-Request-ID"]
    max_age         = 3600
  }
}

# Security headers
middleware "headers" {
  enabled = true

  config {
    security_preset = "strict"

    response_set = {
      X-Frame-Options        = "DENY"
      X-Content-Type-Options = "nosniff"
    }
  }
}

# Compression
middleware "compression" {
  enabled = true

  config {
    min_size = 1024
    level    = "default"  # -1 = default compression
  }
}

# Access logging
middleware "access_log" {
  enabled = true

  config {
    format = "json"
    output = "/var/log/olb/access.log"
  }
}

# Metrics collection
middleware "metrics" {
  enabled = true
}

# ---------------------------------------------------------------------------
# Logging configuration
# ---------------------------------------------------------------------------
logging {
  level  = "info"
  output = "stdout"
  format = "json"

  # File output (optional)
  # file {
  #   path        = "/var/log/olb/olb.log"
  #   max_size    = "100MB"
  #   max_backups = 5
  #   max_age     = 30
  #   compress    = true
  # }
}

# ---------------------------------------------------------------------------
# WAF (Web Application Firewall)
# ---------------------------------------------------------------------------
waf {
  enabled = true
  mode    = "block"  # "enforce" (block), "monitor" (log only), "disabled"

  ip_acl {
    enabled = true

    whitelist {
      cidr   = "10.0.0.0/8"
      reason = "Internal network"
    }

    whitelist {
      cidr   = "192.168.0.0/16"
      reason = "Office VPN"
    }

    blacklist {
      cidr    = "203.0.113.0/24"
      reason  = "Known attacker range"
      expires = "2026-12-31T23:59:59Z"
    }

    auto_ban {
      enabled     = true
      default_ttl = "1h"
      max_ttl     = "24h"
    }
  }

  rate_limit {
    enabled       = true
    sync_interval = "5s"

    store {
      type = "memory"
    }

    rules {
      id             = "global-limit"
      scope          = "ip"
      limit          = 100
      window         = "1m"
      burst          = 150
      action         = "block"
      auto_ban_after = 0
    }
  }

  sanitizer {
    enabled            = true
    max_header_size    = 8192
    max_header_count   = 50
    max_body_size      = 10485760
    max_url_length     = 2048
    max_cookie_size    = 4096
    max_cookie_count   = 20
    block_null_bytes   = true
    normalize_encoding = true
    strip_hop_by_hop   = true
    allowed_methods    = ["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"]
  }

  detection {
    enabled = true
    mode    = "enforce"

    threshold {
      block = 50
      log   = 25
    }

    detectors {
      sqli {
        enabled          = true
        score_multiplier = 1.0
      }
      xss {
        enabled          = true
        score_multiplier = 1.0
      }
      path_traversal {
        enabled          = true
        score_multiplier = 1.0
      }
      cmdi {
        enabled          = true
        score_multiplier = 1.0
      }
      xxe {
        enabled          = true
        score_multiplier = 1.0
      }
      ssrf {
        enabled          = true
        score_multiplier = 1.0
      }
    }
  }

  bot_detection {
    enabled = true
    mode    = "enforce"

    tls_fingerprint {
      enabled           = true
      known_bots_action = "log"
      unknown_action    = "log"
      mismatch_action   = "block"
    }

    user_agent {
      enabled              = true
      block_empty          = true
      block_known_scanners = true
    }

    behavior {
      enabled              = true
      window               = "5m"
      rps_threshold        = 100
      error_rate_threshold = 50
    }
  }

  response {
    security_headers {
      enabled                = true
      x_content_type_options = true
      x_frame_options        = "DENY"
      referrer_policy        = "strict-origin-when-cross-origin"
      permissions_policy     = "camera=(), microphone=(), geolocation=()"
      content_security_policy = "default-src 'self'"

      hsts {
        enabled           = true
        max_age           = 31536000
        include_subdomains = true
        preload           = false
      }
    }

    data_masking {
      enabled            = true
      mask_credit_cards  = true
      mask_ssn           = true
      mask_emails        = false
      mask_api_keys      = true
      strip_stack_traces = true
    }

    error_pages {
      enabled = true
      mode    = "production"
    }
  }

  logging {
    level       = "info"
    format      = "json"
    log_allowed = false
    log_blocked = true
    log_body    = false
  }
}

# ---------------------------------------------------------------------------
# Cluster configuration (Raft consensus)
# ---------------------------------------------------------------------------
cluster {
  enabled        = false
  node_id        = "node-1"
  bind_addr      = "0.0.0.0"
  bind_port      = 9090
  data_dir       = "/var/lib/olb/raft"
  election_tick  = "2s"
  heartbeat_tick = "500ms"
  peers          = ["node-2:10.0.1.11:9090", "node-3:10.0.1.12:9090"]

  node_auth {
    shared_secret    = "${CLUSTER_SECRET}"
    allowed_node_ids = ["node-1", "node-2", "node-3"]
  }
}

# ---------------------------------------------------------------------------
# Server tuning
# ---------------------------------------------------------------------------
server {
  max_connections            = 10000
  max_connections_per_source = 100
  max_connections_per_backend = 1000
  proxy_timeout              = "60s"
  dial_timeout               = "10s"
  max_retries                = 3
  max_idle_conns             = 100
  max_idle_conns_per_host    = 10
  idle_conn_timeout          = "90s"
  drain_timeout              = "30s"
}
