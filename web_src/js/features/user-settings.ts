import {showErrorToast} from '../modules/toast.ts';
import {GET} from '../modules/fetch.ts';
import {toggleElem} from '../utils/dom.ts';
import {registerGlobalInitFunc} from '../modules/observer.ts';

export function initUserSettings() {
  registerGlobalInitFunc('initAccessTokenForm', (form: HTMLFormElement) => {
    const expiry = form.querySelector<HTMLSelectElement>('[name=expiry]')!;
    const access = form.querySelector<HTMLSelectElement>('[name=repository-access]')!;
    expiry.addEventListener('change', () => toggleElem(form.querySelector('[data-token-custom-expiry]')!, expiry.value === 'custom'));
    access.addEventListener('change', () => toggleElem(form.querySelector('[data-token-repository-picker]')!, access.value === 'selected'));
    const search = form.querySelector<HTMLInputElement>('[data-token-repository-search]')!;
    const results = form.querySelector<HTMLElement>('[data-token-search-results]')!;
    const selected = form.querySelector<HTMLElement>('[data-token-selected-repositories]')!;
    const names = new Set<string>();
    const searchButton = form.querySelector<HTMLButtonElement>('[data-token-search-button]')!;
    searchButton.addEventListener('click', searchRepositories);
    async function searchRepositories() {
      searchButton.disabled = true;
      try {
        const response = await GET(`${window.config.appSubUrl}/repo/search?q=${encodeURIComponent(search.value)}&limit=20`);
        if (!response.ok) return;
        const data: unknown = await response.json();
        if (!data || typeof data !== 'object' || !('data' in data) || !Array.isArray(data.data)) return;
        results.replaceChildren();
        for (const item of data.data) {
          const repo: unknown = item && typeof item === 'object' && 'repository' in item ? item.repository : null;
          if (!repo || typeof repo !== 'object' || !('full_name' in repo) || typeof repo.full_name !== 'string') continue;
          const name = repo.full_name;
          const button = document.createElement('button');
          button.type = 'button';
          button.className = 'ui button';
          button.textContent = name;
          button.disabled = names.has(name);
          button.addEventListener('click', () => {
            if (names.has(name)) return;
            names.add(name);
            button.disabled = true;
            const label = document.createElement('label');
            label.className = 'gt-checkbox';
            const input = document.createElement('input');
            input.type = 'checkbox';
            input.name = 'repositories';
            input.value = name;
            input.checked = true;
            label.append(input, document.createTextNode(name));
            selected.append(label);
          });
          results.append(button);
        }
      } catch (error) {
        showErrorToast(String(error));
      } finally {
        searchButton.disabled = false;
      }
    }
  });
  registerGlobalInitFunc('initUserSettingsForm', (elForm: HTMLFormElement) => {
    const usernameInput = elForm.querySelector<HTMLInputElement>('input[name=name]')!;
    usernameInput.addEventListener('input', () => {
      const prompt = elForm.querySelector('#name-change-prompt')!;
      const promptRedirect = elForm.querySelector('#name-change-redirect-prompt')!;
      const showPrompt = usernameInput.value.toLowerCase() !== usernameInput.getAttribute('data-name')!.toLowerCase();
      toggleElem(prompt, showPrompt);
      toggleElem(promptRedirect, showPrompt);
    });
  });
}
