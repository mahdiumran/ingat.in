import { useCallback, useEffect, useState } from 'react'
import { api, ApiUser, clearSession, getStoredUser, isAuthenticated, setUnauthorizedHandler, workItemsApi } from './api'
import { Toast, ToastStack } from './components/ui'
import { NotificationBell } from './components/NotificationBell'
import Audit from './pages/Audit'
import Dashboard from './pages/Dashboard'
import DailyTasks from './pages/DailyTasks'
import Login from './pages/Login'
import MasterData from './pages/MasterData'
import MasterDataKindPage from './pages/MasterDataKindPage'
import NotificationTemplatesPage from './pages/NotificationTemplatesPage'
import RolePermissionsPage from './pages/RolePermissionsPage'

import Profile from './pages/Profile'
import Targets from './pages/Targets'
import Providers from './pages/Providers'
import Policies from './pages/Policies'
import Reminders from './pages/Reminders'
import Rfs from './pages/Rfs'
import Tickets from './pages/Tickets'
import Todos from './pages/Todos'
import Users from './pages/Users'

export type View =
  | 'dashboard'
  | 'todos'
  | 'daily'
  | 'tickets'
  | 'reminders'
  | 'rfs'
  | 'targets'
  | 'providers'
  | 'policies'
  | 'masterdata'
  | 'masterdata-kind'
  | 'masterdata-roles'
  | 'masterdata-templates'
  | 'audit'
  | 'users'
  | 'profile'

type NavItem = { id: View; label: string; icon: string; group: string; adminOnly?: boolean }

/**
 * Navigasi sesuai PLAN.md §8.
 * Item "SLA" (F11) sengaja belum ditampilkan.
 */
const NAV: NavItem[] = [
  { id: 'dashboard', label: 'Dashboard', icon: 'dashboard', group: 'OPERASIONAL' },
  { id: 'daily', label: 'Daily Task', icon: 'checklist', group: 'OPERASIONAL' },
  { id: 'todos', label: 'Todo Tasks', icon: 'task_alt', group: 'OPERASIONAL' },
  { id: 'tickets', label: 'Ticketing', icon: 'confirmation_number', group: 'OPERASIONAL' },
  { id: 'reminders', label: 'Reminder', icon: 'alarm', group: 'OPERASIONAL' },
  { id: 'rfs', label: 'RFS', icon: 'event_available', group: 'OPERASIONAL' },
  { id: 'targets', label: 'Notification Targets', icon: 'group', group: 'NOTIFIKASI' },
  { id: 'providers', label: 'Providers', icon: 'hub', group: 'NOTIFIKASI' },
  { id: 'policies', label: 'Escalation Policies', icon: 'stairs', group: 'NOTIFIKASI' },
  { id: 'masterdata', label: 'Master Data', icon: 'database', group: 'MANAJEMEN' },
  { id: 'audit', label: 'Audit Trail', icon: 'history', group: 'MANAJEMEN', adminOnly: true },
  { id: 'users', label: 'Users & Teams', icon: 'manage_accounts', group: 'MANAJEMEN', adminOnly: true },
  { id: 'profile', label: 'Profil Saya', icon: 'account_circle', group: 'MANAJEMEN' },
]

const NAV_GROUPS = ['OPERASIONAL', 'NOTIFIKASI', 'MANAJEMEN']

/**
 * isNavActive menentukan apakah sebuah item nav dianggap aktif.
 *
 * Halaman turunan Master Data (kind & roles) tetap menyorot menu "Master Data"
 * sehingga konteks navigasi tidak hilang.
 */
export function isNavActive(view: View, itemId: View): boolean {
  if (view === itemId) return true
  if (itemId === 'masterdata' && (view === 'masterdata-kind' || view === 'masterdata-roles' || view === 'masterdata-templates')) {
    return true
  }
  return false
}

type HealthState = 'loading' | 'online' | 'degraded' | 'offline'

