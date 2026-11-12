import {initBranchroom} from './branchroom.ts';
import {createBranchroomTelemetry, branchroomTelemetryPreference} from './branchroom-telemetry.ts';
import {localUserSettings} from '../modules/user-settings.ts';

const privateFixture = 'synthetic-private-scope/user-123?token=fixture-only';
const endpoint = 'https://collector.invalid/v1/events';
const transport = vi.fn<(url: RequestInfo | URL, options?: RequestInit) => Promise<Response>>();

function body(options?: RequestInit): string {
  if (typeof options!.body !== 'string') throw new Error('expected a JSON string');
  return options!.body;
}

beforeEach(() => {
  localUserSettings.setBoolean(branchroomTelemetryPreference, false);
  vi.spyOn(navigator, 'doNotTrack', 'get').mockReturnValue(null);
  transport.mockReset().mockResolvedValue(new Response(null, {status: 204}));
  vi.stubGlobal('fetch', transport);
});

afterEach(() => {
  document.body.replaceChildren();
  document.querySelectorAll('meta[name^="branchroom-telemetry-"]').forEach((element) => { element.remove() });
  Reflect.deleteProperty(navigator, 'globalPrivacyControl');
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

test('collection is off without explicit valid configuration and after privacy opt-out', async () => {
  await createBranchroomTelemetry('settings').record('screen_view');
  await createBranchroomTelemetry('settings', false, endpoint).record('screen_view');
  for (const invalid of ['', 'http://collector.invalid/v1/events', '//collector.invalid/v1/events', '/v1/events?token=fixture', 'https://user:password@collector.invalid/v1/events']) {
    await createBranchroomTelemetry('settings', true, invalid).record('screen_view');
  }
  vi.spyOn(navigator, 'doNotTrack', 'get').mockReturnValue('1');
  await createBranchroomTelemetry('settings', true, endpoint).record('screen_view');
  vi.spyOn(navigator, 'doNotTrack', 'get').mockReturnValue(null);
  Object.defineProperty(navigator, 'globalPrivacyControl', {configurable: true, value: true});
  await createBranchroomTelemetry('settings', true, endpoint).record('screen_view');
  Object.defineProperty(navigator, 'globalPrivacyControl', {configurable: true, value: false});
  localUserSettings.setBoolean(branchroomTelemetryPreference, true);
  await createBranchroomTelemetry('settings', true, endpoint).record('screen_view');
  expect(transport).not.toHaveBeenCalled();
});

test('the payload and transport exclude identity, content and browser credentials', async () => {
  document.body.textContent = privateFixture;
  await createBranchroomTelemetry('workspace', true, endpoint).record('storage_failed');
  expect(transport).toHaveBeenCalledOnce();
  const [url, options] = transport.mock.calls[0];
  expect(url).toBe(endpoint);
  expect(options).toMatchObject({method: 'POST', credentials: 'omit', referrerPolicy: 'no-referrer', redirect: 'error'});
  expect(options!.signal).toBeInstanceOf(AbortSignal);
  expect(options!.headers).toEqual(new Headers({'content-type': 'application/json'}));
  expect(JSON.parse(body(options))).toEqual({version: 1, app: 'branchroom', kind: 'error', name: 'storage_failed', surface: 'web', route: 'workspace'});
  expect(body(options)).not.toContain(privateFixture);
});

test('rate bounds, error deduplication and transport rejection do not create a retry queue', async () => {
  let now = 1000000;
  vi.spyOn(Date, 'now').mockImplementation(() => now);
  const client = createBranchroomTelemetry('settings', true, endpoint);
  await client.record('storage_failed');
  await client.record('storage_failed');
  expect(transport).toHaveBeenCalledOnce();
  for (let i = 0; i < 25; i++) await client.record('screen_view');
  expect(transport).toHaveBeenCalledTimes(20);
  for (let minute = 0; minute < 12; minute++) {
    now += 60000;
    for (let i = 0; i < 20; i++) await client.record('screen_view');
  }
  expect(transport).toHaveBeenCalledTimes(200);
  transport.mockClear().mockRejectedValue(new Error(privateFixture));
  const failing = createBranchroomTelemetry('settings', true, endpoint);
  await expect(failing.record('screen_view')).resolves.toBeUndefined();
  expect(transport).toHaveBeenCalledOnce();
});

test('only one request can be in flight and it receives a two-second abort signal', async () => {
  const timeout = vi.spyOn(AbortSignal, 'timeout');
  const pendingRequest = Promise.withResolvers<Response>();
  transport.mockReturnValue(pendingRequest.promise);
  const client = createBranchroomTelemetry('settings', true, endpoint);
  const pending = client.record('screen_view');
  await client.record('storage_failed');
  expect(transport).toHaveBeenCalledOnce();
  expect(timeout).toHaveBeenCalledWith(2000);
  pendingRequest.resolve(new Response(null, {status: 204}));
  await pending;
});

function mountHandoff() {
  const root = document.createElement('main');
  root.className = 'branchroom-source-handoffs';
  root.innerHTML = '<input id="branchroom-telemetry-disable" type="checkbox"><p id="branchroom-telemetry-status" data-off="Off" data-on="On"></p><div class="item"><input id="clone" readonly><button type="button" class="branchroom-copy" data-copy-target="clone">Copy URL</button><p class="branchroom-copy-status" role="status" data-success="Copied" data-failure="Select and copy manually"></p></div>';
  root.querySelector<HTMLInputElement>('#clone')!.value = privateFixture;
  document.body.append(root);
  return root;
}

test('the real copy failure preserves manual recovery and emits only a fixed category', async () => {
  for (const [name, content] of [['enabled', 'true'], ['endpoint', endpoint]]) {
    const meta = document.createElement('meta');
    meta.name = `branchroom-telemetry-${name}`;
    meta.content = content;
    document.head.append(meta);
  }
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error(privateFixture));
  const root = mountHandoff();
  initBranchroom();
  await vi.waitFor(() => expect(transport).toHaveBeenCalledOnce());
  root.querySelector<HTMLButtonElement>('button')!.click();
  await vi.waitFor(() => expect(root.querySelector('.branchroom-copy-status')!.textContent).toBe('Select and copy manually'));
  expect(copy).toHaveBeenCalledWith(privateFixture);
  expect(document.activeElement).toBe(root.querySelector('#clone'));
  expect(root.querySelector<HTMLButtonElement>('button')!.disabled).toBe(false);
  expect(transport).toHaveBeenCalledTimes(2);
  expect(transport.mock.calls.map(([, options]) => JSON.parse(body(options)))).toEqual([
    {version: 1, app: 'branchroom', kind: 'count', name: 'screen_view', surface: 'web', route: 'workspace'},
    {version: 1, app: 'branchroom', kind: 'error', name: 'storage_failed', surface: 'web', route: 'workspace'},
  ]);
  for (const [, options] of transport.mock.calls) expect(body(options)).not.toContain(privateFixture);
  const storage = vi.spyOn(Storage.prototype, 'setItem');
  const control = root.querySelector<HTMLInputElement>('#branchroom-telemetry-disable')!;
  control.checked = true;
  control.dispatchEvent(new Event('change'));
  expect(storage).toHaveBeenCalledWith('gitea:setting:branchroom-telemetry-disabled', 'true');
  root.querySelector<HTMLButtonElement>('button')!.click();
  await vi.waitFor(() => expect(copy).toHaveBeenCalledTimes(2));
  expect(transport).toHaveBeenCalledTimes(2);
});

test('copy success keeps focus and suppresses repeated activation while pending', async () => {
  const pending = Promise.withResolvers<void>();
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockReturnValue(pending.promise);
  const root = mountHandoff();
  initBranchroom();
  const button = root.querySelector<HTMLButtonElement>('button')!;
  button.focus();
  button.click();
  button.click();
  expect(copy).toHaveBeenCalledOnce();
  expect(document.activeElement).toBe(button);
  expect(button.getAttribute('aria-disabled')).toBe('true');
  pending.resolve();
  await vi.waitFor(() => expect(root.querySelector('.branchroom-copy-status')!.textContent).toBe('Copied'));
  expect(document.activeElement).toBe(button);
  expect(button.hasAttribute('aria-disabled')).toBe(false);
});

test('unreadable preferences disable collection without raw error logging or broken controls', async () => {
  const consoleError = vi.spyOn(console, 'error');
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error(privateFixture) });
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error(privateFixture) });
  await createBranchroomTelemetry('settings', true, endpoint).record('screen_view');
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
  const root = mountHandoff();
  expect(() => initBranchroom()).not.toThrow();
  const control = root.querySelector<HTMLInputElement>('#branchroom-telemetry-disable')!;
  expect(control.checked).toBe(true);
  control.dispatchEvent(new Event('change'));
  root.querySelector<HTMLButtonElement>('button')!.click();
  await vi.waitFor(() => expect(copy).toHaveBeenCalledOnce());
  expect(transport).not.toHaveBeenCalled();
  expect(consoleError).not.toHaveBeenCalled();
});

