import { useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Shield, Plus, Trash2, Clock, CheckCircle, AlertCircle } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

interface Certificate {
  id: string
  domain: string
  issuer: string
  notBefore: string
  notAfter: string
  daysUntilExpiry: number
  autoRenew: boolean
}

const mockCertificates: Certificate[] = [
  {
    id: "1",
    domain: "*.openloadbalancer.dev",
    issuer: "Let's Encrypt R3",
    notBefore: "2025-01-01",
    notAfter: "2025-04-01",
    daysUntilExpiry: 45,
    autoRenew: true
  },
  {
    id: "2",
    domain: "admin.openloadbalancer.dev",
    issuer: "Let's Encrypt R3",
    notBefore: "2025-01-15",
    notAfter: "2025-04-15",
    daysUntilExpiry: 60,
    autoRenew: true
  }
]

export function CertificatesPage() {
  const [certs, setCerts] = useState<Certificate[]>(mockCertificates)

  const getExpiryColor = (days: number) => {
    if (days < 7) return "text-red-500"
    if (days < 30) return "text-amber-500"
    return "text-green-500"
  }

  const getExpiryBg = (days: number) => {
    if (days < 7) return "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300"
    if (days < 30) return "bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300"
    return "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300"
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">TLS Certificates</h1>
          <p className="text-muted-foreground">Manage SSL/TLS certificates</p>
        </div>
        <Button>
          <Plus className="mr-2 h-4 w-4" />
          Add Certificate
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Certificates</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{certs.length}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Auto-Renewal</CardTitle>
            <CheckCircle className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {certs.filter(c => c.autoRenew).length}
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
              {certs.filter(c => c.daysUntilExpiry < 30).length}
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="space-y-4">
        {certs.map((cert) => (
          <Card key={cert.id}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <Shield className="h-5 w-5 text-primary" />
                  <div>
                    <CardTitle className="text-base">{cert.domain}</CardTitle>
                    <CardDescription>{cert.issuer}</CardDescription>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge className={getExpiryBg(cert.daysUntilExpiry)}>
                    {cert.daysUntilExpiry} days
                  </Badge>
                  <Button variant="ghost" size="icon">
                    <Trash2 className="h-4 w-4 text-destructive" />
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <span className="text-muted-foreground">Valid From:</span>
                  <span className="ml-2">{cert.notBefore}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Valid Until:</span>
                  <span className="ml-2">{cert.notAfter}</span>
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
