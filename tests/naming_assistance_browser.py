"""Chromium with production Management/LearningHandler + SQLite (Go fixture).
Only bounded naming results, qB and Emby are fake; no external services.
Run via TestNamingAssistanceBrowserLoop, not as a mock business certification.
"""
import json, os, shutil, urllib.request
from playwright.sync_api import sync_playwright, expect
urls = json.loads(os.environ['NAS_NAMING_URLS'])
control = os.environ['NAS_FIXTURE_URL']
def fixture(case, action):
    with urllib.request.urlopen(f'{control}/{case}/{action}', timeout=30) as r:
        assert r.status == 200
with sync_playwright() as p:
    browser = p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'), args=['--no-sandbox'])
    page = browser.new_page(viewport={'width':1440, 'height':1100})
    exceptions=[]; page.on('pageerror', lambda e:exceptions.append(str(e)))
    writes=[]; page.on('request', lambda r:writes.append(r.url.rsplit('/',1)[-1]) if r.method=='POST' else None)
    def open_case(case):
        page.goto(urls[case]); page.get_by_role('button',name='查看详情').click()
        expect(page.locator('[data-naming=true]')).to_be_visible()
    def hints(directory='Example', display='Example', season='2'):
        for name,value in [('naming_directory',directory),('naming_display_name',display),('naming_season',season)]:
            page.locator(f'[name={name}]').fill(value)
    def refresh(): page.get_by_role('button',name='重新读取建议状态').click()
    def preview(): page.get_by_role('button',name='预览完整整理表').click()
    confirm=page.get_by_role('button',name='人工批准本次命名方案')
    output=page.locator('[data-batch-preview=true]')
    open_case('primary'); expect(page.locator('[name=naming_season]')).to_have_value('')
    hints('Human library','Human Example','4')
    page.get_by_role('button',name='请求命名建议').evaluate('(b)=>{b.click();b.click()}')
    expect(page.locator('[data-naming-state]')).to_contain_text('已请求 · 等待结果')
    assert writes.count('naming-request') == 1
    assert '百分比' not in page.locator('[data-naming-state]').inner_text()
    fixture('primary','suggest'); refresh()
    expect(page.locator('[data-naming-state]')).to_contain_text('建议待审核')
    expect(page.locator('.naming-evidence tbody tr')).to_have_count(4)
    expect(page.locator('.naming-evidence')).to_contain_text('zh-Hans.ass')
    assert page.locator('.naming-evidence img, .naming-evidence script, .naming-evidence a').count()==0
    assert page.evaluate('window.PWNED') is None
    page.get_by_role('button',name='载入完整建议到编辑表').click()
    expect(page.locator('[name=batch_episode]').nth(0)).to_have_value('1')
    expect(confirm).to_be_disabled(); expect(output).to_be_empty()
    preview(); expect(output).to_contain_text('3 个入库文件'); expect(confirm).to_be_disabled()
    page.locator('[name=naming_approval]').focus();page.keyboard.press('Space');expect(confirm).to_be_enabled()
    fixture('primary','assert-preview')
    page.locator('[name=batch_display_name]').evaluate('(n)=>n.value="Edited"')
    confirm.click();expect(confirm).to_be_disabled();assert 'naming-confirm' not in writes
    page.locator('[name=batch_display_name]').fill('Human Example');preview()
    fixture('primary','save-digest')
    page.locator('[name=batch_display_name]').fill('Edited');preview();fixture('primary','assert-old-digest')
    page.locator('[name=batch_display_name]').fill('Human Example')
    # Hold actual success/error responses; event edit, silent edit, cancel, reset and navigation invalidate ownership.
    for outcome in ['success','error']:
        for change in ['input','silent','row','cancel','reset','navigate']:
            page.locator('[name=batch_season]').fill('0' if outcome=='error' else '4')
            held=[];page.route('**/api/naming-preview',lambda route:held.append(route))
            preview();page.wait_for_timeout(60);assert len(held)==1
            if change=='input':page.locator('[name=batch_display_name]').fill('replacement')
            elif change=='silent':page.locator('[name=batch_display_name]').evaluate('(n)=>n.value="replacement"')
            elif change=='row':page.locator('[name=batch_episode]').nth(0).evaluate('(n)=>n.value="9"')
            elif change=='cancel':page.get_by_role('button',name='取消本次预览').click()
            elif change=='reset':page.locator('[data-batch=true]').evaluate('(n)=>n.dispatchEvent(new Event("reset"))')
            else:
                page.get_by_role('button',name='健康与接入',exact=True).click()
                expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
            response=held[0].fetch();assert (response.status==200)==(outcome=='success')
            held[0].fulfill(response=response);page.unroute('**/api/naming-preview');page.wait_for_timeout(60)
            if change=='navigate':
                page.get_by_role('button',name='任务与入库',exact=True).click();page.get_by_role('button',name='查看详情').click()
                page.get_by_role('button',name='载入完整建议到编辑表').click()
                expect(page.locator('[name=batch_episode]').nth(0)).to_have_value('1')
            expect(confirm).to_be_disabled();expect(output).to_be_empty()
            assert page.locator('#message').inner_text()==''
            page.locator('[name=batch_display_name]').fill('Human Example')
            page.locator('[name=batch_episode]').nth(0).fill('1')
    page.locator('[name=batch_season]').fill('4')
    posts_before=writes.count('naming-preview')
    page.get_by_role('button',name='预览完整整理表').evaluate('(b)=>{b.click();b.click()}')
    expect(output).to_contain_text('3 个入库文件');assert writes.count('naming-preview')==posts_before+1
    page.locator('[name=naming_approval]').check();expect(confirm).to_be_enabled()
    page.screenshot(path='/tmp/asuka-naming-real-desktop.png',full_page=True)
    page.set_viewport_size({'width':390,'height':844})
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.screenshot(path='/tmp/asuka-naming-real-mobile.png',full_page=True)
    page.set_viewport_size({'width':1440,'height':1100})
    confirm.evaluate('(b)=>{b.click();b.click()}')
    expect(page.get_by_text('计划已批准',exact=True)).to_be_visible();assert writes.count('naming-confirm')==1
    expect(page.get_by_role('button',name='复用已确认事实为规则草稿')).to_be_visible()
    page.get_by_role('button',name='复用已确认事实为规则草稿').click()
    expect(page.locator('[name=directory]')).to_have_value('Human library')
    expect(page.locator('[name=display_name]')).to_have_value('Human Example')
    expect(page.locator('[name=season]')).to_have_value('4')
    expect(page.locator('[name=aliases]')).to_have_value('');expect(page.locator('[name=end]')).to_have_value('')
    expect(page.get_by_role('button',name='保存草稿版本')).to_be_disabled()
    assert 'rules' not in writes and 'rule-state' not in writes
    fixture('primary','execute');fixture('primary','discover')
    page.get_by_role('button',name='任务与入库',exact=True).click();page.get_by_role('button',name='查看详情').click()
    expect(page.get_by_text('全部发现 · 已就绪',exact=True)).to_be_visible()
    print('PASS naming UI: current preview/digest, approval, real API hardlinks/fakeEmby, bounded rule draft')
    open_case('unresolved');page.get_by_role('button',name='请求命名建议').click()
    fixture('unresolved','unresolved');refresh()
    expect(page.locator('[data-naming-state]')).to_contain_text('依据不足')
    expect(page.locator('.naming-evidence')).to_contain_text('媒体目录：请补充')
    expect(page.locator('.naming-evidence')).to_contain_text('显示名：请补充')
    expect(page.locator('.naming-evidence')).to_contain_text('未调用模型')
    hints();page.get_by_role('button',name='明确重新请求当前快照').click()
    expect(page.locator('[data-naming-state]')).to_contain_text('已请求')
    fixture('unresolved','suggest');refresh();page.get_by_role('button',name='载入完整建议到编辑表').click()
    expect(page.locator('[name=batch_episode]')).to_have_value('1')
    preview();page.locator('[name=naming_approval]').check();confirm.click()
    expect(page.get_by_text('计划已批准',exact=True)).to_be_visible()
    fixture('unresolved','execute');fixture('unresolved','discover')
    for case in ['manual_unresolved','automatic']:
        open_case(case)
        if case=='manual_unresolved':page.get_by_role('button',name='请求命名建议').click()
        else:
            expect(page.locator('.naming-evidence')).to_contain_text('自动入队（已配置启用）')
            expect(page.locator('[name=naming_season]')).to_have_value('')
        fixture(case,'unresolved');refresh()
        expect(page.locator('[data-naming-state]')).to_contain_text('依据不足')
        expect(page.locator('.naming-evidence')).to_contain_text('未调用模型')
        count=writes.count('naming-request')
        for name,value in [('batch_directory','Manual'),('batch_display_name','Manual'),('batch_season','2'),('batch_episode','1')]:page.locator(f'[name={name}]').fill(value)
        preview();page.locator('[name=naming_approval]').check();confirm.click()
        expect(page.get_by_text('计划已批准',exact=True)).to_be_visible()
        fixture(case,'execute');fixture(case,'discover');fixture(case,'assert-no-requeue')
        assert writes.count('naming-request')==count
    open_case('rejected');hints();page.get_by_role('button',name='请求命名建议').click()
    fixture('rejected','suggest');refresh()
    expect(page.locator('[data-naming-state]')).to_contain_text('建议待审核')
    expect(page.get_by_role('button',name='人工拒绝本次建议')).to_be_disabled()
    page.locator('[name=naming_rejection]').focus();page.keyboard.press('Space')
    page.get_by_role('button',name='人工拒绝本次建议').evaluate('(b)=>{b.click();b.click()}')
    expect(page.locator('[data-naming-state]')).to_contain_text('已人工拒绝')
    count=writes.count('naming-request');page.wait_for_timeout(5200);refresh();assert writes.count('naming-request')==count
    fixture('rejected','assert-rejected')
    for name,value in [('batch_directory','Fallback'),('batch_display_name','Fallback'),('batch_season','2'),('batch_episode','1')]:page.locator(f'[name={name}]').fill(value)
    preview();page.get_by_role('button',name='确认整表方案').click()
    expect(page.get_by_text('计划已批准',exact=True)).to_be_visible()
    fixture('rejected','execute');fixture('rejected','discover');fixture('rejected','assert-no-requeue')
    assert writes.count('naming-request')==count
    open_case('offline');page.get_by_role('button',name='请求命名建议').click()
    expect(page.locator('[data-naming-state]')).to_contain_text('已请求')
    fixture('offline','assert-offline')
    # Naming service returns503. Manual full-task workflow still executes with production API.
    for name,value in [('batch_directory','Offline'),('batch_display_name','Offline'),('batch_season','4'),('batch_offset','-66')]:page.locator(f'[name={name}]').fill(value)
    page.locator('[name=batch_mode]').select_option('extracted');page.get_by_role('button',name='提取 / 展示连续编号').click()
    preview();page.get_by_role('button',name='确认整表方案').click()
    expect(page.get_by_text('计划已批准',exact=True)).to_be_visible()
    fixture('offline','execute');fixture('offline','discover')
    count=writes.count('naming-request');fixture('offline','assert-no-requeue');assert writes.count('naming-request')==count
    for case in ['stale','seed']:
        open_case(case);hints();page.get_by_role('button',name='请求命名建议').click()
        fixture(case,'suggest');refresh();page.get_by_role('button',name='载入完整建议到编辑表').click()
        expect(page.locator('[name=batch_episode]')).to_have_value('1')
        count=writes.count('naming-request')
        if case=='stale':
            fixture(case,'stale');refresh()
            expect(page.locator('[data-naming-state]')).to_contain_text('依据已过期')
            preview();page.get_by_role('button',name='确认整表方案').click()
        else:
            preview();fixture(case,'stop-seed')
            page.locator('[name=naming_approval]').check();confirm.click()
            expect(page.locator('[data-naming-status]')).to_contain_text('任务状态已更新')
            expect(page.locator('[data-naming-state]')).to_contain_text('建议待审核')
            expect(page.locator('[name=batch_episode]')).to_have_value('1')
            preview();page.locator('[name=naming_approval]').check();confirm.click()
        expect(page.get_by_text('计划已批准',exact=True)).to_be_visible()
        fixture(case,'execute');fixture(case,'discover');fixture(case,'assert-no-requeue')
        assert writes.count('naming-request')==count
        page.get_by_role('button',name='刷新状态').click();page.get_by_role('button',name='查看对比记录').click()
        expect(page.locator('.comparison-record')).to_contain_text('model_assisted_human')
    page.get_by_role('button',name='健康与接入',exact=True).click()
    expect(page.get_by_text('agent运行状态未知；以任务建议列表核对结果',exact=True)).to_be_visible()
    assert not exceptions,exceptions
    print('PASS unresolved supplement, rejection without requeue/restart, offline manual fallback, safe text, keyboard and 390px')
    browser.close()
