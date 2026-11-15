import {GET} from '../modules/fetch.ts';
import {localUserSettings} from '../modules/user-settings.ts';
import {pathEscapeSegments} from '../utils/url.ts';

type Section = 'recent' | 'repositories' | 'files' | 'issues' | 'commands';
export type PaletteResult = {
  label: string;
  section: Section;
  url?: string;
  key?: string;
  action?: HTMLElement;
};

export function rankPaletteResults(results: PaletteResult[], query: string): PaletteResult[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const score = (result: PaletteResult) => {
    const label = result.label.toLowerCase();
    if (words.some((word) => !label.includes(word))) return -1;
    const text = words.join(' ');
    return label === text ? 3 : label.startsWith(text) ? 2 : 1;
  };
  return results.map((result, index) => ({result, index, score: score(result)}))
    .filter(({score}) => score >= 0)
    .sort((a, b) => b.score - a.score || a.index - b.index)
    .map(({result}) => result);
}

const sections: Section[] = ['recent', 'repositories', 'files', 'issues', 'commands'];
let open: ((query: string) => void) | undefined;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

async function readJSON(url: string, signal: AbortSignal): Promise<unknown> {
  const response = await GET(url, {signal});
  if (!response.ok) throw new Error(`Search failed: ${response.status}`);
  return response.json();
}

