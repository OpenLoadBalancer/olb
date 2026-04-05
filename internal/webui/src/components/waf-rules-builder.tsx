import { useState, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { DragDropContext, Droppable, Draggable } from '@hello-pangea/dnd'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'
import {
  Shield,
  Plus,
  Trash2,
  GripVertical,
  Copy,
  Play,
  AlertTriangle,
  Ban,
  Eye,
  Filter,
  Globe,
  Lock,
  Unlock,
  FileText,
  Download,
  Upload,
  Check,
  X
} from 'lucide-react'

interface WAFRule {
  id: string
  name: string
  description: string
  enabled: boolean
  priority: number
  conditions: WAFCondition[]
  action: 'block' | 'monitor' | 'challenge' | 'allow'
}

interface WAFCondition {
  id: string
  field: string
  operator: string
  value: string
  caseSensitive: boolean
}

const ruleTemplates = [
  {
    name: 'Block SQL Injection',
    description: 'Detect and block common SQL injection patterns',
    conditions: [
      { field: 'query_string', operator: 'contains', value: "' OR '" },
      { field: 'query_string', operator: 'contains', value: 'union select' }
    ],
    action: 'block' as const
  },
  {
    name: 'Rate Limit API',
    description: 'Limit API requests to 100/min per IP',
    conditions: [
      { field: 'path', operator: 'starts_with', value: '/api/' },
      { field: 'rate_limit', operator: 'exceeds', value: '100' }
    ],
    action: 'block' as const
  },
  {
    name: 'Block Bad Bots',
    description: 'Block known malicious user agents',
    conditions: [
      { field: 'user_agent', operator: 'regex', value: '(badbot|scraper|crawler)' }
    ],
    action: 'block' as const
  },
  {
    name: 'Geo Block',
    description: 'Block traffic from specific countries',
    conditions: [
      { field: 'country', operator: 'in', value: 'CN,RU,KP' }
    ],
    action: 'block' as const
  }
]

const fieldOptions = [
  { value: 'ip', label: 'Client IP' },
  { value: 'path', label: 'Path' },
  { value: 'query_string', label: 'Query String' },
  { value: 'method', label: 'HTTP Method' },
  { value: 'user_agent', label: 'User Agent' },
  { value: 'referer', label: 'Referer' },
  { value: 'header', label: 'Header' },
  { value: 'country', label: 'Country' },
  { value: 'rate_limit', label: 'Rate Limit' },
  { value: 'body', label: 'Request Body' }
]

const operatorOptions = [
  { value: 'equals', label: 'Equals' },
  { value: 'not_equals', label: 'Not Equals' },
  { value: 'contains', label: 'Contains' },
  { value: 'not_contains', label: 'Not Contains' },
  { value: 'starts_with', label: 'Starts With' },
  { value: 'ends_with', label: 'Ends With' },
  { value: 'regex', label: 'Matches Regex' },
  { value: 'in', label: 'In List' },
  { value: 'exceeds', label: 'Exceeds' }
]

const actionOptions = [
  { value: 'block', label: 'Block', color: 'bg-destructive', icon: Ban },
  { value: 'monitor', label: 'Monitor', color: 'bg-blue-500', icon: Eye },
  { value: 'challenge', label: 'Challenge', color: 'bg-amber-500', icon: Lock },
  { value: 'allow', label: 'Allow', color: 'bg-green-500', icon: Unlock }
]

function ConditionBuilder({
  condition,
  onChange,
  onDelete
}: {
  condition: WAFCondition
  onChange: (c: WAFCondition) => void
  onDelete: () => void
}) {
  return (
    <div className="flex items-start gap-2 rounded-lg border bg-card p-3">
      <GripVertical className="mt-2 h-4 w-4 text-muted-foreground cursor-grab" />
      <div className="flex-1 grid grid-cols-4 gap-2">
        <Select value={condition.field} onValueChange={(v) => onChange({ ...condition, field: v })}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {fieldOptions.map(f => (
              <SelectItem key={f.value} value={f.value}>{f.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={condition.operator} onValueChange={(v) => onChange({ ...condition, operator: v })}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {operatorOptions.map(o => (
              <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          value={condition.value}
          onChange={(e) => onChange({ ...condition, value: e.target.value })}
          placeholder="Value"
        />
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-2">
            <Switch
              id={`case-${condition.id}`}
              checked={condition.caseSensitive}
              onCheckedChange={(v) => onChange({ ...condition, caseSensitive: v })}
            />
            <Label htmlFor={`case-${condition.id}`} className="text-xs">Case</Label>
          </div>
        </div>
      </div>
      <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" onClick={onDelete}>
        <Trash2 className="h-4 w-4" />
      </Button>
    </div>
  )
}

function RuleCard({
  rule,
  onChange,
  onDelete,
  onDuplicate,
  isDragging
}: {
  rule: WAFRule
  onChange: (r: WAFRule) => void
  onDelete: () => void
  onDuplicate: () => void
  isDragging?: boolean
}) {
  const actionConfig = actionOptions.find(a => a.value === rule.action)
  const ActionIcon = actionConfig?.icon || Shield

  return (
    <Card className={cn(isDragging && 'opacity-50')}>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-2">
            <GripVertical className="h-5 w-5 text-muted-foreground cursor-grab" />
            <div>
              <div className="flex items-center gap-2">
                <Input
                  value={rule.name}
                  onChange={(e) => onChange({ ...rule, name: e.target.value })}
                  className="h-7 w-[200px] font-semibold"
                />
                <Switch
                  checked={rule.enabled}
                  onCheckedChange={(v) => onChange({ ...rule, enabled: v })}
                />
              </div>
              <Input
                value={rule.description}
                onChange={(e) => onChange({ ...rule, description: e.target.value })}
                className="mt-1 h-6 text-xs text-muted-foreground"
                placeholder="Description"
              />
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Badge className={cn(actionConfig?.color, 'text-white')}>
              <ActionIcon className="mr-1 h-3 w-3" />
              {actionConfig?.label}
            </Badge>
            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={onDuplicate}>
              <Copy className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={onDelete}>
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Conditions */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <Label className="text-xs text-muted-foreground">Conditions</Label>
            <Badge variant="outline" className="text-xs">
              {rule.conditions.length}
            </Badge>
          </div>
          <div className="space-y-2">
            {rule.conditions.map((condition, index) => (
              <ConditionBuilder
                key={condition.id}
                condition={condition}
                onChange={(c) => {
                  const newConditions = [...rule.conditions]
                  newConditions[index] = c
                  onChange({ ...rule, conditions: newConditions })
                }}
                onDelete={() => {
                  onChange({
                    ...rule,
                    conditions: rule.conditions.filter((_, i) => i !== index)
                  })
                }}
              />
            ))}
          </div>
          <Button
            variant="outline"
            size="sm"
            className="w-full"
            onClick={() => {
              onChange({
                ...rule,
                conditions: [
                  ...rule.conditions,
                  { id: Math.random().toString(36).substr(2, 9), field: 'path', operator: 'contains', value: '', caseSensitive: false }
                ]
              })
            }}
          >
            <Plus className="mr-2 h-4 w-4" />
            Add Condition
          </Button>
        </div>

        {/* Action Selector */}
        <div className="space-y-2">
          <Label className="text-xs text-muted-foreground">Action</Label>
          <div className="grid grid-cols-4 gap-2">
            {actionOptions.map((action) => {
              const Icon = action.icon
              return (
                <Button
                  key={action.value}
                  variant={rule.action === action.value ? 'default' : 'outline'}
                  size="sm"
                  className={cn(
                    'justify-start',
                    rule.action === action.value && action.color
                  )}
                  onClick={() => onChange({ ...rule, action: action.value as WAFRule['action'] })}
                >
                  <Icon className="mr-2 h-4 w-4" />
                  {action.label}
                </Button>
              )
            })}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

export function WAFRulesBuilder() {
  const [rules, setRules] = useState<WAFRule[]>([
    {
      id: '1',
      name: 'Block SQL Injection',
      description: 'Block common SQL injection attempts',
      enabled: true,
      priority: 1,
      action: 'block',
      conditions: [
        { id: 'c1', field: 'query_string', operator: 'contains', value: "' OR '", caseSensitive: false }
      ]
    }
  ])
  const [showTemplates, setShowTemplates] = useState(false)
  const [previewRule, setPreviewRule] = useState<WAFRule | null>(null)

  const handleDragEnd = (result: any) => {
    if (!result.destination) return
    const items = Array.from(rules)
    const [reorderedItem] = items.splice(result.source.index, 1)
    items.splice(result.destination.index, 0, reorderedItem)
    setRules(items.map((r, i) => ({ ...r, priority: i + 1 })))
  }

  const addRule = () => {
    const newRule: WAFRule = {
      id: Math.random().toString(36).substr(2, 9),
      name: 'New Rule',
      description: '',
      enabled: true,
      priority: rules.length + 1,
      action: 'block',
      conditions: []
    }
    setRules([...rules, newRule])
  }

  const loadTemplate = (template: typeof ruleTemplates[0]) => {
    const newRule: WAFRule = {
      id: Math.random().toString(36).substr(2, 9),
      name: template.name,
      description: template.description,
      enabled: true,
      priority: rules.length + 1,
      action: template.action,
      conditions: template.conditions.map((c, i) => ({
        id: `c${i}`,
        field: c.field,
        operator: c.operator,
        value: c.value,
        caseSensitive: false
      }))
    }
    setRules([...rules, newRule])
    setShowTemplates(false)
    toast.success('Template loaded')
  }

  const exportRules = () => {
    const data = JSON.stringify(rules, null, 2)
    const blob = new Blob([data], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'waf-rules.json'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">WAF Rules</h1>
          <p className="text-muted-foreground">
            Build and manage Web Application Firewall rules
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={exportRules}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
          <Button variant="outline" onClick={() => setShowTemplates(true)}>
            <FileText className="mr-2 h-4 w-4" />
            Templates
          </Button>
          <Button onClick={addRule}>
            <Plus className="mr-2 h-4 w-4" />
            Add Rule
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Total Rules</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{rules.length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Active</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">
              {rules.filter(r => r.enabled).length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Block Rules</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-destructive">
              {rules.filter(r => r.action === 'block').length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Monitor Rules</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-blue-500">
              {rules.filter(r => r.action === 'monitor').length}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Rules List */}
      <DragDropContext onDragEnd={handleDragEnd}>
        <Droppable droppableId="rules">
          {(provided) => (
            <div ref={provided.innerRef} {...provided.droppableProps} className="space-y-4">
              {rules.map((rule, index) => (
                <Draggable key={rule.id} draggableId={rule.id} index={index}>
                  {(provided, snapshot) => (
                    <div
                      ref={provided.innerRef}
                      {...provided.draggableProps}
                      {...provided.dragHandleProps}
                    >
                      <RuleCard
                        rule={rule}
                        onChange={(r) => {
                          const newRules = [...rules]
                          newRules[index] = r
                          setRules(newRules)
                        }}
                        onDelete={() => setRules(rules.filter((_, i) => i !== index))}
                        onDuplicate={() => {
                          const duplicate: WAFRule = {
                            ...rule,
                            id: Math.random().toString(36).substr(2, 9),
                            name: `${rule.name} (Copy)`,
                            priority: rules.length + 1
                          }
                          setRules([...rules, duplicate])
                        }}
                        isDragging={snapshot.isDragging}
                      />
                    </div>
                  )}
                </Draggable>
              ))}
              {provided.placeholder}
            </div>
          )}
        </Droppable>
      </DragDropContext>

      {/* Templates Dialog */}
      <Dialog open={showTemplates} onOpenChange={setShowTemplates}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Rule Templates</DialogTitle>
            <DialogDescription>Choose a template to quickly create a new rule</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 md:grid-cols-2">
            {ruleTemplates.map((template) => (
              <Card key={template.name} className="cursor-pointer hover:bg-muted" onClick={() => loadTemplate(template)}>
                <CardHeader className="pb-2">
                  <CardTitle className="text-base">{template.name}</CardTitle>
                  <CardDescription>{template.description}</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="space-y-1">
                    {template.conditions.map((c, i) => (
                      <div key={i} className="text-xs text-muted-foreground">
                        {c.field} {c.operator} "{c.value}"
                      </div>
                    ))}
                  </div>
                  <Badge className="mt-3" variant={template.action === 'block' ? 'destructive' : 'default'}>
                    {template.action}
                  </Badge>
                </CardContent>
              </Card>
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
