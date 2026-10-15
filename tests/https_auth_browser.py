"""Actual TLS, browser cookies and NAS APIs; no mocked auth responses or TLS bypass.

Only run with NAS_AUTH_DISPOSABLE_TRUST=1 in a throwaway Linux test account.
The final frozen UI will add form-driven login/logout assertions to this harness.
"""
from contextlib import contextmanager
import os
from pathlib import Path
import shutil
import subprocess
import urllib.parse
import urllib.request
import uuid

from playwright.sync_api import Error, expect, sync_playwright

BASE = os.environ['NAS_WEB_URL']
CONTROL = os.environ['NAS_FIXTURE_URL']
PASSWORD = 'synthetic-only-password-0123456789'
COOKIE = '__Host-asuka_session'


def fixture(action):
    with urllib.request.urlopen(CONTROL + '/' + action, timeout=15) as response:
        assert response.status == 200


@contextmanager
def temporary_trust():
    if os.environ.get('NAS_AUTH_DISPOSABLE_TRUST') != '1':
        raise RuntimeError('refusing to modify trust outside an explicitly disposable test account')
    ca = Path(os.environ['NAS_TEST_CA'])
    assert ca.is_file()
    # Chromium uses an existing legacy shared DB before its newer default DB.
    # Do not change HOME, replace a DB, or delete any pre-existing trust entries.
    legacy = Path.home() / '.pki' / 'nssdb'
    modern = Path.home() / '.local' / 'share' / 'pki' / 'nssdb'
    database = modern if modern.exists() and not legacy.exists() else legacy
    database.mkdir(parents=True, exist_ok=True)
    db = 'sql:' + str(database)
    if not (database / 'cert9.db').exists():
        subprocess.run(['certutil', '-N', '--empty-password', '-d', db], check=True)
    alias = 'asuka-disposable-test-' + uuid.uuid4().hex
    subprocess.run(['certutil', '-A', '-d', db, '-n', alias, '-t', 'C,,', '-i', str(ca)], check=True)
    try:
        yield
    finally:
        subprocess.run(['certutil', '-D', '-d', db, '-n', alias], check=True)
        print('PASS removed this run\'s synthetic CA trust entry')


def launch(playwright):
    browser = playwright.chromium.launch(
        executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),
        args=['--no-sandbox'])
    context = browser.new_context(viewport={'width': 1440, 'height': 1100}, ignore_https_errors=False)
    # Never let this fixture browser contact a metadata/model/service host.
    context.route('**/*', lambda route: route.continue_() if
                  urllib.parse.urlsplit(route.request.url).netloc == urllib.parse.urlsplit(BASE).netloc
                  else route.abort())
    return browser, context


def api(page, path, data=None, csrf=None):
    return page.evaluate('''async ({path,data,csrf}) => {
        const options = {credentials: 'same-origin'};
        if (data !== null) {
            options.method = 'POST';
            options.headers = {'Content-Type': 'application/json'};
            if (csrf !== null) options.headers['X-CSRF-Nonce'] = csrf;
            options.body = JSON.stringify(data);
        }
        const response = await fetch('/api/' + path, options);
        return {status: response.status, body: await response.json()};
    }''', {'path': path, 'data': data, 'csrf': csrf})


def login(page):
    result = api(page, 'login', {'username': 'admin', 'password': PASSWORD})
    assert result['status'] == 200
    assert result['body']['authenticated'] and result['body']['mode'] == 'authenticated_https'
    return result['body']['csrf']


def preview_batch(page):
    page.goto(BASE + '#jobs')
    page.get_by_role('button', name='查看详情', exact=True).click()
    form = page.locator('[data-batch=true]')
    expect(form.get_by_role('heading', name='批量整理并入库')).to_be_visible()
    expect(form.locator('[name=batch_episode]')).to_have_count(2)
    form.locator('[name=batch_directory]').fill('Human library')
    form.locator('[name=batch_display_name]').fill('Human Example')
    form.locator('[name=batch_season]').fill('4')
    form.locator('[name=batch_mode]').select_option('extracted')
    form.locator('[name=batch_offset]').fill('-66')
    form.get_by_role('button', name='提取 / 展示连续编号').click()
    form.get_by_role('button', name='预览完整整理表').click()
    expect(form.locator('[data-batch-preview=true]')).to_contain_text('3 个入库文件')
    confirm = form.get_by_role('button', name='确认整表方案')
    expect(confirm).to_be_enabled()
    return confirm


