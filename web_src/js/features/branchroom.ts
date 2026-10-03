import {createBranchroomTelemetry, branchroomTelemetryDisabled, setBranchroomTelemetryDisabled} from './branchroom-telemetry.ts';

export function initBranchroom() {
  const root = document.querySelector<HTMLElement>('.page-content.branchroom, .branchroom-source-handoffs');
  if (!root) return;
  const enabled = document.querySelector('meta[name="branchroom-telemetry-enabled"]')?.getAttribute('content') === 'true';
  const endpoint = document.querySelector('meta[name="branchroom-telemetry-endpoint"]')?.getAttribute('content') || '';
  const telemetry = createBranchroomTelemetry(root.classList.contains('branchroom') ? 'settings' : 'workspace', enabled, endpoint);
  const privacySwitch = root.querySelector<HTMLInputElement>('#branchroom-telemetry-disable')!;
  const privacyStatus = root.querySelector<HTMLElement>('#branchroom-telemetry-status')!;
  privacySwitch.checked = branchroomTelemetryDisabled();
  const updatePrivacyStatus = () => { privacyStatus.textContent = privacyStatus.getAttribute(telemetry.active() ? 'data-on' : 'data-off') };
  updatePrivacyStatus();
  privacySwitch.addEventListener('change', () => {
    telemetry.setDisabled(privacySwitch.checked);
    setBranchroomTelemetryDisabled(privacySwitch.checked);
    updatePrivacyStatus();
  });
  telemetry.record('screen_view');

  root.querySelector<HTMLAnchorElement>('.branchroom-create-link')?.addEventListener('click', () => {
    root.querySelector<HTMLInputElement>('#branchroom-scope-name')!.focus();
  });
  for (const flash of root.querySelectorAll<HTMLElement>('.flash-message')) {
    flash.setAttribute('role', flash.classList.contains('flash-error') ? 'alert' : 'status');
    flash.setAttribute('tabindex', '-1');
    flash.focus();
  }

  for (const trigger of root.querySelectorAll<HTMLButtonElement>('.branchroom-show-dialog')) {
    const dialog = root.querySelector(`#${CSS.escape(trigger.getAttribute('data-dialog')!)}`);
    if (!(dialog instanceof HTMLDialogElement)) continue;
    trigger.addEventListener('click', () => dialog.showModal());
    dialog.querySelector<HTMLButtonElement>('.branchroom-dialog-cancel')!.addEventListener('click', () => dialog.close());
    dialog.addEventListener('close', () => trigger.focus());
    dialog.addEventListener('keydown', (event) => {
      if (event.key !== 'Tab') return;
      const buttons = [...dialog.querySelectorAll<HTMLButtonElement>('button')].filter((button) => !button.disabled);
      const first = buttons[0];
      const last = buttons.at(-1);
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last!.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    });
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
    end.addEventListener('invalid', () => {
      if (end.validity.customError) telemetry.record('validation_failed');
    });
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
        telemetry.record('action_completed');
      } catch {
        field.focus();
        field.select();
        status.textContent = status.getAttribute('data-failure');
        telemetry.record('storage_failed');
      } finally {
        copying = false;
        button.removeAttribute('aria-disabled');
        button.removeAttribute('aria-busy');
      }
    });
  }
}
