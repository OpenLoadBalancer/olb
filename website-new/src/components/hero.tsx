import { useEffect, useRef, useState } from "react";
import { ArrowRight, Github, Sparkles } from "lucide-react";
import { cn } from "@/lib/utils";

const stats = [
  { label: "Dependencies", value: 0, prefix: "", suffix: "" },
  { label: "p99 Latency", value: 1, prefix: "<", suffix: "ms" },
  { label: "Algorithms", value: 16, prefix: "", suffix: "" },
  { label: "Middleware", value: 16, prefix: "", suffix: "" },
  { label: "Packages Tested", value: 69, prefix: "", suffix: "" },
];

function AnimatedNumber({
  value,
  prefix,
  suffix,
  inView,
}: {
  value: number;
  prefix: string;
  suffix: string;
  inView: boolean;
}) {
  const [display, setDisplay] = useState(0);

  useEffect(() => {
    if (!inView) return;

    if (value === 0) {
      setDisplay(0);
      return;
    }

    let start = 0;
    const duration = 1200;
    const startTime = performance.now();

    function tick(now: number) {
      const elapsed = now - startTime;
      const progress = Math.min(elapsed / duration, 1);
      const eased = 1 - Math.pow(1 - progress, 3);
      const current = Math.round(eased * value);
      setDisplay(current);
      if (progress < 1) {
        requestAnimationFrame(tick);
      }
    }

    requestAnimationFrame(tick);
  }, [inView, value]);

  return (
    <span>
      {prefix}
      {display}
      {suffix}
    </span>
  );
}

export function Hero() {
  const statsRef = useRef<HTMLDivElement>(null);
  const [statsInView, setStatsInView] = useState(false);

  useEffect(() => {
    const el = statsRef.current;
    if (!el) return;

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setStatsInView(true);
          observer.disconnect();
        }
      },
      { threshold: 0.3 }
    );

    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return (
    <section className="relative overflow-hidden pt-32 pb-20 sm:pt-40 sm:pb-28">
      {/* Background effects */}
      <div className="absolute inset-0 -z-10">
        {/* Gradient orbs */}
        <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-primary/10 rounded-full blur-3xl animate-pulse" />
        <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-secondary/10 rounded-full blur-3xl animate-pulse [animation-delay:1s]" />

        {/* Grid pattern */}
        <div
          className="absolute inset-0 opacity-[0.03]"
          style={{
            backgroundImage:
              "linear-gradient(var(--color-muted-foreground) 1px, transparent 1px), linear-gradient(90deg, var(--color-muted-foreground) 1px, transparent 1px)",
            backgroundSize: "64px 64px",
          }}
        />

        {/* Top fade */}
        <div className="absolute inset-x-0 top-0 h-40 bg-gradient-to-b from-background to-transparent" />
      </div>

      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 text-center">
        {/* Badge */}
        <div className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full border border-border bg-muted/50 text-sm text-muted-foreground mb-8">
          <Sparkles className="w-3.5 h-3.5 text-primary" />
          <span>v1.0.0 is now available — Production-ready L4/L7 load balancer</span>
        </div>

        {/* Heading */}
        <h1 className="text-4xl sm:text-5xl md:text-6xl lg:text-7xl font-bold tracking-tight leading-[1.1] mb-6">
          <span className="text-foreground">Zero-dependency load balancer.</span>
          <br />
          <span className="bg-gradient-to-r from-primary via-accent to-cyan-400 bg-clip-text text-transparent">
            One binary. Total control.
          </span>
        </h1>

        {/* Subtitle */}
        <p className="max-w-2xl mx-auto text-lg sm:text-xl text-muted-foreground leading-relaxed mb-10">
          A high-performance L4/L7 load balancer for any backend — Node.js,
          Python, Java, Go, or anything that speaks HTTP/TCP. 16 algorithms,
          6-layer WAF, Raft clustering, and a real-time dashboard — all in a
          single binary with zero external dependencies. Written in pure Go.
        </p>

        {/* CTA Buttons */}
        <div className="flex flex-col sm:flex-row items-center justify-center gap-4 mb-16">
          <a
            href="https://github.com/openloadbalancer/olb/releases"
            target="_blank"
            rel="noopener noreferrer"
            className="group inline-flex items-center gap-2 px-6 py-3 rounded-xl bg-gradient-to-r from-primary to-secondary text-white font-medium text-sm shadow-lg shadow-primary/25 hover:shadow-primary/40 transition-all duration-300 hover:scale-[1.02]"
          >
            Download
            <ArrowRight className="w-4 h-4 transition-transform group-hover:translate-x-0.5" />
          </a>
          <a
            href="https://github.com/openloadbalancer/olb"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 px-6 py-3 rounded-xl border border-border text-foreground font-medium text-sm hover:bg-muted transition-all duration-300 hover:scale-[1.02]"
          >
            <Github className="w-4 h-4" />
            View on GitHub
          </a>
        </div>

        {/* Stats bar */}
        <div
          ref={statsRef}
          className="max-w-4xl mx-auto grid grid-cols-2 sm:grid-cols-3 md:grid-cols-5 gap-4 sm:gap-6"
        >
          {stats.map((stat) => (
            <div
              key={stat.label}
              className="flex flex-col items-center gap-1 p-4 rounded-xl bg-card/50 border border-border backdrop-blur-sm"
            >
              <span className="text-2xl sm:text-3xl font-bold bg-gradient-to-r from-primary to-secondary bg-clip-text text-transparent">
                <AnimatedNumber
                  value={stat.value}
                  prefix={stat.prefix}
                  suffix={stat.suffix}
                  inView={statsInView}
                />
              </span>
              <span className="text-xs sm:text-sm text-muted-foreground whitespace-nowrap">
                {stat.label}
              </span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
