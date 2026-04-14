import { useState, useMemo } from 'react'
import { Outlet, Link, useLocation } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { useTheme } from '@/hooks/useTheme'
import { Menu, LogOut, Sun, Moon, Monitor, FileText, Search, TestTube, Activity, ChevronDown, ChevronUp } from 'lucide-react'
import MobileNav from './MobileNav'
import Button from './ui/button'
import Tooltip from './ui/tooltip'
import { cn } from '@/lib/utils'

export default function ArbiterLayout() {
  const { user } = useAuth()
  const { theme, setTheme } = useTheme()
  const location = useLocation()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [adminSitesExpanded, setAdminSitesExpanded] = useState(false)

  const navItems = useMemo(() => {
    return [
      { path: '/', label: 'Policies', icon: FileText },
      { path: '/effective', label: 'Effective', icon: Search },
      { path: '/tester', label: 'Tester', icon: TestTube },
      { path: '/telemetry', label: 'Telemetry', icon: Activity },
    ]
  }, [])

  const isActive = (path: string) => {
    if (path === '/') {
      return location.pathname === '/'
    }
    return location.pathname.startsWith(path)
  }

  // Extract base domain helper function
  // For .home.arpa domains, use last 3 parts (e.g., arbiter.lan.home.arpa -> lan.home.arpa)
  // For .domostack.me domains, use last 2 parts (e.g., arbiter.domostack.me -> domostack.me)
  const getBaseDomain = () => {
    const currentHostname = window.location.hostname
    const parts = currentHostname.split('.')

    // Handle IP addresses or localhost - use as-is
    if (parts[parts.length - 1].match(/^\d+$/) || currentHostname === 'localhost') {
      return currentHostname
    }

    // For .home.arpa domains, take last 3 parts (e.g., lan.home.arpa)
    if (currentHostname.endsWith('.home.arpa') || currentHostname === 'home.arpa') {
      if (parts.length >= 3) {
        return parts.slice(-3).join('.')
      }
      // Fallback if not enough parts
      return parts.slice(-2).join('.')
    }

    // For .domostack.me domains, take last 2 parts (e.g., domostack.me)
    if (currentHostname.endsWith('.domostack.me') || currentHostname === 'domostack.me') {
      return parts.slice(-2).join('.')
    }

    // Default: take last 2 parts for standard domains
    if (parts.length >= 2) {
      return parts.slice(-2).join('.')
    }

    // Fallback: use as-is
    return currentHostname
  }

  // Generate admin site URL
  const getAdminSiteUrl = (site: string) => {
    const protocol = window.location.protocol
    const baseDomain = getBaseDomain()
    return `${protocol}//${site}.${baseDomain}`
  }

  const getLogoutUrl = () => {
    const currentHostname = window.location.hostname
    const protocol = window.location.protocol
    const baseDomain = getBaseDomain()
    return `${protocol}//gatekeeper.${baseDomain}?rd=${encodeURIComponent(currentHostname)}`
  }

  const handleLogout = () => {
    window.location.href = getLogoutUrl()
  }

  const adminSites = [
    { name: 'Gatekeeper', url: getAdminSiteUrl('gatekeeper') },
    { name: 'Killswitch', url: getAdminSiteUrl('killswitch') },
    { name: 'Arbiter', url: getAdminSiteUrl('arbiter') },
    { name: 'Control Plane', url: getAdminSiteUrl('control-plane') },
  ]

  return (
    <div className="min-h-screen bg-background">
      {/* Desktop Sidebar */}
      <aside className="hidden md:fixed md:inset-y-0 md:flex md:w-64 md:flex-col md:border-r">
        <div className="flex flex-col flex-grow pt-5 pb-4 overflow-y-auto bg-card border-r">
          <div className="flex items-center flex-shrink-0 px-4 gap-2">
            <img src="/favicon.svg" alt="Arbiter" className="h-6 w-6" />
            <h1 className="text-xl font-bold">Arbiter</h1>
          </div>
          <nav className="mt-5 flex-1 px-2 space-y-1">
            {navItems.map((item) => {
              const Icon = item.icon
              const active = isActive(item.path)
              return (
                <Link
                  key={item.path}
                  to={item.path}
                  className={cn(
                    'group flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors',
                    active
                      ? 'bg-primary text-primary-foreground'
                      : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                  )}
                >
                  <Icon className="mr-3 h-5 w-5 flex-shrink-0" />
                  {item.label}
                </Link>
              )
            })}
          </nav>
          <div className="flex-shrink-0 border-t p-4 space-y-3">
            {/* User Info */}
            {user && (
              <div className="min-w-0">
                <p className="text-sm font-medium text-foreground truncate">
                  {user.display_name || user.username}
                </p>
                {user.email && (
                  <p className="text-xs text-muted-foreground truncate">{user.email}</p>
                )}
              </div>
            )}

            {/* Theme Select */}
            <div className="flex gap-1">
              <Button
                variant={theme === 'light' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('light')}
                className="flex-1"
                aria-label="Light theme"
                title="Light"
              >
                <Sun className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === 'dark' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('dark')}
                className="flex-1"
                aria-label="Dark theme"
                title="Dark"
              >
                <Moon className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === 'system' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setTheme('system')}
                className="flex-1"
                aria-label="System theme"
                title="System"
              >
                <Monitor className="h-4 w-4" />
              </Button>
            </div>

            {/* Horizontal divider */}
            <div className="border-t"></div>

            {/* Admin Sites Links */}
            <div className="space-y-2">
              <button
                onClick={() => setAdminSitesExpanded(!adminSitesExpanded)}
                className="flex items-center justify-between w-full text-xs font-medium text-muted-foreground uppercase tracking-wide mb-2 px-1 hover:text-foreground transition-colors"
              >
                <span>Admin Sites</span>
                {adminSitesExpanded ? (
                  <ChevronUp className="h-3 w-3" />
                ) : (
                  <ChevronDown className="h-3 w-3" />
                )}
              </button>
              {adminSitesExpanded && (
                <div className="space-y-2">
                  {adminSites.map((site) => (
                    <div key={site.name} className="w-full [&>div]:block [&>div]:w-full">
                      <Tooltip content={site.url}>
                        <a
                          href={site.url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="block w-full"
                        >
                          <Button
                            variant="outline"
                            size="sm"
                            className="w-full"
                          >
                            {site.name}
                          </Button>
                        </a>
                      </Tooltip>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Horizontal divider */}
            <div className="border-t"></div>

            {/* Logout Button */}
            <div className="w-full [&>div]:block [&>div]:w-full">
              <Tooltip content={getLogoutUrl()}>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleLogout}
                  className="w-full"
                  aria-label="Logout"
                >
                  <LogOut className="h-4 w-4 mr-2" />
                  <span className="text-xs">Logout</span>
                </Button>
              </Tooltip>
            </div>
          </div>
        </div>
      </aside>

      {/* Mobile Header */}
      <div className="md:hidden sticky top-0 z-30 flex h-16 items-center gap-4 border-b bg-background px-4">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setMobileMenuOpen(true)}
          className="md:hidden"
          aria-label="Open menu"
        >
          <Menu className="h-6 w-6" />
        </Button>
        <div className="flex items-center gap-2">
          <img src="/favicon.svg" alt="Arbiter" className="h-5 w-5" />
          <h1 className="text-lg font-semibold">Arbiter</h1>
        </div>
        {user && (
          <div className="ml-auto flex items-center gap-2">
            <span className="text-sm text-muted-foreground truncate max-w-[120px]">
              {user.display_name || user.username}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={handleLogout}
              aria-label="Logout"
            >
              <LogOut className="h-4 w-4 mr-1" />
              <span className="text-xs">Logout</span>
            </Button>
          </div>
        )}
      </div>

      {/* Mobile Navigation */}
      <MobileNav
        open={mobileMenuOpen}
        onClose={() => setMobileMenuOpen(false)}
        navItems={navItems}
        isActive={isActive}
        theme={theme}
        onThemeChange={(newTheme) => setTheme(newTheme)}
        user={user}
        onLogout={handleLogout}
      />

      {/* Main Content */}
      <div className="md:pl-64">
        <main className="py-6">
          <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
            <Outlet />
          </div>
        </main>
      </div>

    </div>
  )
}
