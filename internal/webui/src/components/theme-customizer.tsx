import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Slider } from '@/components/ui/slider'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import {
  Moon,
  Sun,
  Monitor,
  Palette,
  Type,
  Layout,
  Check,
  RotateCcw,
  Save
} from 'lucide-react'

interface ThemeSettings {
  mode: 'light' | 'dark' | 'system'
  primaryColor: string
  borderRadius: number
  fontSize: 'sm' | 'base' | 'lg'
  compactMode: boolean
  animations: boolean
  highContrast: boolean
}

const defaultSettings: ThemeSettings = {
  mode: 'system',
  primaryColor: '#3b82f6',
  borderRadius: 0.5,
  fontSize: 'base',
  compactMode: false,
  animations: true,
  highContrast: false
}

const colorOptions = [
  { name: 'Blue', value: '#3b82f6', class: 'bg-blue-500' },
  { name: 'Green', value: '#22c55e', class: 'bg-green-500' },
  { name: 'Purple', value: '#8b5cf6', class: 'bg-purple-500' },
  { name: 'Orange', value: '#f97316', class: 'bg-orange-500' },
  { name: 'Pink', value: '#ec4899', class: 'bg-pink-500' },
  { name: 'Red', value: '#ef4444', class: 'bg-red-500' },
  { name: 'Teal', value: '#14b8a6', class: 'bg-teal-500' },
  { name: 'Slate', value: '#64748b', class: 'bg-slate-500' }
]

