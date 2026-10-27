import { ArrowRight } from "lucide-react";
import { Github } from "./github-icon";
import { cn } from "@/lib/utils";

export function CTA() {
  return (
    <section className="relative py-20 sm:py-28">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div
          className={cn(
            "relative overflow-hidden rounded-3xl",
            "bg-gradient-to-br from-primary/10 via-secondary/5 to-accent/10",
            "border border-primary/20"
          )}
        >
          {/* Background decoration */}
          <div className="absolute inset-0 -z-10">
            <div className="absolute top-0 left-1/4 w-96 h-96 bg-primary/10 rounded-full blur-3xl" />
            <div className="absolute bottom-0 right-1/4 w-96 h-96 bg-secondary/10 rounded-full blur-3xl" />
          </div>

          {/* Gradient border shimmer */}
          <div className="absolute inset-0 rounded-3xl bg-gradient-to-r from-primary/20 via-secondary/20 to-accent/20 opacity-50 blur-sm -z-10" />

          <div className="relative px-6 py-16 sm:px-12 sm:py-20 lg:px-20 lg:py-24 text-center">
            <h2 className="text-3xl sm:text-4xl lg:text-5xl font-bold tracking-tight text-foreground mb-4">
              Ready to balance{" "}
              <span className="bg-gradient-to-r from-primary via-accent to-secondary bg-clip-text text-transparent">
                your traffic?
              </span>
            </h2>

            <p className="max-w-xl mx-auto text-lg text-muted-foreground leading-relaxed mb-10">
              Get started with OpenLoadBalancer in under 5 minutes. One binary,
              zero configuration headaches.
            </p>

            <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
              <a
                href="https://github.com/openloadbalancer/olb/releases"
                target="_blank"
                rel="noopener noreferrer"
                className="group inline-flex items-center gap-2 px-8 py-3.5 rounded-xl bg-gradient-to-r from-primary to-secondary text-white font-medium text-sm shadow-lg shadow-primary/25 hover:shadow-primary/40 transition-all duration-300 hover:scale-[1.02]"
              >
                Get Started
                <ArrowRight className="w-4 h-4 transition-transform group-hover:translate-x-0.5" />
              </a>
              <a
                href="https://github.com/openloadbalancer/olb"
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-2 px-8 py-3.5 rounded-xl border border-border text-foreground font-medium text-sm hover:bg-muted/50 transition-all duration-300 hover:scale-[1.02] backdrop-blur-sm"
              >
                <Github className="w-4 h-4" />
                Star on GitHub
              </a>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