test('native range validation retains values and records only the fixed invalid category', async () => {
  for (const [name, content] of [['enabled', 'true'], ['endpoint', endpoint]]) {
    const meta = document.createElement('meta');
    meta.name = `branchroom-telemetry-${name}`;
    meta.content = content;
    document.head.append(meta);
  }
  const root = mountHandoff();
  root.className = 'page-content branchroom';
  root.querySelector('#branchroom-telemetry-disable')!.remove();
  root.querySelector('#branchroom-telemetry-status')!.remove();
  const form = document.createElement('form');
  form.innerHTML = '<input type="datetime-local" name="not_before"><input type="datetime-local" name="expires"><p class="branchroom-time-error" data-error-message="Expiry must follow start"></p>';
  root.append(form);
  initBranchroom();
  await vi.waitFor(() => expect(transport).toHaveBeenCalledOnce());
  const start = form.querySelector<HTMLInputElement>('[name="not_before"]')!;
  const end = form.querySelector<HTMLInputElement>('[name="expires"]')!;
  start.value = '2026-10-20T12:00';
  end.value = '2026-10-19T12:00';
  end.dispatchEvent(new Event('input'));
  const submit = vi.fn();
  form.addEventListener('submit', submit);
  form.requestSubmit();
  expect(submit).not.toHaveBeenCalled();
  expect(start.value).toBe('2026-10-20T12:00');
  expect(end.getAttribute('aria-invalid')).toBe('true');
  await vi.waitFor(() => expect(transport).toHaveBeenCalledTimes(2));
  expect(JSON.parse(body(transport.mock.calls[1][1]))).toEqual({version: 1, app: 'branchroom', kind: 'error', name: 'validation_failed', surface: 'web', route: 'settings'});
  end.value = '2026-10-21T12:00';
  end.dispatchEvent(new Event('input'));
  expect(end.validity.valid).toBe(true);
  expect(end.hasAttribute('aria-invalid')).toBe(false);
  expect(form.querySelector('.branchroom-time-error')!.textContent).toBe('');
});

