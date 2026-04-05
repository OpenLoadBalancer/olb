import { useState } from 'react'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Progress } from '@/components/ui/progress'
import { cn } from '@/lib/utils'
import {
  ChevronRight,
  ChevronLeft,
  Check,
  Server,
  Layers,
  Route,
  Shield,
  Settings,
  AlertCircle,
  Sparkles,
  Copy,
  Download
} from 'lucide-react'
import { toast } from 'sonner'

interface WizardStep {
  id: string
  title: string
  description: string
  icon: React.ElementType
  component: React.ComponentType<WizardStepProps>
}

interface WizardStepProps {
  data: WizardData
  onChange: (data: Partial<WizardData>) => void
  errors: Record<string, string>
}

interface WizardData {
  // Step 1: Backend
  backendName: string
  backendAddress: string
  backendWeight: number
  backendHealthCheck: boolean
  backendHealthPath: string

  // Step 2: Pool
  poolName: string
  poolAlgorithm: string
  poolHealthCheck: boolean

  // Step 3: Listener
  listenerName: string
  listenerPort: number
  listenerProtocol: string
  listenerTls: boolean

  // Step 4: Route
  routePath: string
  routeMethods: string[]
  routeStripPath: boolean

  // Step 5: WAF
  wafEnabled: boolean
  wafMode: string
  wafRateLimit: boolean
}

const initialData: WizardData = {
  backendName: '',
  backendAddress: '',
  backendWeight: 1,
  backendHealthCheck: true,
  backendHealthPath: '/health',
  poolName: '',
  poolAlgorithm: 'round_robin',
  poolHealthCheck: true,
  listenerName: '',
  listenerPort: 80,
  listenerProtocol: 'http',
  listenerTls: false,
  routePath: '/',
  routeMethods: ['GET', 'POST'],
  routeStripPath: false,
  wafEnabled: false,
  wafMode: 'monitor',
  wafRateLimit: true
}

// Step 1: Backend Configuration
function BackendStep({ data, onChange, errors }: WizardStepProps) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="backendName">Backend Name *</Label>
        <Input
          id="backendName"
          value={data.backendName}
          onChange={(e) => onChange({ backendName: e.target.value })}
          placeholder="e.g., web-server-01"
          className={cn(errors.backendName && 'border-destructive')}
        />
        {errors.backendName && (
          <p className="text-sm text-destructive">{errors.backendName}</p>
        )}
      </div>
      <div className="space-y-2">
        <Label htmlFor="backendAddress">Backend Address *</Label>
        <Input
          id="backendAddress"
          value={data.backendAddress}
          onChange={(e) => onChange({ backendAddress: e.target.value })}
          placeholder="e.g., 192.168.1.10:8080"
          className={cn(errors.backendAddress && 'border-destructive')}
        />
        {errors.backendAddress && (
          <p className="text-sm text-destructive">{errors.backendAddress}</p>
        )}
      </div>
      <div className="space-y-2">
        <Label htmlFor="backendWeight">Weight</Label>
        <Input
          id="backendWeight"
          type="number"
          min={1}
          max={100}
          value={data.backendWeight}
          onChange={(e) => onChange({ backendWeight: parseInt(e.target.value) })}
        />
      </div>
      <div className="flex items-center space-x-2">
        <Switch
          id="backendHealthCheck"
          checked={data.backendHealthCheck}
          onCheckedChange={(checked) => onChange({ backendHealthCheck: checked })}
        />
        <Label htmlFor="backendHealthCheck">Enable Health Checks</Label>
      </div>
      {data.backendHealthCheck && (
        <div className="space-y-2 pl-6">
          <Label htmlFor="backendHealthPath">Health Check Path</Label>
          <Input
            id="backendHealthPath"
            value={data.backendHealthPath}
            onChange={(e) => onChange({ backendHealthPath: e.target.value })}
            placeholder="/health"
          />
        </div>
      )}
    </div>
  )
}

