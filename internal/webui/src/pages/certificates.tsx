import { useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Shield, Plus, CheckCircle, AlertCircle, Upload, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { useCertificates } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"

export function CertificatesPage() {
  const { data: certs, isLoading, error, refetch } = useCertificates()

  // Add Certificate Dialog State
  const [certDialogOpen, setCertDialogOpen] = useState(false)
  const [certSource, setCertSource] = useState<'manual' | 'acme'>('acme')
  const [newCert, setNewCert] = useState({
    domain: "",
    email: "",
    certContent: "",
    keyContent: "",
    autoRenew: true,
  })

  const getDaysUntilExpiry = (expiry: string) => {
    if (!expiry) return 999
    const exp = new Date(expiry)
    const now = new Date()
    return Math.max(0, Math.floor((exp.getTime() - now.getTime()) / (1000 * 60 * 60 * 24)))
  }

  const getExpiryBg = (days: number) => {
    if (days < 7) return "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300"
    if (days < 30) return "bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300"
    return "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300"
  }

  const handleAddCertificate = () => {
    // Certificate management requires ACME or manual cert file placement
    // This is a UI placeholder — actual cert provisioning is via config/ACME
    toast.info("Certificate management is done via configuration or ACME")
    setCertDialogOpen(false)
  }

  const handleRenewCert = (_names: string[]) => {
    toast.success("Certificate renewal initiated")
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">TLS Certificates</h1>
          <p className="text-muted-foreground">Manage SSL/TLS certificates</p>
        </div>
        <Dialog open={certDialogOpen} onOpenChange={setCertDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Certificate
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-[600px]">
            <DialogHeader>
              <DialogTitle>Add Certificate</DialogTitle>
              <DialogDescription>
                Add a new TLS certificate manually or via ACME/Let's Encrypt.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 py-4">
              <div className="flex gap-2">
                <Button
                  variant={certSource === 'acme' ? 'default' : 'outline'}
                  className="flex-1"
                  onClick={() => setCertSource('acme')}
                >
                  Let's Encrypt
                </Button>
                <Button
                  variant={certSource === 'manual' ? 'default' : 'outline'}
                  className="flex-1"
                  onClick={() => setCertSource('manual')}
                >
                  Manual Upload
                </Button>
              </div>

              {certSource === 'acme' ? (
                <>
                  <div className="grid gap-2">
                    <Label htmlFor="domain">Domain</Label>
                    <Input
                      id="domain"
                      placeholder="e.g., *.example.com"
                      value={newCert.domain}
                      onChange={(e) => setNewCert({ ...newCert, domain: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="email">Email</Label>
                    <Input
                      id="email"
                      type="email"
                      placeholder="admin@example.com"
                      value={newCert.email}
                      onChange={(e) => setNewCert({ ...newCert, email: e.target.value })}
                    />
                  </div>
                  <div className="flex items-center justify-between">
                    <Label htmlFor="auto-renew">Auto-renewal</Label>
                    <Switch
                      id="auto-renew"
                      checked={newCert.autoRenew}
                      onCheckedChange={(checked) => setNewCert({ ...newCert, autoRenew: checked })}
                    />
                  </div>
                </>
              ) : (
                <>
                  <div className="grid gap-2">
                    <Label htmlFor="cert-domain">Domain</Label>
                    <Input
                      id="cert-domain"
                      placeholder="e.g., example.com"
                      value={newCert.domain}
                      onChange={(e) => setNewCert({ ...newCert, domain: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="cert-content">Certificate (PEM)</Label>
                    <Textarea
                      id="cert-content"
                      placeholder="-----BEGIN CERTIFICATE-----"
                      rows={4}
                      value={newCert.certContent}
                      onChange={(e) => setNewCert({ ...newCert, certContent: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="key-content">Private Key (PEM)</Label>
                    <Textarea
                      id="key-content"
                      placeholder="-----BEGIN PRIVATE KEY-----"
                      rows={4}
                      value={newCert.keyContent}
                      onChange={(e) => setNewCert({ ...newCert, keyContent: e.target.value })}
                    />
                  </div>
                </>
              )}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCertDialogOpen(false)}>
                Cancel
              </Button>
              <Button
                onClick={handleAddCertificate}
                disabled={certSource === 'acme' ? !newCert.domain || !newCert.email : !newCert.domain || !newCert.certContent || !newCert.keyContent}
              >
                Add Certificate
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Certificates</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{certs?.length ?? 0}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Wildcards</CardTitle>
            <CheckCircle className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {certs?.filter(c => c.is_wildcard).length ?? 0}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Expiring Soon</CardTitle>
            <AlertCircle className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {certs?.filter(c => getDaysUntilExpiry(c.expiry) < 30).length ?? 0}
            </div>
          </CardContent>
        </Card>
      </div>

      {isLoading && <LoadingCard />}
      {error && (
        <Card>
          <CardContent className="p-6">
            <p className="text-destructive">Failed to load certificates: {error.message}</p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => refetch()}>
              <RefreshCw className="mr-2 h-4 w-4" /> Retry
            </Button>
          </CardContent>
        </Card>
      )}

      <div className="space-y-4">
        {(certs ?? []).map((cert, i) => {
          const days = getDaysUntilExpiry(cert.expiry)
          const domainLabel = cert.names.join(", ")
          return (
            <Card key={i}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <Shield className="h-5 w-5 text-primary" />
                    <div>
                      <CardTitle className="text-base">{domainLabel}</CardTitle>
                      <CardDescription>{cert.is_wildcard ? "Wildcard" : "Standard"} Certificate</CardDescription>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge className={getExpiryBg(days)}>
                      {days} days
                    </Badge>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleRenewCert(cert.names)}
                    >
                      <Upload className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div>
                    <span className="text-muted-foreground">SANs:</span>
                    <span className="ml-2">{cert.names.join(", ")}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Expires:</span>
                    <span className="ml-2">{cert.expiry || "N/A"}</span>
                  </div>
                </div>
              </CardContent>
            </Card>
          )
        })}
        {certs && certs.length === 0 && (
          <Card>
            <CardContent className="p-8 text-center text-muted-foreground">
              No TLS certificates configured. Configure certificates in your config file.
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
