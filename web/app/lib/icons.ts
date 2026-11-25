import {
  mdiAlertCircleOutline,
  mdiAlertOutline,
  mdiCheckCircleOutline,
  mdiClose,
  mdiCubeOutline,
  mdiInformationOutline,
  mdiLayersOutline,
  mdiMenu,
  mdiTagMultipleOutline,
  mdiTimelineTextOutline,
  mdiTrayFull,
  mdiTranslate,
  mdiViewDashboardOutline,
  mdiWeatherNight,
  mdiWeatherSunny,
} from '@mdi/js'

/**
 * SVG paths (24x24 viewBox) of the icons the interface uses, keyed by their Material Design Icons name.
 * Add an entry here when a page needs a new icon; only the listed icons end up in the bundle.
 */
export const ICONS: Readonly<Record<string, string>> = {
  'mdi-alert-circle-outline': mdiAlertCircleOutline,
  'mdi-alert-outline': mdiAlertOutline,
  'mdi-check-circle-outline': mdiCheckCircleOutline,
  'mdi-close': mdiClose,
  'mdi-cube-outline': mdiCubeOutline,
  'mdi-information-outline': mdiInformationOutline,
  'mdi-layers-outline': mdiLayersOutline,
  'mdi-menu': mdiMenu,
  'mdi-tag-multiple-outline': mdiTagMultipleOutline,
  'mdi-timeline-text-outline': mdiTimelineTextOutline,
  'mdi-tray-full': mdiTrayFull,
  'mdi-translate': mdiTranslate,
  'mdi-view-dashboard-outline': mdiViewDashboardOutline,
  'mdi-weather-night': mdiWeatherNight,
  'mdi-weather-sunny': mdiWeatherSunny,
}

/** Path data for an icon name. Unknown names render an empty glyph instead of breaking the page. */
export function iconPath(name: string): string {
  return ICONS[name] ?? ''
}