export default function App() {
  const [authenticated, setAuthenticated] = useState<boolean>(() => isAuthenticated())
  const [user, setUser] = useState<ApiUser | null>(() => getStoredUser())
  const [view, setView] = useState<View>('dashboard')
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(
    () => localStorage.getItem('ingatin_sidebar_collapsed') === '1',
  )
  const [isNarrow, setIsNarrow] = useState<boolean>(
    () => typeof window !== 'undefined' && window.matchMedia('(max-width: 820px)').matches,
  )
  const [health, setHealth] = useState<HealthState>('loading')
  const [appVersion, setAppVersion] = useState('—')
  const [schemaVersion, setSchemaVersion] = useState<number | null>(null)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])
  // focusItem dipakai lonceng notifikasi untuk membuka detail item langsung.
  const [focusItem, setFocusItem] = useState<{ id: string; view: View } | null>(null)
  // masterDataKind menyimpan kelompok master data yang sedang dikelola.
  const [masterDataKind, setMasterDataKind] = useState<string>('customer')

  const pushToast = useCallback((kind: Toast['kind'], title: string, body?: string) => {
    const id = Date.now() + Math.floor(Math.random() * 1000)
    setToasts((prev) => [...prev.slice(-3), { id, kind, title, body }])
    window.setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 6000)
  }, [])

  const dismissToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  // Sesi tidak dapat dipulihkan → kembali ke login.
  useEffect(() => {
    setUnauthorizedHandler(() => {
      clearSession()
      setAuthenticated(false)
      setUser(null)
    })
    return () => setUnauthorizedHandler(null)
  }, [])

  // Pantau breakpoint.
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 820px)')
    const onChange = (e: MediaQueryListEvent) => setIsNarrow(e.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  // Verifikasi sesi + ambil profil saat aplikasi dibuka.
  useEffect(() => {
    if (!authenticated) return
    let alive = true
    api
      .me()
      .then((me) => {
        if (!alive) return
        setUser(me)
        localStorage.setItem('ingatin_user', JSON.stringify(me))
      })
      .catch(() => {
        if (alive) {
          clearSession()
          setAuthenticated(false)
          setUser(null)
        }
      })
    return () => {
      alive = false
    }
  }, [authenticated])

  // Cek kesehatan backend berkala.
  useEffect(() => {
    if (!authenticated) return
    let alive = true
    async function check() {
      try {
        const h = await api.health()
        if (!alive) return
        setHealth(h.database === 'ok' && h.status === 'ok' ? 'online' : 'degraded')
        setAppVersion(h.version)
      } catch {
        if (alive) setHealth('offline')
      }
      try {
        const v = await api.version()
        if (alive) setSchemaVersion(v.schema_version)
      } catch {
        /* informatif saja */
      }
    }
    check()
    const timer = window.setInterval(check, 30_000)
    return () => {
      alive = false
      window.clearInterval(timer)
    }
  }, [authenticated])

  async function logout() {
    await api.logout()
    setAuthenticated(false)
    setUser(null)
    setView('dashboard')
  }

  function navigate(next: View) {
    setView(next)
    setMobileNavOpen(false)
    window.scrollTo({ top: 0 })
  }

  // openNotificationItem menerima id work item dari lonceng lalu membuka
  // halaman daftar yang tepat dan meminta halaman itu menampilkan detailnya.
  const openNotificationItem = useCallback(async (itemId: string) => {
    try {
      const item = await workItemsApi.get(itemId)
      const t = item.item.item_type
      const v: View =
        t === 'reminder'
          ? 'reminders'
          : t === 'rfs'
            ? 'rfs'
            : t === 'daily_task'
              ? 'daily'
              : t === 'incident' || t === 'request' || t === 'change'
                ? 'tickets'
                : 'todos'
      setFocusItem({ id: itemId, view: v })
      setView(v)
      setMobileNavOpen(false)
      window.scrollTo({ top: 0 })
    } catch {
      // Bila item sudah dihapus, buka halaman todo sebagai fallback.
      setView('todos')
    }
  }, [])

  function toggleSidebar() {
    setSidebarCollapsed((prev) => {
      const next = !prev
      localStorage.setItem('ingatin_sidebar_collapsed', next ? '1' : '0')
      return next
    })
  }

  if (!authenticated) {
    return (
      <>
        <Login
          onAuthenticated={(loggedIn) => {
            setUser(loggedIn)
            localStorage.setItem('ingatin_user', JSON.stringify(loggedIn))
            setAuthenticated(true)
            setView('dashboard')
          }}
        />
        <ToastStack toasts={toasts} onDismiss={dismissToast} />
      </>
    )
  }

  const isAdmin = user?.role === 'admin'
  const visibleNav = NAV.filter((n) => !n.adminOnly || isAdmin)
  const collapsed = sidebarCollapsed || isNarrow
  const activeLabel =
    visibleNav.find((n) => n.id === view)?.label ??
    (view === 'masterdata-kind' || view === 'masterdata-roles' || view === 'masterdata-templates' ? 'Master Data' : 'Dashboard')

  return (
    <div className="min-h-screen bg-background">
      <div className="flex">
        <aside
          className={`hidden shrink-0 border-r border-border bg-surface-container-lowest min-[821px]:block ${
            collapsed ? 'w-[72px]' : 'w-[232px]'
          } transition-[width] duration-150`}
        >
          <SidebarContent
            collapsed={collapsed}
            view={view}
            nav={visibleNav}
            onNavigate={navigate}
            onToggleCollapse={toggleSidebar}
          />
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
          <Topbar
            title={activeLabel}
            health={health}
            user={user}
            isNarrow={isNarrow}
            onOpenNav={() => setMobileNavOpen(true)}
            onLogout={logout}
            onOpenProfile={() => navigate('profile')}
            onOpenItem={openNotificationItem}
            toast={pushToast}
          />

          <main className="flex-1 px-4 py-6 pb-24 sm:px-6 min-[821px]:pb-6">
            {view === 'dashboard' && (
              <Dashboard
                user={user}
                schemaVersion={schemaVersion}
                appVersion={appVersion}
                health={health}
                onNavigate={navigate}
              />
            )}
            {view === 'todos' && <Todos user={user} toast={pushToast} focusItemId={focusItem?.view === 'todos' ? focusItem.id : null} onFocusConsumed={() => setFocusItem(null)} />}
            {view === 'daily' && <DailyTasks user={user} toast={pushToast} focusItemId={focusItem?.view === 'daily' ? focusItem.id : null} onFocusConsumed={() => setFocusItem(null)} />}
            {view === 'tickets' && <Tickets user={user} toast={pushToast} focusItemId={focusItem?.view === 'tickets' ? focusItem.id : null} onFocusConsumed={() => setFocusItem(null)} />}
            {view === 'reminders' && <Reminders user={user} toast={pushToast} focusItemId={focusItem?.view === 'reminders' ? focusItem.id : null} onFocusConsumed={() => setFocusItem(null)} />}
            {view === 'rfs' && <Rfs user={user} toast={pushToast} focusItemId={focusItem?.view === 'rfs' ? focusItem.id : null} onFocusConsumed={() => setFocusItem(null)} />}
            {view === 'targets' && <Targets user={user} toast={pushToast} />}
            {view === 'providers' && <Providers user={user} toast={pushToast} />}
            {view === 'policies' && <Policies user={user} toast={pushToast} />}
            {view === 'masterdata' && (
              <MasterData
                user={user}
                onOpenKind={(kind) => {
                  setMasterDataKind(kind)
                  navigate('masterdata-kind')
                }}
                onOpenRoles={() => navigate('masterdata-roles')}
                onOpenTemplates={() => navigate('masterdata-templates')}
              />
            )}
            {view === 'masterdata-kind' && (
              <MasterDataKindPage
                user={user}
                toast={pushToast}
                kind={masterDataKind}
                onBack={() => navigate('masterdata')}
              />
            )}
            {view === 'masterdata-roles' && isAdmin && (
              <RolePermissionsPage user={user} toast={pushToast} onBack={() => navigate('masterdata')} />
            )}
            {view === 'masterdata-templates' && isAdmin && (
              <NotificationTemplatesPage user={user} toast={pushToast} onBack={() => navigate('masterdata')} />
            )}
            {view === 'audit' && isAdmin && <Audit user={user} />}
            {view === 'users' && isAdmin && <Users user={user} toast={pushToast} />}
            {view === 'profile' && <Profile user={user} onUserChange={setUser} toast={pushToast} />}
          </main>
        </div>
      </div>

      {isNarrow && mobileNavOpen && (
        <MobileSheet
          view={view}
          nav={visibleNav}
          onNavigate={navigate}
          onClose={() => setMobileNavOpen(false)}
          onLogout={logout}
        />
      )}

      {isNarrow && <BottomNav view={view} nav={visibleNav} onNavigate={navigate} />}

      <ToastStack toasts={toasts} onDismiss={dismissToast} />
    </div>
  )
}

