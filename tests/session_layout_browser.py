"""Browser/session UI contract fixture, NOT real authentication/TLS validation.
The frozen feature-lane contract is 6b18a6e:docs/MANAGEMENT_ACCESS_CONTRACT.md.
Only loopback HTTP, synthetic inputs, bounded DTOs; no upstream or persistent secret.
"""
import json
import os
from pathlib import Path
import shutil
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect


def run_session_contract_checks():
    assets = Path(__file__).resolve().parents[1] / 'internal/nas/web'
    state = {'authenticated': False, 'login_error': None, 'version': 1}
    calls = []
    sources = ['Fixture [02].mkv', 'Fixture [01].mkv']

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_): pass

        def reply(self, value, status=200, headers=None):
            raw = json.dumps(value).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Cache-Control', 'no-store')
            for key, value in (headers or {}).items(): self.send_header(key, value)
            self.end_headers()
            try: self.wfile.write(raw)
            except (BrokenPipeError, ConnectionResetError): pass

        def session(self):
            return {'mode': 'authenticated_https', 'authenticated': state['authenticated'], **({'csrf': 'fixture-session-nonce', 'expires_at': 1999999999} if state['authenticated'] else {})}

        def do_GET(self):
            path = urlparse(self.path).path
            if not path.startswith('/api/'):
                name = 'index.html' if path == '/' else path.lstrip('/')
                if name not in ['index.html', 'app.js', 'style.css']:
                    self.reply({}, 404); return
                self.send_response(200)
                self.send_header('Content-Type', 'text/javascript' if name.endswith('.js') else 'text/css' if name.endswith('.css') else 'text/html')
                self.end_headers(); self.wfile.write((assets / name).read_bytes()); return
            calls.append(('GET', self.path, None, dict(self.headers)))
            if path == '/api/session': self.reply(self.session()); return
            if not state['authenticated']: self.reply({'error': 'authentication_required'}, 401); return
            job = {'id': 'session-fixture', 'version': state['version'], 'download': 'completed', 'import': 'needs_confirmation', 'discovery': 'pending', 'refresh': 'pending', 'seed': 'seeding', 'seed_days': 7, 'sources': sources, 'block_reason': 'episode_unresolved'}
            detail = {'job': job, 'ready': False, 'contexts': [{'subscription': 'fixture', 'title': 'Fixture'}], 'batch_hints': []}
            data = {
                '/api/workflow': {'counts': {'all': 1, 'downloading': 0, 'naming': 1, 'rules': 0, 'emby': 0, 'failed': 0, 'ready': 0}, 'scope': {'total': 1, 'scanned': 1, 'truncated': False}},
                '/api/jobs': {'items': [detail], 'next': ''}, '/api/jobs/session-fixture': detail,
                '/api/naming': {'items': []}, '/api/comparisons': {'items': []},
                '/api/rules': [], '/api/epoch': {'epoch': 0}, '/api/audit': [], '/api/proposals': {'items': []},
                '/api/subscriptions': {'items': [{'id':'fixture-feed','name':'Fixture','profile':'fixture','enabled':True,'available':True,'version':state['version'],'status':{}}], 'profiles': ['fixture'], 'total': 1, 'next': ''},
            }
            self.reply(data.get(path, {}))

        def do_POST(self):
            data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            calls.append(('POST', self.path, data, dict(self.headers)))
            if self.path == '/api/login':
                error = state['login_error']
                if error:
                    self.reply({'error': error}, 429 if error == 'login_throttled' else 503 if error == 'authentication_unavailable' else 401, {'Retry-After': '1'} if error == 'login_throttled' else None); return
                state['authenticated'] = True; self.reply(self.session()); return
            if not state['authenticated']: self.reply({'error': 'authentication_required'}, 401); return
            if self.headers.get('X-CSRF-Nonce') != 'fixture-session-nonce': self.reply({'error': 'csrf_rejected'}, 403); return
            if self.path == '/api/logout': state['authenticated'] = False; self.reply({'ok': True}); return
            if self.path == '/api/batch-preview':
                org = data['organization']
                if org['mode'] == 'sequential' and not org['order_confirmed']:
                    self.reply({'error': 'order_confirmation_required'}, 409); return
                self.reply({'job_version': state['version'], 'digest': 'fixture-digest', 'plan': {'version': state['version'], 'batch': org, 'manifest': [{'source': s, 'fingerprint': 'f' * 64} for s in sources], 'files': [{'source': r['source'], 'target': f"{org['directory']}/Season 01/Fixture - S01E{r['episode']:02}.mkv", 'fingerprint': 'f' * 64} for r in org['rows']]}}); return
            self.reply({'ok': True})

    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'), args=['--no-sandbox'])
            page = browser.new_page(viewport={'width': 1440, 'height': 1000})
            errors = []; external = []
            base = f'http://127.0.0.1:{server.server_port}'
            page.on('pageerror', lambda e: errors.append(str(e)))
            page.on('request', lambda r: external.append(r.url) if not r.url.startswith(base) else None)
            def click(name): page.get_by_role('button', name=name, exact=True).click()
            def login():
                page.locator('[name=login_username]').fill('fixture-user')
                page.locator('[name=login_password]').fill('synthetic-fixture-only')
                click('登录工作台')
            def screenshot(name):
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
                page.screenshot(path='/tmp/asuka-workflow-session-mock-' + name + '.png', full_page=True, animations='disabled')
            page.goto(base)
            expect(page.get_by_role('heading', name='欢迎回到 asuka')).to_be_visible()
            assert [p for _, p, _, _ in calls] == ['/api/session']
            expect(page.locator('#workspace-shell')).to_be_hidden()
            screenshot('desktop')
            page.set_viewport_size({'width': 390, 'height': 844}); screenshot('mobile')
            page.emulate_media(reduced_motion='reduce')
            for error in ['authentication_failed', 'authentication_unavailable', 'login_throttled']:
                state['login_error'] = error; login()
                expect(page.locator('.login-status')).to_have_class('login-status error')
                expect(page.locator('[name=login_password]')).to_have_value('')
                assert 'synthetic-fixture-only' not in page.content()
                if error == 'login_throttled':
                    expect(page.get_by_role('button', name='登录工作台')).to_be_disabled()
                    count = len(calls); page.wait_for_timeout(1100); assert len(calls) == count
                    expect(page.get_by_role('button', name='登录工作台')).to_be_enabled()
            state['login_error'] = None; login()
            expect(page.locator('#workspace-shell')).to_be_visible()
            expect(page.get_by_role('button', name='退出登录')).to_be_visible()
            expect(page.locator('.local')).to_contain_text('已认证管理会话')
            assert 'fixture-session-nonce' not in page.content()
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
            page.set_viewport_size({'width':1440, 'height':1000})
            click('任务与入库'); click('查看详情')
            for name, value in [('batch_directory', 'My fixture library'), ('batch_display_name', 'My draft'), ('batch_season', '1')]:
                page.locator(f'[name={name}]').fill(value)
            page.locator('[name=naming_display_name]').fill('Human naming facts')
            page.locator('[name=batch_mode]').select_option('sequential')
            page.get_by_role('button', name='下移 Fixture [02].mkv', exact=True).click()
            page.locator('[name=batch_start]').fill('7'); click('提取 / 展示连续编号')
            page.locator('[name=batch_order_confirmed]').check(); click('预览完整整理表')
            expect(page.get_by_role('button', name='确认整表方案')).to_be_enabled()
            state['authenticated'] = False; state['version'] = 2
            click('确认整表方案')
            expect(page.get_by_role('heading', name='会话已结束')).to_be_visible()
            assert page.locator('#view > *').count() == 0
            screenshot('expired')
            sent = len([c for c in calls if c[1] == '/api/batch-confirm'])
            login()
            expect(page.locator('[name=batch_display_name]')).to_have_value('My draft')
            expect(page.locator('[name=naming_display_name]')).to_have_value('Human naming facts')
            expect(page.get_by_role('button', name='确认整表方案')).to_be_disabled()
            expect(page.locator('[data-batch-preview=true]')).to_be_empty()
            expect(page.locator('[name=batch_order_confirmed]')).not_to_be_checked()
            expect(page.locator('[name=batch_episode]').first).to_have_value('7')
            expect(page.locator('.source-name').first).to_contain_text('Fixture [01].mkv')
            assert len([c for c in calls if c[1] == '/api/batch-confirm']) == sent
            assert page.evaluate('localStorage.length + sessionStorage.length') == 0
            page.locator('[name=batch_order_confirmed]').check(); click('预览完整整理表')
            expect(page.get_by_role('button', name='确认整表方案')).to_be_enabled()
            assert [c for c in calls if c[1] == '/api/batch-preview'][-1][2]['version'] == 2
            # Expiry during a full-page refresh preserves the pre-load draft too.
            state['authenticated'] = False; click('刷新状态')
            expect(page.get_by_role('heading', name='会话已结束')).to_be_visible(); login()
            expect(page.locator('[name=batch_display_name]')).to_have_value('My draft')
            expect(page.get_by_role('button', name='确认整表方案')).to_be_disabled()
            # A changed manifest is preserved as read-only evidence, never mapped by row index.
            state['authenticated'] = False; click('刷新状态')
            expect(page.get_by_role('heading', name='会话已结束')).to_be_visible()
            sources[0] = 'Different fixture [03].mkv'; login()
            expect(page.get_by_role('heading', name='未自动恢复的草稿')).to_be_visible()
            expect(page.locator('#view pre')).to_contain_text('My draft')
            expect(page.locator('[name=batch_episode]').first).to_have_value('')
            expect(page.get_by_role('button', name='确认整表方案')).to_be_disabled()
            click('退出登录')
            expect(page.get_by_role('heading', name='欢迎回到 asuka')).to_be_visible()
            logout = [c for c in calls if c[1] == '/api/logout'][-1]
            assert logout[2] == {} and logout[3]['X-CSRF-Nonce'] == 'fixture-session-nonce'
            login(); expect(page.locator('[name=batch_display_name]')).to_have_value('')
            # Subscription drafts keep the current edit identity/CAS; rule drafts require a new preview.
            click('订阅管理'); click('编辑')
            page.locator('[name=subscription_name]').fill('Recovered subscription')
            state['authenticated'] = False; state['version'] = 3; click('刷新状态')
            expect(page.get_by_role('heading', name='会话已结束')).to_be_visible(); login()
            expect(page.locator('[name=subscription_name]')).to_have_value('Recovered subscription')
            expect(page.locator('[name=subscription_id]')).to_have_attribute('readonly','')
            assert not [c for c in calls if c[0] == 'POST' and c[1] == '/api/subscriptions']
            click('保存订阅')
            expect(page.locator('#message')).to_contain_text('操作已保存')
            assert [c for c in calls if c[0] == 'POST' and c[1] == '/api/subscriptions'][-1][2]['version'] == 3
            click('命名规则');page.locator('[name=display_name]').fill('Recovered rule')
            page.locator('[name=aliases]').fill('Explicit fixture alias')
            state['authenticated'] = False;click('刷新状态')
            expect(page.get_by_role('heading', name='会话已结束')).to_be_visible();login()
            expect(page.locator('[name=display_name]')).to_have_value('Recovered rule')
            expect(page.locator('[name=aliases]')).to_have_value('Explicit fixture alias')
            expect(page.get_by_role('button',name='保存草稿版本')).to_be_disabled()
            # A late 401 from the old session cannot log out the newly authenticated one.
            click('日常总览'); held = []
            page.route('**/api/workflow', lambda r: held.append(r)); click('刷新状态');page.wait_for_timeout(60)
            assert len(held) == 1
            state['authenticated'] = False; click('任务与入库')
            expect(page.get_by_role('heading', name='会话已结束')).to_be_visible(); login()
            held[0].fulfill(status=401, json={'error': 'authentication_required'});page.unroute('**/api/workflow')
            expect(page.locator('.task-card')).to_have_count(1)
            expect(page.locator('#session-gate')).to_be_hidden()
            for method, path, body, headers in calls:
                assert 'Authorization' not in headers
                if path == '/api/login':
                    assert set(body) == {'username', 'password'} and 'X-CSRF-Nonce' not in headers
            assert not errors, errors
            assert not external, external
            print('PASS MOCK SESSION CONTRACT ONLY: anonymous gate/no protected read, fixed login errors/Retry-After/no retry loop, password/nonce non-persistence, expired confirm/no replay, fresh-version preview, row order/hints and draft restore, changed-manifest fallback, fresh subscription CAS/rule draft gate, refresh expiry, logout discard, late old-session 401, desktop/390px/reduced motion; not TLS/backend authentication proof')
            browser.close()
    finally:
        server.shutdown(); server.server_close()


if __name__ == '__main__':
    run_session_contract_checks()
