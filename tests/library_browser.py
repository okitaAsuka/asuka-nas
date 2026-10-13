"""Chromium: local library evidence -> explicit rule -> fake NAS loop. No real services."""
import json
import os
import shutil
import urllib.request
from playwright.sync_api import sync_playwright, expect

base = os.environ['NAS_WEB_URL']
control = os.environ['NAS_FIXTURE_URL']

def fixture(action):
    with urllib.request.urlopen(control + '/' + action, timeout=15) as response:
        assert response.status == 200

with sync_playwright() as p:
    browser = p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'), args=['--no-sandbox'])
    page = browser.new_page(viewport={'width': 1440, 'height': 1050})
    exceptions = []
    page.on('pageerror', lambda e: exceptions.append(str(e)))
    page.goto(base)
    page.get_by_role('button', name='查看详情').click()
    page.get_by_role('button', name='补充可复用映射规则').click()
    expect(page.locator('[name=season]')).to_have_value('')
    page.locator('[name=mapping_context]').select_option('0')
    expect(page.locator('[name=sample_title]')).to_have_value('Example Arc [02]')
    expect(page.locator('[name=sample_source]')).to_have_value('Example [02].mkv')
    page.locator('[name=library_directory]').fill('Example')
    scans = []
    page.on('request', lambda r: scans.append(r.url) if r.method == 'POST' and r.url.endswith('/library-candidates') else None)
    page.get_by_role('button', name='读取目录候选').evaluate('(b)=>{b.click();b.click()}')
    expect(page.get_by_role('button', name='选入规则草稿')).to_be_visible()
    assert len(scans) == 1, scans
    evidence = page.locator('pre').last.inner_text()
    assert 'episode_nfo' in evidence and 'filename' in evidence and '67' in evidence
    page.get_by_role('button', name='选入规则草稿').click()
    expect(page.locator('[name=directory]')).to_have_value('Example')
    expect(page.locator('[name=display_name]')).to_have_value('Example')
    expect(page.locator('[name=season]')).to_have_value('1')
    expect(page.locator('[name=aliases]')).to_have_value('')
    page.locator('[name=id]').fill('arc')
    page.locator('[name=aliases]').fill('Example Arc')
    page.get_by_role('button', name='预览命名', exact=True).click()
    expect(page.get_by_role('button', name='保存草稿版本')).to_be_enabled()
    fixture('change-library')
    page.get_by_role('button', name='保存草稿版本').click()
    expect(page.locator('#message')).to_contain_text('候选或目标目录已变化')
    assert page.get_by_role('button', name='启用此版本').count() == 0
    page.get_by_role('button', name='读取目录候选').click()
    expect(page.get_by_role('button', name='选入规则草稿')).to_be_visible()
    page.get_by_role('button', name='选入规则草稿').click()
    page.get_by_role('button', name='预览命名', exact=True).click()
    expect(page.get_by_role('button', name='保存草稿版本')).to_be_enabled()
    saves = []
    page.on('request', lambda r: saves.append(r.url) if r.method == 'POST' and r.url.endswith('/candidate-rule') else None)
    page.get_by_role('button', name='保存草稿版本').evaluate('(b)=>{b.click();b.click()}')
    expect(page.get_by_role('button', name='启用此版本')).to_be_visible()
    assert len(saves) == 1, saves
    fixture('tick')
    page.get_by_role('button', name='任务与入库', exact=True).click()
    expect(page.get_by_text('待人工确认', exact=True)).to_be_visible()
    page.get_by_role('button', name='命名规则', exact=True).click()
    page.get_by_role('button', name='启用此版本').click()
    expect(page.get_by_role('button', name='停用', exact=True)).to_be_visible()
    fixture('tick')
    page.get_by_role('button', name='任务与入库', exact=True).click()
    page.get_by_role('button', name='查看详情').click()
    expect(page.get_by_text('已硬链接', exact=True)).to_be_visible()
    fixture('discover')
    page.get_by_role('button', name='刷新状态').click()
    expect(page.get_by_text('全部发现 · 已就绪', exact=True)).to_be_visible()
    fixture('next')
    page.get_by_role('button', name='任务与入库', exact=True).click()
    expect(page.get_by_text('已就绪', exact=True)).to_have_count(2)
    page.get_by_role('button', name='事件记录', exact=True).click()
    expect(page.get_by_text('discovered', exact=True)).to_have_count(2)
    # Model a late transport that ignores AbortSignal: input/selection generation must still win.
    page.get_by_role('button', name='命名规则', exact=True).click()
    page.locator('[name=library_directory]').fill('Example')
    page.evaluate('''() => {
      window.__originalFetch = window.fetch;
      window.fetch = (url, options) => String(url).endsWith('/library-candidates')
        ? new Promise(resolve => { window.__heldCandidate = resolve; })
        : window.__originalFetch(url, options);
    }''')
    page.get_by_role('button', name='读取目录候选').click()
    page.wait_for_function('typeof window.__heldCandidate === "function"')
    page.locator('[name=library_directory]').fill('NewSelection')
    page.evaluate('''() => window.__heldCandidate(new Response(JSON.stringify({ directory: "Example", digest: "old", candidates: [{season: 9, display_name:"OLD", episodes:[1], sources:[], conflicts:[], unknown:[], series_id:"old"}] }), {status:200,headers:{"Content-Type":"application/json"}}))''')
    expect(page.get_by_role('button', name='读取目录候选')).to_be_enabled()
    assert page.get_by_role('button', name='选入规则草稿').count() == 0
    expect(page.locator('[name=library_directory]')).to_have_value('NewSelection')
    page.get_by_role('button', name='读取目录候选').click()
    page.wait_for_function('typeof window.__heldCandidate === "function"')
    page.get_by_role('button', name='取消目录读取').click()
    page.evaluate('''() => window.__heldCandidate(new Response(JSON.stringify({directory:"NewSelection",digest:"old",candidates:[]}),{status:200,headers:{"Content-Type":"application/json"}}))''')
    expect(page.get_by_role('button', name='读取目录候选')).to_be_enabled()
    expect(page.get_by_text('目录读取已取消；旧响应不会载入草稿。', exact=True)).to_be_visible()
    page.get_by_role('button', name='读取目录候选').click()
    page.get_by_role('button', name='健康与接入', exact=True).click()
    expect(page.get_by_role('heading', name='本地服务与接入边界')).to_be_visible()
    page.evaluate('''() => {window.__heldCandidate(new Response('{"directory":"old","digest":"old","candidates":[]}', {status:200,headers:{"Content-Type":"application/json"}}));window.fetch=window.__originalFetch;}''')
    page.wait_for_timeout(100)
    expect(page.get_by_role('heading', name='本地服务与接入边界')).to_be_visible()
    assert not exceptions, exceptions
    browser.close()
print('PASS Chromium library mapping: needs_confirmation -> source clues -> bounded candidates -> stale-save refusal -> draft -> explicit enable -> hardlink -> Emby -> event; next episode automatic; duplicate scan/save, cancelled and late transport ownership.')
