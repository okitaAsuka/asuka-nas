"""Real Chromium + production authenticated Management/SQLite; one auth fixture.

Reuses the frozen UI owner's form-based validation, with an untrusted-CA negative
control and explicit disposable trust opt-in. Never ignores TLS errors.
"""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import shutil
import subprocess
import urllib.parse
import urllib.request
import uuid

from playwright.sync_api import Error, expect, sync_playwright

if os.environ.get('CI') != 'true' or os.environ.get('NAS_AUTH_DISPOSABLE_TRUST') != '1':
    raise RuntimeError('auth fixture requires explicit disposable CI trust; no host trust changes made')
base, control = os.environ['NAS_AUTH_URL'], os.environ['NAS_AUTH_CONTROL']
ca = Path(os.environ['NAS_AUTH_CA'])
assert base.startswith('https://127.0.0.1:') and control.startswith('http://127.0.0.1:')


@contextmanager
def temporary_trust():
    # NSS tools are a CI prerequisite, never downloaded by the application/test.
    certutil = shutil.which('certutil')
    assert certutil and ca.is_file()
    legacy = Path.home() / '.pki/nssdb'
    modern = Path.home() / '.local/share/pki/nssdb'
    database = modern if modern.exists() and not legacy.exists() else legacy
    database.mkdir(parents=True, exist_ok=True)

    def cert(*args):
        subprocess.run([certutil, '-d', 'sql:' + str(database), *args], check=True, timeout=15)

    if not (database / 'cert9.db').exists():
        cert('-N', '--empty-password')
    nickname = 'asuka-disposable-test-' + uuid.uuid4().hex
    cert('-A', '-t', 'C,,', '-n', nickname, '-i', str(ca))
    try:
        yield
    finally:
        # Remove only our entry; never replace/delete a database or change HOME.
        cert('-D', '-n', nickname)
        print('PASS removed this run\'s synthetic CA trust entry')


def launch(playwright):
    browser = playwright.chromium.launch(
        executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),
        args=['--no-sandbox'])
    context = browser.new_context(viewport={'width': 1440, 'height': 1000}, ignore_https_errors=False)
    context.route('**/*', lambda route: route.continue_() if
                  urllib.parse.urlsplit(route.request.url).netloc == urllib.parse.urlsplit(base).netloc
                  else route.abort())
    return browser, context


