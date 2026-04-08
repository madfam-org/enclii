# Cloudflare Configuration for Enclii
# Manages DNS, Tunnel, R2 buckets, and Zero Trust Access

# =============================================================================
# CLOUDFLARE TUNNEL
# =============================================================================

resource "cloudflare_tunnel" "enclii" {
  account_id = var.cloudflare_account_id
  name       = "enclii-${var.environment}"
  secret     = base64encode(random_password.tunnel_secret.result)
}

resource "random_password" "tunnel_secret" {
  length  = 64
  special = false
}

# Tunnel configuration for routing traffic to services
resource "cloudflare_tunnel_config" "enclii" {
  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_tunnel.enclii.id

  config {
    # Switchyard API
    ingress_rule {
      hostname = "${var.subdomain_api}.${var.domain}"
      service  = "http://switchyard-api.enclii.svc.cluster.local:80"

      origin_request {
        connect_timeout = "30s"
        no_tls_verify   = false
      }
    }

    # Switchyard UI
    ingress_rule {
      hostname = "${var.subdomain_app}.${var.domain}"
      service  = "http://switchyard-ui.enclii.svc.cluster.local:80"
    }

    # Monitoring (protected by Access)
    ingress_rule {
      hostname = "metrics.${var.domain}"
      service  = "http://prometheus.monitoring.svc.cluster.local:9090"
    }

    ingress_rule {
      hostname = "grafana.${var.domain}"
      service  = "http://grafana.monitoring.svc.cluster.local:3000"
    }

    # Landing page (apex domain)
    ingress_rule {
      hostname = var.domain
      service  = "http://landing-page.enclii.svc.cluster.local:80"
    }

    # Landing page (www subdomain)
    ingress_rule {
      hostname = "www.${var.domain}"
      service  = "http://landing-page.enclii.svc.cluster.local:80"
    }

    # Documentation site
    ingress_rule {
      hostname = "docs.${var.domain}"
      service  = "http://docs-site.enclii.svc.cluster.local:80"
    }

    # Status page (Enclii Platform)
    ingress_rule {
      hostname = "status.${var.domain}"
      service  = "http://status-enclii.status.svc.cluster.local:80"
    }

    # Default catch-all
    ingress_rule {
      service = "http_status:404"
    }
  }
}

# =============================================================================
# DNS RECORDS
# =============================================================================

resource "cloudflare_record" "api" {
  zone_id = data.cloudflare_zone.main.id
  name    = var.subdomain_api
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1 # Auto TTL when proxied
}

