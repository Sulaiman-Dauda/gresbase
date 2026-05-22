'use client'

import { useQuery } from '@tanstack/react-query'
import { AppLayout } from '@/components/layout/app-layout'
import { api } from '@/lib/api'
import { Shield, Loader2, Users, UserCog, Eye } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

export default function RolesPage() {
  const { data: admins } = useQuery({ queryKey: ['admins'], queryFn: api.getAdmins })

  const roles = [
    {
      name: 'admin',
      label: 'Administrator',
      description: 'Full platform access — manage collections, users, certificates, and settings',
      permissions: ['collections.*', 'records.*', 'admins.*', 'settings.*', 'logs.read', 'certs.*', 'keys.*'],
      icon: Shield,
    },
    {
      name: 'editor',
      label: 'Editor',
      description: 'Can manage collections and records, but not platform settings',
      permissions: ['collections.*', 'records.*', 'logs.read', 'files.*'],
      icon: UserCog,
    },
    {
      name: 'viewer',
      label: 'Viewer',
      description: 'Read-only access to collections, records, and audit logs',
      permissions: ['collections.read', 'records.read', 'logs.read', 'files.read'],
      icon: Eye,
    },
  ]

  return (
    <AppLayout>
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Roles & Permissions</h1>
          <p className="text-sm text-muted-foreground mt-1">Role-based access control for admin users</p>
        </div>

        {/* User counts by role */}
        <div className="grid gap-4 sm:grid-cols-3">
          {roles.map(role => {
            const count = admins?.filter((a: any) => a.role === role.name).length ?? 0
            return (
              <Card key={role.name}>
                <CardHeader className="flex flex-row items-center justify-between pb-2">
                  <CardTitle className="text-sm font-medium capitalize">{role.label}</CardTitle>
                  <role.icon className="h-4 w-4 text-muted-foreground"/>
                </CardHeader>
                <CardContent>
                  <div className="text-2xl font-bold">{count}</div>
                  <p className="text-xs text-muted-foreground">users</p>
                </CardContent>
              </Card>
            )
          })}
        </div>

        {/* Detailed role cards */}
        <div className="grid gap-4">
          {roles.map(role => (
            <Card key={role.name}>
              <CardHeader>
                <div className="flex items-center gap-2">
                  <Badge variant="outline">{role.name}</Badge>
                  <CardTitle className="text-base">{role.label}</CardTitle>
                </div>
                <CardDescription>{role.description}</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="flex flex-wrap gap-1.5">
                  {role.permissions.map(perm => (
                    <Badge key={perm} variant="secondary" className="text-xs font-mono">{perm}</Badge>
                  ))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>

        {admins && admins.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle className="text-base flex items-center gap-2">
                <Users className="h-4 w-4"/>Users ({admins.length})
              </CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              <div className="divide-y divide-border">
                {admins.map((a: any) => (
                  <div key={a.id} className="flex items-center gap-3 px-6 py-2.5">
                    <div className="flex h-8 w-8 items-center justify-center rounded-full bg-accent">
                      <span className="text-xs font-medium">{a.email[0]?.toUpperCase()}</span>
                    </div>
                    <div className="flex-1">
                      <p className="text-sm font-medium">{a.email}</p>
                    </div>
                    <Badge variant={a.role === 'admin' ? 'default' : 'outline'} className="text-xs">
                      {a.role}
                    </Badge>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        )}
      </div>
    </AppLayout>
  )
}
