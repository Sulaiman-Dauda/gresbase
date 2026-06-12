'use client'

import * as React from 'react'
import Link from 'next/link'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import {
  Activity,
  ChevronLeft,
  ChevronRight,
  Database,
  Eye,
  Gauge,
  Key,
  Loader2,
  LogOut,
  Moon,
  Plus,
  ScrollText,
  Search,
  Settings,
  Shield,
  Sun,
  UserCog,
} from 'lucide-react'
import { useTheme } from 'next-themes'
import { cn } from '@/lib/utils'
import { useStore } from '@/lib/store'
import { api } from '@/lib/api'
import type { Collection } from '@/lib/types'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'

const LAST_ACTIVE_COLLECTION_KEY = 'gresbase_last_active_collection'

const SYSTEM_NAV = [
  { title: 'Admins', href: '/users', icon: UserCog },
  { title: 'Logs', href: '/logs', icon: ScrollText },
  { title: 'Metrics', href: '/metrics', icon: Gauge },
  { title: 'Settings', href: '/settings', icon: Settings },
]

const POWER_NAV = [
  { title: 'Realtime', href: '/realtime', icon: Activity },
  { title: 'API Keys', href: '/api-keys', icon: Key },
]

function collectionIcon(type: Collection['type']) {
  if (type === 'auth') return Shield
  if (type === 'view') return Eye
  return Database
}

function AppLayoutShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const router = useRouter()
  const { theme, setTheme } = useTheme()
  const { isAuthenticated, admin, checkAuth, logout } = useStore()

  const [collapsed, setCollapsed] = React.useState(false)
  const [commandOpen, setCommandOpen] = React.useState(false)
  const [authChecked, setAuthChecked] = React.useState(false)
  const [collections, setCollections] = React.useState<Collection[]>([])
  const [collectionsLoading, setCollectionsLoading] = React.useState(false)
  const [collectionFilter, setCollectionFilter] = React.useState('')

  const visibleCollections = collectionFilter
    ? collections.filter((c) => c.name.toLowerCase().includes(collectionFilter.toLowerCase()))
    : collections

  const activeCollectionId = pathname === '/collections' ? searchParams.get('collection') : null
  const homeMode = pathname === '/collections' ? searchParams.get('home') === '1' : false
  const activeCollection = collections.find((c) => c.id === activeCollectionId) || null

  const loadCollections = React.useCallback(async () => {
    if (!isAuthenticated) return
    setCollectionsLoading(true)
    try {
      const data = await api.getCollections()
      setCollections(Array.isArray(data) ? data : [])
    } catch {
      setCollections([])
    } finally {
      setCollectionsLoading(false)
    }
  }, [isAuthenticated])

  React.useEffect(() => {
    checkAuth().finally(() => setAuthChecked(true))
  }, [checkAuth])

  React.useEffect(() => {
    if (authChecked && !isAuthenticated) {
      router.push('/')
    }
  }, [authChecked, isAuthenticated, router])

  React.useEffect(() => {
    if (!authChecked || !isAuthenticated) return
    loadCollections()

    const reload = () => loadCollections()
    window.addEventListener('gresbase:collections:changed', reload)
    return () => window.removeEventListener('gresbase:collections:changed', reload)
  }, [authChecked, isAuthenticated, loadCollections])

  React.useEffect(() => {
    if (!activeCollectionId) return
    try {
      window.localStorage.setItem(LAST_ACTIVE_COLLECTION_KEY, activeCollectionId)
    } catch {
      // ignore localStorage errors
    }
  }, [activeCollectionId])

  React.useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setCommandOpen((open) => !open)
      }
      if (e.key === 'b' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setCollapsed((c) => !c)
      }
    }
    document.addEventListener('keydown', down)
    return () => document.removeEventListener('keydown', down)
  }, [])

  if (!authChecked) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (!isAuthenticated) return null

  const adminEmail = admin?.email || 'admin@gresbase.dev'
  const adminInitial = adminEmail.charAt(0).toUpperCase()
  const staticNav = [{ title: 'Collections', href: '/collections?home=1', icon: Database }, ...SYSTEM_NAV, ...POWER_NAV]
  const currentContextLabel = activeCollection
    ? `Collections / ${activeCollection.name}`
    : pathname === '/collections'
      ? homeMode
        ? 'Collections / all'
        : 'Collections'
      : staticNav.find((item) => pathname === item.href)?.title || 'Gresbase'

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <aside
        className={cn(
          'flex flex-col border-r border-border bg-sidebar transition-all duration-200',
          collapsed ? 'w-[64px]' : 'w-[256px]'
        )}
      >
        <div className="border-b border-border px-2.5 py-2.5">
          <button
            className={cn(
              'flex w-full items-center gap-3 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent/20',
              collapsed && 'justify-center px-1'
            )}
            onClick={() => router.push('/collections?home=1')}
          >
            <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg bg-foreground">
              <span className="text-xs font-bold text-background">G</span>
            </div>
            {!collapsed && (
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-semibold">Gresbase</div>
                <div className="truncate text-[11px] text-muted-foreground">Backend Platform</div>
              </div>
            )}
          </button>
        </div>

        <div className="px-2.5 py-2">
          <Link
            href="/collections?home=1"
            className={cn(
              'flex items-center gap-2 rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground transition-colors hover:border-foreground/30 hover:bg-accent/20 hover:text-foreground',
              collapsed && 'justify-center px-1'
            )}
            title="New collection"
          >
            <Plus className="h-3.5 w-3.5 flex-shrink-0" />
            {!collapsed && <span>New collection</span>}
          </Link>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 pb-2">
          <SidebarSection label="Workbench" collapsed={collapsed}>
            <NavItem
              href="/collections?home=1"
              icon={Database}
              active={pathname === '/collections' && !activeCollectionId}
              collapsed={collapsed}
              title="Collections"
            />
          </SidebarSection>

          <SidebarSection label="Collections" collapsed={collapsed}>
            {collectionsLoading && !collapsed ? (
              <div className="px-3 py-2 text-xs text-muted-foreground">Loading collections…</div>
            ) : null}
            {!collapsed && collections.length > 6 ? (
              <div className="px-1 pb-1">
                <input
                  value={collectionFilter}
                  onChange={(e) => setCollectionFilter(e.target.value)}
                  placeholder="Filter collections…"
                  className="h-7 w-full rounded-md border border-border bg-background px-2 text-xs text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </div>
            ) : null}
            {visibleCollections.map((collection) => {
              const Icon = collectionIcon(collection.type)
              const isActive = pathname === '/collections' && activeCollectionId === collection.id
              return (
                <NavItem
                  key={collection.id}
                  href={`/collections?collection=${collection.id}`}
                  icon={Icon}
                  active={isActive}
                  collapsed={collapsed}
                  title={collection.name}
                  subtle
                />
              )
            })}
            {!collapsed && collectionFilter && visibleCollections.length === 0 && collections.length > 0 ? (
              <div className="px-3 py-2 text-xs text-muted-foreground">No collections match “{collectionFilter}”.</div>
            ) : null}
            {!collectionsLoading && collections.length === 0 && !collapsed ? (
              <div className="px-3 py-2 text-xs text-muted-foreground">No collections yet.</div>
            ) : null}
          </SidebarSection>

          <SidebarSection label="System" collapsed={collapsed}>
            {SYSTEM_NAV.map((item) => (
              <NavItem
                key={item.href}
                href={item.href}
                icon={item.icon}
                active={pathname === item.href || pathname.startsWith(item.href + '/')}
                collapsed={collapsed}
                title={item.title}
              />
            ))}
          </SidebarSection>

          <SidebarSection label="Power" collapsed={collapsed}>
            {POWER_NAV.map((item) => (
              <NavItem
                key={item.href}
                href={item.href}
                icon={item.icon}
                active={pathname === item.href || pathname.startsWith(item.href + '/')}
                collapsed={collapsed}
                title={item.title}
              />
            ))}
          </SidebarSection>
        </nav>

        <div className="border-t border-border px-2 py-2">
          <div
            className={cn(
              'mb-1 flex items-center gap-2 rounded-md px-2 py-1.5',
              !collapsed && 'hover:bg-accent/20'
            )}
          >
            <div className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-accent text-[10px] font-semibold text-accent-foreground">
              {adminInitial}
            </div>
            {!collapsed && (
              <div className="min-w-0 flex-1">
                <p className="truncate text-xs font-medium text-foreground">{adminEmail}</p>
                <p className="text-[10px] text-muted-foreground">Admin</p>
              </div>
            )}
          </div>

          <button
            onClick={() => setCollapsed(!collapsed)}
            className={cn(
              'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent/20 hover:text-foreground',
              collapsed && 'justify-center'
            )}
          >
            {collapsed ? (
              <ChevronRight className="h-4 w-4" />
            ) : (
              <>
                <ChevronLeft className="h-4 w-4" />
                <span>Collapse</span>
              </>
            )}
          </button>
        </div>
      </aside>

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-12 items-center gap-3 border-b border-border bg-background/80 px-4 backdrop-blur-sm">
          <button
            onClick={() => setCommandOpen(true)}
            className="flex items-center gap-2 rounded-lg border border-border bg-background px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-accent/20 hover:text-foreground"
          >
            <Search className="h-3.5 w-3.5" />
            <span className="hidden text-xs sm:inline">Search workbench…</span>
            <kbd className="hidden rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground sm:inline">⌘K</kbd>
          </button>

          <div className="min-w-0 flex-1 truncate text-sm text-muted-foreground">{currentContextLabel}</div>

          <button
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent/20 hover:text-foreground"
          >
            {theme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </button>

          <button
            onClick={() => {
              logout()
              router.push('/')
            }}
            className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent/20 hover:text-foreground"
          >
            <LogOut className="h-4 w-4" />
          </button>
        </header>

        <main className="flex-1 overflow-y-auto p-6">{children}</main>
      </div>

      <CommandDialog open={commandOpen} onOpenChange={setCommandOpen}>
        <CommandInput placeholder="Search collections, pages, and power tools..." />
        <CommandList>
          <CommandEmpty>No results found.</CommandEmpty>

          <CommandGroup heading="Navigate">
            {[{ title: 'Collections', href: '/collections?home=1', icon: Database }, ...SYSTEM_NAV, ...POWER_NAV].map((item) => (
              <CommandItem
                key={item.href}
                onSelect={() => {
                  router.push(item.href)
                  setCommandOpen(false)
                }}
              >
                <item.icon className="mr-2 h-4 w-4" />
                {item.title}
              </CommandItem>
            ))}
          </CommandGroup>

          <CommandSeparator />

          <CommandGroup heading="Collections">
            {collections.map((collection) => {
              const Icon = collectionIcon(collection.type)
              return (
                <CommandItem
                  key={collection.id}
                  onSelect={() => {
                    router.push(`/collections?collection=${collection.id}`)
                    setCommandOpen(false)
                  }}
                >
                  <Icon className="mr-2 h-4 w-4" />
                  {collection.name}
                </CommandItem>
              )
            })}
          </CommandGroup>

          <CommandSeparator />

          <CommandGroup heading="Quick actions">
            <CommandItem
              onSelect={() => {
                router.push('/collections?home=1')
                setCommandOpen(false)
              }}
            >
              <Plus className="mr-2 h-4 w-4" />
              Create collection
            </CommandItem>
            <CommandItem
              onSelect={() => {
                router.push('/api-keys')
                setCommandOpen(false)
              }}
            >
              <Key className="mr-2 h-4 w-4" />
              Manage API keys
            </CommandItem>
            <CommandItem
              onSelect={() => {
                router.push('/realtime')
                setCommandOpen(false)
              }}
            >
              <Activity className="mr-2 h-4 w-4" />
              Open realtime tester
            </CommandItem>
          </CommandGroup>
        </CommandList>
        <div className="flex items-center gap-3 border-t border-border px-3 py-2 text-[11px] text-muted-foreground">
          <span><kbd className="rounded border border-border bg-muted px-1 py-0.5 font-mono">⌘K</kbd> search</span>
          <span><kbd className="rounded border border-border bg-muted px-1 py-0.5 font-mono">⌘B</kbd> toggle sidebar</span>
          <span><kbd className="rounded border border-border bg-muted px-1 py-0.5 font-mono">↵</kbd> open</span>
          <span><kbd className="rounded border border-border bg-muted px-1 py-0.5 font-mono">esc</kbd> close</span>
        </div>
      </CommandDialog>
    </div>
  )
}

