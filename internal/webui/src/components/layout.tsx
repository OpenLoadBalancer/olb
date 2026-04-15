import { useState, useEffect, useRef, useCallback } from "react"
import { Menu, Server, ChevronRight, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ThemeToggle } from "@/components/theme-provider"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { navItems } from "@/lib/nav"
import { cn } from "@/lib/utils"
import { Link, useLocation } from "react-router"

export function Layout({ children }: { children: React.ReactNode }) {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const location = useLocation()
  const sidebarRef = useRef<HTMLElement>(null)
  const openButtonRef = useRef<HTMLButtonElement>(null)
  const previouslyFocusedElement = useRef<HTMLElement | null>(null)

  // Close sidebar on route change (mobile)
  useEffect(() => { setSidebarOpen(false) }, [location.pathname])

  // Focus trap and restore for mobile sidebar
  useEffect(() => {
    if (!sidebarOpen) return

    // Save the currently focused element so we can restore it on close
    previouslyFocusedElement.current = document.activeElement as HTMLElement

    // Move focus into the sidebar
    const sidebar = sidebarRef.current
    if (sidebar) {
      // Focus the first focusable element (close button)
      const firstFocusable = sidebar.querySelector<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
      )
      firstFocusable?.focus()
    }

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setSidebarOpen(false)
        return
      }

      if (e.key === "Tab" && sidebar) {
        const focusable = sidebar.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
        )
        if (focusable.length === 0) return

        const first = focusable[0]
        const last = focusable[focusable.length - 1]

        if (e.shiftKey) {
          // Shift+Tab: if focus is on first element, wrap to last
          if (document.activeElement === first && last) {
            e.preventDefault()
            last.focus()
          }
        } else {
          // Tab: if focus is on last element, wrap to first
          if (document.activeElement === last && first) {
            e.preventDefault()
            first.focus()
          }
        }
      }
    }

    document.addEventListener("keydown", handleKeyDown)
    return () => document.removeEventListener("keydown", handleKeyDown)
  }, [sidebarOpen])

  // Restore focus when sidebar closes
  useEffect(() => {
    if (!sidebarOpen && previouslyFocusedElement.current) {
      previouslyFocusedElement.current.focus()
      previouslyFocusedElement.current = null
    }
  }, [sidebarOpen])

  const closeSidebar = useCallback(() => setSidebarOpen(false), [])

  return (
    <div className="min-h-screen bg-background">
      {/* Skip to content link — accessible navigation */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:top-4 focus:left-4 focus:z-[100] focus:rounded-md focus:bg-primary focus:px-4 focus:py-2 focus:text-primary-foreground focus:outline-none focus:ring-2 focus:ring-ring"
      >
        Skip to content
      </a>

      {/* aria-live region for screen reader status announcements */}
      <div aria-live="polite" aria-atomic="true" className="sr-only" id="a11y-status" />

      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 lg:hidden"
          onClick={closeSidebar}
          aria-hidden="true"
        />
      )}

      <aside
        ref={sidebarRef}
        id="sidebar-nav"
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
            onClick={closeSidebar}
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
            ref={openButtonRef}
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
            <Tooltip>
              <TooltipTrigger asChild>
                <ThemeToggle />
              </TooltipTrigger>
              <TooltipContent>Switch theme</TooltipContent>
            </Tooltip>
          </div>
        </header>
        <main id="main-content" role="main" className="p-4 lg:p-6" tabIndex={-1}>
          {children}
        </main>
      </div>
    </div>
  )
}