/* ------------------------------------------------------------------------- */

function SidebarContent({
  collapsed,
  view,
  nav,
  onNavigate,
  onToggleCollapse,
}: {
  collapsed: boolean
  view: View
  nav: NavItem[]
  onNavigate: (v: View) => void
  onToggleCollapse: () => void
}) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-16 items-center justify-between gap-2 border-b border-border px-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-accent-soft bg-success-container/40">
            <span className="material-symbols-outlined text-[22px] text-primary">notifications_active</span>
          </div>
          {!collapsed && (
            <div className="flex min-w-0 flex-col">
              <span className="truncate font-headline text-lg font-bold leading-tight tracking-tight">
                Ingat.in
              </span>
              <span className="truncate text-label-sm uppercase tracking-widest text-text-secondary">
                NOC Reminder Hub
              </span>
            </div>
          )}
        </div>
        {!collapsed && (
          <button
            onClick={onToggleCollapse}
            className="btn-ghost h-8 w-8 px-0"
            title="Ciutkan sidebar"
            aria-label="Ciutkan sidebar"
          >
            <span className="material-symbols-outlined text-[18px]">left_panel_close</span>
          </button>
        )}
      </div>

      {collapsed && (
        <button
          onClick={onToggleCollapse}
          className="btn-ghost mx-auto mt-2 h-8 w-8 px-0"
          title="Bentangkan sidebar"
          aria-label="Bentangkan sidebar"
        >
          <span className="material-symbols-outlined text-[18px]">left_panel_open</span>
        </button>
      )}

      <nav className="flex-1 overflow-y-auto px-2 py-3">
        {NAV_GROUPS.map((group) => {
          const items = nav.filter((n) => n.group === group)
          if (items.length === 0) return null
          return (
            <div key={group} className="mb-3">
              {!collapsed && <div className="px-3 pb-1.5 kicker">{group}</div>}
              {items.map((item) => (
                <button
                  key={item.id}
                  onClick={() => onNavigate(item.id)}
                  title={collapsed ? item.label : undefined}
                  className={`nav-item ${isNavActive(view, item.id) ? 'nav-item-active' : ''} ${
                    collapsed ? 'justify-center px-0' : ''
                  }`}
                >
                  <span className="material-symbols-outlined text-[19px]">{item.icon}</span>
                  {!collapsed && <span className="truncate">{item.label}</span>}
                </button>
              ))}
            </div>
          )
        })}
      </nav>
    </div>
  )
}

