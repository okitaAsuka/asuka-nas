"""Actual Chromium + production management API/SQLite; fixtures/model/upstreams are fake."""
import json, os, shutil, urllib.request
from playwright.sync_api import sync_playwright, expect

control = os.environ['NAS_FIXTURE_URL']
def fixture(action):
    with urllib.request.urlopen(control + '/' + action, timeout=20) as response:
        assert response.status == 200
        return response.read()
config = json.loads(fixture('scenarios'))
with sync_playwright() as p:
    browser = p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'), args=['--no-sandbox'])
    page = browser.new_page(viewport={'width':1440, 'height':1100})
    exceptions = []
    page.on('pageerror', lambda e: exceptions.append(str(e)))
    def open_scenario(name):
        page.goto(config['urls'][name])
        page.get_by_role('button', name='命名规则', exact=True).click()
        expect(page.get_by_role('heading', name='agent 待审建议')).to_be_visible()
        page.get_by_role('button', name='审核建议').first.click()
        expect(page.get_by_role('heading', name='审核 agent 规则建议')).to_be_visible()
    def trial():
        page.get_by_role('button', name='历史试算当前版本').click()
        expect(page.locator('[data-proposal-trial]')).to_contain_text('完整试算覆盖集合')
    def confirm():
        page.locator('[name=proposal_approval]').check()
        if config['scope'] and page.locator('[name=proposal_scope_approval]').is_visible():
            page.locator('[name=proposal_scope_approval]').check()
    status = page.locator('[data-proposal-status]')
    output = page.locator('[data-proposal-trial]')
    approve = page.get_by_role('button', name='人工批准并启用规则')
    open_scenario('approve')
    expect(page.get_by_text('待人工终审', exact=True)).to_be_visible()
    expect(page.get_by_text('Example [01].mkv', exact=True).first).to_be_visible()
    expect(page.get_by_text('订阅：fixture · 来源标题：Example [01]', exact=True)).to_be_visible()
    page.get_by_text('查看完整人工对比与实际执行', exact=True).click()
    page.get_by_role('button', name='查看对比记录').click()
    expect(page.get_by_text('整包完成', exact=True)).to_be_visible()
    expect(page.get_by_text('原识别：季 未声明 · 相对集 1 · 总集 未声明', exact=True)).to_be_visible()
    expect(approve).to_be_disabled()
    # Actual successes and backend version errors delivered after context changes.
    for outcome in ['success', 'error']:
        for change in ['input', 'silent', 'cancel', 'navigate', 'candidate']:
            held = []
            page.route('**/api/proposal-preview', lambda route: held.append(route))
            page.get_by_role('button', name='历史试算当前版本').click()
            page.wait_for_timeout(60); assert len(held) == 1
            if change == 'input': page.locator('[name=proposal_explanation]').fill('替换中的草稿')
            elif change == 'silent': page.locator('[name=proposal_explanation]').evaluate('(n)=>n.value="静默替换草稿"')
            elif change == 'cancel': page.get_by_role('button', name='取消本次审核 / 试算').click()
            elif change == 'candidate':
                page.get_by_text('候选审计与版本历史', exact=False).click()
                page.get_by_role('button', name='查看 revision 1', exact=True).click()
            else:
                page.get_by_role('button', name='健康与接入', exact=True).click()
                expect(page.get_by_role('heading', name='本地服务与接入边界')).to_be_visible()
            if outcome == 'error':
                body = held[0].request.post_data_json; body['version'] = 999
                response = held[0].fetch(post_data=json.dumps(body))
            else: response = held[0].fetch()
            assert (response.status == 200) == (outcome == 'success')
            held[0].fulfill(response=response); page.unroute('**/api/proposal-preview'); page.wait_for_timeout(80)
            if change == 'navigate':
                assert page.locator('#message').inner_text() == ''
                open_scenario('approve')
            else:
                expect(output).to_be_empty(); expect(approve).to_be_disabled()
                assert '规则环境或试算' not in status.inner_text()
                page.get_by_role('button', name='重新读取候选状态').click()
                expect(page.locator('[name=proposal_explanation]')).to_have_value('本次人工明确第二季第一集；只适用已观察范围。')
    posts = []
    page.on('request', lambda r: posts.append(r.url) if r.method == 'POST' else None)
    page.get_by_role('button', name='历史试算当前版本').evaluate('(b)=>{b.click();b.click()}')
    expect(output).to_contain_text('0 个冲突')
    assert len([x for x in posts if x.endswith('/proposal-preview')]) == 1
    confirm(); expect(approve).to_be_enabled()
    # Silent mutation must be checked again at the decision boundary.
    page.locator('[name=proposal_explanation]').evaluate('(n)=>n.value="未保存的草稿"')
    approve.click(); expect(approve).to_be_disabled()
    assert not [x for x in posts if x.endswith('/proposal-approve')]
    expect(status).to_contain_text('尚未保存')
    page.locator('[name=proposal_season]').fill('0')
    page.get_by_role('button', name='保存为新 revision').click()
    expect(page.locator('[name=proposal_season]')).to_be_focused()
    expect(page.locator('[name=proposal_season]')).to_have_attribute('aria-invalid', 'true')
    page.locator('[name=proposal_season]').fill('2')
    # Backend failure retains current fields, with focus on actionable status.
    page.locator('[name=proposal_subscription]').fill('unknown')
    page.get_by_role('button', name='保存为新 revision').click()
    expect(status).to_contain_text('依据')
    expect(page.locator('[name=proposal_subscription]')).to_have_value('unknown')
    expect(status).to_be_focused()
    page.locator('[name=proposal_subscription]').fill('fixture')
    page.locator('[name=proposal_explanation]').fill('人工核对原始记录后保留第2季映射')
    if config['scope']:
        page.locator('[name=proposal_scope_declaration]').check()
        page.locator('[name=proposal_end]').fill('2')
        page.locator('[name=proposal_scope_reason]').fill('人工确认此订阅第二季第1至2集沿用相同映射；第2集尚未验证。')
    posts.clear()
    page.get_by_role('button', name='保存为新 revision').evaluate('(b)=>{b.click();b.click()}')
    expect(page.get_by_text('revision 2 / version 1', exact=False).first).to_be_visible()
    assert len([x for x in posts if x.endswith('/proposal-revise')]) == 1
    expect(output).to_be_empty(); expect(approve).to_be_disabled()
    page.get_by_text('候选审计与版本历史', exact=False).click()
    page.get_by_role('button', name='查看 revision 1', exact=True).click()
    expect(page.get_by_text('已过期 · 不可批准', exact=True)).to_be_visible()
    expect(page.get_by_role('button', name='历史试算当前版本')).to_be_disabled()
    page.get_by_text('候选审计与版本历史', exact=False).click()
    page.get_by_role('button', name='查看 revision 2', exact=True).click()
    trial()
    page.locator('[name=proposal_approval]').check()
    if config['scope']:
        expect(output).to_contain_text('已验证集数 · 1 集：1')
        expect(output).to_contain_text('人工声明但未验证 · 1 集：2')
        expect(approve).to_be_disabled()
        page.locator('[name=proposal_scope_approval]').focus(); page.keyboard.press('Space')
    expect(approve).to_be_enabled()
    page.screenshot(path='/tmp/asuka-rule-review-desktop.png', full_page=True)
    page.set_viewport_size({'width':390,'height':844})
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'review mobile overflow'
    page.screenshot(path='/tmp/asuka-rule-review-mobile.png', full_page=True)
    expect(page.locator('[name=proposal_explanation]')).to_be_visible()
    page.set_viewport_size({'width':1440,'height':1100})
    posts.clear(); approve.evaluate('(b)=>{b.click();b.click()}')
    expect(page.get_by_text('已批准 · 规则已启用', exact=True)).to_be_visible()
    assert len([x for x in posts if x.endswith('/proposal-approve')]) == 1
    expect(approve).to_be_disabled()
    if config['scope']:
        fixture('next')
        page.get_by_role('button',name='任务与入库',exact=True).click()
        expect(page.get_by_text('已硬链接',exact=True)).to_have_count(2)
        print('PASS real approval → next E02 automatic hardlink/discovery under declared finite scope')
    else: print('NOT RUN next E02: backend scope declaration commit not integrated')
    open_scenario('epoch'); trial(); confirm(); fixture('epoch'); approve.click()
    expect(status).to_contain_text('重新历史试算'); expect(approve).to_be_disabled(); expect(output).to_be_empty()
    trial(); confirm(); approve.focus(); page.keyboard.press('Enter')
    expect(page.get_by_text('已批准 · 规则已启用',exact=True)).to_be_visible()
    open_scenario('reject')
    expect(page.get_by_role('button',name='人工拒绝此建议')).to_be_disabled()
    page.locator('[name=proposal_rejection]').check()
    held=[]; page.route('**/api/proposal-reject',lambda route:held.append(route))
    page.get_by_role('button',name='人工拒绝此建议').evaluate('(b)=>{b.click();b.click()}')
    page.wait_for_timeout(60); assert len(held)==1
    page.get_by_role('button',name='健康与接入',exact=True).click()
    response=held[0].fetch(); assert response.status==200
    held[0].fulfill(response=response); page.unroute('**/api/proposal-reject'); page.wait_for_timeout(80)
    expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
    assert page.locator('#message').inner_text()==''
    open_scenario('reject'); expect(page.get_by_text('已拒绝 · 未启用',exact=True)).to_be_visible()
    expect(approve).to_be_disabled()
    open_scenario('stale'); trial(); confirm(); fixture('stale'); approve.click()
    expect(status).to_contain_text('重新读取'); expect(approve).to_be_disabled()
    page.get_by_role('button',name='重新读取候选状态').click()
    expect(page.get_by_text('已过期 · 不可批准',exact=True)).to_be_visible()
    expect(page.get_by_text('以下是历史冻结投影',exact=False)).to_be_visible()
    open_scenario('conflict'); trial(); confirm()
    expect(output).to_contain_text('冲突 · 不可批准'); expect(approve).to_be_disabled()
    page.goto(config['urls']['pages']);page.get_by_role('button',name='命名规则',exact=True).click()
    expect(page.get_by_role('button',name='审核建议')).to_have_count(20)
    page.get_by_role('button',name='下一页建议').click()
    expect(page.get_by_role('button',name='审核建议')).to_have_count(3)
    page.get_by_role('button',name='建议首页').click()
    expect(page.get_by_role('button',name='审核建议')).to_have_count(20)
    assert not exceptions, exceptions
    print('PASS production proposal API: evidence, revision, stale history, trial/CAS, epoch recovery, conflict, rejection, paging, double-click, keyboard, 390px, late success/error/cancel')
    browser.close()
