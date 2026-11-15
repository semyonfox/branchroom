import {initInbox} from './inbox.ts';

test('bulk actions require a selection and closing requires a reason', () => {
  document.body.innerHTML = `<form class="forge-inbox-form">
    <div class="forge-inbox-bulk tw-hidden">
      <button name="action" value="done" disabled data-palette-action="Mark done">Done</button>
      <button name="action" value="close" disabled data-palette-action="Close">Close</button>
      <select name="reason"><option value="">Choose</option><option value="duplicate">Duplicate</option></select>
    </div>
    <article class="forge-inbox-row"><input type="checkbox" name="items" value="1"></article>
  </form>`;
  initInbox();
  const toolbar = document.querySelector('.forge-inbox-bulk')!;
  const checkbox = document.querySelector<HTMLInputElement>('input')!;
  const reason = document.querySelector<HTMLSelectElement>('select')!;
  const close = document.querySelector<HTMLButtonElement>('[value="close"]')!;
  const availableCommands = () => document.querySelectorAll('[data-palette-action]:not(:disabled)').length;
  expect(toolbar.classList.contains('tw-hidden')).toBe(true);
  expect(availableCommands()).toBe(0);
  checkbox.checked = true;
  checkbox.dispatchEvent(new Event('change', {bubbles: true}));
  expect(toolbar.classList.contains('tw-hidden')).toBe(false);
  expect(availableCommands()).toBe(1);
  expect(close.disabled).toBe(true);
  reason.value = 'duplicate';
  reason.dispatchEvent(new Event('change', {bubbles: true}));
  expect(close.disabled).toBe(false);
  checkbox.checked = false;
  checkbox.dispatchEvent(new Event('change', {bubbles: true}));
  expect(toolbar.classList.contains('tw-hidden')).toBe(true);
  expect(availableCommands()).toBe(0);
  document.body.replaceChildren();
});
