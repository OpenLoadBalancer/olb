import { useState, useEffect, useRef } from "react"
import { Menu, Server, ChevronRight, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ThemeToggle } from "@/components/theme-provider"
import { navItems } from "@/lib/nav"
import { cn } from "@/lib/utils"
import { Link, useLocation } from "react-router"

export function Layout({ children }: { children: React.ReactNode }) {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const location = useLocation()
  const sidebarRef = useRef<HTMLElement>(null)
  const mainRef = useRef<HTMLDivElement>(null)

  useEffect(() => { setSidebarOpen(false) }, [location.pathname])

  // Focus management: trap focus in sidebar when open on mobile
  useEffect(() => {
    if (sidebarOpen && sidebarRef.current) {
      const firstLink = sidebarRef.current.querySelector<HTMLElement>("a")
      firstLink?.focus()
    }
  }, [sidebarOpen])

  // Close sidebar on Escape
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && sidebarOpen) {
        setSidebarOpen(false)
      }
    }
    document.addEventListener("keydown", handleKeyDown)
    return () => document.removeEventListener("keydown", handleKeyDown)
  }, [sidebarOpen])

  return (
    <div className="min-h-screen bg-background">
      {/* Skip to content link */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:top-4 focus:left-4 focus:z-[100] focus:rounded-md focus:bg-primary focus:px-4 focus:py-2 focus:text-primary-foreground focus:outline-none focus:ring-2 focus:ring-ring"
      >
        Skip to content
      </a>

      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 lg:hidden"
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}

      <aside
        ref={sidebarRef}
        role="navigation"
        aria-label="Main navigation"
        className={cn(
          "fixed top-0 left-0 z-50 h-full w-64 border-r bg-card transition-transform duration-200 ease-in-out lg:translate-x-0",
          sidebarOpen ? "translate-x-0" : "-translate-x-full"
        )}
      >
        <div className="flex h-14 items-center justify-between border-b px-4">
          <div className="flex items-center">
            <Server className="mr-2 h-6 w-6 text-primary" aria-hidden="true" />
            <span className="font-semibold">OpenLoadBalancer</span>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            onClick={() => setSidebarOpen(false)}
            aria-label="Close navigation menu"
          >
            <X className="h-5 w-5" />
          </Button>
        </div>
        <nav className="space-y-1 p-2" aria-label="Primary">
          {navItems.map((item) => (
            <Link
              key={item.href}
              to={item.href}
              aria-current={location.pathname === item.href ? "page" : undefined}
              className={cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                location.pathname === item.href
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )}
            >
              <item.icon className="h-4 w-4" aria-hidden="true" />
              {item.title}
              {location.pathname === item.href && <ChevronRight className="ml-auto h-4 w-4" aria-hidden="true" />}
            </Link>
          ))}
        </nav>
      </aside>

      <div className="lg:pl-64">
        <header role="banner" className="sticky top-0 z-30 flex h-14 items-center gap-4 border-b bg-background px-4 lg:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            onClick={() => setSidebarOpen(true)}
            aria-label="Open navigation menu"
            aria-expanded={sidebarOpen}
            aria-controls="sidebar-nav"
          >
            <Menu className="h-5 w-5" />
          </Button>
          <div className="flex flex-1 items-center justify-end gap-2">
            <ThemeToggle />
          </div>
        </header>
        <main id="main-content" ref={mainRef} role="main" className="p-4 lg:p-6" tabIndex={-1}>
          {children}
        </main>
      </div>
    </div>
  )
}
