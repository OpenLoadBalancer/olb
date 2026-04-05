import { useTheme } from '@/providers/theme-provider'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { NotificationCenter, useNotifications } from '@/components/notifications'
import { CommandPalette, useCommandPalette } from '@/components/command-palette'
import { Moon, Sun, User, LogOut, Settings, Search, Command } from 'lucide-react'

export function Header() {
  const { theme, setTheme } = useTheme()
  const { notifications, markAsRead, markAllAsRead, dismiss, clearAll } = useNotifications()
  const { open, setOpen } = useCommandPalette()

  return (
    <>
      <CommandPalette open={open} onOpenChange={setOpen} />
      <header className="flex h-16 items-center justify-between border-b bg-card px-6">
        {/* Left side - Breadcrumbs */}
        <div className="hidden lg:block">
          <Breadcrumbs />
        </div>
        <div className="lg:hidden" />

        {/* Right side */}
        <div className="flex items-center gap-4">
          {/* Search Trigger */}
          <Button
            variant="outline"
            className="relative h-9 w-9 justify-start rounded-md bg-muted/50 text-sm font-normal shadow-none hover:bg-accent hover:text-accent-foreground md:w-64 md:pr-12"
            onClick={() => setOpen(true)}
          >
            <Search className="h-4 w-4 md:mr-2" />
            <span className="hidden md:inline-flex">Search...</span>
            <kbd className="pointer-events-none absolute right-1.5 top-1.5 hidden h-6 select-none items-center gap-1 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium opacity-100 md:flex">
              <span className="text-xs">Ctrl</span>K
            </kbd>
          </Button>

          {/* Theme Toggle */}
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            className="h-9 w-9"
          >
            {theme === 'dark' ? (
              <Sun className="h-5 w-5" />
            ) : (
              <Moon className="h-5 w-5" />
            )}
          </Button>

          {/* Notifications */}
          <NotificationCenter
            notifications={notifications}
            onMarkAsRead={markAsRead}
            onMarkAllAsRead={markAllAsRead}
            onDismiss={dismiss}
            onClearAll={clearAll}
          />

          {/* User Menu */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="relative h-9 w-9 rounded-full">
                <Avatar className="h-9 w-9">
                  <AvatarImage src="" alt="Admin" />
                  <AvatarFallback>AD</AvatarFallback>
                </Avatar>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="w-56" align="end" forceMount>
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col space-y-1">
                  <p className="text-sm font-medium">Admin</p>
                  <p className="text-xs text-muted-foreground">admin@openloadbalancer.dev</p>
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem>
                <User className="mr-2 h-4 w-4" />
                <span>Profile</span>
              </DropdownMenuItem>
              <DropdownMenuItem>
                <Settings className="mr-2 h-4 w-4" />
                <span>Settings</span>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem className="text-destructive">
                <LogOut className="mr-2 h-4 w-4" />
                <span>Log out</span>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>
    </>
  )
}
