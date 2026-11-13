export function initBranchroom() {
  const root = document.querySelector<HTMLElement>('.page-content.branchroom, .branchroom-source-handoffs');
  if (!root) return;

  root.querySelector<HTMLAnchorElement>('.branchroom-create-link')?.addEventListener('click', () => {
    root.querySelector<HTMLInputElement>('#branchroom-scope-name')!.focus();
  });
  for (const flash of root.querySelectorAll<HTMLElement>('.flash-message')) {
    flash.setAttribute('role', flash.classList.contains('flash-error') ? 'alert' : 'status');
    flash.setAttribute('tabindex', '-1');
    flash.focus();
  }

  for (const form of root.querySelectorAll<HTMLFormElement>('form')) {
    const start = form.querySelector<HTMLInputElement>('input[name="not_before"]');
    const end = form.querySelector<HTMLInputElement>('input[name="expires"]');
    const error = form.querySelector<HTMLElement>('.branchroom-time-error');
    if (!start || !end || !error) continue;
    const validate = () => {
      const invalid = Boolean(start.value && end.value && end.value <= start.value);
      const message = invalid ? error.getAttribute('data-error-message')! : '';
      end.setCustomValidity(message);
      error.textContent = message;
      end.toggleAttribute('aria-invalid', invalid);
      if (invalid) end.setAttribute('aria-invalid', 'true');
      return !invalid;
    };
    start.addEventListener('input', validate);
    end.addEventListener('input', validate);
    form.addEventListener('submit', (event) => {
      if (validate()) return;
      event.preventDefault();
      end.focus();
    });
  }

  for (const button of root.querySelectorAll<HTMLButtonElement>('.branchroom-copy')) {
    const field = root.querySelector(`#${CSS.escape(button.getAttribute('data-copy-target')!)}`);
    const status = button.closest('.item')!.querySelector<HTMLElement>('.branchroom-copy-status')!;
    if (!(field instanceof HTMLInputElement)) continue;
    let copying = false;
    button.addEventListener('click', async () => {
      if (copying) return;
      copying = true;
      button.setAttribute('aria-disabled', 'true');
      button.setAttribute('aria-busy', 'true');
      status.textContent = '';
      try {
        await navigator.clipboard.writeText(field.value);
        status.textContent = status.getAttribute('data-success');
      } catch {
        field.focus();
        field.select();
        status.textContent = status.getAttribute('data-failure');
      } finally {
        copying = false;
        button.removeAttribute('aria-disabled');
        button.removeAttribute('aria-busy');
      }
    });
  }
}