function Topbar({
  title,
  health,
  user,
  isNarrow,
  onOpenNav,
  onLogout,
  onOpenProfile,
  onOpenItem,
  toast,
}: {
  title: string
  health: HealthState
  user: ApiUser | null
  isNarrow: boolean
  onOpenNav: () => void
  onLogout: () => void
  onOpenProfile: () => void
  onOpenItem: (itemId: string) => void
  toast: (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void
}) {
  const healthUI: Record<HealthState, { label: string; dot: string }> = {
    loading: { label: 'Memeriksa', dot: 'bg-unknown' },
    online: { label: 'Backend online', dot: 'bg-success' },
    degraded: { label: 'Database bermasalah', dot: 'bg-warning' },
    offline: { label: 'Backend offline', dot: 'bg-critical' },
  }
  const h = healthUI[health]

  const roleLabel: Record<string, string> = {
    admin: 'Administrator',
    noc: 'NOC',
    agent: 'Agent',
    sales: 'Sales',
    viewer: 'Viewer',
    customer: 'Customer',
  }

  return (
    <header className="sticky top-0 z-30 border-b border-border bg-[#FDFDFD]/90 backdrop-blur-md shadow-sm">
      <div className="flex h-16 items-center justify-between gap-3 px-4 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          {isNarrow && (
            <button onClick={onOpenNav} className="btn-ghost h-9 w-9 px-0" aria-label="Buka menu" title="Buka menu">
              <span className="material-symbols-outlined text-[20px]">menu</span>
            </button>
          )}
          <div className="min-w-0">
            <div className="kicker">Ingat.in</div>
            <h1 className="truncate font-headline text-xl font-semibold leading-tight">{title}</h1>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span
            className="hidden items-center gap-2 rounded-full border border-border bg-surface-container-lowest px-3 py-1.5 text-label-sm font-semibold text-text-secondary lg:inline-flex"
            title={h.label}
          >
            <span className={`h-2 w-2 rounded-full ${h.dot}`} aria-hidden />
            {h.label}
          </span>

          <NotificationBell onOpenItem={onOpenItem} toast={toast} />

          {user && (
            <button
              onClick={onOpenProfile}
              className="hidden items-center gap-2 rounded-full border border-border bg-surface-container-lowest px-2.5 py-1.5 text-left hover:bg-surface-container-low sm:inline-flex"
              title="Buka profil"
            >
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary-container text-label-sm font-bold text-on-primary-container">
                {user.username.slice(0, 2).toUpperCase()}
              </span>
              <span className="flex flex-col leading-tight">
                <span className="text-label-md font-semibold">{user.username}</span>
                <span className="text-[10px] uppercase tracking-wider text-text-secondary">
                  {roleLabel[user.role] ?? user.role}
                </span>
              </span>
            </button>
          )}

          <button onClick={onLogout} className="btn-secondary" title="Keluar">
            <span className="material-symbols-outlined text-[18px]">logout</span>
            <span className="hidden sm:inline">Keluar</span>
          </button>
        </div>
      </div>
    </header>
  )
}

