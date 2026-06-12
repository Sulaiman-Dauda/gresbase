'use client'

import { useEffect, useState, useCallback } from 'react'
import { AppLayout } from '@/components/layout/app-layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import {
  Users, Plus, Trash2, Shield, Mail, Clock,
  Loader2, Search, UserPlus, CheckCircle, XCircle
} from 'lucide-react'

interface AdminUser {
  id: string
  email: string
  role: string
  avatar?: string
  tenant_id: string
  last_login_at?: string
  created_at: string
  updated_at: string
}

export default function UsersPage() {
  const [users, setUsers] = useState<AdminUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [newEmail, setNewEmail] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newRole, setNewRole] = useState('admin')
  const [creating, setCreating] = useState(false)
  const [me, setMe] = useState<AdminUser | null>(null)

  const fetchUsers = useCallback(async () => {
    setLoading(true)
    try {
      const [userList, myProfile] = await Promise.all([
        api.getAdmins().catch(() => []),
        api.getMe().catch(() => null),
      ])
      setUsers(Array.isArray(userList) ? userList : [])
      setMe(myProfile)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchUsers() }, [fetchUsers])

  const handleCreate = async () => {
    if (!newEmail.trim() || !newPassword.trim()) return
    setCreating(true)
    try {
      await api.createAdmin({ email: newEmail.trim(), password: newPassword, role: newRole })
      setNewEmail('')
      setNewPassword('')
      setNewRole('admin')
      setShowCreate(false)
      fetchUsers()
    } catch (err: any) {
      setError(err.message)
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (id: string, email: string) => {
    if (me?.id === id) {
      setError('You cannot delete your own account')
      return
    }
    if (!confirm(`Delete user "${email}"? This cannot be undone.`)) return
    try {
      await api.deleteAdmin(id)
      fetchUsers()
    } catch (err: any) {
      setError(err.message)
    }
  }

  const filtered = users.filter(u =>
    u.email.toLowerCase().includes(search.toLowerCase()) ||
    u.role.toLowerCase().includes(search.toLowerCase())
  )

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <AppLayout>
      <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Admin Users</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {users.length} user{users.length !== 1 ? 's' : ''} — manage admin accounts
          </p>
        </div>
        <Button onClick={() => setShowCreate(!showCreate)} size="sm">
          <UserPlus className="mr-2 h-4 w-4" />
          Add User
        </Button>
      </div>

      {error && (
        <Card className="border-red-500/50 bg-red-500/5">
          <CardContent className="py-3 flex items-center justify-between">
            <p className="text-sm text-red-500">{error}</p>
            <Button variant="ghost" size="sm" onClick={() => setError(null)}>
              <XCircle className="h-4 w-4" />
            </Button>
          </CardContent>
        </Card>
      )}

      {me && (
        <Card className="border-primary/30 bg-primary/5">
          <CardContent className="flex items-center gap-4 py-4">
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-primary/20">
              <Shield className="h-5 w-5 text-primary" />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <span className="font-medium">{me.email}</span>
                <Badge variant="outline">{me.role}</Badge>
                <Badge variant="secondary" className="text-[10px]">you</Badge>
              </div>
              <div className="text-xs text-muted-foreground mt-0.5">
                ID: {me.id.slice(0, 8)}... | Tenant: {me.tenant_id}
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {showCreate && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">Create New User</CardTitle>
          </CardHeader>
          <CardContent className="flex items-end gap-3">
            <div className="flex-1 space-y-2">
              <label className="text-xs text-muted-foreground">Email</label>
              <Input
                type="email"
                placeholder="user@example.com"
                value={newEmail}
                onChange={e => setNewEmail(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleCreate()}
              />
            </div>
            <div className="flex-1 space-y-2">
              <label className="text-xs text-muted-foreground">Password</label>
              <Input
                type="password"
                placeholder="Min 8 characters"
                value={newPassword}
                onChange={e => setNewPassword(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleCreate()}
              />
            </div>
            <div className="space-y-2">
              <label className="text-xs text-muted-foreground">Role</label>
              <select
                value={newRole}
                onChange={e => setNewRole(e.target.value)}
                className="h-9 rounded-lg border border-border bg-background px-3 text-sm"
              >
                <option value="admin">Admin</option>
                <option value="superuser">Superuser</option>
                <option value="viewer">Viewer</option>
              </select>
            </div>
            <Button onClick={handleCreate} disabled={creating || !newEmail || !newPassword} size="sm">
              {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
              Create
            </Button>
          </CardContent>
        </Card>
      )}

      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search users by email or role..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>

      <div className="space-y-2">
        {filtered.map((user) => (
          <Card key={user.id} className="transition-all hover:border-primary/30">
            <CardContent className="flex items-center gap-4 py-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-muted">
                <span className="text-sm font-medium">
                  {user.email.slice(0, 2).toUpperCase()}
                </span>
              </div>

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium truncate">{user.email}</span>
                  {me?.id === user.id && (
                    <Badge variant="secondary" className="text-[10px]">you</Badge>
                  )}
                </div>
                <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                  <Badge variant="outline" className={
                    user.role === 'superuser' ? 'bg-purple-500/10 text-purple-500' :
                    user.role === 'admin' ? 'bg-blue-500/10 text-blue-500' :
                    'bg-muted text-muted-foreground'
                  }>
                    <Shield className="mr-1 h-3 w-3" />
                    {user.role}
                  </Badge>
                  <span className="flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    Joined {new Date(user.created_at).toLocaleDateString()}
                  </span>
                  {user.last_login_at && user.last_login_at !== '1970-01-01T00:00:00Z' && (
                    <span>Last login: {new Date(user.last_login_at).toLocaleDateString()}</span>
                  )}
                </div>
              </div>

              <Button
                variant="ghost"
                size="icon"
                onClick={() => handleDelete(user.id, user.email)}
                disabled={me?.id === user.id}
                title={me?.id === user.id ? 'Cannot delete yourself' : 'Delete user'}
              >
                <Trash2 className="h-4 w-4 text-muted-foreground hover:text-red-500" />
              </Button>
            </CardContent>
          </Card>
        ))}

        {filtered.length === 0 && !loading && (
          <Card>
            <CardContent className="flex flex-col items-center py-12 text-center">
              <Users className="h-12 w-12 text-muted-foreground opacity-50 mb-4" />
              <h3 className="text-lg font-medium">No users found</h3>
              <p className="text-sm text-muted-foreground mt-1">
                {search ? 'No users match your search.' : 'Create your first admin user above.'}
              </p>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
    </AppLayout>
  )
}
