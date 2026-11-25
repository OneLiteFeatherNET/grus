/** Main navigation. `key` names the i18n entry under common.nav.<key>; `icon` is an ICONS key. */
export interface NavItem {
  to: string
  key: 'overview' | 'pools' | 'instances' | 'templates' | 'events'
  icon: string
}

export const NAV_ITEMS: readonly NavItem[] = [
  { to: '/', key: 'overview', icon: 'mdi-view-dashboard-outline' },
  { to: '/pools', key: 'pools', icon: 'mdi-layers-outline' },
  { to: '/instances', key: 'instances', icon: 'mdi-cube-outline' },
  { to: '/templates', key: 'templates', icon: 'mdi-tag-multiple-outline' },
  { to: '/events', key: 'events', icon: 'mdi-timeline-text-outline' },
]
