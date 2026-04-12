import * as React from "react"
import { Monitor, Moon, Sun } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"

type Theme = "dark" | "light" | "system"

const ThemeProviderContext = React.createContext<{
  theme: Theme
  setTheme: (theme: Theme) => void
} | undefined>(undefined)

export function ThemeProvider({
  children,
  defaultTheme = "system",
  storageKey = "olb-theme",
}: {
  children: React.ReactNode
  defaultTheme?: Theme
  storageKey?: string
}) {
  const [theme, setTheme] = React.useState<Theme>(
    () => (localStorage.getItem(storageKey) as Theme) || defaultTheme
  )

  React.useEffect(() => {
    const root = window.document.documentElement
    root.classList.remove("light", "dark")
    if (theme === "system") {
      const systemTheme = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
      root.classList.add(systemTheme)
    } else {
      root.classList.add(theme)
    }
  }, [theme])

  return (
    <ThemeProviderContext.Provider value={{ theme, setTheme: (t) => { localStorage.setItem(storageKey, t); setTheme(t); } }}>
      {children}
    </ThemeProviderContext.Provider>
  )
}

export const useTheme = () => {
  const context = React.useContext(ThemeProviderContext)
  if (!context) throw new Error("useTheme must be used within ThemeProvider")
  return context
}

const themeIcons: Record<Exclude<Theme, "system">, React.ReactNode> = {
  light: <Sun className="h-4 w-4" aria-hidden="true" />,
  dark: <Moon className="h-4 w-4" aria-hidden="true" />,
}

export function ThemeToggle({ className }: { className?: string }) {
  const { theme, setTheme } = useTheme()
  const activeTheme = theme === "system"
    ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
    : theme

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className={cn("size-9", className)} aria-label="Switch theme">
          {themeIcons[activeTheme]}
          <span className="sr-only">Switch theme</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => setTheme("light")}>
          <Sun className="mr-2 h-4 w-4" aria-hidden="true" />
          <span>Light</span>
          {theme === "light" && <CheckIcon className="ml-auto h-4 w-4" />}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => setTheme("dark")}>
          <Moon className="mr-2 h-4 w-4" aria-hidden="true" />
          <span>Dark</span>
          {theme === "dark" && <CheckIcon className="ml-auto h-4 w-4" />}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => setTheme("system")}>
          <Monitor className="mr-2 h-4 w-4" aria-hidden="true" />
          <span>System</span>
          {theme === "system" && <CheckIcon className="ml-auto h-4 w-4" />}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function CheckIcon({ className }: { className: string }) {
  return (
    <svg className={className} xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <polyline points="20 6 9 17 4 12" />
    </svg>
  )
}
