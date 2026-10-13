"""Actual Chromium + production Management/SQLite; only fixture qB/Emby are fake.
No real NAS, accounts, model or external network calls. Invoked from Go browser gate.
"""
import os, shutil
from playwright.sync_api import sync_playwright, expect
base = os.environ['NAS_WORKFLOW_URL']
empty = os.environ['NAS_WORKFLOW_EMPTY_URL']
with sync_playwright() as p:
    browser = p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'), args=['--no-sandbox'])
    page = browser.new_page(viewport={'width':1440, 'height':1000})
    errors=[]; writes=[]; external=[]
    page.on('pageerror', lambda e:errors.append(str(e)))
    page.on('request', lambda r:writes.append(r.url) if r.method=='POST' else None)
    page.on('request', lambda r:external.append(r.url) if not r.url.startswith((base,empty)) else None)
    def nav(name): page.get_by_role('button',name=name,exact=True).click()
    def search(text):
        page.locator('[name=job_search]').fill(text)
        nav('应用筛选')
        expect(page.locator('#view')).to_have_attribute('aria-busy','false')
    def capture(name, full=True):
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        page.screenshot(path=f'/tmp/asuka-workflow-{name}.png',full_page=full)
    page.goto(base)
    expect(page.locator('[data-workflow=downloading] strong')).to_have_text('21')
    expect(page.locator('[data-workflow=naming] strong')).to_have_text('22')
    expect(page.locator('[data-workflow=emby] strong')).to_have_text('21')
    expect(page.locator('[data-workflow=failed] strong')).to_have_text('21')
    expect(page.locator('.scope-note')).to_contain_text('全库 106')
    capture('real-overview-desktop')
    page.set_viewport_size({'width':390,'height':844});capture('real-overview-mobile')
    page.set_viewport_size({'width':1440,'height':1000})
    page.locator('[data-workflow=naming]').focus();page.keyboard.press('Enter')
    expect(page.locator('.task-card')).to_have_count(22)
    expect(page.locator('[name=job_filter]')).to_have_value('naming')
    search('Example')
    expect(page.locator('.task-card')).to_have_count(1)
    expect(page.locator('.task-card')).to_contain_text('季集未知')
    nav('命名 / 人工整理')
    expect(page.locator('[data-naming=true]')).to_be_visible()
    expect(page.locator('[name=naming_season]')).to_have_value('')
    expect(page.locator('#job-naming')).to_be_focused()
    deep_url=page.url
    page.reload();expect(page.locator('[data-naming=true]')).to_be_visible()
    nav('来源证据');expect(page.locator('#job-evidence')).to_be_focused()
    nav('对比与审计');expect(page.locator('#job-comparison')).to_be_focused()
    nav('安全恢复');expect(page.locator('#job-recovery')).to_be_focused()
    nav('← 返回任务');expect(page.locator('.task-card')).to_have_count(1)
    expect(page.locator('[name=job_search]')).to_have_value('Example')
    expect(page.locator('[name=job_filter]')).to_have_value('naming')
    page.go_back();expect(page.locator('[data-naming=true]')).to_be_visible()
    nav('任务与入库');expect(page.locator('.task-card')).to_have_count(100)
    expect(page.locator('.task-card').filter(has_text='名称未知')).to_have_count(1)
    assert page.locator('.task-card img,.task-card a,.task-card script').count()==0
    assert page.evaluate('window.PWNED') is None
    nav('下一页');expect(page.locator('.task-card')).to_have_count(6)
    selected=page.locator('.task-card').first.get_attribute('data-job-id')
    page.get_by_role('button',name='查看详情').first.click()
    expect(page.locator('#job-recovery')).to_contain_text(selected)
    nav('← 返回任务');expect(page.locator('.task-card')).to_have_count(6)
    assert page.locator('.task-card').first.get_attribute('data-job-id')==selected
    page.reload();expect(page.locator('.task-card')).to_have_count(6)
    # Server-side literal search, not SQL wildcards, and filter changes reset pagination.
    search('%_');expect(page.get_by_role('heading',name='没有符合条件的任务')).to_be_visible()
    nav('清除筛选并返回首页');expect(page.locator('.task-card')).to_have_count(100)
    page.locator('[name=job_filter]').select_option('emby');expect(page.locator('.task-card')).to_have_count(21)
    expect(page.locator('.task-card').first).to_contain_text('已硬链接')
    search('Fixture Show');capture('real-tasks-desktop',full=False)
    page.set_viewport_size({'width':390,'height':844});capture('real-tasks-mobile',full=False)
    page.set_viewport_size({'width':1440,'height':1000})
    # Real API responses held until navigation or a newer refresh invalidate their ownership.
    for status in ['success','error']:
        held=[];page.route('**/api/jobs?*',lambda route:held.append(route))
        nav('刷新状态');page.wait_for_timeout(80);assert len(held)==1
        nav('健康与接入');expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
        if status=='success':held[0].fulfill(response=held[0].fetch())
        else:held[0].fulfill(status=503,json={'error':'store_failed'})
        page.unroute('**/api/jobs?*');page.wait_for_timeout(80)
        expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
        expect(page.locator('#message')).to_be_empty()
        nav('任务与入库');expect(page.locator('.task-card')).to_have_count(100)
    held=[];page.route('**/api/jobs?*',lambda route:held.append(route))
    nav('刷新状态');page.wait_for_timeout(80)
    nav('刷新状态');page.wait_for_timeout(80);assert len(held)==2
    held[1].fulfill(response=held[1].fetch());expect(page.locator('.task-card')).to_have_count(100)
    held[0].fulfill(status=503,json={'error':'store_failed'});page.unroute('**/api/jobs?*');page.wait_for_timeout(80)
    expect(page.locator('#message')).to_be_empty()
    # Current load failure has a working retry, retaining the route and filters.
    page.route('**/api/jobs?*',lambda route:route.abort())
    nav('刷新状态');expect(page.get_by_role('heading',name='暂时无法读取')).to_be_visible()
    capture('real-load-error',full=False)
    page.unroute('**/api/jobs?*');nav('重新加载');expect(page.locator('.task-card')).to_have_count(100)
    nav('订阅管理')
    expect(page.get_by_role('heading',name='接入步骤 · 配置文件管理')).to_be_visible()
    page.get_by_role('button',name='停用',exact=True).evaluate('(b)=>{b.click();b.click()}')
    expect(page.get_by_role('button',name='启用',exact=True)).to_be_visible()
    assert len(writes)==1,writes
    assert 'hidden-secret' not in page.content()
    nav('健康与接入');expect(page.get_by_text('未知 · 未发起连接探测',exact=True)).to_have_count(3)
    capture('real-setup-desktop')
    page.set_viewport_size({'width':390,'height':844});capture('real-setup-mobile')
    page.set_viewport_size({'width':1440,'height':1000})
    # Empty database, missing source profile, and initial session failure recovery.
    page.route('**/api/session',lambda route:route.abort())
    page.goto(empty);expect(page.get_by_role('heading',name='暂时无法读取')).to_be_visible()
    page.unroute('**/api/session');nav('重新加载')
    expect(page.get_by_role('heading',name='从第一条订阅开始')).to_be_visible()
    capture('real-empty-desktop')
    nav('订阅管理');expect(page.get_by_role('button',name='保存订阅')).to_be_disabled()
    expect(page.locator('.setup-guide')).to_contain_text('缺少 RSS 来源档案')
    assert page.locator('[name=rss_url],[name=password],[name=token]').count()==0
    nav('健康与接入');expect(page.get_by_text('缺失',exact=True)).to_have_count(2)
    assert not external,external
    assert not errors,errors
    print('PASS WORKFLOW production Management + SQLite: exact scoped counts, safe summaries, server search/filter/page, selected task/refresh/deep return/history, late success/error/new refresh, load/session retry, double-click subscription CAS, configured/missing/unknown setup, empty data, desktop/390px; no upstream calls')
    browser.close()