function createPalette() {
  const dialog = document.querySelector<HTMLDialogElement>('#forge-palette')!;
  // native modal dialogs make the rest of the page inert and restore focus on close
  document.body.append(dialog);
  const text = (key: string) => dialog.getAttribute(`data-${key}`)!;
  const repoLink = dialog.getAttribute('data-repo-link') || '';
  const signedIn = dialog.getAttribute('data-signed-in') === 'true';
  const repoSearchURL = `${window.config.appSubUrl}/${signedIn ? 'repo/search' : 'api/v1/repos/search'}`;
  const issueSearchURL = signedIn ? `${repoLink}/issues/search` : `${window.config.appSubUrl}/api/v1/repos${repoLink.slice(window.config.appSubUrl.length)}/issues`;
  const finder = document.querySelector('[data-tree-list-url]');
  const ref = finder?.getAttribute('data-current-ref-name-sub-url') || `branch/${dialog.getAttribute('data-default-branch') || ''}`;
  const treeURL = finder?.getAttribute('data-tree-list-url') || (repoLink && dialog.getAttribute('data-default-branch') ? `${repoLink}/tree-list/${pathEscapeSegments(ref)}` : '');
  const input = document.createElement('input');
  input.id = 'forge-palette-input';
  input.type = 'search';
  input.autocomplete = 'off';
  input.placeholder = dialog.getAttribute('aria-label')!;
  input.setAttribute('aria-label', input.placeholder);
  input.setAttribute('role', 'combobox');
  input.setAttribute('aria-autocomplete', 'list');
  input.setAttribute('aria-controls', 'forge-palette-results');
  input.setAttribute('aria-expanded', 'false');
  const header = document.createElement('div');
  header.className = 'forge-palette-header';
  const scope = document.createElement('span');
  scope.textContent = repoLink ? repoLink.slice(window.config.appSubUrl.length + 1) : '';
  header.append(input, scope);
  const list = document.createElement('div');
  list.id = 'forge-palette-results';
  list.className = 'forge-palette-results';
  list.setAttribute('role', 'listbox');
  list.setAttribute('aria-label', input.placeholder);
  const status = document.createElement('div');
  status.className = 'forge-palette-status';
  status.setAttribute('role', 'status');
  const hint = document.createElement('div');
  hint.className = 'forge-palette-hint';
  hint.textContent = text('hint');
  dialog.append(header, list, status, hint);

  let results: PaletteResult[] = [];
  let visible: PaletteResult[] = [];
  let selected = 0;
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let files: string[] | undefined;
  let loading = false;
  let failed = false;

  function commands(): PaletteResult[] {
    const items: PaletteResult[] = [
      {section: 'commands', label: text('inbox'), url: `${window.config.appSubUrl}/notifications`},
      {section: 'commands', label: text('new-repository'), url: `${window.config.appSubUrl}/repo/create`},
    ];
    if (repoLink) items.push({section: 'commands', label: text('new-issue'), url: `${repoLink}/issues/new`});
    for (const action of document.querySelectorAll<HTMLElement>('[data-palette-action]')) {
      if (action.matches(':disabled, [aria-disabled="true"]')) continue;
      items.push({section: 'commands', label: action.getAttribute('data-palette-action')!, key: action.getAttribute('data-palette-key') || undefined, action});
    }
    return items;
  }

  function recents(): PaletteResult[] {
    try {
      const stored: unknown = JSON.parse(localUserSettings.getString('forge-palette-recent', '[]'));
      if (!Array.isArray(stored)) return [];
      return stored.filter(isRecord).filter((item) => typeof item.label === 'string' && typeof item.url === 'string' && item.url.startsWith('/') && !item.url.startsWith('//') && item.url !== location.pathname + location.search)
        .map((item) => ({section: 'recent', label: String(item.label), url: String(item.url)}));
    } catch {
      return [];
    }
  }

  function select(index: number) {
    selected = index;
    for (const [i, option] of list.querySelectorAll<HTMLElement>('[role="option"]').entries()) {
      option.setAttribute('aria-selected', String(i === selected));
    }
    const option = list.querySelector<HTMLElement>(`#forge-palette-option-${selected}`);
    if (option) {
      input.setAttribute('aria-activedescendant', option.id);
      option.scrollIntoView({block: 'nearest'});
    } else {
      input.removeAttribute('aria-activedescendant');
    }
  }

  function choose(index: number) {
    const result = visible[index];
    if (!result) return;
    dialog.close();
    if (result.action) result.action.click();
    else if (result.url) window.location.assign(result.url);
  }

  function render() {
    list.replaceChildren();
    visible = [];
    const items = [...recents(), ...commands(), ...results];
    for (const section of sections) {
      const matches = rankPaletteResults(items.filter((item) => item.section === section), input.value);
      if (!matches.length) continue;
      const group = document.createElement('div');
      group.setAttribute('role', 'group');
      group.setAttribute('aria-labelledby', `forge-palette-section-${section}`);
      const heading = document.createElement('div');
      heading.id = `forge-palette-section-${section}`;
      heading.className = 'forge-palette-section';
      heading.setAttribute('role', 'presentation');
      heading.textContent = text(section);
      group.append(heading);
      for (const result of matches) {
        const index = visible.length;
        visible.push(result);
        const row = document.createElement('div');
        row.id = `forge-palette-option-${index}`;
        row.className = `forge-palette-option forge-palette-${section}`;
        row.setAttribute('role', 'option');
        const label = document.createElement('span');
        label.textContent = result.label;
        row.append(label);
        if (result.key) {
          const key = document.createElement('kbd');
          key.textContent = result.key;
          row.append(key);
        }
        row.addEventListener('mousedown', (event) => event.preventDefault());
        row.addEventListener('click', () => choose(index));
        row.addEventListener('mouseenter', () => select(index));
        group.append(row);
      }
      list.append(group);
    }
    status.textContent = failed ? text('error') : loading ? text('loading') : visible.length ? '' : text('empty');
    select(0);
  }

  async function search(query: string) {
    controller = new AbortController();
    const {signal} = controller;
    const encoded = encodeURIComponent(query.replace(/^#(?=\d+$)/, ''));
    const tasks = [
      (async () => {
        const data = await readJSON(`${repoSearchURL}?q=${encoded}&limit=6`, signal);
        if (!isRecord(data) || !Array.isArray(data.data)) throw new Error('Invalid repositories');
        return data.data.filter(isRecord).flatMap((item): PaletteResult[] => {
          if (typeof item.full_name !== 'string') return [];
          return [{section: 'repositories', label: item.full_name, url: `${window.config.appSubUrl}/${item.full_name}`}];
        });
      })(),
    ];
    if (treeURL) tasks.push((async () => {
      if (!files) {
        const data = await readJSON(treeURL, signal);
        if (!Array.isArray(data) || data.some((item) => typeof item !== 'string')) throw new Error('Invalid files');
        files = data;
      }
      return rankPaletteResults(files.map((file) => ({section: 'files', label: file, url: `${repoLink}/src/${pathEscapeSegments(ref)}/${pathEscapeSegments(file)}`})), query).slice(0, 6);
    })());
    if (repoLink) tasks.push((async () => {
      const data = await readJSON(`${issueSearchURL}?q=${encoded}&state=all&type=all&limit=6`, signal);
      if (!Array.isArray(data)) throw new Error('Invalid issues');
      return data.filter(isRecord).flatMap((item): PaletteResult[] => {
        if (typeof item.number !== 'number' || typeof item.title !== 'string') return [];
        return [{section: 'issues', label: `#${item.number} ${item.title}`, url: `${repoLink}/${item.pull_request ? 'pulls' : 'issues'}/${item.number}`}];
      });
    })());
    const responses = await Promise.allSettled(tasks);
    if (signal.aborted) return;
    results = responses.flatMap((response) => response.status === 'fulfilled' ? response.value : []);
    failed = responses.some((response) => response.status === 'rejected');
    loading = false;
    render();
  }

  function updateSearch() {
    clearTimeout(timer);
    controller?.abort();
    results = [];
    failed = false;
    const query = input.value.trim();
    loading = Boolean(query);
    render();
    if (query) timer = setTimeout(() => search(query), 180);
  }
  input.addEventListener('input', updateSearch);
  dialog.addEventListener('keydown', (event) => {
    if (event.isComposing) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      dialog.close();
    } else if (['ArrowDown', 'ArrowUp', 'Tab'].includes(event.key)) {
      event.preventDefault();
      if (!visible.length) return;
      const direction = event.key === 'ArrowUp' || (event.key === 'Tab' && event.shiftKey) ? -1 : 1;
      let index = (selected + direction + visible.length) % visible.length;
      if (event.key === 'Tab') {
        while (index !== selected && visible[index].section === visible[selected].section) index = (index + direction + visible.length) % visible.length;
      }
      select(index);
      input.focus();
    } else if (event.key === 'Enter') {
      event.preventDefault();
      choose(selected);
    }
    event.stopPropagation();
  });
  dialog.addEventListener('click', (event) => {
    const rect = dialog.getBoundingClientRect();
    if (event.target === dialog && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom)) dialog.close();
  });
  dialog.addEventListener('close', () => {
    clearTimeout(timer);
    controller?.abort();
    input.setAttribute('aria-expanded', 'false');
  });
  return (query: string) => {
    if (dialog.open) return;
    input.value = query;
    results = [];
    loading = false;
    failed = false;
    dialog.showModal();
    input.setAttribute('aria-expanded', 'true');
    updateSearch();
    input.focus();
  };
}

export function openCommandPalette(query = '') {
  open ||= createPalette();
  open(query);
}
