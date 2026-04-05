import { useState, useEffect, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import api from '@/lib/api'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import {
  Send,
  Play,
  Save,
  Download,
  Upload,
  Clock,
  CheckCircle,
  AlertCircle,
  Code,
  Trash2,
  Plus,
  FileJson,
  History,
  Copy,
  Check
} from 'lucide-react'

interface RequestHistory {
  id: string
  method: string
  url: string
  timestamp: Date
  status?: number
  duration?: number
}

interface ApiEndpoint {
  method: string
  path: string
  description: string
  params?: { name: string; type: string; required: boolean; description: string }[]
  body?: Record<string, unknown>
}

const predefinedEndpoints: ApiEndpoint[] = [
  {
    method: 'GET',
    path: '/api/v1/backends',
    description: 'List all backends'
  },
  {
    method: 'POST',
    path: '/api/v1/backends',
    description: 'Create a new backend',
    body: { name: '', address: '', weight: 1, pool: '' }
  },
  {
    method: 'GET',
    path: '/api/v1/pools',
    description: 'List all pools'
  },
  {
    method: 'POST',
    path: '/api/v1/pools',
    description: 'Create a new pool',
    body: { name: '', algorithm: 'round_robin', backends: [] }
  },
  {
    method: 'GET',
    path: '/api/v1/listeners',
    description: 'List all listeners'
  },
  {
    method: 'GET',
    path: '/api/v1/metrics',
    description: 'Get current metrics'
  },
  {
    method: 'POST',
    path: '/api/v1/config/reload',
    description: 'Reload configuration'
  },
  {
    method: 'GET',
    path: '/api/v1/health',
    description: 'Health check'
  }
]

const httpMethods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']

export function ApiPlayground() {
  const [method, setMethod] = useState('GET')
  const [url, setUrl] = useState('/api/v1/backends')
  const [headers, setHeaders] = useState('Content-Type: application/json')
  const [body, setBody] = useState('')
  const [response, setResponse] = useState<{
    status: number
    statusText: string
    duration: number
    data: unknown
    headers: Record<string, string>
  } | null>(null)
  const [loading, setLoading] = useState(false)
  const [history, setHistory] = useState<RequestHistory[]>([])
  const [savedRequests, setSavedRequests] = useState<{ name: string; method: string; url: string; headers: string; body: string }[]>([])
  const [showHistory, setShowHistory] = useState(true)
  const [copied, setCopied] = useState(false)

  // Load saved requests from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('api-playground-saved')
    if (saved) {
      setSavedRequests(JSON.parse(saved))
    }
    const hist = localStorage.getItem('api-playground-history')
    if (hist) {
      setHistory(JSON.parse(hist).map((h: RequestHistory) => ({
        ...h,
        timestamp: new Date(h.timestamp)
      })))
    }
  }, [])

  const executeRequest = useCallback(async () => {
    setLoading(true)
    const startTime = performance.now()

    try {
      const requestConfig: {
        method: string
        url: string
        headers?: Record<string, string>
        data?: unknown
      } = {
        method,
        url
      }

      // Parse headers
      const headerLines = headers.split('\n').filter(Boolean)
      const parsedHeaders: Record<string, string> = {}
      headerLines.forEach(line => {
        const [key, ...valueParts] = line.split(':')
        if (key && valueParts.length > 0) {
          parsedHeaders[key.trim()] = valueParts.join(':').trim()
        }
      })
      if (Object.keys(parsedHeaders).length > 0) {
        requestConfig.headers = parsedHeaders
      }

      // Add body for POST/PUT/PATCH
      if (['POST', 'PUT', 'PATCH'].includes(method) && body) {
        try {
          requestConfig.data = JSON.parse(body)
        } catch {
          requestConfig.data = body
        }
      }

      const result = await api.request(requestConfig)
      const duration = Math.round(performance.now() - startTime)

      setResponse({
        status: result.status,
        statusText: result.statusText,
        duration,
        data: result.data,
        headers: result.headers as Record<string, string>
      })

      // Add to history
      const newHistoryItem: RequestHistory = {
        id: Math.random().toString(36).substr(2, 9),
        method,
        url,
        timestamp: new Date(),
        status: result.status,
        duration
      }
      const updatedHistory = [newHistoryItem, ...history].slice(0, 50)
      setHistory(updatedHistory)
      localStorage.setItem('api-playground-history', JSON.stringify(updatedHistory))

      toast.success(`Request completed in ${duration}ms`)
    } catch (error: any) {
      const duration = Math.round(performance.now() - startTime)
      setResponse({
        status: error.response?.status || 0,
        statusText: error.response?.statusText || 'Error',
        duration,
        data: error.response?.data || error.message,
        headers: error.response?.headers || {}
      })
      toast.error('Request failed')
    } finally {
      setLoading(false)
    }
  }, [method, url, headers, body, history])

  const saveRequest = () => {
    const name = prompt('Enter a name for this request:')
    if (name) {
      const newSaved = [...savedRequests, { name, method, url, headers, body }]
      setSavedRequests(newSaved)
      localStorage.setItem('api-playground-saved', JSON.stringify(newSaved))
      toast.success('Request saved')
    }
  }

  const loadRequest = (saved: typeof savedRequests[0]) => {
    setMethod(saved.method)
    setUrl(saved.url)
    setHeaders(saved.headers)
    setBody(saved.body)
  }

  const deleteSaved = (index: number) => {
    const newSaved = savedRequests.filter((_, i) => i !== index)
    setSavedRequests(newSaved)
    localStorage.setItem('api-playground-saved', JSON.stringify(newSaved))
  }

  const loadEndpoint = (endpoint: ApiEndpoint) => {
    setMethod(endpoint.method)
    setUrl(endpoint.path)
    if (endpoint.body) {
      setBody(JSON.stringify(endpoint.body, null, 2))
    } else {
      setBody('')
    }
  }

  const copyResponse = () => {
    if (response) {
      navigator.clipboard.writeText(JSON.stringify(response.data, null, 2))
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  return (
    <div className="space-y-4">
      {/* Request Builder */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Code className="h-5 w-5" />
            API Playground
          </CardTitle>
          <CardDescription>Test API endpoints interactively</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* URL Bar */}
          <div className="flex gap-2">
            <Select value={method} onValueChange={setMethod} className="w-[100px]">
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {httpMethods.map(m => (
                  <SelectItem key={m} value={m}>{m}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="/api/v1/..."
              className="flex-1 font-mono"
            />
            <Button onClick={executeRequest} disabled={loading}>
              {loading ? (
                <div className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
              ) : (
                <Send className="mr-2 h-4 w-4" />
              )}
              Send
            </Button>
            <Button variant="outline" onClick={saveRequest}>
              <Save className="mr-2 h-4 w-4" />
              Save
            </Button>
          </div>

          <Tabs defaultValue="body">
            <TabsList>
              <TabsTrigger value="body">Body</TabsTrigger>
              <TabsTrigger value="headers">Headers</TabsTrigger>
            </TabsList>
            <TabsContent value="body" className="space-y-2">
              <Textarea
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder='{"key": "value"}'
                className="min-h-[150px] font-mono text-sm"
              />
              <p className="text-xs text-muted-foreground">JSON body for POST/PUT/PATCH requests</p>
            </TabsContent>
            <TabsContent value="headers" className="space-y-2">
              <Textarea
                value={headers}
                onChange={(e) => setHeaders(e.target.value)}
                placeholder="Header: Value"
                className="min-h-[150px] font-mono text-sm"
              />
              <p className="text-xs text-muted-foreground">One header per line</p>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-3">
        {/* Sidebar */}
        <div className="space-y-4">
          {/* Endpoints */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm">Endpoints</CardTitle>
            </CardHeader>
            <CardContent>
              <ScrollArea className="h-[200px]">
                <div className="space-y-1">
                  {predefinedEndpoints.map((endpoint) => (
                    <button
                      key={`${endpoint.method}-${endpoint.path}`}
                      onClick={() => loadEndpoint(endpoint)}
                      className="w-full flex items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-muted transition-colors"
                    >
                      <Badge
                        variant={
                          endpoint.method === 'GET'
                            ? 'default'
                            : endpoint.method === 'POST'
                            ? 'secondary'
                            : endpoint.method === 'DELETE'
                            ? 'destructive'
                            : 'outline'
                        }
                        className="w-[60px] justify-center text-xs"
                      >
                        {endpoint.method}
                      </Badge>
                      <span className="truncate text-muted-foreground">{endpoint.path}</span>
                    </button>
                  ))}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>

          {/* Saved Requests */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm">Saved Requests</CardTitle>
            </CardHeader>
            <CardContent>
              {savedRequests.length === 0 ? (
                <p className="text-sm text-muted-foreground">No saved requests</p>
              ) : (
                <ScrollArea className="h-[150px]">
                  <div className="space-y-1">
                    {savedRequests.map((saved, index) => (
                      <div
                        key={index}
                        className="flex items-center gap-2 rounded px-2 py-1.5 hover:bg-muted group"
                      >
                        <button
                          onClick={() => loadRequest(saved)}
                          className="flex-1 text-left text-sm truncate"
                        >
                          <Badge variant="outline" className="mr-2 text-xs">{saved.method}</Badge>
                          {saved.name}
                        </button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6 opacity-0 group-hover:opacity-100"
                          onClick={() => deleteSaved(index)}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              )}
            </CardContent>
          </Card>

          {/* History */}
          <Card>
            <CardHeader className="pb-3 flex flex-row items-center justify-between">
              <CardTitle className="text-sm">History</CardTitle>
              <Switch checked={showHistory} onCheckedChange={setShowHistory} />
            </CardHeader>
            {showHistory && (
              <CardContent>
                {history.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No requests yet</p>
                ) : (
                  <ScrollArea className="h-[150px]">
                    <div className="space-y-1">
                      {history.map((item) => (
                        <div
                          key={item.id}
                          className="flex items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-muted cursor-pointer"
                          onClick={() => {
                            setMethod(item.method)
                            setUrl(item.url)
                          }}
                        >
                          <Badge
                            variant={
                              item.status && item.status < 400 ? 'default' : 'destructive'
                            }
                            className="w-[40px] justify-center text-xs"
                          >
                            {item.method}
                          </Badge>
                          <span className="flex-1 truncate text-muted-foreground">{item.url}</span>
                          {item.duration && (
                            <span className="text-xs text-muted-foreground">{item.duration}ms</span>
                          )}
                        </div>
                      ))}
                    </div>
                  </ScrollArea>
                )}
              </CardContent>
            )}
          </Card>
        </div>

        {/* Response */}
        <Card className="lg:col-span-2">
          <CardHeader className="flex flex-row items-center justify-between">
            <div>
              <CardTitle>Response</CardTitle>
              <CardDescription>
                {response ? (
                  <span className="flex items-center gap-2">
                    <Badge
                      variant={response.status < 400 ? 'default' : 'destructive'}
                    >
                      {response.status} {response.statusText}
                    </Badge>
                    <span className="text-muted-foreground">
                      {response.duration}ms
                    </span>
                  </span>
                ) : (
                  'Send a request to see the response'
                )}
              </CardDescription>
            </div>
            {response && (
              <Button variant="outline" size="sm" onClick={copyResponse}>
                {copied ? <Check className="mr-2 h-4 w-4" /> : <Copy className="mr-2 h-4 w-4" />}
                {copied ? 'Copied' : 'Copy'}
              </Button>
            )}
          </CardHeader>
          <CardContent>
            {response ? (
              <Tabs defaultValue="body">
                <TabsList>
                  <TabsTrigger value="body">Body</TabsTrigger>
                  <TabsTrigger value="headers">Headers</TabsTrigger>
                </TabsList>
                <TabsContent value="body">
                  <ScrollArea className="h-[400px] rounded-md border bg-muted p-4">
                    <pre className="font-mono text-sm">
                      {JSON.stringify(response.data, null, 2)}
                    </pre>
                  </ScrollArea>
                </TabsContent>
                <TabsContent value="headers">
                  <ScrollArea className="h-[400px] rounded-md border bg-muted p-4">
                    <div className="space-y-1 font-mono text-sm">
                      {Object.entries(response.headers).map(([key, value]) => (
                        <div key={key}>
                          <span className="text-muted-foreground">{key}:</span> {value}
                        </div>
                      ))}
                    </div>
                  </ScrollArea>
                </TabsContent>
              </Tabs>
            ) : (
              <div className="flex h-[400px] flex-col items-center justify-center text-muted-foreground">
                <Code className="mb-4 h-12 w-12 opacity-20" />
                <p>No response yet</p>
                <p className="text-sm">Send a request to see the response here</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
