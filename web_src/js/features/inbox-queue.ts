import {POST} from '../modules/fetch.ts';
import {hideModal} from '../modules/modal.ts';
import {showErrorToast} from '../modules/toast.ts';

type Sent = {ok: boolean, json: {redirect?: string, errorMessage?: string} | null};

// merge, review and branch update only follow redirect_to on success, so getting it back is how success is told apart.
// endpoints that answer with plain JSON (data-expect="ok") succeed on any 2xx without an error message
async function send(url: string, data: FormData, redirect: string, expectOk = false): Promise<Sent> {
  data.set('redirect_to', redirect);
  const resp = await POST(url, {data});
  const json = resp.ok ? await resp.json() : null;
  return {ok: expectOk ? resp.ok && !json?.errorMessage : json?.redirect === redirect, json};
}

// the page moves off as soon as the click lands and the request runs meanwhile, the bar stays put to show progress
function setLeaving(bar: HTMLElement, button: HTMLElement | null, leaving: boolean) {
  document.body.classList.toggle('forge-queue-leaving', leaving);
  for (const el of bar.querySelectorAll<HTMLButtonElement>('button')) el.disabled = leaving || el.hasAttribute('data-off');
  if (!button) return;
  const label = button.querySelector('span') ?? button;
  if (leaving) {
    button.setAttribute('data-label', label.textContent);
    label.textContent = button.getAttribute('data-busy') ?? label.textContent;
  } else if (button.hasAttribute('data-label')) {
    label.textContent = button.getAttribute('data-label');
  }
  button.classList.toggle('is-busy', leaving);
}

async function submitBarForm(bar: HTMLElement, form: HTMLFormElement, submitter: HTMLButtonElement | null, next: string) {
  const data = new FormData(form, submitter);
  const isMerge = form.matches('.forge-queue-merge-form');
  const type = data.get('type');
  const did = form.getAttribute('data-did') ?? (typeof type === 'string' ? type : 'comment');
  // the merge dialog closes and its progress shows on the bar's merge button instead
  const shown = isMerge ? bar.querySelector<HTMLButtonElement>('.forge-queue-merge') : submitter ?? form.querySelector<HTMLButtonElement>('button[type="submit"]');
  if (isMerge) {
    shown?.setAttribute('data-busy', submitter?.getAttribute('data-busy') ?? '');
    hideModal(form.closest('dialog')!);
  }
  setLeaving(bar, shown, true);
  const fail = (message: string) => {
    setLeaving(bar, shown, false);
    showErrorToast(message);
  };

  const approveURL = form.getAttribute('data-approve-url');
  if (approveURL) {
    const approval = new FormData();
    approval.set('type', 'approve');
    approval.set('content', '');
    approval.set('commit_id', form.getAttribute('data-approve-commit')!);
    const approved = await send(approveURL, approval, `${next}&did=approve`);
    if (!approved.ok) return fail(approved.json?.errorMessage ?? form.getAttribute('data-approve-failed')!);
  }
  const target = `${next}&did=${did}`;
  // read the attribute: a field named "action" (the review request form has one) shadows form.action
  const result = await send(form.getAttribute('action')!, data, target, form.getAttribute('data-expect') === 'ok');
  if (result.ok) window.location.assign(target);
  else if (result.json?.redirect) window.location.assign(result.json.redirect); // the pull request flashes why
  else fail(result.json?.errorMessage ?? bar.getAttribute('data-failed')!);
}

function initChangesForm(bar: HTMLElement) {
  const toggle = bar.querySelector<HTMLButtonElement>('.forge-queue-changes-toggle');
  const form = bar.querySelector<HTMLFormElement>('.forge-queue-changes-form');
  if (!toggle || !form) return;
  const note = form.querySelector('textarea')!;
  const setOpen = (open: boolean) => {
    form.hidden = !open;
    toggle.setAttribute('aria-expanded', String(open));
    if (open) note.focus();
  };
  toggle.addEventListener('click', () => setOpen(Boolean(form.hidden)));
  form.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return; // Ctrl+Enter is the site-wide quick submit
    setOpen(false);
    toggle.focus();
  });
}

// while working through an inbox view, acting on an item moves on to the next one
export function initInboxQueue() {
  const bar = document.querySelector<HTMLElement>('.forge-queue-bar');
  if (!bar) return;
  const next = bar.getAttribute('data-queue-next')!;
  for (const el of bar.querySelectorAll('button[disabled]')) el.setAttribute('data-off', '');
  initChangesForm(bar);
  bar.querySelector('.forge-queue-skip')!.addEventListener('click', () => document.body.classList.add('forge-queue-leaving'));

  const params = new URLSearchParams(window.location.search);
  if (params.has('done')) {
    params.delete('done');
    params.delete('did');
    const query = params.toString();
    window.history.replaceState(window.history.state, '', `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`);
  }

  document.addEventListener('submit', (event) => {
    const form = event.target as HTMLFormElement;
    const submitter = event.submitter instanceof HTMLButtonElement ? event.submitter : null;
    if (form.matches('.forge-queue-form')) {
      event.preventDefault();
      event.stopImmediatePropagation();
      if (!form.reportValidity()) return;
      submitBarForm(bar, form, submitter, next);
      return;
    }
    // the pull request's own merge box and full review hand back to the queue too
    const own = /\/(merge|files\/reviews\/submit)$/.exec(form.getAttribute('action') ?? '');
    if (!own) return;
    let input = form.querySelector<HTMLInputElement>('input[name="redirect_to"]');
    if (!input) {
      input = document.createElement('input');
      input.type = 'hidden';
      input.name = 'redirect_to';
      form.append(input);
    }
    input.value = `${next}&did=${own[1] === 'merge' ? 'merged' : submitter?.value ?? 'comment'}`;
  }, {capture: true});

  document.addEventListener('keydown', (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return;
    if (event.target instanceof Element && event.target.closest('input, textarea, select, [contenteditable="true"], dialog[open]')) return;
    const target = bar.querySelector<HTMLElement>(`[data-queue-key="${CSS.escape(event.key)}"]:not([disabled])`);
    if (!target) return;
    event.preventDefault();
    target.click();
  });
}
