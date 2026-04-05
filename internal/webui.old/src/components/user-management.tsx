import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { DataTable } from '@/components/data-table'
import { toast } from 'sonner'
import { cn, formatDate } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  Users,
  UserPlus,
  User,
  Shield,
  Key,
  Lock,
  Unlock,
  Trash2,
  Edit,
  Mail,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  RefreshCw,
  Eye,
  EyeOff,
  Search,
  Filter,
  MoreHorizontal,
  Activity,
  Clock
} from 'lucide-react'

interface UserAccount {
  id: string
  username: string
  email: string
  fullName: string
  role: 'admin' | 'operator' | 'viewer'
  status: 'active' | 'inactive' | 'locked'
  lastLogin?: Date
  createdAt: Date
  mfaEnabled: boolean
  apiKeys: number
}

interface ApiKey {
  id: string
  name: string
  key: string
  createdAt: Date
  expiresAt?: Date
  lastUsed?: Date
  permissions: string[]
}

const mockUsers: UserAccount[] = [
  {
    id: '1',
    username: 'admin',
    email: 'admin@openloadbalancer.dev',
    fullName: 'System Administrator',
    role: 'admin',
    status: 'active',
    lastLogin: new Date(Date.now() - 3600000),
    createdAt: new Date(Date.now() - 86400000 * 30),
    mfaEnabled: true,
    apiKeys: 2
  },
  {
    id: '2',
    username: 'operator1',
    email: 'ops1@company.com',
    fullName: 'John Operator',
    role: 'operator',
    status: 'active',
    lastLogin: new Date(Date.now() - 86400000),
    createdAt: new Date(Date.now() - 86400000 * 15),
    mfaEnabled: false,
    apiKeys: 1
  },
  {
    id: '3',
    username: 'viewer1',
    email: 'viewer@company.com',
    fullName: 'Jane Viewer',
    role: 'viewer',
    status: 'active',
    lastLogin: new Date(Date.now() - 86400000 * 2),
    createdAt: new Date(Date.now() - 86400000 * 10),
    mfaEnabled: false,
    apiKeys: 0
  },
  {
    id: '4',
    username: 'olduser',
    email: 'old@company.com',
    fullName: 'Old User',
    role: 'viewer',
    status: 'inactive',
    lastLogin: new Date(Date.now() - 86400000 * 60),
    createdAt: new Date(Date.now() - 86400000 * 90),
    mfaEnabled: false,
    apiKeys: 0
  }
]

const mockApiKeys: ApiKey[] = [
  {
    id: '1',
    name: 'Production API',
    key: 'olb_live_********************************',
    createdAt: new Date(Date.now() - 86400000 * 7),
    lastUsed: new Date(Date.now() - 3600000),
    permissions: ['read', 'write']
  },
  {
    id: '2',
    name: 'Monitoring Dashboard',
    key: 'olb_read_********************************',
    createdAt: new Date(Date.now() - 86400000 * 30),
    lastUsed: new Date(Date.now() - 1800000),
    permissions: ['read']
  },
  {
    id: '3',
    name: 'CI/CD Pipeline',
    key: 'olb_cicd_********************************',
    createdAt: new Date(Date.now() - 86400000 * 60),
    expiresAt: new Date(Date.now() + 86400000 * 30),
    lastUsed: new Date(Date.now() - 86400000),
    permissions: ['read', 'write', 'deploy']
  }
]

const userColumns: ColumnDef<UserAccount>[] = [
  {
    accessorKey: 'username',
    header: 'User',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <div className="h-8 w-8 rounded-full bg-primary/10 flex items-center justify-center">
          <User className="h-4 w-4 text-primary" />
        </div>
        <div>
          <p className="font-medium">{row.original.fullName}</p>
          <p className="text-xs text-muted-foreground">@{row.original.username}</p>
        </div>
      </div>
    )
  },
  {
    accessorKey: 'email',
    header: 'Email'
  },
  {
    accessorKey: 'role',
    header: 'Role',
    cell: ({ row }) => {
      const roles: Record<string, { label: string; color: string }> = {
        admin: { label: 'Admin', color: 'bg-purple-500' },
        operator: { label: 'Operator', color: 'bg-blue-500' },
        viewer: { label: 'Viewer', color: 'bg-gray-500' }
      }
      const role = roles[row.original.role]
      return (
        <Badge className={role.color}>
          {role.label}
        </Badge>
      )
    }
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => {
      const status = row.original.status
      const statusColors: Record<string, string> = {
        active: 'text-green-500',
        inactive: 'text-gray-500',
        locked: 'text-red-500'
      }
      return (
        <div className="flex items-center gap-1">
          <div className={cn('h-2 w-2 rounded-full', statusColors[status])} />
          <span className="capitalize">{status}</span>
        </div>
      )
    }
  },
  {
    accessorKey: 'mfaEnabled',
    header: 'MFA',
    cell: ({ row }) => (
      row.original.mfaEnabled ? (
        <Shield className="h-4 w-4 text-green-500" />
      ) : (
        <span className="text-muted-foreground">-</span>
      )
    )
  },
  {
    accessorKey: 'lastLogin',
    header: 'Last Login',
    cell: ({ row }) => (
      row.original.lastLogin ? formatDate(row.original.lastLogin) : 'Never'
    )
  }
]

