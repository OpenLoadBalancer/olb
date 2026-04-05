import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Lock,
  Plus,
  Trash2,
  CheckCircle,
  XCircle,
  AlertCircle,
  RefreshCw,
  Shield,
  FileText,
  Clock
} from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'

interface Certificate {
  id: string
  domain: string
  issuer: string
  issued_at: string
  expires_at: string
  status: 'active' | 'expired' | 'expiring_soon'
  auto_renew: boolean
  type: 'custom' | 'letsencrypt'
}

export function CertsPage() {
  const queryClient = useQueryClient()
  const [isAddOpen, setIsAddOpen] = useState(false)
  const [activeTab, setActiveTab] = useState('all')

  const { data: certificates = [], isLoading } = useQuery<Certificate[]>({
    queryKey: ['certificates'],
    queryFn: async () => {
      const response = await api.get('/api/v1/certificates')
      return response.data
    }
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/certificates/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
      toast.success('Certificate deleted')
    }
  })

  const renewMutation = useMutation({
    mutationFn: (id: string) => api.post(`/api/v1/certificates/${id}/renew`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
      toast.success('Certificate renewal initiated')
    }
  })

  const filteredCerts =
    activeTab === 'all'
      ? certificates
      : certificates.filter((c) => c.status === activeTab)

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'active':
        return (
          <Badge variant="success" className="flex items-center gap-1 w-fit">
            <CheckCircle className="h-3 w-3" />
            Active
          </Badge>
        )
      case 'expired':
        return (
          <Badge variant="destructive" className="flex items-center gap-1 w-fit">
            <XCircle className="h-3 w-3" />
            Expired
          </Badge>
        )
      case 'expiring_soon':
        return (
          <Badge variant="warning" className="flex items-center gap-1 w-fit">
            <AlertCircle className="h-3 w-3" />
            Expiring Soon
          </Badge>
        )
      default:
        return <Badge variant="secondary">{status}</Badge>
    }
  }

  const daysUntilExpiry = (expiresAt: string) => {
    const days = Math.ceil(
      (new Date(expiresAt).getTime() - Date.now()) / (1000 * 60 * 60 * 24)
    )
    return days
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Certificates</h1>
          <p className="text-muted-foreground">
            Manage SSL/TLS certificates for HTTPS listeners
          </p>
        </div>
        <Dialog open={isAddOpen} onOpenChange={setIsAddOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Certificate
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>Add Certificate</DialogTitle>
              <DialogDescription>
                Upload a custom certificate or request one from Let&apos;s Encrypt
              </DialogDescription>
            </DialogHeader>
            <Tabs defaultValue="upload" className="w-full">
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="upload">Upload</TabsTrigger>
                <TabsTrigger value="letsencrypt">Let&apos;s Encrypt</TabsTrigger>
              </TabsList>
              <TabsContent value="upload" className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="cert">Certificate (PEM)</Label>
                  <textarea
                    id="cert"
                    className="w-full h-32 rounded-md border border-input bg-background px-3 py-2 text-sm"
                    placeholder="-----BEGIN CERTIFICATE-----"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="key">Private Key (PEM)</Label>
                  <textarea
                    id="key"
                    className="w-full h-32 rounded-md border border-input bg-background px-3 py-2 text-sm"
                    placeholder="-----BEGIN PRIVATE KEY-----"
                  />
                </div>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setIsAddOpen(false)}>
                    Cancel
                  </Button>
                  <Button>Upload Certificate</Button>
                </DialogFooter>
              </TabsContent>
              <TabsContent value="letsencrypt" className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="domain">Domain</Label>
                  <Input id="domain" placeholder="example.com" />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="email">Email (for notifications)</Label>
                  <Input id="email" type="email" placeholder="admin@example.com" />
                </div>
                <div className="rounded-lg bg-muted p-4 text-sm">
                  <div className="flex items-start gap-2">
                    <Shield className="h-4 w-4 mt-0.5 text-green-500" />
                    <div>
                      <p className="font-medium">Auto-renewal enabled</p>
                      <p className="text-muted-foreground">
                        Certificates will be automatically renewed 30 days before expiry
                      </p>
                    </div>
                  </div>
                </div>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setIsAddOpen(false)}>
                    Cancel
                  </Button>
                  <Button>Request Certificate</Button>
                </DialogFooter>
              </TabsContent>
            </Tabs>
          </DialogContent>
        </Dialog>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="active">Active</TabsTrigger>
          <TabsTrigger value="expiring_soon">Expiring Soon</TabsTrigger>
          <TabsTrigger value="expired">Expired</TabsTrigger>
        </TabsList>
      </Tabs>

      <Card>
        <CardHeader>
          <CardTitle>Certificates</CardTitle>
          <CardDescription>
            {filteredCerts.length} certificate{filteredCerts.length !== 1 ? 's' : ''} found
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex h-32 items-center justify-center">
              <RefreshCw className="h-6 w-6 animate-spin" />
            </div>
          ) : filteredCerts.length === 0 ? (
            <div className="flex h-32 flex-col items-center justify-center text-center">
              <Lock className="h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-muted-foreground">No certificates found</p>
              <Button variant="outline" className="mt-4" onClick={() => setIsAddOpen(true)}>
                Add your first certificate
              </Button>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domain</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Issuer</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredCerts.map((cert) => {
                  const days = daysUntilExpiry(cert.expires_at)
                  return (
                    <TableRow key={cert.id}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Lock className="h-4 w-4 text-muted-foreground" />
                          <span className="font-medium">{cert.domain}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">
                          {cert.type === 'letsencrypt' ? "Let's Encrypt" : 'Custom'}
                        </Badge>
                      </TableCell>
                      <TableCell>{cert.issuer}</TableCell>
                      <TableCell>{getStatusBadge(cert.status)}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Clock className="h-4 w-4 text-muted-foreground" />
                          <span>
                            {days > 0 ? `${days} days` : 'Expired'}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          {cert.type === 'letsencrypt' && (
                            <Button
                              variant="ghost"
                              size="icon"
                              onClick={() => renewMutation.mutate(cert.id)}
                              disabled={renewMutation.isPending}
                            >
                              <RefreshCw className="h-4 w-4" />
                            </Button>
                          )}
                          <Button variant="ghost" size="icon">
                            <FileText className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => deleteMutation.mutate(cert.id)}
                            disabled={deleteMutation.isPending}
                          >
                            <Trash2 className="h-4 w-4 text-destructive" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