export function ThemeCustomizer() {
  const [settings, setSettings] = useState<ThemeSettings>(defaultSettings)
  const [hasChanges, setHasChanges] = useState(false)

  useEffect(() => {
    const saved = localStorage.getItem('theme-settings')
    if (saved) {
      try {
        setSettings(JSON.parse(saved))
      } catch {
        setSettings(defaultSettings)
      }
    }
  }, [])

  const updateSetting = <K extends keyof ThemeSettings>(
    key: K,
    value: ThemeSettings[K]
  ) => {
    setSettings(prev => ({ ...prev, [key]: value }))
    setHasChanges(true)
  }

  const saveSettings = () => {
    localStorage.setItem('theme-settings', JSON.stringify(settings))
    applyTheme(settings)
    setHasChanges(false)
    toast.success('Theme settings saved')
  }

  const resetSettings = () => {
    setSettings(defaultSettings)
    setHasChanges(true)
    toast.info('Theme reset to defaults')
  }

  const applyTheme = (s: ThemeSettings) => {
    const root = document.documentElement
    root.style.setProperty('--primary', s.primaryColor)
    root.style.setProperty('--radius', `${s.borderRadius}rem`)

    // Apply font size
    const fontSizes = { sm: '14px', base: '16px', lg: '18px' }
    root.style.fontSize = fontSizes[s.fontSize]

    // Apply compact mode
    if (s.compactMode) {
      root.classList.add('compact')
    } else {
      root.classList.remove('compact')
    }

    // Apply high contrast
    if (s.highContrast) {
      root.classList.add('high-contrast')
    } else {
      root.classList.remove('high-contrast')
    }

    // Apply animations
    if (!s.animations) {
      root.classList.add('reduce-motion')
    } else {
      root.classList.remove('reduce-motion')
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Appearance</h1>
          <p className="text-muted-foreground">
            Customize the look and feel of your admin interface
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={resetSettings}>
            <RotateCcw className="mr-2 h-4 w-4" />
            Reset
          </Button>
          <Button onClick={saveSettings} disabled={!hasChanges}>
            <Save className="mr-2 h-4 w-4" />
            Save Changes
          </Button>
        </div>
      </div>

      <Tabs defaultValue="theme" className="space-y-4">
        <TabsList>
          <TabsTrigger value="theme">
            <Palette className="mr-2 h-4 w-4" />
            Theme
          </TabsTrigger>
          <TabsTrigger value="layout">
            <Layout className="mr-2 h-4 w-4" />
            Layout
          </TabsTrigger>
          <TabsTrigger value="typography">
            <Type className="mr-2 h-4 w-4" />
            Typography
          </TabsTrigger>
        </TabsList>

        <TabsContent value="theme" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Color Scheme</CardTitle>
              <CardDescription>Choose your preferred color mode</CardDescription>
            </CardHeader>
            <CardContent>
              <RadioGroup
                value={settings.mode}
                onValueChange={(v) => updateSetting('mode', v as ThemeSettings['mode'])}
                className="grid grid-cols-3 gap-4"
              >
                <div>
                  <RadioGroupItem value="light" id="light" className="peer sr-only" />
                  <Label
                    htmlFor="light"
                    className="flex flex-col items-center justify-between rounded-md border-2 border-muted bg-popover p-4 hover:bg-accent hover:text-accent-foreground peer-data-[state=checked]:border-primary [&:has([data-state=checked])]:border-primary"
                  >
                    <Sun className="mb-3 h-6 w-6" />
                    Light
                  </Label>
                </div>
                <div>
                  <RadioGroupItem value="dark" id="dark" className="peer sr-only" />
                  <Label
                    htmlFor="dark"
                    className="flex flex-col items-center justify-between rounded-md border-2 border-muted bg-popover p-4 hover:bg-accent hover:text-accent-foreground peer-data-[state=checked]:border-primary [&:has([data-state=checked])]:border-primary"
                  >
                    <Moon className="mb-3 h-6 w-6" />
                    Dark
                  </Label>
                </div>
                <div>
                  <RadioGroupItem value="system" id="system" className="peer sr-only" />
                  <Label
                    htmlFor="system"
                    className="flex flex-col items-center justify-between rounded-md border-2 border-muted bg-popover p-4 hover:bg-accent hover:text-accent-foreground peer-data-[state=checked]:border-primary [&:has([data-state=checked])]:border-primary"
                  >
                    <Monitor className="mb-3 h-6 w-6" />
                    System
                  </Label>
                </div>
              </RadioGroup>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Primary Color</CardTitle>
              <CardDescription>Choose your brand accent color</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-4 gap-4">
                {colorOptions.map((color) => (
                  <button
                    key={color.value}
                    onClick={() => updateSetting('primaryColor', color.value)}
                    className={cn(
                      'group relative flex h-12 w-full items-center justify-center rounded-md transition-all',
                      color.class,
                      settings.primaryColor === color.value && 'ring-2 ring-primary ring-offset-2'
                    )}
                  >
                    {settings.primaryColor === color.value && (
                      <Check className="h-5 w-5 text-white" />
                    )}
                    <span className="sr-only">{color.name}</span>
                  </button>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="layout" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Layout Options</CardTitle>
              <CardDescription>Adjust the layout density and spacing</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label>Border Radius</Label>
                  <span className="text-sm text-muted-foreground">{settings.borderRadius}rem</span>
                </div>
                <Slider
                  value={[settings.borderRadius * 100]}
                  onValueChange={([v]) => updateSetting('borderRadius', v / 100)}
                  min={0}
                  max={100}
                  step={5}
                />
              </div>

              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Compact Mode</Label>
                  <p className="text-sm text-muted-foreground">Reduce spacing for denser UI</p>
                </div>
                <Switch
                  checked={settings.compactMode}
                  onCheckedChange={(v) => updateSetting('compactMode', v)}
                />
              </div>

              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Animations</Label>
                  <p className="text-sm text-muted-foreground">Enable smooth transitions</p>
                </div>
                <Switch
                  checked={settings.animations}
                  onCheckedChange={(v) => updateSetting('animations', v)}
                />
              </div>

              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>High Contrast</Label>
                  <p className="text-sm text-muted-foreground">Increase contrast for accessibility</p>
                </div>
                <Switch
                  checked={settings.highContrast}
                  onCheckedChange={(v) => updateSetting('highContrast', v)}
                />
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="typography" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Font Size</CardTitle>
              <CardDescription>Adjust the base font size</CardDescription>
            </CardHeader>
            <CardContent>
              <RadioGroup
                value={settings.fontSize}
                onValueChange={(v) => updateSetting('fontSize', v as ThemeSettings['fontSize'])}
                className="grid grid-cols-3 gap-4"
              >
                <div>
                  <RadioGroupItem value="sm" id="sm" className="peer sr-only" />
                  <Label
                    htmlFor="sm"
                    className="flex flex-col items-center justify-center rounded-md border-2 border-muted bg-popover p-4 hover:bg-accent hover:text-accent-foreground peer-data-[state=checked]:border-primary [&:has([data-state=checked])]:border-primary"
                  >
                    <span className="text-sm">Small</span>
                    <span className="text-xs text-muted-foreground">14px</span>
                  </Label>
                </div>
                <div>
                  <RadioGroupItem value="base" id="base" className="peer sr-only" />
                  <Label
                    htmlFor="base"
                    className="flex flex-col items-center justify-center rounded-md border-2 border-muted bg-popover p-4 hover:bg-accent hover:text-accent-foreground peer-data-[state=checked]:border-primary [&:has([data-state=checked])]:border-primary"
                  >
                    <span className="text-base">Default</span>
                    <span className="text-xs text-muted-foreground">16px</span>
                  </Label>
                </div>
                <div>
                  <RadioGroupItem value="lg" id="lg" className="peer sr-only" />
                  <Label
                    htmlFor="lg"
                    className="flex flex-col items-center justify-center rounded-md border-2 border-muted bg-popover p-4 hover:bg-accent hover:text-accent-foreground peer-data-[state=checked]:border-primary [&:has([data-state=checked])]:border-primary"
                  >
                    <span className="text-lg">Large</span>
                    <span className="text-xs text-muted-foreground">18px</span>
                  </Label>
                </div>
              </RadioGroup>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Preview */}
      <Card className="border-primary/20">
        <CardHeader>
          <CardTitle>Preview</CardTitle>
          <CardDescription>See your changes in real-time</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap gap-2">
            <Button>Primary Button</Button>
            <Button variant="secondary">Secondary</Button>
            <Button variant="outline">Outline</Button>
            <Button variant="ghost">Ghost</Button>
            <Button variant="destructive">Destructive</Button>
          </div>
          <div className="flex items-center gap-2">
            <div className="h-4 w-4 rounded-full bg-primary" />
            <span className="text-sm text-muted-foreground">Primary Color</span>
          </div>
          <div className="rounded-lg border bg-card p-4">
            <p className="text-sm">
              This is a preview card with the current theme settings applied.
            </p>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
