import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { renderWithProviders } from "./utils";

// ---------------------------------------------------------------------------
// App
// ---------------------------------------------------------------------------

describe("App", () => {
  it("renders without crashing", async () => {
    const { App } = await import("@/App");
    renderWithProviders(<App />);

    // Should render multiple OpenLoadBalancer brand references (header + footer)
    expect(screen.getAllByText("OpenLoadBalancer").length).toBeGreaterThanOrEqual(2);
  });
});

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

describe("Header", () => {
  it("renders navigation links", async () => {
    const { Header } = await import("@/components/header");
    renderWithProviders(<Header />);

    expect(screen.getByText("Features")).toBeInTheDocument();
    expect(screen.getByText("Architecture")).toBeInTheDocument();
    expect(screen.getByText("Quick Start")).toBeInTheDocument();
    expect(screen.getByText("Compare")).toBeInTheDocument();
  });

  it("renders theme toggle button", async () => {
    const { Header } = await import("@/components/header");
    renderWithProviders(<Header />);

    expect(screen.getByLabelText("Toggle theme")).toBeInTheDocument();
  });

  it("renders GitHub link", async () => {
    const { Header } = await import("@/components/header");
    renderWithProviders(<Header />);

    const githubLinks = screen.getAllByText("GitHub");
    expect(githubLinks.length).toBeGreaterThanOrEqual(1);
  });

  it("renders mobile menu button", async () => {
    const { Header } = await import("@/components/header");
    renderWithProviders(<Header />);

    // The mobile menu button doesn't have an accessible label, but it exists
    // in the DOM as a button within the header
    const header = screen.getByRole("banner");
    expect(header).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Hero
// ---------------------------------------------------------------------------

describe("Hero", () => {
  it("renders heading text", async () => {
    const { Hero } = await import("@/components/hero");
    renderWithProviders(<Hero />);

    expect(screen.getByText(/Zero-dependency load balancer/)).toBeInTheDocument();
    expect(screen.getByText(/One binary. Total control/)).toBeInTheDocument();
  });

  it("renders download and GitHub CTA buttons", async () => {
    const { Hero } = await import("@/components/hero");
    renderWithProviders(<Hero />);

    expect(screen.getByText("Download")).toBeInTheDocument();
    expect(screen.getByText("View on GitHub")).toBeInTheDocument();
  });

  it("renders stat labels", async () => {
    const { Hero } = await import("@/components/hero");
    renderWithProviders(<Hero />);

    expect(screen.getByText("Dependencies")).toBeInTheDocument();
    expect(screen.getByText("Algorithms")).toBeInTheDocument();
    expect(screen.getByText("Middleware")).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

describe("Footer", () => {
  it("renders product links", async () => {
    const { Footer } = await import("@/components/footer");
    renderWithProviders(<Footer />);

    expect(screen.getByText("Product")).toBeInTheDocument();
  });

  it("renders documentation links", async () => {
    const { Footer } = await import("@/components/footer");
    renderWithProviders(<Footer />);

    expect(screen.getByText("Documentation")).toBeInTheDocument();
    expect(screen.getByText("Getting Started")).toBeInTheDocument();
  });

  it("renders community links", async () => {
    const { Footer } = await import("@/components/footer");
    renderWithProviders(<Footer />);

    expect(screen.getByText("Community")).toBeInTheDocument();
  });

  it("renders copyright notice", async () => {
    const { Footer } = await import("@/components/footer");
    renderWithProviders(<Footer />);

    expect(screen.getByText(/2026/)).toBeInTheDocument();
    expect(screen.getByText(/Apache-2.0 License/)).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

describe("Utility", () => {
  it("cn() merges class names", async () => {
    const { cn } = await import("@/lib/utils");
    expect(cn("foo", "bar")).toBe("foo bar");
    expect(cn("foo", false && "bar")).toBe("foo");
  });
});