// Step 2: Pool Configuration
function PoolStep({ data, onChange }: WizardStepProps) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="poolName">Pool Name *</Label>
        <Input
          id="poolName"
          value={data.poolName}
          onChange={(e) => onChange({ poolName: e.target.value })}
          placeholder="e.g., web-pool"
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="poolAlgorithm">Load Balancing Algorithm</Label>
        <Select
          value={data.poolAlgorithm}
          onValueChange={(value) => onChange({ poolAlgorithm: value })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="round_robin">Round Robin</SelectItem>
            <SelectItem value="weighted_round_robin">Weighted Round Robin</SelectItem>
            <SelectItem value="least_connections">Least Connections</SelectItem>
            <SelectItem value="least_response_time">Least Response Time</SelectItem>
            <SelectItem value="ip_hash">IP Hash</SelectItem>
            <SelectItem value="random">Random</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <Alert>
        <AlertCircle className="h-4 w-4" />
        <AlertDescription>
          The backend "{data.backendName}" will be automatically added to this pool.
        </AlertDescription>
      </Alert>
    </div>
  )
}

// Step 3: Listener Configuration
function ListenerStep({ data, onChange }: WizardStepProps) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="listenerName">Listener Name *</Label>
        <Input
          id="listenerName"
          value={data.listenerName}
          onChange={(e) => onChange({ listenerName: e.target.value })}
          placeholder="e.g., http-listener"
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="listenerPort">Port *</Label>
        <Input
          id="listenerPort"
          type="number"
          min={1}
          max={65535}
          value={data.listenerPort}
          onChange={(e) => onChange({ listenerPort: parseInt(e.target.value) })}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="listenerProtocol">Protocol</Label>
        <Select
          value={data.listenerProtocol}
          onValueChange={(value) => onChange({ listenerProtocol: value })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="http">HTTP</SelectItem>
            <SelectItem value="https">HTTPS</SelectItem>
            <SelectItem value="tcp">TCP</SelectItem>
            <SelectItem value="udp">UDP</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="flex items-center space-x-2">
        <Switch
          id="listenerTls"
          checked={data.listenerTls}
          onCheckedChange={(checked) => onChange({ listenerTls: checked })}
        />
        <Label htmlFor="listenerTls">Enable TLS</Label>
      </div>
    </div>
  )
}

// Step 4: Route Configuration
function RouteStep({ data, onChange }: WizardStepProps) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="routePath">Path Pattern *</Label>
        <Input
          id="routePath"
          value={data.routePath}
          onChange={(e) => onChange({ routePath: e.target.value })}
          placeholder="e.g., /api/*"
        />
      </div>
      <div className="space-y-2">
        <Label>HTTP Methods</Label>
        <div className="flex gap-2">
          {['GET', 'POST', 'PUT', 'DELETE', 'PATCH'].map((method) => (
            <Badge
              key={method}
              variant={data.routeMethods.includes(method) ? 'default' : 'outline'}
              className="cursor-pointer"
              onClick={() => {
                const methods = data.routeMethods.includes(method)
                  ? data.routeMethods.filter((m) => m !== method)
                  : [...data.routeMethods, method]
                onChange({ routeMethods: methods })
              }}
            >
              {method}
            </Badge>
          ))}
        </div>
      </div>
      <div className="flex items-center space-x-2">
        <Switch
          id="routeStripPath"
          checked={data.routeStripPath}
          onCheckedChange={(checked) => onChange({ routeStripPath: checked })}
        />
        <Label htmlFor="routeStripPath">Strip Path Prefix</Label>
      </div>
    </div>
  )
}

