// while working through an inbox view, a merge or review hands back to the queue for the next item
export function initInboxQueue() {
  const bar = document.querySelector<HTMLElement>('.forge-queue-bar');
  if (!bar) return;
  const redirect = bar.getAttribute('data-queue-redirect')!;
  document.addEventListener('submit', (event) => {
    const form = event.target as HTMLFormElement;
    const action = form.getAttribute('action') ?? '';
    if (!/\/(merge|files\/reviews\/submit)$/.test(action)) return;
    let input = form.querySelector<HTMLInputElement>('input[name="redirect_to"]');
    if (!input) {
      input = document.createElement('input');
      input.type = 'hidden';
      input.name = 'redirect_to';
      form.append(input);
    }
    input.value = redirect;
  }, {capture: true});

  const openMerge = () => document.querySelector<HTMLButtonElement>('.forge-merge-actions .show-modal');
  const openReview = () => document.querySelector<HTMLButtonElement>('.review-box-button:not([disabled])');
  for (const [selector, find] of [['.forge-queue-merge', openMerge], ['.forge-queue-review', openReview]] as const) {
    bar.querySelector(selector)?.addEventListener('click', (event) => {
      const button = find();
      if (!button) return; // not on this page, follow the link
      event.preventDefault();
      button.click();
    });
  }
  // after every other init, so the modal and review box handlers are bound
  setTimeout(() => {
    if (window.location.hash === '#merge') openMerge()?.click();
    if (window.location.hash === '#review') openReview()?.click();
  }, 0);

  document.addEventListener('keydown', (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return;
    if (event.target instanceof Element && event.target.closest('input, textarea, select, [contenteditable="true"], dialog[open]')) return;
    const selector = {m: '.forge-queue-merge', r: '.forge-queue-review', n: '.forge-queue-skip'}[event.key];
    const link = selector ? bar.querySelector<HTMLElement>(selector) : null;
    if (!link) return;
    event.preventDefault();
    link.click();
  });
}
