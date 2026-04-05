import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator
} from '@/components/ui/command'
import {
  LayoutDashboard,
  Server,
  Layers,
  Route,
  Radio,
  Settings,
  Shield,
  FileText,
  Award,
  Moon,
  Sun,
  LogOut,
  User,
  Bell,
  Search,
  Keyboard
} from 'lucide-react'
import { useNavigate, useLocation } from 'react-router'
import { useTheme } from '@/providers/theme-provider'

interface CommandPaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

interface CommandItemType {
  id: string
  name: string
  icon: React.ElementType
  shortcut?: string
  action: () => void
  section: string
}

export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const { theme, setTheme } = useTheme()
  const [search, setSearch] = useState('')

  const navigationItems: CommandItemType[] = useMemo(
    () => [
      {
        id: 'dashboard',
        name: 'Dashboard',
        icon: LayoutDashboard,
        shortcut: 'G D',
        action: () => {
          navigate('/')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'backends',
        name: 'Backends',
        icon: Server,
        shortcut: 'G B',
        action: () => {
          navigate('/backends')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'pools',
        name: 'Pools',
        icon: Layers,
        shortcut: 'G P',
        action: () => {
          navigate('/pools')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'routes',
        name: 'Routes',
        icon: Route,
        shortcut: 'G R',
        action: () => {
          navigate('/routes')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'listeners',
        name: 'Listeners',
        icon: Radio,
        shortcut: 'G L',
        action: () => {
          navigate('/listeners')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'middleware',
        name: 'Middleware',
        icon: Settings,
        action: () => {
          navigate('/middleware')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'waf',
        name: 'WAF Security',
        icon: Shield,
        action: () => {
          navigate('/waf')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'certs',
        name: 'Certificates',
        icon: Certificate,
        action: () => {
          navigate('/certs')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'logs',
        name: 'Logs & Analytics',
        icon: FileText,
        action: () => {
          navigate('/logs')
          onOpenChange(false)
        },
        section: 'Navigation'
      },
      {
        id: 'settings',
        name: 'Settings',
        icon: Settings,
        shortcut: 'G S',
        action: () => {
          navigate('/settings')
          onOpenChange(false)
        },
        section: 'Navigation'
      }
    ],
    [navigate, onOpenChange]
  )

  const actionItems: CommandItemType[] = useMemo(
    () => [
      {
        id: 'toggle-theme',
        name: theme === 'dark' ? 'Switch to Light Theme' : 'Switch to Dark Theme',
        icon: theme === 'dark' ? Sun : Moon,
        shortcut: 'Ctrl+Shift+D',
        action: () => {
          setTheme(theme === 'dark' ? 'light' : 'dark')
          onOpenChange(false)
        },
        section: 'Actions'
      },
      {
        id: 'notifications',
        name: 'Open Notifications',
        icon: Bell,
        shortcut: 'Ctrl+Shift+N',
        action: () => {
          // Trigger notifications panel
          onOpenChange(false)
        },
        section: 'Actions'
      },
      {
        id: 'shortcuts',
        name: 'Keyboard Shortcuts',
        icon: Keyboard,
        shortcut: '?',
        action: () => {
          // Show shortcuts help
          onOpenChange(false)
        },
        section: 'Actions'
      },
      {
        id: 'search',
        name: 'Search',
        icon: Search,
        shortcut: 'Ctrl+F',
        action: () => {
          // Trigger search
          onOpenChange(false)
        },
        section: 'Actions'
      },
      {
        id: 'logout',
        name: 'Logout',
        icon: LogOut,
        action: () => {
          navigate('/login')
          onOpenChange(false)
        },
        section: 'Actions'
      }
    ],
    [theme, setTheme, navigate, onOpenChange]
  )

  const allItems = useMemo(() => [...navigationItems, ...actionItems], [navigationItems, actionItems])

  // Keyboard shortcut handlers
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Ctrl+K or Cmd+K opens command palette
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        onOpenChange(true)
        return
      }

      // ? opens command palette
      if (e.key === '?' && !open) {
        e.preventDefault()
        onOpenChange(true)
        return
      }

      // Navigation shortcuts (G + letter)
      if (!open && e.key === 'g') {
        const handler = (ev: KeyboardEvent) => {
          const item = navigationItems.find(i => i.shortcut === `G ${ev.key.toUpperCase()}`)
          if (item) {
            ev.preventDefault()
            item.action()
          }
          window.removeEventListener('keydown', handler)
        }
        window.addEventListener('keydown', handler, { once: true })
        setTimeout(() => window.removeEventListener('keydown', handler), 1000)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [open, onOpenChange, navigationItems])

  const filteredItems = useMemo(() => {
    if (!search) return allItems
    const lowerSearch = search.toLowerCase()
    return allItems.filter(
      item =>
        item.name.toLowerCase().includes(lowerSearch) ||
        item.section.toLowerCase().includes(lowerSearch)
    )
  }, [search, allItems])

  const groupedItems = useMemo(() => {
    const groups: Record<string, CommandItemType[]> = {}
    filteredItems.forEach(item => {
      if (!groups[item.section]) groups[item.section] = []
      groups[item.section].push(item)
    })
    return groups
  }, [filteredItems])

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange}>
      <CommandInput
        placeholder="Type a command or search..."
        value={search}
        onValueChange={setSearch}
      />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>
        {Object.entries(groupedItems).map(([section, items], index) => (
          <div key={section}>
            {index > 0 && <CommandSeparator />}
            <CommandGroup heading={section}>
              {items.map(item => (
                <CommandItem key={item.id} onSelect={item.action} className="cursor-pointer">
                  <item.icon className="mr-2 h-4 w-4" />
                  <span>{item.name}</span>
                  {item.shortcut && (
                    <div className="ml-auto flex gap-1">
                      {item.shortcut.split(' ').map((key, i) => (
                        <kbd
                          key={i}
                          className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono"
                        >
                          {key}
                        </kbd>
                      ))}
                    </div>
                  )}
                </CommandItem>
              ))}
            </CommandGroup>
          </div>
        ))}
      </CommandList>
    </CommandDialog>
  )
}

// Hook to use command palette
export function useCommandPalette() {
  const [open, setOpen] = useState(false)
  return { open, setOpen }
}