// Step 5: WAF Configuration
function WAFStep({ data, onChange }: WizardStepProps) {
  return (
    <div className="space-y-4">
      <div className="flex items-center space-x-2">
        <Switch
          id="wafEnabled"
          checked={data.wafEnabled}
          onCheckedChange={(checked) => onChange({ wafEnabled: checked })}
        />
        <Label htmlFor="wafEnabled">Enable WAF</Label>
      </div>
      {data.wafEnabled && (
        <>
          <div className="space-y-2">
            <Label htmlFor="wafMode">WAF Mode</Label>
            <Select
              value={data.wafMode}
              onValueChange={(value) => onChange({ wafMode: value })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="monitor">Monitor Only</SelectItem>
                <SelectItem value="enforce">Enforce</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-center space-x-2">
            <Switch
              id="wafRateLimit"
              checked={data.wafRateLimit}
              onCheckedChange={(checked) => onChange({ wafRateLimit: checked })}
            />
            <Label htmlFor="wafRateLimit">Enable Rate Limiting</Label>
          </div>
        </>
      )}
    </div>
  )
}

// Step 6: Review
function ReviewStep({ data }: WizardStepProps) {
  const config = {
    backends: [
      {
        name: data.backendName,
        address: data.backendAddress,
        weight: data.backendWeight,
        health_checks: data.backendHealthCheck
          ? { path: data.backendHealthPath, interval: '10s' }
          : undefined
      }
    ],
    pools: [
      {
        name: data.poolName,
        algorithm: data.poolAlgorithm,
        backends: [data.backendName]
      }
    ],
    listeners: [
      {
        name: data.listenerName,
        address: `:${data.listenerPort}`,
        protocol: data.listenerProtocol,
        tls: data.listenerTls ? {} : undefined,
        routes: [
          {
            path: data.routePath,
            methods: data.routeMethods,
            pool: data.poolName,
            strip_path: data.routeStripPath
          }
        ]
      }
    ],
    waf: data.wafEnabled
      ? {
          enabled: true,
          mode: data.wafMode,
          rate_limit: data.wafRateLimit
        }
      : undefined
  }

  const configYaml = JSON.stringify(config, null, 2)

  const handleCopy = () => {
    navigator.clipboard.writeText(configYaml)
    toast.success('Configuration copied to clipboard')
  }

  const handleDownload = () => {
    const blob = new Blob([configYaml], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'openlb-config.json'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-4">
      <Alert className="bg-green-500/10 text-green-500 border-green-500/20">
        <Check className="h-4 w-4" />
        <AlertDescription>
          Configuration is complete and ready to deploy.
        </AlertDescription>
      </Alert>
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label>Generated Configuration</Label>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={handleCopy}>
              <Copy className="mr-2 h-4 w-4" />
              Copy
            </Button>
            <Button variant="outline" size="sm" onClick={handleDownload}>
              <Download className="mr-2 h-4 w-4" />
              Download
            </Button>
          </div>
        </div>
        <pre className="rounded-md bg-muted p-4 text-xs overflow-auto max-h-[300px]">
          {configYaml}
        </pre>
      </div>
    </div>
  )
}

const steps: WizardStep[] = [
  {
    id: 'backend',
    title: 'Backend',
    description: 'Configure your backend server',
    icon: Server,
    component: BackendStep
  },
  {
    id: 'pool',
    title: 'Pool',
    description: 'Set up the backend pool',
    icon: Layers,
    component: PoolStep
  },
  {
    id: 'listener',
    title: 'Listener',
    description: 'Configure the listener',
    icon: Settings,
    component: ListenerStep
  },
  {
    id: 'route',
    title: 'Route',
    description: 'Define routing rules',
    icon: Route,
    component: RouteStep
  },
  {
    id: 'waf',
    title: 'WAF',
    description: 'Configure security',
    icon: Shield,
    component: WAFStep
  },
  {
    id: 'review',
    title: 'Review',
    description: 'Review and deploy',
    icon: Sparkles,
    component: ReviewStep
  }
]

export function ConfigWizard() {
  const [currentStep, setCurrentStep] = useState(0)
  const [data, setData] = useState<WizardData>(initialData)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [isSubmitting, setIsSubmitting] = useState(false)

  const validateStep = (step: number): boolean => {
    const newErrors: Record<string, string> = {}

    switch (step) {
      case 0:
        if (!data.backendName.trim()) newErrors.backendName = 'Backend name is required'
        if (!data.backendAddress.trim()) newErrors.backendAddress = 'Backend address is required'
        break
      case 1:
        if (!data.poolName.trim()) newErrors.poolName = 'Pool name is required'
        break
      case 2:
        if (!data.listenerName.trim()) newErrors.listenerName = 'Listener name is required'
        break
      case 3:
        if (!data.routePath.trim()) newErrors.routePath = 'Route path is required'
        break
    }

    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }

  const handleNext = () => {
    if (validateStep(currentStep)) {
      setCurrentStep((prev) => Math.min(prev + 1, steps.length - 1))
    }
  }

  const handleBack = () => {
    setCurrentStep((prev) => Math.max(prev - 1, 0))
    setErrors({})
  }

  const handleSubmit = async () => {
    setIsSubmitting(true)
    // Simulate API call
    await new Promise((resolve) => setTimeout(resolve, 2000))
    toast.success('Configuration deployed successfully!')
    setIsSubmitting(false)
  }

  const CurrentStepComponent = steps[currentStep].component
  const progress = ((currentStep + 1) / steps.length) * 100

  return (
    <Card className="max-w-2xl mx-auto">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Sparkles className="h-5 w-5" />
          Quick Setup Wizard
        </CardTitle>
        <CardDescription>
          Follow these steps to configure your load balancer
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Progress */}
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Step {currentStep + 1} of {steps.length}</span>
            <span className="font-medium">{steps[currentStep].title}</span>
          </div>
          <Progress value={progress} className="h-2" />
        </div>

        {/* Step indicators */}
        <div className="flex items-center justify-between">
          {steps.map((step, index) => {
            const Icon = step.icon
            const isActive = index === currentStep
            const isCompleted = index < currentStep

            return (
              <div
                key={step.id}
                className={cn(
                  'flex flex-col items-center gap-2',
                  isActive && 'text-primary',
                  isCompleted && 'text-green-500',
                  !isActive && !isCompleted && 'text-muted-foreground'
                )}
              >
                <div
                  className={cn(
                    'flex h-10 w-10 items-center justify-center rounded-full border-2 transition-colors',
                    isActive && 'border-primary bg-primary/10',
                    isCompleted && 'border-green-500 bg-green-500/10',
                    !isActive && !isCompleted && 'border-muted'
                  )}
                >
                  {isCompleted ? (
                    <Check className="h-5 w-5" />
                  ) : (
                    <Icon className="h-5 w-5" />
                  )}
                </div>
                <span className="text-xs font-medium hidden sm:block">{step.title}</span>
              </div>
            )
          })}
        </div>

        <Separator />

        {/* Step content */}
        <div className="min-h-[300px]">
          <CurrentStepComponent
            data={data}
            onChange={(newData) => setData((prev) => ({ ...prev, ...newData }))}
            errors={errors}
          />
        </div>
      </CardContent>
      <CardFooter className="flex justify-between">
        <Button
          variant="outline"
          onClick={handleBack}
          disabled={currentStep === 0}
        >
          <ChevronLeft className="mr-2 h-4 w-4" />
          Back
        </Button>
        {currentStep === steps.length - 1 ? (
          <Button onClick={handleSubmit} disabled={isSubmitting}>
            {isSubmitting ? (
              <>
                <div className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                Deploying...
              </>
            ) : (
              <>
                <Sparkles className="mr-2 h-4 w-4" />
                Deploy Configuration
              </>
            )}
          </Button>
        ) : (
          <Button onClick={handleNext}>
            Next
            <ChevronRight className="ml-2 h-4 w-4" />
          </Button>
        )}
      </CardFooter>
    </Card>
  )
}
