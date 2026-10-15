"""Real Chromium + production authenticated Management/SQLite via temporary Go harness.
Run only on a disposable CI runner. Trust exactly its synthetic CA in Chromium's
NSS store, remove it afterwards; never ignore TLS errors or change NAS trust.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import urllib.request
from playwright.sync_api import sync_playwright, expect

if os.environ.get('CI') != 'true':
    raise RuntimeError('Authenticated integration harness requires a disposable CI runner; no host trust changes made')
base, control = os.environ['NAS_AUTH_URL'], os.environ['NAS_AUTH_CONTROL']
ca = Path(os.environ['NAS_AUTH_CA'])
assert base.startswith('https://127.0.0.1:') and control.startswith('http://127.0.0.1:')

with tempfile.TemporaryDirectory(prefix='asuka-ui-nss-tools-') as tools:
    certutil = shutil.which('certutil')
    if not certutil:
        # Extract the distribution's NSS CLI into a temporary directory, no system install.
        subprocess.run(['apt-get', 'download', 'libnss3-tools'], cwd=tools, check=True, capture_output=True, timeout=60)
        packages = list(Path(tools).glob('libnss3-tools_*.deb'))
        assert len(packages) == 1
        subprocess.run(['dpkg-deb', '--extract', str(packages[0]), tools], check=True, capture_output=True, timeout=30)
        certutil = str(Path(tools) / 'usr/bin/certutil')
    old = Path.home() / '.pki/nssdb'
    database = old if old.exists() else Path.home() / '.local/share/pki/nssdb'
    created = not database.exists()
    database.mkdir(parents=True, exist_ok=True)
    def cert(*args):
        subprocess.run([certutil, '-d', 'sql:' + str(database), *args], check=True, capture_output=True, timeout=15)
    if not (database / 'cert9.db').exists(): cert('-N', '--empty-password')
    nickname = 'asuka-ui-synthetic-' + str(os.getpid())
    cert('-A', '-t', 'C,,', '-n', nickname, '-i', str(ca))
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'), args=['--no-sandbox'])
            context = browser.new_context(viewport={'width':1440,'height':1000}, ignore_https_errors=False)
            page = context.new_page(); errors = []; writes = []; external = []
            page.on('pageerror', lambda e: errors.append(str(e)))
            page.on('request', lambda r: writes.append(r.url) if r.method == 'POST' else None)
            page.on('request', lambda r: external.append(r.url) if not r.url.startswith(base) else None)
            def click(name): page.get_by_role('button',name=name,exact=True).click()
            def login(password=os.environ['NAS_AUTH_PASSWORD']):
                page.locator('[name=login_username]').fill('admin')
                page.locator('[name=login_password]').fill(password)
                click('登录工作台')
            def capture(name):
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
                page.screenshot(path='/tmp/asuka-workflow-auth-real-' + name + '.png',full_page=True,animations='disabled')
            def fixture_state():
                with urllib.request.urlopen(control + '/state', timeout=5) as r: return json.load(r)
            response = page.goto(base)
            assert response.status == 200
            security = response.security_details()
            assert security and security['protocol'].startswith('TLS')
            expect(page.get_by_role('heading',name='欢迎回到 asuka')).to_be_visible()
            assert page.evaluate("async () => (await fetch('/api/jobs')).status") == 401
            capture('login-desktop')
            page.set_viewport_size({'width':390,'height':844});capture('login-mobile')
            page.emulate_media(reduced_motion='reduce')
            login('incorrect-synthetic-password')
            expect(page.locator('.login-status')).to_contain_text('用户名或密码不正确')
            expect(page.locator('[name=login_password]')).to_have_value('')
            login()
            expect(page.locator('[data-workflow=naming] strong')).to_have_text('1')
            cookie = [c for c in context.cookies() if c['name'] == '__Host-asuka_session'][0]
            assert cookie['secure'] and cookie['httpOnly'] and cookie['sameSite'] == 'Strict'
            assert cookie['value'] not in page.evaluate('document.cookie')
            assert page.evaluate('localStorage.length + sessionStorage.length') == 0
            capture('overview-mobile')
            page.set_viewport_size({'width':1440,'height':1000})
            click('任务与入库');click('查看详情')
            for name,value in [('batch_directory','Authenticated fixture'),('batch_display_name','Fixture story'),('batch_season','3'),('batch_episode','2')]:
                page.locator(f'[name={name}]').fill(value)
            click('预览完整整理表')
            confirm = page.get_by_role('button',name='确认整表方案')
            expect(confirm).to_be_enabled();capture('preview-desktop')
            page.set_viewport_size({'width':390,'height':844});capture('preview-mobile')
            # Expire the real server-side session; no client clock/cookie trick or mocked API.
            urllib.request.urlopen(urllib.request.Request(control + '/expire', data=b'', method='POST'), timeout=5).close()
            confirm.click()
            expect(page.get_by_role('heading',name='会话已结束')).to_be_visible()
            assert not fixture_state()['approved']
            assert page.locator('#view > *').count() == 0
            capture('expired-mobile')
            count = len([url for url in writes if url.endswith('/api/batch-confirm')])
            login()
            expect(page.locator('[name=batch_display_name]')).to_have_value('Fixture story')
            expect(confirm).to_be_disabled()
            expect(page.locator('[data-batch-preview=true]')).to_be_empty()
            assert len([url for url in writes if url.endswith('/api/batch-confirm')]) == count
            assert not fixture_state()['approved']
            click('预览完整整理表');expect(confirm).to_be_enabled();confirm.click()
            expect(page.locator('#message')).to_contain_text('人工意图已确认')
            assert fixture_state()['approved']
            click('健康与接入')
            expect(page.locator('#view')).to_contain_text('当前模式：已认证 HTTPS 管理')
            expect(page.locator('.setup-guide')).to_contain_text('当前使用独立 HTTPS 会话')
            assert '未知' in page.locator('#view').inner_text()
            click('退出登录')
            expect(page.get_by_role('heading',name='欢迎回到 asuka')).to_be_visible()
            assert page.evaluate("async () => (await fetch('/api/jobs')).status") == 401
            assert not [c for c in context.cookies() if c['name'] == '__Host-asuka_session']
            assert page.evaluate('localStorage.length + sessionStorage.length') == 0
            assert not errors, errors
            assert not external, external
            print('PASS REAL AUTH BROWSER: production trusted TLS/Management/SQLite, anonymous 401, login failure/success, Secure HttpOnly Strict cookie, expiry clears preview/approval and retains draft, no automatic confirm replay, explicit fresh preview/confirm, logout revocation, desktop/390px/reduced motion; synthetic CA/account/files/fake upstreams only')
            browser.close()
    finally:
        cert('-D', '-n', nickname)
        if created: shutil.rmtree(database)
