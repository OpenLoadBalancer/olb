import { useState, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { DataTable } from '@/components/data-table'
import api from '@/lib/api'
import { toast } from 'sonner'
import { cn, formatDate, formatDistanceToNow } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  Certificate,
  Plus,
  Trash2,
  RefreshCw,
  Download,
  Copy,
  Check,
  AlertTriangle,
  Lock,
  Globe,
  Calendar,
  Shield,
  Upload,
  Eye,
  EyeOff,
  FileText
} from 'lucide-react'

interface CertificateData {
  id: string
  domain: string
  issuer: string
  validFrom: Date
  validUntil: Date
  fingerprint: string
  algorithm: string
  keySize: number
  san: string[]
  autoRenew: boolean
  status: 'active' | 'expiring' | 'expired' | 'revoked'
}

// Mock data
const mockCertificates: CertificateData[] = [
  {
    id: '1',
    domain: 'api.example.com',
    issuer: "Let's Encrypt",
    validFrom: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000),
    validUntil: new Date(Date.now() + 60 * 24 * 60 * 60 * 1000),
    fingerprint: 'SHA256:abcd1234...',
    algorithm: 'RSA',
    keySize: 2048,
    san: ['api.example.com', '*.api.example.com'],
    autoRenew: true,
    status: 'active'
  },
  {
    id: '2',
    domain: 'app.example.com',
    issuer: "Let's Encrypt",
    validFrom: new Date(Date.now() - 60 * 24 * 60 * 60 * 1000),
    validUntil: new Date(Date.now() + 5 * 24 * 60 * 60 * 1000),
    fingerprint: 'SHA256:efgh5678...',
    algorithm: 'RSA',
    keySize: 4096,
    san: ['app.example.com'],
    autoRenew: true,
    status: 'expiring'
  },
  {
    id: '3',
    domain: 'legacy.example.com',
    issuer: 'Self-Signed',
    validFrom: new Date(Date.now() - 400 * 24 * 60 * 60 * 1000),
    validUntil: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000),
    fingerprint: 'SHA256:ijkl9012...',
    algorithm: 'RSA',
    keySize: 2048,
    san: ['legacy.example.com'],
    autoRenew: false,
    status: 'expired'
  }
]

const columns: ColumnDef<CertificateData>[] = [
  {
    accessorKey: 'domain',
    header: 'Domain',
    cell: ({ row }) => {
      const cert = row.original
      return (
        <div className="flex items-center gap-2">
          <Lock className={cn(
            'h-4 w-4',
            cert.status === 'active' && 'text-green-500',
            cert.status === 'expiring' && 'text-amber-500',
            cert.status === 'expired' && 'text-destructive'
          )} />
          <div>
            <div className="font-medium">{cert.domain}</div>
            <div className="text-xs text-muted-foreground">{cert.issuer}</div>
          </div>
        </div>
      )
    }
  },
  {
    accessorKey: 'validUntil',
    header: 'Expires',
    cell: ({ row }) => {
      const cert = row.original
      const daysUntil = Math.ceil((cert.validUntil.getTime() - Date.now()) / (1000 * 60 * 60 * 24))
      return (
        <div>
          <div className={cn(
            daysUntil < 7 && 'text-destructive',
            daysUntil < 30 && daysUntil >= 7 && 'text-amber-500'
          )}>
            {formatDistanceToNow(cert.validUntil)}
          </div>
          <div className="text-xs text-muted-foreground">
            {formatDate(cert.validUntil)}
          </div>
        </div>
      )
    }
  },
  {
    accessorKey: 'algorithm',
    header: 'Algorithm',
    cell: ({ row }) => (
      <Badge variant="outline">{row.original.algorithm}-{row.original.keySize}</Badge>
    )
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => {
      const status = row.original.status
      const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; label: string }> = {
        active: { variant: 'default', label: 'Active' },
        expiring: { variant: 'secondary', label: 'Expiring Soon' },
        expired: { variant: 'destructive', label: 'Expired' },
        revoked: { variant: 'destructive', label: 'Revoked' }
      }
      const config = variants[status]
      return <Badge variant={config.variant}>{config.label}</Badge>
    }
  },
  {
    id: 'actions',
    header: '',
    cell: ({ row }) => <CertificateActions certificate={row.original} />
  }
]