with sync_playwright() as playwright:
    # Negative control before trust installation: normal validation must reject this CA.
    browser, context = launch(playwright)
    try:
        try:
            context.new_page().goto(BASE, timeout=15000)
        except Error as error:
            assert 'ERR_CERT_AUTHORITY_INVALID' in str(error), str(error)
        else:
            raise AssertionError('untrusted synthetic CA was accepted before installation')
    finally:
        browser.close()
    print('PASS untrusted CA rejected with normal Chromium certificate verification')

    with temporary_trust():
        browser, context = launch(playwright)
        try:
            page = context.new_page()
            exceptions = []
            page.on('pageerror', lambda error: exceptions.append(str(error)))
            writes = []
            page.on('request', lambda request: writes.append(request.url)
                    if request.method == 'POST' and request.url.endswith('/api/batch-confirm') else None)
            response = page.goto(BASE)
            assert response.status == 200 and page.evaluate('window.isSecureContext')
            anonymous = api(page, 'session')
            assert anonymous == {'status': 200, 'body': {'mode': 'authenticated_https', 'authenticated': False}}
            assert api(page, 'jobs')['status'] == 401
            print('PASS anonymous bootstrap exposes no CSRF or private jobs')

            csrf = login(page)
            cookie = next(value for value in context.cookies() if value['name'] == COOKIE)
            assert cookie['secure'] and cookie['httpOnly'] and cookie['sameSite'] == 'Strict'
            assert cookie['path'] == '/' and COOKIE not in page.evaluate('document.cookie')
            assert api(page, 'jobs')['status'] == 200
            confirm = preview_batch(page)
            fixture('assert-unconfirmed')
            fixture('expire')
            with page.expect_response(lambda result: result.url.endswith('/api/batch-confirm')) as denied:
                confirm.click()
            assert denied.value.status == 401
            fixture('assert-unconfirmed')
            assert len(writes) == 1

            # Authentication does not replay the old consequential POST. Current UI
            # has no login form yet; this uses an actual same-origin browser request.
            csrf = login(page)
            page.reload(wait_until='networkidle')
            fixture('assert-unconfirmed')
            assert len(writes) == 1
            confirm = preview_batch(page)
            with page.expect_response(lambda result: result.url.endswith('/api/batch-confirm')) as accepted:
                confirm.click()
            assert accepted.value.status == 200 and len(writes) == 2
            fixture('assert-confirmed')
            print('PASS session loss rejects confirmation; relogin does not replay; fresh full preview requires explicit confirmation')

            assert api(page, 'logout', {}, csrf)['status'] == 200
            assert not any(value['name'] == COOKIE for value in context.cookies())
            assert api(page, 'jobs')['status'] == 401
            assert api(page, 'batch-confirm', {}, csrf)['status'] == 401
            fixture('execute')
            fixture('discover')
            csrf = login(page)
            page.goto(BASE + '#jobs')
            page.get_by_role('button', name='查看详情', exact=True).click()
            expect(page.get_by_text('全部发现 · 已就绪', exact=True)).to_be_visible()
            page.get_by_role('button', name='查看对比记录').click()
            expect(page.get_by_text('整包完成', exact=False)).to_be_visible()
            assert page.evaluate('localStorage.length === 0 && sessionStorage.length === 0')
            assert PASSWORD not in page.content() and not exceptions, exceptions
            page.screenshot(path='/tmp/asuka-auth-batch-complete.png', full_page=True)
            print('PASS logout revokes browser session; approved intent executes independently; all video discovery confirmed')
        finally:
            browser.close()