function MobileSheet({
  view,
  nav,
  onNavigate,
  onClose,
  onLogout,
}: {
  view: View
  nav: NavItem[]
  onNavigate: (v: View) => void
  onClose: () => void
  onLogout: () => void
}) {
  return (
    <div className="fixed inset-0 z-50 min-[821px]:hidden">
      <div className="absolute inset-0 bg-slate-950/60 backdrop-blur-sm" onClick={onClose} aria-hidden />
      <div className="absolute inset-x-0 bottom-0 max-h-[85vh] overflow-y-auto rounded-t-2xl bg-surface-container-lowest p-5 shadow-modal">
        <div className="mb-3 flex items-center justify-between border-b border-border pb-3">
          <span className="font-headline text-lg font-bold">Navigasi</span>
          <button className="btn-ghost h-8 w-8 px-0" onClick={onClose} aria-label="Tutup menu">
            <span className="material-symbols-outlined text-[20px]">close</span>
          </button>
        </div>
        {NAV_GROUPS.map((group) => {
          const items = nav.filter((n) => n.group === group)
          if (items.length === 0) return null
          return (
            <div key={group} className="mb-3">
              <div className="kicker pb-1.5">{group}</div>
              {items.map((item) => (
                <button
                  key={item.id}
                  onClick={() => onNavigate(item.id)}
                  className={`nav-item ${isNavActive(view, item.id) ? 'nav-item-active' : ''}`}
                >
                  <span className="material-symbols-outlined text-[19px]">{item.icon}</span>
                  <span>{item.label}</span>
                </button>
              ))}
            </div>
          )
        })}
        <button onClick={onLogout} className="btn-secondary mt-2 w-full">
          <span className="material-symbols-outlined text-[18px]">logout</span>
          Keluar
        </button>
      </div>
    </div>
  )
}

function BottomNav({
  view,
  nav,
  onNavigate,
}: {
  view: View
  nav: NavItem[]
  onNavigate: (v: View) => void
}) {
  const preferred = ['dashboard', 'daily', 'todos', 'tickets', 'reminders', 'rfs', 'providers']
  const items = preferred
    .map((id) => nav.find((n) => n.id === id))
    .filter((n): n is NavItem => !!n)

  return (
    <nav className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-surface-container-lowest min-[821px]:hidden">
      <div className="no-scrollbar flex overflow-x-auto px-1 py-1">
        {items.map((item) => (
          <button
            key={item.id}
            onClick={() => onNavigate(item.id)}
            className={`flex min-w-[72px] flex-1 flex-col items-center gap-0.5 rounded-control px-2 py-1.5 text-label-sm font-semibold ${
              isNavActive(view, item.id) ? 'text-primary' : 'text-text-secondary'
            }`}
          >
            <span className="material-symbols-outlined text-[20px]">{item.icon}</span>
            <span className="truncate">{item.label}</span>
          </button>
        ))}
      </div>
    </nav>
  )
}