function CertificateActions({ certificate }: { certificate: CertificateData }) {
  const queryClient = useQueryClient()
  const [showDetails, setShowDetails] = useState(false)

  const { mutate: renew } = useMutation({
    mutationFn: async () => {
      await api.post(`/api/v1/certificates/${certificate.id}/renew`)
    },
    onSuccess: () => {
      toast.success('Certificate renewal initiated')
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
    }
  })

  const { mutate: deleteCert } = useMutation({
    mutationFn: async () => {
      await api.delete(`/api/v1/certificates/${certificate.id}`)
    },
    onSuccess: () => {
      toast.success('Certificate deleted')
      queryClient.invalidateQueries({ queryKey: ['certificates'] })
    }
  })

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success('Copied to clipboard')
  }

  return (
    <div className="flex items-center gap-2">
      <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => setShowDetails(true)}>
        <Eye className="h-4 w-4" />
      </Button>
      {certificate.status === 'expiring' && (
        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => renew()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      )}
      <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive" onClick={() => deleteCert()}>
        <Trash2 className="h-4 w-4" />
      </Button>

      <Dialog open={showDetails} onOpenChange={setShowDetails}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Certificate Details</DialogTitle>
            <DialogDescription>{certificate.domain}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <Label className="text-muted-foreground">Issuer</Label>
                <p className="font-medium">{certificate.issuer}</p>
              </div>
              <div>
                <Label className="text-muted-foreground">Algorithm</Label>
                <p className="font-medium">{certificate.algorithm}-{certificate.keySize}</p>
              </div>
              <div>
                <Label className="text-muted-foreground">Valid From</Label>
                <p className="font-medium">{formatDate(certificate.validFrom)}</p>
              </div>
              <div>
                <Label className="text-muted-foreground">Valid Until</Label>
                <p className="font-medium">{formatDate(certificate.validUntil)}</p>
              </div>
            </div>
            <div>
              <Label className="text-muted-foreground">Fingerprint</Label>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded bg-muted p-2 text-sm">{certificate.fingerprint}</code>
                <Button variant="outline" size="icon" className="h-8 w-8" onClick={() => handleCopy(certificate.fingerprint)}>
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
            </div>
            <div>
              <Label className="text-muted-foreground">Subject Alternative Names</Label>
              <div className="flex flex-wrap gap-2 mt-1">
                {certificate.san.map((name) => (
                  <Badge key={name} variant="secondary">{name}</Badge>
                ))}
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// CSR Generator Dialog
function CSRGeneratorDialog() {
  const [open, setOpen] = useState(false)
  const [domain, setDomain] = useState('')
  const [country, setCountry] = useState('')
  const [organization, setOrganization] = useState('')
  const [keySize, setKeySize] = useState('2048')
  const [generated, setGenerated] = useState<{ csr: string; privateKey: string } | null>(null)
  const [showPrivateKey, setShowPrivateKey] = useState(false)

  const handleGenerate = async () => {
    // Simulate CSR generation
    await new Promise(resolve => setTimeout(resolve, 1500))
    setGenerated({
      csr: `-----BEGIN CERTIFICATE REQUEST-----
MIICiDCCAXACAQAwQzELMAkGA1UEBhMCVVMxEzARBgNVBAgMClNvbWUtU3RhdGUx
ITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDCBnzANBgkqhkiG9w0B
AQEFAAOBjQAwgYkCgYEA...
-----END CERTIFICATE REQUEST-----`,
      privateKey: `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7...`
    })
    toast.success('CSR generated successfully')
  }

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success('Copied to clipboard')
  }

  const handleDownload = (content: string, filename: string) => {
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline">
          <FileText className="mr-2 h-4 w-4" />
          Generate CSR
        </Button>
      </DialogTrigger>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Generate Certificate Signing Request</DialogTitle>
          <DialogDescription>Create a new CSR for your domain</DialogDescription>
        </DialogHeader>
        {!generated ? (
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="csr-domain">Domain *</Label>
              <Input
                id="csr-domain"
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder="e.g., api.example.com"
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="csr-country">Country Code</Label>
                <Input
                  id="csr-country"
                  value={country}
                  onChange={(e) => setCountry(e.target.value)}
                  placeholder="US"
                  maxLength={2}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="csr-keysize">Key Size</Label>
                <Select value={keySize} onValueChange={setKeySize}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="2048">2048-bit</SelectItem>
                    <SelectItem value="4096">4096-bit</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="csr-org">Organization</Label>
              <Input
                id="csr-org"
                value={organization}
                onChange={(e) => setOrganization(e.target.value)}
                placeholder="My Company Inc."
              />
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Certificate Signing Request (CSR)</Label>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => handleCopy(generated.csr)}>
                    <Copy className="mr-2 h-4 w-4" />
                    Copy
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => handleDownload(generated.csr, `${domain}.csr`)}>
                    <Download className="mr-2 h-4 w-4" />
                    Download
                  </Button>
                </div>
              </div>
              <textarea
                readOnly
                value={generated.csr}
                className="h-32 w-full rounded-md border bg-muted p-2 font-mono text-xs"
              />
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Private Key (keep secure!)</Label>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => setShowPrivateKey(!showPrivateKey)}>
                    {showPrivateKey ? <EyeOff className="mr-2 h-4 w-4" /> : <Eye className="mr-2 h-4 w-4" />}
                    {showPrivateKey ? 'Hide' : 'Show'}
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => handleDownload(generated.privateKey, `${domain}.key`)}>
                    <Download className="mr-2 h-4 w-4" />
                    Download
                  </Button>
                </div>
              </div>
              <textarea
                readOnly
                value={showPrivateKey ? generated.privateKey : '•'.repeat(100)}
                className="h-32 w-full rounded-md border bg-muted p-2 font-mono text-xs"
              />
            </div>
          </div>
        )}
        <DialogFooter>
          {!generated ? (
            <Button onClick={handleGenerate} disabled={!domain}>
              Generate CSR
            </Button>
          ) : (
            <Button variant="outline" onClick={() => { setGenerated(null); setShowPrivateKey(false); }}>
              Generate Another
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Upload Certificate Dialog
function UploadCertificateDialog() {
  const [open, setOpen] = useState(false)
  const [certData, setCertData] = useState('')
  const [keyData, setKeyData] = useState('')
  const [autoRenew, setAutoRenew] = useState(true)

  const handleUpload = async () => {
    // Simulate upload
    await new Promise(resolve => setTimeout(resolve, 1000))
    toast.success('Certificate uploaded successfully')
    setOpen(false)
    setCertData('')
    setKeyData('')
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Upload className="mr-2 h-4 w-4" />
          Upload Certificate
        </Button>
      </DialogTrigger>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Upload Certificate</DialogTitle>
          <DialogDescription>Upload your SSL certificate and private key</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="cert-data">Certificate (PEM format)</Label>
            <textarea
              id="cert-data"
              value={certData}
              onChange={(e) => setCertData(e.target.value)}
              placeholder="-----BEGIN CERTIFICATE-----\n..."
              className="h-32 w-full rounded-md border bg-background p-2 font-mono text-xs"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="key-data">Private Key</Label>
            <textarea
              id="key-data"
              value={keyData}
              onChange={(e) => setKeyData(e.target.value)}
              placeholder="-----BEGIN PRIVATE KEY-----\n..."
              className="h-32 w-full rounded-md border bg-background p-2 font-mono text-xs"
            />
          </div>
          <div className="flex items-center space-x-2">
            <Switch
              id="auto-renew"
              checked={autoRenew}
              onCheckedChange={setAutoRenew}
            />
            <Label htmlFor="auto-renew">Enable auto-renewal</Label>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={handleUpload} disabled={!certData || !keyData}>
            Upload Certificate
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function CertificateManager() {
  const { data: certificates = mockCertificates, isLoading } = useQuery<CertificateData[]>({
    queryKey: ['certificates'],
    queryFn: async () => {
      const response = await api.get('/api/v1/certificates')
      return response.data
    }
  })

  // Calculate stats
  const stats = {
    total: certificates.length,
    active: certificates.filter(c => c.status === 'active').length,
    expiring: certificates.filter(c => c.status === 'expiring').length,
    expired: certificates.filter(c => c.status === 'expired').length
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Certificates</h1>
          <p className="text-muted-foreground">
            Manage SSL/TLS certificates for your listeners
          </p>
        </div>
        <div className="flex items-center gap-2">
          <CSRGeneratorDialog />
          <UploadCertificateDialog />
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Total Certificates</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.total}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Active</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{stats.active}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Expiring Soon</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-amber-500">{stats.expiring}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Expired</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-destructive">{stats.expired}</div>
          </CardContent>
        </Card>
      </div>

      {/* Tabs */}
      <Tabs defaultValue="all">
        <TabsList>
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="active">Active</TabsTrigger>
          <TabsTrigger value="expiring">Expiring</TabsTrigger>
          <TabsTrigger value="expired">Expired</TabsTrigger>
        </TabsList>
        <TabsContent value="all" className="mt-6">
          <DataTable
            data={certificates}
            columns={columns}
            isLoading={isLoading}
            searchPlaceholder="Search certificates..."
            emptyMessage="No certificates found"
          />
        </TabsContent>
        <TabsContent value="active" className="mt-6">
          <DataTable
            data={certificates.filter(c => c.status === 'active')}
            columns={columns}
            isLoading={isLoading}
            searchPlaceholder="Search certificates..."
            emptyMessage="No active certificates"
          />
        </TabsContent>
        <TabsContent value="expiring" className="mt-6">
          <DataTable
            data={certificates.filter(c => c.status === 'expiring')}
            columns={columns}
            isLoading={isLoading}
            searchPlaceholder="Search certificates..."
            emptyMessage="No expiring certificates"
          />
        </TabsContent>
        <TabsContent value="expired" className="mt-6">
          <DataTable
            data={certificates.filter(c => c.status === 'expired')}
            columns={columns}
            isLoading={isLoading}
            searchPlaceholder="Search certificates..."
            emptyMessage="No expired certificates"
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