with sync_playwright() as playwright:
    browser, context = launch(playwright)
    try:
        try:
            context.new_page().goto(base, timeout=15000)
        except Error as error:
            assert 'ERR_CERT_AUTHORITY_INVALID' in str(error), str(error)
        else:
            raise AssertionError('untrusted synthetic CA accepted before installation')
    finally:
        browser.close()
    print('PASS untrusted CA rejected with normal Chromium certificate verification')

    with temporary_trust():
        browser, context = launch(playwright)
        try:
            page = context.new_page()
            errors, writes, external = [], [], []
            page.add_init_script("window.fixtureCSPViolations=[];document.addEventListener('securitypolicyviolation',e=>window.fixtureCSPViolations.push(e.violatedDirective))")
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.on('request', lambda request: writes.append(request.url) if request.method == 'POST' else None)
            page.on('request', lambda request: external.append(request.url) if not request.url.startswith(base) else None)

            def click(name):
                page.get_by_role('button', name=name, exact=True).click()

            def login(password=os.environ['NAS_AUTH_PASSWORD']):
                page.locator('[name=login_username]').fill('admin')
                page.locator('[name=login_password]').fill(password)
                click('登录工作台')

            def capture(name, full=True):
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
                page.screenshot(path='/tmp/asuka-workflow-auth-real-' + name + '.png', full_page=full, animations='disabled')

            def fixture_state():
                with urllib.request.urlopen(control + '/state', timeout=5) as response:
                    return json.load(response)

            response = page.goto(base)
            assert response.status == 200
            security = response.security_details()
            assert security and security['protocol'].startswith('TLS') and page.evaluate('window.isSecureContext')
            expect(page.get_by_role('heading', name='欢迎回到 asuka')).to_be_visible()
            assert page.evaluate("async () => (await fetch('/api/session')).json()") == {'mode': 'authenticated_https', 'authenticated': False}
            assert page.evaluate("async () => (await fetch('/api/jobs')).status") == 401
            capture('login-desktop')
            page.set_viewport_size({'width': 390, 'height': 844})
            capture('login-mobile')
            page.emulate_media(reduced_motion='reduce')
            login('incorrect-synthetic-password')
            expect(page.locator('.login-status')).to_contain_text('用户名或密码不正确')
            expect(page.locator('[name=login_password]')).to_have_value('')
            login()
            expect(page.locator('[data-workflow=naming] strong')).to_have_text('1')
            cookie = [value for value in context.cookies() if value['name'] == '__Host-asuka_session'][0]
            assert cookie['secure'] and cookie['httpOnly'] and cookie['sameSite'] == 'Strict' and cookie['path'] == '/'
            assert cookie['value'] not in page.evaluate('document.cookie')
            assert page.evaluate('localStorage.length + sessionStorage.length') == 0
            assert page.locator('[data-workflow=naming]').evaluate('(n)=>n.getBoundingClientRect().bottom <= innerHeight')
            capture('overview-mobile')
            capture('overview-mobile-first-screen', full=False)
            page.set_viewport_size({'width': 1440, 'height': 1000})
            click('任务与入库')
            click('查看详情')
            for name, value in [('batch_directory', 'Authenticated fixture'), ('batch_display_name', 'Fixture story'), ('batch_season', '3')]:
                page.locator(f'[name={name}]').fill(value)
            page.locator('[name=batch_mode]').select_option('manual')
            expect(page.locator('[name=batch_episode]')).to_have_count(2)
            page.locator('[name=batch_episode]').nth(0).fill('2')
            page.locator('[name=batch_episode]').nth(1).fill('3')
            click('预览完整整理表')
            expect(page.locator('[data-batch-preview=true]')).to_contain_text('3 个入库文件')
            confirm = page.get_by_role('button', name='确认整表方案')
            expect(confirm).to_be_enabled()
            capture('preview-desktop')
            page.set_viewport_size({'width': 390, 'height': 844})
            capture('preview-mobile')
            # Expire the actual server session, not a mock API or browser clock.
            urllib.request.urlopen(urllib.request.Request(control + '/expire', data=b'', method='POST'), timeout=5).close()
            with page.expect_response(lambda result: result.url.endswith('/api/batch-confirm')) as denied:
                confirm.click()
            assert denied.value.status == 401
            expect(page.get_by_role('heading', name='会话已结束')).to_be_visible()
            assert fixture_state() == {'approved': False, 'comparisons': 0, 'library_entries': 0}
            assert page.locator('#view > *').count() == 0
            capture('expired-mobile')
            count = len([url for url in writes if url.endswith('/api/batch-confirm')])
            assert count == 1
            login()
            expect(page.locator('[name=batch_display_name]')).to_have_value('Fixture story')
            expect(page.locator('[name=batch_episode]').nth(0)).to_have_value('2')
            expect(page.locator('[name=batch_episode]').nth(1)).to_have_value('3')
            expect(confirm).to_be_disabled()
            expect(page.locator('[data-batch-preview=true]')).to_be_empty()
            assert len([url for url in writes if url.endswith('/api/batch-confirm')]) == count
            assert fixture_state() == {'approved': False, 'comparisons': 0, 'library_entries': 0}
            click('预览完整整理表')
            expect(confirm).to_be_enabled()
            confirm.click()
            expect(page.locator('#message')).to_contain_text('人工意图已确认')
            assert fixture_state() == {'approved': True, 'comparisons': 1, 'library_entries': 0}
            assert len([url for url in writes if url.endswith('/api/batch-confirm')]) == 2
            click('健康与接入')
            expect(page.locator('#view')).to_contain_text('当前模式：已认证 HTTPS 管理')
            expect(page.locator('.setup-guide')).to_contain_text('当前使用独立 HTTPS 会话')
            assert '未知' in page.locator('#view').inner_text()
            click('退出登录')
            expect(page.get_by_role('heading', name='欢迎回到 asuka')).to_be_visible()
            assert page.evaluate("async () => (await fetch('/api/jobs')).status") == 401
            assert not [value for value in context.cookies() if value['name'] == '__Host-asuka_session']
            assert page.evaluate('localStorage.length + sessionStorage.length') == 0
            assert not page.evaluate('window.fixtureCSPViolations'), 'production CSP blocked a UI resource'
            assert not errors, errors
            assert not external, external
            print('PASS REAL AUTH BROWSER: production trusted TLS/Management/SQLite, anonymous 401/no nonce, login failure/success, Secure HttpOnly Strict cookie, expiry clears preview/approval and retains full-batch draft, no automatic replay, explicit fresh preview/confirm, logout revocation, CSP, desktop/390px/reduced motion; synthetic CA/account/files/fake upstreams only')
        finally:
            browser.close()
