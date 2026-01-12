import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { X, Sun, Moon, Monitor, LogOut, ChevronDown, ChevronUp } from 'lucide-react'
import { cn } from '@/lib/utils'
import Button from './ui/button'
import Tooltip from './ui/tooltip'

interface User {
  id: number
  email: string
  username: string
  display_name?: string | null
}

interface MobileNavProps {
  open: boolean
  onClose: () => void
  navItems: Array<{ path: string; label: string; icon: React.ComponentType<{ className?: string }> }>
  isActive: (path: string) => boolean
  theme: 'light' | 'dark' | 'system'
  onThemeChange: (theme: 'light' | 'dark' | 'system') => void
  user: User | null
  onLogout: () => void
}

export default function MobileNav({ open, onClose, navItems, isActive, theme, onThemeChange, user, onLogout }: MobileNavProps) {
  const location = useLocation()
  const [adminSitesExpanded, setAdminSitesExpanded] = useState(false)

  // Close menu when route changes
  useEffect(() => {
    if (open) {
      onClose()
    }
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // Prevent body scroll when menu is open
  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => {
      document.body.style.overflow = ''
    }
  }, [open])

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

  // Generate logout URL
  const getLogoutUrl = () => {
    const currentHostname = window.location.hostname
    const protocol = window.location.protocol
    const baseDomain = getBaseDomain()
    return `${protocol}//gatekeeper.${baseDomain}?rd=${encodeURIComponent(currentHostname)}`
  }

  const adminSites = [
    { name: 'Gatekeeper', url: getAdminSiteUrl('gatekeeper') },
    { name: 'Killswitch', url: getAdminSiteUrl('killswitch') },
    { name: 'Arbiter', url: getAdminSiteUrl('arbiter') },
  ]

  return (
    <>
      {/* Backdrop */}
      {open && (
        <div
          className="fixed inset-0 bg-black/50 z-40 md:hidden animate-in fade-in duration-200"
          onClick={onClose}
          aria-hidden="true"
        />
      )}

      {/* Slide-in sidebar */}
      <aside
        className={cn(
          'fixed left-0 top-0 bottom-0 w-64 bg-background border-r z-50 transform transition-transform duration-300 ease-in-out md:hidden flex flex-col overflow-hidden',
          open ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <div className="flex items-center justify-between p-4 border-b flex-shrink-0">
          <div className="flex items-center gap-2">
            <img src="/favicon.svg" alt="Arbiter" className="h-5 w-5" />
            <h2 className="text-lg font-semibold">Menu</h2>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            className="h-8 w-8 p-0"
            aria-label="Close menu"
          >
            <X className="h-5 w-5" />
          </Button>
        </div>

        <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
          <nav className="flex-1 p-4 space-y-1 overflow-y-auto min-h-0">
            {navItems.map((item) => {
              const Icon = item.icon
              const active = isActive(item.path)
              return (
                <Link
                  key={item.path}
                  to={item.path}
                  onClick={onClose}
                  className={cn(
                    'flex items-center gap-3 px-4 py-3 rounded-md text-sm font-medium transition-colors',
                    active
                      ? 'bg-primary text-primary-foreground'
                      : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                  )}
                >
                  <Icon className="h-5 w-5" />
                  {item.label}
                </Link>
              )
            })}
          </nav>

          {/* Footer section with user info, theme, admin sites, and logout */}
          <div className="flex-shrink-0 border-t flex flex-col overflow-hidden">
            {/* Scrollable section for user info, theme, and admin sites */}
            <div className="p-3 sm:p-4 space-y-2 overflow-y-auto flex-1 min-h-0">
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
              <div className="flex gap-1 flex-shrink-0">
                <Button
                  variant={theme === 'light' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => onThemeChange('light')}
                  className="flex-1"
                  aria-label="Light theme"
                  title="Light"
                >
                  <Sun className="h-4 w-4" />
                </Button>
                <Button
                  variant={theme === 'dark' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => onThemeChange('dark')}
                  className="flex-1"
                  aria-label="Dark theme"
                  title="Dark"
                >
                  <Moon className="h-4 w-4" />
                </Button>
                <Button
                  variant={theme === 'system' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => onThemeChange('system')}
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
                            onClick={onClose}
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
            </div>

            {/* Fixed logout button at bottom */}
            <div className="border-t p-3 sm:p-4 flex-shrink-0">
              <div className="w-full [&>div]:block [&>div]:w-full">
                <Tooltip content={getLogoutUrl()}>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      onLogout()
                      onClose()
                    }}
                    className="w-full"
                    aria-label="Logout"
                  >
                    <LogOut className="h-4 w-4 mr-2" />
                    <span>Logout</span>
                  </Button>
                </Tooltip>
              </div>
            </div>
          </div>
        </div>
      </aside>
    </>
  )
}