const apiKeyColumns: ColumnDef<ApiKey>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Key className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{row.original.name}</span>
      </div>
    )
  },
  {
    accessorKey: 'key',
    header: 'Key',
    cell: ({ row }) => (
      <code className="text-xs bg-muted px-2 py-1 rounded">
        {row.original.key}
      </code>
    )
  },
  {
    accessorKey: 'permissions',
    header: 'Permissions',
    cell: ({ row }) => (
      <div className="flex gap-1">
        {row.original.permissions.map(p => (
          <Badge key={p} variant="outline" className="text-xs">
            {p}
          </Badge>
        ))}
      </div>
    )
  },
  {
    accessorKey: 'lastUsed',
    header: 'Last Used',
    cell: ({ row }) => (
      row.original.lastUsed ? formatDate(row.original.lastUsed) : 'Never'
    )
  },
  {
    accessorKey: 'expiresAt',
    header: 'Expires',
    cell: ({ row }) => (
      row.original.expiresAt ? (
        <span className={cn(
          row.original.expiresAt < new Date() ? 'text-red-500' : 'text-muted-foreground'
        )}>
          {formatDate(row.original.expiresAt)}
        </span>
      ) : (
        <span className="text-muted-foreground">Never</span>
      )
    )
  }
]

export function UserManagement() {
  const [activeTab, setActiveTab] = useState('users')
  const [showUserDialog, setShowUserDialog] = useState(false)
  const [showApiKeyDialog, setShowApiKeyDialog] = useState(false)
  const [showKeyReveal, setShowKeyReveal] = useState(false)
  const [selectedUser, setSelectedUser] = useState<UserAccount | null>(null)
  const [newApiKey, setNewApiKey] = useState<string | null>(null)

  const handleCreateUser = () => {
    setShowUserDialog(false)
    toast.success('User created successfully')
  }

  const handleCreateApiKey = () => {
    setNewApiKey('olb_live_' + Array(32).fill(0).map(() => Math.random().toString(36)[2]).join(''))
    setShowApiKeyDialog(false)
    toast.success('API key created successfully')
  }

  const handleDeleteUser = (user: UserAccount) => {
    toast.success(`Deleted user: ${user.username}`)
  }

  const handleToggleStatus = (user: UserAccount) => {
    toast.success(`${user.username} ${user.status === 'active' ? 'deactivated' : 'activated'}`)
  }

  const handleRevokeKey = (key: ApiKey) => {
    toast.success(`Revoked API key: ${key.name}`)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">User Management</h1>
          <p className="text-muted-foreground">
            Manage users, roles, and API access
          </p>
        </div>
        <div className="flex items-center gap-2">
          {activeTab === 'users' ? (
            <Button onClick={() => setShowUserDialog(true)}>
              <UserPlus className="mr-2 h-4 w-4" />
              Add User
            </Button>
          ) : (
            <Button onClick={() => setShowApiKeyDialog(true)}>
              <Key className="mr-2 h-4 w-4" />
              Create API Key
            </Button>
          )}
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="users">
            <Users className="mr-2 h-4 w-4" />
            Users
          </TabsTrigger>
          <TabsTrigger value="api-keys">
            <Key className="mr-2 h-4 w-4" />
            API Keys
          </TabsTrigger>
          <TabsTrigger value="roles">
            <Shield className="mr-2 h-4 w-4" />
            Roles
          </TabsTrigger>
        </TabsList>

        <TabsContent value="users" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-4">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Total Users</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold">{mockUsers.length}</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Active</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold text-green-500">
                  {mockUsers.filter(u => u.status === 'active').length}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Admins</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold text-purple-500">
                  {mockUsers.filter(u => u.role === 'admin').length}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">MFA Enabled</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold">
                  {mockUsers.filter(u => u.mfaEnabled).length}
                </div>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Users</CardTitle>
              <CardDescription>Manage user accounts and permissions</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={mockUsers}
                columns={userColumns}
                actions={[
                  {
                    label: 'Edit',
                    icon: Edit,
                    onClick: (user) => {
                      setSelectedUser(user)
                      setShowUserDialog(true)
                    }
                  },
                  {
                    label: user => user.status === 'active' ? 'Deactivate' : 'Activate',
                    icon: user => user.status === 'active' ? Lock : Unlock,
                    onClick: handleToggleStatus
                  },
                  {
                    label: 'Delete',
                    icon: Trash2,
                    variant: 'destructive',
                    onClick: handleDeleteUser
                  }
                ]}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="api-keys" className="space-y-4">
          {newApiKey && (
            <Card className="border-green-500/50 bg-green-500/10">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-green-600">
                  <CheckCircle2 className="h-5 w-5" />
                  API Key Created
                </CardTitle>
                <CardDescription>
                  Copy this key now. You won't be able to see it again.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-background p-3 rounded-lg font-mono text-sm">
                    {showKeyReveal ? newApiKey : newApiKey.slice(0, 12) + '•'.repeat(40)}
                  </code>
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={() => setShowKeyReveal(!showKeyReveal)}
                  >
                    {showKeyReveal ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </Button>
                  <Button
                    variant="outline"
                    onClick={() => {
                      navigator.clipboard.writeText(newApiKey)
                      toast.success('API key copied to clipboard')
                    }}
                  >
                    Copy
                  </Button>
                </div>
                <Button variant="outline" onClick={() => setNewApiKey(null)}>
                  Dismiss
                </Button>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardHeader>
              <CardTitle>API Keys</CardTitle>
              <CardDescription>Manage API access keys</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={mockApiKeys}
                columns={apiKeyColumns}
                actions={[
                  {
                    label: 'Revoke',
                    icon: Trash2,
                    variant: 'destructive',
                    onClick: handleRevokeKey
                  }
                ]}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="roles" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-3">
            {[
              {
                role: 'admin',
                label: 'Administrator',
                description: 'Full access to all features',
                permissions: ['View', 'Create', 'Edit', 'Delete', 'Settings', 'Users'],
                color: 'bg-purple-500'
              },
              {
                role: 'operator',
                label: 'Operator',
                description: 'Can manage configuration',
                permissions: ['View', 'Create', 'Edit', 'Delete'],
                color: 'bg-blue-500'
              },
              {
                role: 'viewer',
                label: 'Viewer',
                description: 'Read-only access',
                permissions: ['View'],
                color: 'bg-gray-500'
              }
            ].map(role => (
              <Card key={role.role}>
                <CardHeader>
                  <div className="flex items-center gap-2">
                    <div className={cn('h-3 w-3 rounded-full', role.color)} />
                    <CardTitle>{role.label}</CardTitle>
                  </div>
                  <CardDescription>{role.description}</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="space-y-2">
                    <Label className="text-sm text-muted-foreground">Permissions</Label>
                    <div className="flex flex-wrap gap-1">
                      {role.permissions.map(p => (
                        <Badge key={p} variant="secondary" className="text-xs">
                          {p}
                        </Badge>
                      ))}
                    </div>
                  </div>
                  <div className="mt-4 pt-4 border-t">
                    <p className="text-sm text-muted-foreground">
                      {mockUsers.filter(u => u.role === role.role).length} users
                    </p>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>
      </Tabs>

      {/* Create User Dialog */}
      <Dialog open={showUserDialog} onOpenChange={setShowUserDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Create User</DialogTitle>
            <DialogDescription>Add a new user to the system</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Username</Label>
                <Input placeholder="johndoe" />
              </div>
              <div className="space-y-2">
                <Label>Email</Label>
                <Input type="email" placeholder="john@example.com" />
              </div>
            </div>
            <div className="space-y-2">
              <Label>Full Name</Label>
              <Input placeholder="John Doe" />
            </div>
            <div className="space-y-2">
              <Label>Role</Label>
              <Select defaultValue="viewer">
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="admin">Administrator</SelectItem>
                  <SelectItem value="operator">Operator</SelectItem>
                  <SelectItem value="viewer">Viewer</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>Temporary Password</Label>
              <Input type="password" defaultValue="TempPass123!" />
              <p className="text-xs text-muted-foreground">
                User will be required to change on first login
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowUserDialog(false)}>
              Cancel
            </Button>
            <Button onClick={handleCreateUser}>
              <UserPlus className="mr-2 h-4 w-4" />
              Create User
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Create API Key Dialog */}
      <Dialog open={showApiKeyDialog} onOpenChange={setShowApiKeyDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Create API Key</DialogTitle>
            <DialogDescription>Create a new API access key</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Name</Label>
              <Input placeholder="Production API" />
            </div>
            <div className="space-y-2">
              <Label>Permissions</Label>
              <div className="space-y-2">
                {['Read', 'Write', 'Delete', 'Deploy'].map(perm => (
                  <div key={perm} className="flex items-center gap-2">
                    <Switch id={perm} />
                    <Label htmlFor={perm} className="text-sm">{perm}</Label>
                  </div>
                ))}
              </div>
            </div>
            <div className="space-y-2">
              <Label>Expiration</Label>
              <Select defaultValue="never">
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="never">Never</SelectItem>
                  <SelectItem value="30">30 days</SelectItem>
                  <SelectItem value="90">90 days</SelectItem>
                  <SelectItem value="365">1 year</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowApiKeyDialog(false)}>
              Cancel
            </Button>
            <Button onClick={handleCreateApiKey}>
              <Key className="mr-2 h-4 w-4" />
              Create Key
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
