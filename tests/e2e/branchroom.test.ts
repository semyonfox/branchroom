import {env} from 'node:process';
import {execFile} from 'node:child_process';
import {mkdtemp, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {promisify} from 'node:util';
import {test, expect} from '@playwright/test';
import type {Browser, Locator, Page} from '@playwright/test';
import {apiCreateRepo, apiCreateUser, apiHeaders, baseUrl, login, loginUser, randomString, testUserPassword} from './utils.ts';

const run = promisify(execFile);
const timeoutFactor = Number(env.GITEA_TEST_E2E_TIMEOUT_FACTOR) || 1;

async function git(cwd: string, ...args: Array<string>) {
  return run('git', args, {cwd, env: {...process.env, GIT_TERMINAL_PROMPT: '0', GIT_CONFIG_NOSYSTEM: '1'}});
}

function withCredentials(cloneURL: string, username: string) {
  const url = new URL(cloneURL);
  url.username = username;
  url.password = testUserPassword;
  return url.href;
}

async function handoffURL(browser: Browser, username: string, owner: string, repo: string) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await loginUser(page, username);
  const response = await page.goto(`/${owner}/${repo}/-/branchroom`);
  const status = response!.status();
  const value = status === 200 ? await page.locator('[id^="branchroom-source-clone-"]').first().inputValue() : '';
  await context.close();
  return {status, value};
}

// every settings action is a POST that redirects back with a flash, so wait for that round trip
async function submit(page: Page, button: Locator) {
  await Promise.all([page.waitForResponse((response) => response.request().method() === 'POST'), button.click()]);
  await page.waitForLoadState();
  await expect(page.locator('.flash-message.flash-success')).toBeVisible();
}

async function confirm(page: Page, name: string) {
  await page.getByRole('button', {name}).click();
  await submit(page, page.locator('dialog[open]').getByRole('button', {name}));
}

async function grant(page: Page, prefix: string, button: string, username: string) {
  await page.locator(`[id^="${prefix}"]`).fill(username);
  await submit(page, page.getByRole('button', {name: button}));
}

test('restricted workspace end to end: provision, clone, push, rotate, revoke, purge, delete', async ({page, browser, request}) => {
  test.setTimeout(90000 * timeoutFactor);
  const owner = env.GITEA_TEST_E2E_USER;
  const member = `br-member-${randomString(8)}`;
  const repo = `br-repo-${randomString(8)}`;
  const work = await mkdtemp(join(tmpdir(), 'branchroom-e2e-'));
  try {
    await Promise.all([apiCreateUser(request, member), apiCreateRepo(request, {name: repo}), login(page)]);
    const collaborator = await request.put(`${baseUrl()}/api/v1/repos/${owner}/${repo}/collaborators/${member}`, {headers: apiHeaders(), data: {permission: 'write'}});
    expect(collaborator.ok()).toBeTruthy();

    const settings = `/${owner}/${repo}/settings/branchroom`;
    await page.goto(settings);
    await page.getByLabel('Workspace name').fill('embargo');
    await submit(page, page.getByRole('button', {name: 'Create restricted workspace'}));
    await submit(page, page.getByRole('button', {name: 'Set up workspace'}));
    await expect(page.getByRole('button', {name: 'Rotate clone URL'})).toBeVisible();

    expect((await handoffURL(browser, member, owner, repo)).status).toBe(404);
    await grant(page, 'branchroom-grant-user-', 'Allow seeing this workspace', member);
    expect((await handoffURL(browser, member, owner, repo)).status).toBe(404);
    await grant(page, 'branchroom-source-grant-user-', 'Allow cloning and fetching', member);
    await grant(page, 'branchroom-source-write-grant-user-', 'Allow pushing', member);

    const first = await handoffURL(browser, member, owner, repo);
    expect(first.status).toBe(200);
    expect(first.value).toContain('/_branchroom/');
    const clone = join(work, 'clone');
    await git(work, 'clone', withCredentials(first.value, member), clone);
    await git(clone, '-c', `user.name=${member}`, '-c', 'user.email=member@example.invalid', 'commit', '--allow-empty', '-m', 'restricted change');
    await git(clone, 'push', 'origin', 'HEAD:refs/heads/branchroom/fix');
    await expect(git(clone, 'push', 'origin', 'HEAD:refs/heads/main')).rejects.toThrow();

    const publicRefs = await git(work, 'ls-remote', `${baseUrl()}/${owner}/${repo}.git`);
    expect(publicRefs.stdout).not.toContain('branchroom/fix');
    const restrictedRefs = await git(work, 'ls-remote', withCredentials(first.value, member));
    expect(restrictedRefs.stdout).toContain('refs/heads/branchroom/fix');

    await page.goto(settings);
    await confirm(page, 'Rotate clone URL');
    await expect(git(work, 'ls-remote', withCredentials(first.value, member))).rejects.toThrow();
    const rotated = await handoffURL(browser, member, owner, repo);
    expect(rotated.value).not.toBe(first.value);
    await git(work, 'ls-remote', withCredentials(rotated.value, member));

    await submit(page, page.locator('form[action*="/source-grants/"]').getByRole('button', {name: 'Revoke'}));
    expect((await handoffURL(browser, member, owner, repo)).status).toBe(404);
    await expect(git(work, 'ls-remote', withCredentials(rotated.value, member))).rejects.toThrow();

    await confirm(page, 'Close workspace');
    await expect(page.getByText('Only people with permission to see and clone this workspace can access its source.')).toHaveCount(0);
    await confirm(page, 'Delete workspace permanently');

    const deleted = await request.delete(`${baseUrl()}/api/v1/repos/${owner}/${repo}`, {headers: apiHeaders()});
    expect(deleted.status()).toBe(204);
  } finally {
    await rm(work, {recursive: true, force: true});
  }
});

test('repository deletion is refused while a restricted store is unpurged', async ({page, request}) => {
  test.setTimeout(30000 * timeoutFactor);
  const owner = env.GITEA_TEST_E2E_USER;
  const repo = `br-retain-${randomString(8)}`;
  await Promise.all([apiCreateRepo(request, {name: repo}), login(page)]);
  await page.goto(`/${owner}/${repo}/settings/branchroom`);
  await page.getByLabel('Workspace name').fill('retained');
  await submit(page, page.getByRole('button', {name: 'Create restricted workspace'}));
  await submit(page, page.getByRole('button', {name: 'Set up workspace'}));
  await expect(page.getByRole('button', {name: 'Rotate clone URL'})).toBeVisible();

  const refused = await request.delete(`${baseUrl()}/api/v1/repos/${owner}/${repo}`, {headers: apiHeaders()});
  expect(refused.status()).toBe(409);
  await confirm(page, 'Delete workspace permanently');
  const deleted = await request.delete(`${baseUrl()}/api/v1/repos/${owner}/${repo}`, {headers: apiHeaders()});
  expect(deleted.status()).toBe(204);
});
