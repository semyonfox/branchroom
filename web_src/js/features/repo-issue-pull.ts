import {createApp} from 'vue';
import {GET} from '../modules/fetch.ts';
import {fomanticQuery} from '../modules/fomantic/base.ts';
import {createElementFromHTML, activePageTimerRefresh} from '../utils/dom.ts';
import {registerGlobalEventFunc} from '../modules/observer.ts';
import {parseDom} from '../utils.ts';
import type {JQueryElem} from '../types.ts';

export function initRepoPullRequestUpdate(el: HTMLElement) {
  const elDropdown = el.querySelector(':scope > .ui.dropdown');
  if (!elDropdown) return;
  const elButton = el.querySelector<HTMLButtonElement>(':scope > button')!;

  fomanticQuery(elDropdown).dropdown({
    onChange(_text: string, _value: string, $choice: JQueryElem) {
      const choiceEl = $choice[0];
      elButton.textContent = choiceEl.textContent;
      elButton.setAttribute('data-url', choiceEl.getAttribute('data-update-url')!);
    },
  });
}

function onCommitStatusChecksToggle(btn: HTMLElement) {
  const panel = btn.closest('.commit-status-toggle')!.parentElement!;
  const list = panel.querySelector<HTMLElement>('.commit-status-list')!;
  list.style.maxHeight = list.style.maxHeight ? '' : '0px'; // toggle
  btn.textContent = btn.getAttribute(list.style.maxHeight ? 'data-show-all' : 'data-hide-all');
}

async function initRepoPullRequestMergeForm(box: HTMLElement) {
  const el = box.querySelector('#pull-request-merge-form');
  if (!el) return;

  const data = JSON.parse(el.getAttribute('data-merge-form-props')!);
  const {default: PullRequestMergeForm} = await import('../components/PullRequestMergeForm.vue');
  const view = createApp(PullRequestMergeForm, {mergeFormProps: data});
  view.mount(el); // TODO: can unmount when reloaded?
}

// the status card and the next step are rendered with the page, so once the merge box has settled they are a step behind
async function refreshPullSummary(pullLink: string) {
  const resp = await GET(pullLink);
  if (!resp.ok) return;
  const doc = parseDom(await resp.text(), 'text/html');
  for (const selector of ['.forge-status-card', '.forge-next-step']) {
    const stale = document.querySelector(selector);
    const fresh = doc.querySelector(selector);
    if (stale && fresh && !stale.contains(document.activeElement)) stale.replaceWith(fresh);
  }
}

function initRepoPullMergeBoxRefresh(el: Element) {
  // The merge box has complex buttons & form, if the user has interacted with any element, don't refresh.
  // Otherwise, the user won't be able to merge or schedule a merge (auto-merge) when the PR status is not ready.
  let interacted = false;
  const interactionEvents = ['focusin', 'mousedown', 'click', 'keydown', 'input'];
  for (const event of interactionEvents) {
    el.addEventListener(event, () => { interacted = true }, {capture: true});
  }

  activePageTimerRefresh({
    once: true, // on successful refresh, the data-global-init will re-initialize the element
    interval: () => interacted ? 0 : Number(el.getAttribute('data-pull-merge-box-reloading-interval')),
    async callback() {
      if (interacted) return;
      const pullLink = el.getAttribute('data-pull-link')!;
      const resp = await GET(`${pullLink}/merge_box`);
      if (!resp.ok) return;
      const respText = (await resp.text()).trim();
      if (!respText) {
        el.remove(); // merge box might not exist if the PR has changed (e.g.: merged and the head branch has been deleted)
        return;
      }
      const newEl = createElementFromHTML(respText);
      const scrollTop = el.querySelector<HTMLElement>('.commit-status-list')?.scrollTop;
      el.replaceWith(newEl); // don't morph, do full replacement to make sure data-global-init and Vue components are re-initialized
      if (scrollTop) newEl.querySelector<HTMLElement>('.commit-status-list')?.scrollTo({top: scrollTop, behavior: 'instant'});
      if (!newEl.hasAttribute('data-pull-merge-box-reloading-interval')) await refreshPullSummary(pullLink);
    },
  });
}

export function initRepoPullMergeBox(el: HTMLElement) {
  registerGlobalEventFunc('click', 'onCommitStatusChecksToggle', onCommitStatusChecksToggle);
  initRepoPullRequestMergeForm(el);
  initRepoPullMergeBoxRefresh(el);
}