export function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <React.Suspense
      fallback={
        <div className="flex min-h-screen items-center justify-center bg-background">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      }
    >
      <AppLayoutShell>{children}</AppLayoutShell>
    </React.Suspense>
  )
}

function SidebarSection({
  label,
  collapsed,
  children,
}: {
  label: string
  collapsed: boolean
  children: React.ReactNode
}) {
  return (
    <div className="mb-3">
      {!collapsed ? (
        <h4 className="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/60">
          {label}
        </h4>
      ) : (
        <div className="mx-2 my-2 border-t border-border/50" />
      )}
      <div className="space-y-px">{children}</div>
    </div>
  )
}

function NavItem({
  href,
  icon: Icon,
  active,
  collapsed,
  title,
  subtle = false,
}: {
  href: string
  icon: React.ComponentType<{ className?: string }>
  active: boolean
  collapsed: boolean
  title: string
  subtle?: boolean
}) {
  return (
    <Link
      href={href}
      title={collapsed ? title : undefined}
      className={cn(
        'group relative flex items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] font-medium transition-colors',
        active
          ? 'bg-accent/40 text-foreground'
          : subtle
            ? 'text-muted-foreground/90 hover:bg-accent/15 hover:text-foreground'
            : 'text-muted-foreground hover:bg-accent/20 hover:text-foreground',
        collapsed && 'justify-center px-1'
      )}
    >
      {active && <span className="absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r-full bg-foreground" />}
      <Icon className={cn('h-4 w-4 flex-shrink-0', active ? 'text-foreground' : 'text-muted-foreground group-hover:text-foreground')} />
      {!collapsed && <span className="truncate">{title}</span>}
    </Link>
  )
}
