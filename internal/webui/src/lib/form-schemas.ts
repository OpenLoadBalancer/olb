import { z } from "zod"

// ── Pool / Backend ──────────────────────────────────────────────

export const createPoolSchema = z.object({
  name: z
    .string()
    .min(1, "Pool name is required")
    .max(63, "Pool name must be 63 characters or fewer")
    .regex(/^[a-z0-9-]+$/, "Pool name must contain only lowercase letters, numbers, and hyphens"),
  algorithm: z.string().min(1, "Algorithm is required"),
  healthCheckEnabled: z.boolean(),
  healthCheckType: z.enum(["http", "tcp", "grpc"]),
  healthCheckPath: z.string(),
  healthCheckInterval: z.enum(["5s", "10s", "30s", "1m"]),
})

export const addBackendSchema = z.object({
  address: z
    .string()
    .min(1, "Backend address is required")
    .regex(/^[\w.-]+:\d+$/, "Address must be in host:port format (e.g., 10.0.1.10:8080)"),
  weight: z.coerce.number<number>().int().min(1).max(100),
})

export type CreatePoolFormValues = z.infer<typeof createPoolSchema>
export type AddBackendFormValues = z.infer<typeof addBackendSchema>

// ── Listener / Route ────────────────────────────────────────────

export const createListenerSchema = z.object({
  name: z
    .string()
    .min(1, "Listener name is required")
    .max(63, "Listener name must be 63 characters or fewer")
    .regex(/^[a-z0-9-]+$/, "Listener name must contain only lowercase letters, numbers, and hyphens"),
  address: z
    .string()
    .min(1, "Address is required")
    .regex(/^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}|\*|0\.0\.0\.0)?(:\d{1,5})?$/, "Address must be a valid port (e.g., :8080) or host:port"),
  protocol: z.enum(["http", "https", "tcp", "udp"]),
})

export const addRouteSchema = z.object({
  path: z
    .string()
    .min(1, "Path pattern is required")
    .regex(/^\//, "Path must start with /"),
  pool: z.string().min(1, "Target pool is required"),
  methods: z.array(z.string()).min(1, "At least one HTTP method is required"),
  priority: z.coerce.number<number>().int().min(0).max(100),
  strip_prefix: z.boolean(),
})

export type CreateListenerFormValues = z.infer<typeof createListenerSchema>
export type AddRouteFormValues = z.infer<typeof addRouteSchema>

// ── Cluster ─────────────────────────────────────────────────────

export const addNodeSchema = z.object({
  address: z
    .string()
    .min(1, "Node address is required")
    .regex(/^[\w.-]+:\d+$/, "Address must be in host:port format (e.g., 10.0.1.13:12000)"),
})

export type AddNodeFormValues = z.infer<typeof addNodeSchema>

// ── Certificates ────────────────────────────────────────────────

export const addCertificateAcmeSchema = z.object({
  domain: z
    .string()
    .min(1, "Domain is required")
    .regex(/^(\*\.)?[\w.-]+\.[a-z]{2,}$/, "Enter a valid domain or wildcard domain (e.g., *.example.com)"),
  email: z.string().email("Enter a valid email address"),
  autoRenew: z.boolean(),
})

export const addCertificateManualSchema = z.object({
  domain: z
    .string()
    .min(1, "Domain is required")
    .regex(/^(\*\.)?[\w.-]+\.[a-z]{2,}$/, "Enter a valid domain or wildcard domain"),
  certContent: z.string().min(1, "Certificate PEM is required"),
  keyContent: z.string().min(1, "Private key PEM is required"),
})

export type AddCertificateAcmeFormValues = z.infer<typeof addCertificateAcmeSchema>
export type AddCertificateManualFormValues = z.infer<typeof addCertificateManualSchema>
