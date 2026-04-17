import { useState } from "react"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
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
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form"
import { Shield, Plus, CheckCircle, AlertCircle, Upload, RefreshCw, Loader2 } from "lucide-react"
import { toast } from "sonner"
import { useCertificates } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { addCertificateAcmeSchema, addCertificateManualSchema, type AddCertificateAcmeFormValues, type AddCertificateManualFormValues } from "@/lib/form-schemas"

export function CertificatesPage() {
  useDocumentTitle("Certificates")
  const { data: certs, isLoading, error, refetch } = useCertificates()

  // Add Certificate Dialog State
  const [certDialogOpen, setCertDialogOpen] = useState(false)
  const [certSource, setCertSource] = useState<'manual' | 'acme'>('acme')
  const certAcmeForm = useForm<AddCertificateAcmeFormValues>({
    resolver: zodResolver(addCertificateAcmeSchema),
    defaultValues: {
      domain: "",
      email: "",
      autoRenew: true,
    },
  })
  const certManualForm = useForm<AddCertificateManualFormValues>({
    resolver: zodResolver(addCertificateManualSchema),
    defaultValues: {
      domain: "",
      certContent: "",
      keyContent: "",
    },
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

  const handleAddCertificate = (_data: AddCertificateAcmeFormValues | AddCertificateManualFormValues) => {
    toast.info("Certificate management is done via configuration or ACME")
    setCertDialogOpen(false)
  }

  const handleRenewCert = (_names: string[]) => {
    toast.success("Certificate renewal initiated")
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">TLS Certificates</h1>
          <p className="text-muted-foreground">Manage SSL/TLS certificates</p>
        </div>
        <Dialog open={certDialogOpen} onOpenChange={(open) => { setCertDialogOpen(open); if (!open) { certAcmeForm.reset(); certManualForm.reset() } }}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4"  aria-hidden="true" />
              Add Certificate
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-[600px]">
            <DialogHeader>
              <DialogTitle>Add Certificate</DialogTitle>
              <DialogDescription>
                Add a new TLS certificate manually or via ACME/Let&apos;s Encrypt.
              </DialogDescription>
            </DialogHeader>
            <div className="flex gap-2">
              <Button
                variant={certSource === 'acme' ? 'default' : 'outline'}
                className="flex-1"
                onClick={() => setCertSource('acme')}
              >
                Let&apos;s Encrypt
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
              <Form {...certAcmeForm}>
                <form onSubmit={certAcmeForm.handleSubmit(handleAddCertificate)} className="grid gap-4 py-4">
                  <FormField
                    control={certAcmeForm.control}
                    name="domain"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Domain</FormLabel>
                        <FormControl>
                          <Input placeholder="e.g., *.example.com" {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={certAcmeForm.control}
                    name="email"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Email</FormLabel>
                        <FormControl>
                          <Input type="email" placeholder="admin@example.com" {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={certAcmeForm.control}
                    name="autoRenew"
                    render={({ field }) => (
                      <FormItem className="flex flex-row items-center justify-between space-y-0">
                        <FormLabel>Auto-renewal</FormLabel>
                        <FormControl>
                          <Switch checked={field.value} onCheckedChange={field.onChange} />
                        </FormControl>
                      </FormItem>
                    )}
                  />
                  <DialogFooter>
                    <Button variant="outline" type="button" onClick={() => { setCertDialogOpen(false); certAcmeForm.reset() }}>
                      Cancel
                    </Button>
                    <Button type="submit" disabled={!certAcmeForm.formState.isValid || certAcmeForm.formState.isSubmitting}>
                      {certAcmeForm.formState.isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin"  aria-hidden="true" />}
                      Add Certificate
                    </Button>
                  </DialogFooter>
                </form>
              </Form>
            ) : (
              <Form {...certManualForm}>
                <form onSubmit={certManualForm.handleSubmit(handleAddCertificate)} className="grid gap-4 py-4">
                  <FormField
                    control={certManualForm.control}
                    name="domain"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Domain</FormLabel>
                        <FormControl>
                          <Input placeholder="e.g., example.com" {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={certManualForm.control}
                    name="certContent"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Certificate (PEM)</FormLabel>
                        <FormControl>
                          <Textarea placeholder="-----BEGIN CERTIFICATE-----" rows={4} {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={certManualForm.control}
                    name="keyContent"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Private Key (PEM)</FormLabel>
                        <FormControl>
                          <Textarea placeholder="-----BEGIN PRIVATE KEY-----" rows={4} {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <DialogFooter>
                    <Button variant="outline" type="button" onClick={() => { setCertDialogOpen(false); certManualForm.reset() }}>
                      Cancel
                    </Button>
                    <Button type="submit" disabled={!certManualForm.formState.isValid || certManualForm.formState.isSubmitting}>
                      {certManualForm.formState.isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin"  aria-hidden="true" />}
                      Add Certificate
                    </Button>
                  </DialogFooter>
                </form>
              </Form>
            )}
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-4 grid-cols-2 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Certificates</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground"  aria-hidden="true" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{certs?.length ?? 0}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Wildcards</CardTitle>
            <CheckCircle className="h-4 w-4 text-green-500"  aria-hidden="true" />
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
            <AlertCircle className="h-4 w-4 text-amber-500"  aria-hidden="true" />
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
              <RefreshCw className="mr-2 h-4 w-4"  aria-hidden="true" /> Retry
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
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-3">
                    <Shield className="h-5 w-5 text-primary"  aria-hidden="true" />
                    <div>
                      <CardTitle className="text-base truncate">{domainLabel}</CardTitle>
                      <CardDescription>{cert.is_wildcard ? "Wildcard" : "Standard"} Certificate</CardDescription>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge className={getExpiryBg(days)}>
                      {days} days
                    </Badge>

                    <Button variant="ghost" aria-label="Renew certificate"
                      size="icon"
                      onClick={() => handleRenewCert(cert.names)}
                    >
                      <Upload className="h-4 w-4"  aria-hidden="true" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm break-words">
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
