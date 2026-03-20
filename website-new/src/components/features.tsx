import { useEffect, useRef, useState } from "react";
import {
  Shuffle,
  Package,
  Layers,
  ShieldCheck,
  Lock,
  HeartPulse,
  Network,
  LayoutDashboard,
  SlidersHorizontal,
  Bot,
  RefreshCw,
  Zap,
} from "lucide-react";
import { cn } from "@/lib/utils";

const features = [
  {
    icon: Shuffle,
    title: "14 Load Balancing Algorithms",
    description:
      "Round robin, weighted, least connections, IP hash, consistent hash, maglev, power-of-two, ring hash, sticky sessions, and more.",
  },
  {
    icon: Package,
    title: "Zero Dependencies",
    description:
      "Built entirely with Go standard library. No external modules, no supply chain risk. Single binary deployment.",
  },
  {
    icon: Layers,
    title: "L4 + L7 Proxy",
    description:
      "TCP/UDP (L4) and HTTP/HTTPS (L7) proxy with WebSocket, gRPC, and SSE protocol detection and handling.",
  },
  {
    icon: ShieldCheck,
    title: "6-Layer WAF",
    description:
      "IP ACL, rate limiting, request sanitization, attack detection (SQLi, XSS, path traversal, command injection, XXE, SSRF), bot detection, response security.",
  },
  {
    icon: Lock,
    title: "TLS & mTLS",
    description:
      "Automatic TLS termination with SNI, OCSP stapling, certificate hot-reload, ACME/Let's Encrypt, and mutual TLS client authentication.",
  },
  {
    icon: HeartPulse,
    title: "Health Checks",
    description:
      "Active HTTP/TCP/gRPC health checks plus passive traffic-based health detection with configurable thresholds and auto-recovery.",
  },
  {
    icon: Network,
    title: "Raft Clustering",
    description:
      "Distributed consensus with Raft protocol, SWIM gossip for peer discovery, and distributed state synchronization.",
  },
  {
    icon: LayoutDashboard,
    title: "Real-Time Dashboard",
    description:
      "Embedded Web UI with live metrics, backend status, route management, configuration editor, and WAF monitoring.",
  },
  {
    icon: SlidersHorizontal,
    title: "16 Middleware",
    description:
      "Panic recovery, body limit, timeout, rate limiting, circuit breaker, CORS, compression, retry, caching, and more. All config-gated.",
  },
  {
    icon: Bot,
    title: "MCP Integration",
    description:
      "SSE-compliant MCP server with 17 tools (8 core + 9 WAF) for AI-powered management. Bearer token auth, audit logging. Connect from Claude Desktop, Cursor, or any MCP client.",
  },
  {
    icon: RefreshCw,
    title: "Hot Config Reload",
    description:
      "Change configuration without dropping connections. Atomic swap of routing, pools, and middleware with graceful drain.",
  },
  {
    icon: Zap,
    title: "Sub-Millisecond Latency",
    description:
      "Zero-allocation hot path with sync.Pool, atomic counters, and optimized connection pooling. <1ms p99 latency overhead.",
  },
];

function FeatureCard({
  feature,
  index,
}: {
  feature: (typeof features)[number];
  index: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setVisible(true);
          observer.disconnect();
        }
      },
      { threshold: 0.15 }
    );

    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const Icon = feature.icon;

  return (
    <div
      ref={ref}
      className={cn(
        "group relative p-6 rounded-2xl border border-border bg-card/50 backdrop-blur-sm transition-all duration-500 hover:shadow-lg hover:shadow-primary/5 hover:border-primary/20 hover:scale-[1.02]",
        visible
          ? "opacity-100 translate-y-0"
          : "opacity-0 translate-y-6"
      )}
      style={{ transitionDelay: `${(index % 4) * 75}ms` }}
    >
      {/* Subtle gradient hover overlay */}
      <div className="absolute inset-0 rounded-2xl bg-gradient-to-br from-primary/5 to-secondary/5 opacity-0 group-hover:opacity-100 transition-opacity duration-300" />

      <div className="relative">
        <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-primary/10 to-secondary/10 flex items-center justify-center mb-4 group-hover:from-primary/20 group-hover:to-secondary/20 transition-colors">
          <Icon className="w-5 h-5 text-primary" />
        </div>
        <h3 className="text-base font-semibold text-foreground mb-2">
          {feature.title}
        </h3>
        <p className="text-sm text-muted-foreground leading-relaxed">
          {feature.description}
        </p>
      </div>
    </div>
  );
}

export function Features() {
  return (
    <section id="features" className="relative py-20 sm:py-28">
      {/* Background accent */}
      <div className="absolute inset-0 -z-10">
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] bg-primary/5 rounded-full blur-3xl" />
      </div>

      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        {/* Section header */}
        <div className="text-center max-w-2xl mx-auto mb-16">
          <h2 className="text-3xl sm:text-4xl font-bold tracking-tight text-foreground mb-4">
            Everything you need.{" "}
            <span className="bg-gradient-to-r from-primary to-secondary bg-clip-text text-transparent">
              Nothing you don't.
            </span>
          </h2>
          <p className="text-lg text-muted-foreground leading-relaxed">
            A complete load balancing solution for any backend stack.
            Written in pure Go with zero dependencies, designed for modern cloud-native infrastructure.
          </p>
        </div>

        {/* Features grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 sm:gap-6">
          {features.map((feature, i) => (
            <FeatureCard key={feature.title} feature={feature} index={i} />
          ))}
        </div>
      </div>
    </section>
  );
}