function unreadableSignal(signal: 'doNotTrack' | 'globalPrivacyControl') {
  const getter = vi.fn(() => { throw new Error(privateFixture) });
  if (signal === 'doNotTrack') {
    vi.spyOn(navigator, 'doNotTrack', 'get').mockImplementation(getter);
  } else {
    Object.defineProperty(navigator, 'globalPrivacyControl', {configurable: true, get: getter});
  }
  return getter;
}

test.each(['doNotTrack', 'globalPrivacyControl'] as const)('unreadable %s disables configured collection without rejected calls', async (signal) => {
  unreadableSignal(signal);
  const client = createBranchroomTelemetry('settings', true, endpoint);
  expect(() => client.active()).not.toThrow();
  expect(client.active()).toBe(false);
  await expect(client.record('screen_view')).resolves.toBeUndefined();
  await expect(client.record('storage_failed')).resolves.toBeUndefined();
  expect(transport).not.toHaveBeenCalled();
});

test.each(['doNotTrack', 'globalPrivacyControl'] as const)('default-off and page-disabled clients do not read %s', async (signal) => {
  const getter = unreadableSignal(signal);
  const unconfigured = createBranchroomTelemetry('settings');
  const pageDisabled = createBranchroomTelemetry('workspace', true, endpoint);
  pageDisabled.setDisabled(true);
  for (const client of [unconfigured, pageDisabled]) {
    expect(client.active()).toBe(false);
    await expect(client.record('screen_view')).resolves.toBeUndefined();
    await expect(client.record('storage_failed')).resolves.toBeUndefined();
  }
  expect(getter).not.toHaveBeenCalled();
  expect(transport).not.toHaveBeenCalled();
});

test.each(['doNotTrack', 'globalPrivacyControl'] as const)('unreadable %s leaves privacy status, dialog, shortcut and copy recovery working', async (signal) => {
  for (const [name, content] of [['enabled', 'true'], ['endpoint', endpoint]]) {
    const meta = document.createElement('meta');
    meta.name = `branchroom-telemetry-${name}`;
    meta.content = content;
    document.head.append(meta);
  }
  unreadableSignal(signal);
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error(privateFixture));
  const consoleError = vi.spyOn(console, 'error');
  const root = mountHandoff();
  root.className = 'page-content branchroom';
  root.insertAdjacentHTML('beforeend', '<a class="branchroom-create-link" href="#branchroom-scope-name">Create scope</a><input id="branchroom-scope-name"><button type="button" class="branchroom-show-dialog" data-dialog="confirm">Purge</button><dialog id="confirm"><button type="button" class="branchroom-dialog-cancel" autofocus>Cancel</button><button type="button">Confirm</button></dialog>');
  expect(() => initBranchroom()).not.toThrow();
  expect(root.querySelector('#branchroom-telemetry-status')!.textContent).toBe('Off');
  root.querySelector<HTMLAnchorElement>('.branchroom-create-link')!.click();
  expect(document.activeElement).toBe(root.querySelector('#branchroom-scope-name'));
  const trigger = root.querySelector<HTMLButtonElement>('.branchroom-show-dialog')!;
  const dialog = root.querySelector<HTMLDialogElement>('dialog')!;
  trigger.click();
  expect(dialog.open).toBe(true);
  const cancel = dialog.querySelector<HTMLButtonElement>('.branchroom-dialog-cancel')!;
  expect(document.activeElement).toBe(cancel);
  dialog.querySelector<HTMLButtonElement>('button:last-child')!.focus();
  dialog.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true, cancelable: true}));
  expect(document.activeElement).toBe(cancel);
  cancel.click();
  await vi.waitFor(() => expect(document.activeElement).toBe(trigger));
  root.querySelector<HTMLButtonElement>('.branchroom-copy')!.click();
  await vi.waitFor(() => expect(root.querySelector('.branchroom-copy-status')!.textContent).toBe('Select and copy manually'));
  expect(copy).toHaveBeenCalledWith(privateFixture);
  expect(document.activeElement).toBe(root.querySelector('#clone'));
  expect(transport).not.toHaveBeenCalled();
  expect(consoleError).not.toHaveBeenCalled();
});
