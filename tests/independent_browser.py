"""Actual Chromium UI + local fake HTTP. No external service or production credentials."""
import os, json, urllib.request, shutil
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

base=os.environ['NAS_WEB_URL']; control=os.environ['NAS_FIXTURE_URL']; pack=os.environ['NAS_PACK_URL']
def fixture(action):
    with urllib.request.urlopen(control+'/'+action,timeout=15) as response: assert response.status==200
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),args=['--no-sandbox'])
    page=browser.new_page(viewport={'width':1440,'height':1050})
    exceptions=[];page.on('pageerror',lambda e:exceptions.append(str(e)))
    page.goto(base + "#jobs");expect(page.get_by_role('heading',name='任务进度')).to_be_visible()
    page.get_by_role('button',name='订阅管理',exact=True).click()
    expect(page.get_by_role('heading',name='订阅来源')).to_be_visible()
    page.get_by_role('button',name='停用',exact=True).click()
    expect(page.get_by_role('button',name='启用',exact=True)).to_be_visible()
    page.locator('[name=subscription_id]').fill('browser')
    page.locator('[name=subscription_name]').fill('<img src=x onerror=window.PWNED=1>')
    page.get_by_role('button',name='保存订阅',exact=True).click()
    expect(page.get_by_text('<img src=x onerror=window.PWNED=1> · browser',exact=True)).to_be_visible()
    assert page.evaluate('window.PWNED') is None
    assert 'hidden-secret' not in page.content()
    fixture('poll');page.get_by_role('button',name='任务与入库',exact=True).click()
    expect(page.get_by_role('button',name='查看详情')).to_have_count(1)
    fixture('tick');page.get_by_role('button',name='查看详情').click()
    expect(page.get_by_role('heading',name='人工单集预览')).to_be_visible()
    expect(page.get_by_text('Example [02] <img src=x onerror=window.PWNED=2>',exact=True)).to_be_visible()
    assert page.evaluate('window.PWNED') is None
    page.locator('[name=target]').fill('Example/Season 01/Example - S01E02.mkv')
    page.get_by_role('button',name='生成文件预览').click()
    confirm=page.get_by_role('button',name='确认此指纹绑定方案');expect(confirm).to_be_visible()
    posts=[];page.on('request',lambda r:posts.append(r.url) if r.method=='POST' and r.url.endswith('/job-action') else None)
    confirm.evaluate('(b)=>{b.click(); b.click()}')
    expect(page.get_by_text('计划已批准',exact=True)).to_be_visible()
    assert len(posts)==1,posts
    fixture('tick');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_text('已硬链接',exact=True)).to_be_visible()
    assert page.get_by_text('全部发现 · 已就绪',exact=True).count()==0
    fixture('discover');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_text('全部发现 · 已就绪',exact=True)).to_be_visible()
    expect(page.get_by_role('button',name='重新规划')).to_be_disabled()
    page.screenshot(path='/tmp/asuka-web-job.png',full_page=True)
    page.get_by_role('button',name='命名规则',exact=True).click()
    expect(page.get_by_role('heading',name='不可变版本规则')).to_be_visible()
    values={'id':'example','revision':'1','series_id':'example','order':'custom','display_name':'Example','directory':'Example','season':'1','sample_title':'Example [03]','sample_source':'Example [03].mkv'}
    for name,value in values.items():page.locator(f'[name={name}]').fill(value)
    page.locator('[name=rule_subscription]').select_option('browser')
    page.locator('[name=aliases]').fill('Example')
    page.get_by_role('button',name='预览命名',exact=True).click()
    expect(page.locator('pre')).to_contain_text('S01E03')
    page.get_by_role('button',name='保存草稿版本').click()
    expect(page.get_by_role('button',name='启用此版本')).to_be_visible()
    page.get_by_role('button',name='启用此版本').click()
    expect(page.get_by_role('button',name='停用',exact=True)).to_be_visible()
    # Independent: programmatic rule selection must invalidate an in-flight preview.
    page.get_by_role('button',name='载入为新版本').click()
    page.locator('[name=season]').fill('2')
    page.locator('[name=sample_title]').fill('Example [03]')
    page.locator('[name=sample_source]').fill('Example [03].mkv')
    held_preview=[]
    page.route('**/api/rule-preview',lambda route:held_preview.append(route))
    page.get_by_role('button',name='预览命名',exact=True).click()
    page.wait_for_timeout(100)
    assert len(held_preview)==1
    page.get_by_role('button',name='载入为新版本').click()
    assert page.locator('[name=season]').input_value()=='1'
    response=held_preview[0].fetch()
    held_preview[0].fulfill(response=response)
    page.unroute('**/api/rule-preview')
    page.wait_for_timeout(150)
    expect(page.locator('pre')).to_be_hidden()
    expect(page.get_by_role('button',name='保存草稿版本')).to_be_disabled()
    print('PASS: programmatic reload invalidates late real API preview and disables saving')
    # Each response is fetched from the real API. Delay success AND domain error.
    for outcome in ['success','error']:
        for replacement in ['input','load','silent','reset','navigate']:
            page.get_by_role('button',name='载入为新版本').click()
            page.locator('[name=season]').fill('2' if outcome=='success' else '0')
            page.locator('[name=sample_title]').fill('Example [03]')
            page.locator('[name=sample_source]').fill('Example [03].mkv')
            delayed=[]
            page.route('**/api/rule-preview',lambda route:delayed.append(route))
            page.get_by_role('button',name='预览命名',exact=True).click()
            page.wait_for_timeout(50)
            assert len(delayed)==1
            if replacement=='input':page.locator('[name=season]').fill('1')
            elif replacement=='load':page.get_by_role('button',name='载入为新版本').click()
            elif replacement=='silent':page.locator('[name=season]').evaluate('(e)=>e.value="1"')
            elif replacement=='reset':page.locator('[name=season]').evaluate('(e)=>{e.value="1";e.closest("section").dispatchEvent(new Event("reset"))}')
            else:
                page.get_by_role('button',name='健康与接入',exact=True).click()
                expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
                page.get_by_role('button',name='命名规则',exact=True).click()
                expect(page.get_by_role('heading',name='不可变版本规则')).to_be_visible()
            response=delayed[0].fetch()
            assert (response.status==200)==(outcome=='success')
            delayed[0].fulfill(response=response);page.unroute('**/api/rule-preview')
            page.wait_for_timeout(80)
            expect(page.locator('pre')).to_be_hidden()
            expect(page.get_by_role('button',name='保存草稿版本')).to_be_disabled()
            assert page.locator('#message').inner_text()==''
    # Valid preview permits save; silent edits afterwards cannot save that review.
    page.get_by_role('button',name='载入为新版本').click()
    page.locator('[name=sample_title]').fill('Example [03]')
    page.locator('[name=sample_source]').fill('Example [03].mkv')
    page.get_by_role('button',name='预览命名',exact=True).click()
    expect(page.locator('pre')).to_contain_text('S01E03')
    expect(page.get_by_role('button',name='保存草稿版本')).to_be_enabled()
    writes=[];page.on('request',lambda r:writes.append(r.url) if r.method=='POST' and r.url.endswith('/api/rules') else None)
    page.locator('[name=season]').evaluate('(e)=>e.value="2"')
    page.get_by_role('button',name='保存草稿版本').click()
    expect(page.get_by_role('button',name='保存草稿版本')).to_be_disabled()
    assert not writes
    print('PASS preview ownership: input/load/silent/reset/navigation, delayed success/error, save binds full reviewed snapshot')
    fixture('history')
    page.get_by_role('button',name='订阅管理',exact=True).click()
    expect(page.get_by_text('共 102 条历史订阅，本页 100 条；配置缺失项保留历史、不抓取。',exact=True)).to_be_visible()
    page.get_by_role('button',name='下一页订阅').click()
    expect(page.get_by_text('<img src=x onerror=window.PWNED=1> · browser',exact=True)).to_be_visible()
    expect(page.get_by_text('共 102 条历史订阅，本页 2 条；配置缺失项保留历史、不抓取。',exact=True)).to_be_visible()
    page.get_by_role('button',name='命名规则',exact=True).click()
    page.get_by_role('button',name='载入为新版本').click()
    assert page.locator('[name=rule_subscription]').input_value()=='browser'
    print('PASS subscriptions: visible total, next page, rule selector includes later-page subscription')
    fixture('next');page.get_by_role('button',name='任务与入库',exact=True).click()
    expect(page.get_by_role('button',name='查看详情')).to_have_count(2)
    expect(page.get_by_text('已就绪',exact=True)).to_have_count(2)
    # Held GET arrives after navigation: it must never overwrite the current view.
    held=[]
    page.route('**/api/jobs?*',lambda route:held.append(route))
    page.get_by_role('button',name='刷新状态').click()
    page.wait_for_timeout(100)
    page.get_by_role('button',name='健康与接入',exact=True).click()
    expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
    for route in held:route.fulfill(status=200,content_type='application/json',body='{"items":[],"next":""}')
    page.unroute('**/api/jobs?*');page.wait_for_timeout(100)
    expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
    # Explicit failure and subsequent user recovery, not swallowed promises.
    page.route('**/api/jobs?*',lambda route:route.fulfill(status=503,content_type='application/json',body='{"error":"operation_failed"}'))
    page.get_by_role('button',name='任务与入库',exact=True).click()
    expect(page.locator('#message')).to_contain_text('操作失败')
    page.unroute('**/api/jobs?*');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_role('button',name='查看详情')).to_have_count(2)
    page.get_by_role('button',name='事件记录',exact=True).click()
    expect(page.get_by_text('discovered',exact=True)).to_have_count(2)
    page.screenshot(path='/tmp/asuka-web-events.png',full_page=True)
    page.set_viewport_size({'width':390,'height':844})
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'mobile overflow'
    page.screenshot(path='/tmp/asuka-web-mobile.png',full_page=True)
    # Reopened SQLite containing six successful links: retry through real UI.
    page.set_viewport_size({'width':1440,'height':1050});page.goto(pack + "#jobs")
    page.get_by_role('button',name='查看详情').click()
    expect(page.get_by_text('阻塞 / 错误：permission_denied',exact=True)).to_be_visible()
    expect(page.get_by_role('button',name='重新规划')).to_be_disabled()
    page.get_by_role('button',name='恢复原冻结方案').click()
    expect(page.locator('#message')).to_contain_text('操作已保存')
    fixture('pack-recover');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_text('已硬链接',exact=True)).to_be_visible()
    fixture('pack-discover');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_text('全部发现 · 已就绪',exact=True)).to_be_visible()
    assert not exceptions,exceptions
    browser.close()
print('PASS Chromium: subscription -> fake RSS/qB -> manual fingerprint confirmation -> hardlink -> Emby discovery -> events; enabled mapping auto-next; durable partial-pack retry; XSS, duplicate clicks, late GET, HTTP error recovery, mobile viewport.')
