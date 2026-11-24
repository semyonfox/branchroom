import {initBranchroom} from './branchroom.ts';

const cloneURL = 'https://example.invalid/_branchroom/owner/repo/key.git';

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

function mountHandoff() {
  const root = document.createElement('main');
  root.className = 'branchroom-source-handoffs';
  root.innerHTML = '<div class="item"><input id="clone" readonly><button type="button" class="branchroom-copy" data-copy-target="clone">Copy URL</button><p class="branchroom-copy-status" role="status" data-success="Copied" data-failure="Select and copy manually"></p></div>';
  root.querySelector<HTMLInputElement>('#clone')!.value = cloneURL;
  document.body.append(root);
  return root;
}

test('copy failure falls back to selecting the URL for manual copy', async () => {
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'));
  const root = mountHandoff();
  initBranchroom();
  root.querySelector<HTMLButtonElement>('button')!.click();
  await vi.waitFor(() => expect(root.querySelector('.branchroom-copy-status')!.textContent).toBe('Select and copy manually'));
  expect(copy).toHaveBeenCalledWith(cloneURL);
  expect(document.activeElement).toBe(root.querySelector('#clone'));
  expect(root.querySelector<HTMLButtonElement>('button')!.hasAttribute('aria-disabled')).toBe(false);
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
  expect(button.getAttribute('aria-disabled')).toBe('true');
  pending.resolve();
  await vi.waitFor(() => expect(root.querySelector('.branchroom-copy-status')!.textContent).toBe('Copied'));
  expect(document.activeElement).toBe(button);
  expect(button.hasAttribute('aria-disabled')).toBe(false);
});

test('the settings page validates the grant time window without losing values', () => {
  const root = document.createElement('main');
  root.className = 'page-content branchroom';
  const form = document.createElement('form');
  form.innerHTML = '<input type="datetime-local" name="not_before"><input type="datetime-local" name="expires"><p class="branchroom-time-error" data-error-message="Expiry must follow start"></p>';
  root.append(form);
  document.body.append(root);
  expect(() => initBranchroom()).not.toThrow();
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
  end.value = '2026-10-21T12:00';
  end.dispatchEvent(new Event('input'));
  expect(end.validity.valid).toBe(true);
  expect(end.hasAttribute('aria-invalid')).toBe(false);
  expect(form.querySelector('.branchroom-time-error')!.textContent).toBe('');
});
