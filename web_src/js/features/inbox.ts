import {toggleElem} from '../utils/dom.ts';

export function initInbox() {
  const form = document.querySelector<HTMLFormElement>('.forge-inbox-form');
  if (!form) return;
  const rows = Array.from(form.querySelectorAll<HTMLElement>('.forge-inbox-row'));
  if (!rows.length) return;
  const toolbar = form.querySelector<HTMLElement>('.forge-inbox-bulk')!;
  const reason = form.querySelector<HTMLSelectElement>('[name="reason"]')!;
  function updateSelection() {
    const selected = Boolean(form!.querySelector('input[name="items"]:checked'));
    toggleElem(toolbar, selected);
    for (const button of toolbar.querySelectorAll<HTMLButtonElement>('button[name="action"]')) {
      button.disabled = !selected || (button.value === 'close' && !reason.value);
    }
  }
  form.addEventListener('change', updateSelection);
  updateSelection();
  let current = 0;
  document.addEventListener('keydown', (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return;
    if (event.target instanceof Element && event.target.closest('input, textarea, select, button, [contenteditable="true"]')) return;
    const key = event.key.toLowerCase();
    if (!['j', 'k', 'x', 'e', 's', 'enter'].includes(key)) return;
    event.preventDefault();
    if (key === 'j' || key === 'k') {
      current = Math.max(0, Math.min(rows.length - 1, current + (key === 'j' ? 1 : -1)));
      rows[current].focus();
    } else if (key === 'x') {
      const checkbox = rows[current].querySelector<HTMLInputElement>('input')!;
      checkbox.checked = !checkbox.checked;
      updateSelection();
    } else if (key === 'enter') {
      rows[current].querySelector<HTMLAnchorElement>('.forge-inbox-primary')!.click();
    } else {
      if (!form.querySelector('input[name="items"]:checked')) return;
      const selector = key === 'e' ? 'button[value="done"]' : 'button[value="snooze-day"]';
      form.requestSubmit(form.querySelector<HTMLButtonElement>(selector));
    }
  });
}