resource "cloudflare_record" "app" {
  zone_id = data.cloudflare_zone.main.id
  name    = var.subdomain_app
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "metrics" {
  zone_id = data.cloudflare_zone.main.id
  name    = "metrics"
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "grafana" {
  zone_id = data.cloudflare_zone.main.id
  name    = "grafana"
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "landing" {
  zone_id = data.cloudflare_zone.main.id
  name    = "@"  # Apex domain
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "www" {
  zone_id = data.cloudflare_zone.main.id
  name    = "www"
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "docs" {
  zone_id = data.cloudflare_zone.main.id
  name    = "docs"
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

resource "cloudflare_record" "status" {
  zone_id = data.cloudflare_zone.main.id
  name    = "status"
  value   = cloudflare_tunnel.enclii.cname
  type    = "CNAME"
  proxied = true
  ttl     = 1
}

# =============================================================================
# R2 BUCKETS
# =============================================================================

resource "cloudflare_r2_bucket" "backups" {
  account_id = var.cloudflare_account_id
  name       = var.r2_bucket_backups
  location   = "WEUR" # Western Europe
}

resource "cloudflare_r2_bucket" "artifacts" {
  account_id = var.cloudflare_account_id
  name       = var.r2_bucket_artifacts
  location   = "WEUR"
}

resource "cloudflare_r2_bucket" "terraform_state" {
  account_id = var.cloudflare_account_id
  name       = "enclii-terraform-state"
  location   = "WEUR"
}

# =============================================================================
# ZERO TRUST ACCESS (Protect internal services)
# =============================================================================

resource "cloudflare_access_application" "monitoring" {
  zone_id          = data.cloudflare_zone.main.id
  name             = "Enclii Monitoring"
  domain           = "metrics.${var.domain}"
  type             = "self_hosted"
  session_duration = "24h"

  auto_redirect_to_identity = true
}

resource "cloudflare_access_application" "grafana" {
  zone_id          = data.cloudflare_zone.main.id
  name             = "Enclii Grafana"
  domain           = "grafana.${var.domain}"
  type             = "self_hosted"
  session_duration = "24h"

  auto_redirect_to_identity = true
}

# Access policy - require email domain
resource "cloudflare_access_policy" "monitoring_policy" {
  application_id = cloudflare_access_application.monitoring.id
  zone_id        = data.cloudflare_zone.main.id
  name           = "Allow team members"
  precedence     = 1
  decision       = "allow"

  include {
    email_domain = [var.allowed_email_domain]
  }
}

resource "cloudflare_access_policy" "grafana_policy" {
  application_id = cloudflare_access_application.grafana.id
  zone_id        = data.cloudflare_zone.main.id
  name           = "Allow team members"
  precedence     = 1
  decision       = "allow"

  include {
    email_domain = [var.allowed_email_domain]
  }
}

# =============================================================================
# WAF & SECURITY RULES
# =============================================================================

resource "cloudflare_ruleset" "security" {
  zone_id     = data.cloudflare_zone.main.id
  name        = "Enclii Security Rules"
  description = "Security rules for Enclii"
  kind        = "zone"
  phase       = "http_request_firewall_custom"

  # Block requests from sanctioned countries (customize as needed)
  rules {
    action      = "block"
    expression  = "(ip.geoip.country in {\"KP\" \"IR\" \"CU\" \"SY\"})"
    description = "Block sanctioned countries"
    enabled     = true
  }

  # Rate limit API endpoints (api.enclii.dev)
  # Free tier supports 1 rate-limit rule. For per-domain rules, upgrade to Pro:
  #   api.enclii.dev  — 200 req/min per IP (API)
  #   app.enclii.dev  — 100 req/min per IP (dashboard)
  #   *.fn.enclii.dev — 50 req/min per IP  (serverless, cold-start aware)
  # See: https://developers.cloudflare.com/waf/rate-limiting-rules/
  rules {
    action      = "block"
    expression  = "(http.host eq \"${var.subdomain_api}.${var.domain}\" and http.request.uri.path contains \"/api/\" and rate(1m) > 200)"
    description = "Rate limit API (200 req/min per IP)"
    enabled     = true
  }

  # Challenge suspicious requests
  rules {
    action      = "managed_challenge"
    expression  = "(cf.threat_score > 30)"
    description = "Challenge high threat score"
    enabled     = true
  }
}

# =============================================================================
# ADDITIONAL VARIABLES
# =============================================================================

variable "allowed_email_domain" {
  description = "Email domain allowed for Zero Trust Access"
  type        = string
  default     = "enclii.dev"
}

# =============================================================================
# OUTPUTS
# =============================================================================

output "tunnel_id" {
  description = "Cloudflare Tunnel ID"
  value       = cloudflare_tunnel.enclii.id
}

output "tunnel_cname" {
  description = "Cloudflare Tunnel CNAME"
  value       = cloudflare_tunnel.enclii.cname
}

output "tunnel_token" {
  description = "Cloudflare Tunnel token for cloudflared"
  value       = cloudflare_tunnel.enclii.tunnel_token
  sensitive   = true
}

output "r2_bucket_backups_name" {
  description = "R2 backups bucket name"
  value       = cloudflare_r2_bucket.backups.name
}

output "r2_bucket_artifacts_name" {
  description = "R2 artifacts bucket name"
  value       = cloudflare_r2_bucket.artifacts.name
}
