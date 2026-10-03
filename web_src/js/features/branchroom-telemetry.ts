import {POST} from '../modules/fetch.ts';

const countNames = ['app_open', 'screen_view', 'action_completed', 'action_failed'] as const;
const errorNames = ['unexpected_error', 'request_failed', 'render_failed', 'storage_failed', 'permission_failed', 'media_failed', 'validation_failed'] as const;
type EventName = typeof countNames[number] | typeof errorNames[number];
type Route = 'settings' | 'workspace';

export const branchroomTelemetryPreference = 'branchroom-telemetry-disabled';
const preferenceKey = `gitea:setting:${branchroomTelemetryPreference}`;

export function branchroomTelemetryDisabled(): boolean {
  try {
    // eslint-disable-next-line no-restricted-properties -- shared preference reads log raw errors and fail open
    return window.localStorage.getItem(preferenceKey) === 'true';
  } catch {
    return true;
  }
}

export function setBranchroomTelemetryDisabled(disabled: boolean) {
  try {
    // eslint-disable-next-line no-restricted-properties -- shared preference writes log raw storage errors
    window.localStorage.setItem(preferenceKey, String(disabled));
  } catch {
    // the current-page preference still applies when storage is unavailable
  }
}

export function branchroomTelemetryOptedOut(): boolean {
  return navigator.doNotTrack === '1' || navigator.doNotTrack === 'yes' ||
    ('globalPrivacyControl' in navigator && navigator.globalPrivacyControl === true) ||
    branchroomTelemetryDisabled();
}

function validEndpoint(endpoint: string): boolean {
  if (endpoint.length > 2048 || endpoint.includes('\\')) return false;
  const relative = endpoint.startsWith('/') && !endpoint.startsWith('//');
  try {
    const url = new URL(endpoint, 'https://branchroom.invalid');
    return (relative || (endpoint.startsWith('https://') && url.protocol === 'https:')) &&
      !url.username && !url.password && !url.search && !url.hash && url.pathname.endsWith('/v1/events');
  } catch {
    return false;
  }
}

export function createBranchroomTelemetry(route: Route, enabled = false, endpoint = '') {
  let inFlight = false;
  let sent = 0;
  let windowStart = Date.now();
  let windowCount = 0;
  let disabledForPage = false;
  const lastError = new Map<EventName, number>();
  const configured = enabled && validEndpoint(endpoint) && (route === 'settings' || route === 'workspace');
  const active = () => configured && !disabledForPage && !branchroomTelemetryOptedOut();

  async function record(name: EventName) {
    if (!active() || inFlight || sent >= 200) return;
    const isCount = countNames.some((value) => value === name);
    if (!isCount && errorNames.every((value) => value !== name)) return;
    const now = Date.now();
    if (now - windowStart >= 60000) {
      windowStart = now;
      windowCount = 0;
    }
    if (windowCount >= 20 || (!isCount && lastError.has(name) && now - lastError.get(name)! < 60000)) return;
    if (!isCount) lastError.set(name, now);
    sent++;
    windowCount++;
    inFlight = true;
    try {
      await POST(endpoint, {
        data: {version: 1, app: 'branchroom', kind: isCount ? 'count' : 'error', name, surface: 'web', route},
        credentials: 'omit',
        referrerPolicy: 'no-referrer',
        redirect: 'error',
        signal: AbortSignal.timeout(2000),
      });
    } catch {
      // collection must not affect the workspace flow
    } finally {
      inFlight = false;
    }
  }
  return {active, record, setDisabled: (disabled: boolean) => { disabledForPage = disabled }};
}
