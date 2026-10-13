"""Actual Chromium and production NAS API; temporary sources, fake qB/Emby only."""
import os,json,shutil,urllib.request,re
from playwright.sync_api import sync_playwright,expect
base=os.environ['NAS_WEB_URL'];control=os.environ['NAS_FIXTURE_URL']
def fixture(action):
    with urllib.request.urlopen(control+'/'+action,timeout=15) as response:assert response.status==200
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),args=['--no-sandbox'])
    page=browser.new_page(viewport={'width':1440,'height':1100})
    exceptions=[];page.on('pageerror',lambda e:exceptions.append(str(e)))
    page.goto(base + "#jobs");page.get_by_role('button',name='查看详情').click()
    form=page.locator('[data-batch=true]');preview=form.locator('[data-batch-preview=true]')
    expect(form.get_by_role('heading',name='批量整理并入库')).to_be_visible()
    expect(form.locator('[name=batch_episode]')).to_have_count(2)
    assert page.get_by_role('heading',name='人工单集预览').count()==0
    page.get_by_role('button',name='打开命名规则（人工维护）').click()
    expect(page.get_by_role('heading',name='不可变版本规则')).to_be_visible()
    page.get_by_role('button',name='任务与入库',exact=True).click()
    page.get_by_role('button',name='查看详情').click()
    def fill():
        page.locator('[name=batch_directory]').fill('Human library')
        page.locator('[name=batch_display_name]').fill('Human Example')
        page.locator('[name=batch_season]').fill('4')
        page.locator('[name=batch_mode]').select_option('extracted')
        page.locator('[name=batch_offset]').fill('-66')
        page.get_by_role('button',name='提取 / 展示连续编号').click()
    fill()
    expect(form.locator('[name=batch_episode]').nth(0)).to_have_value('1')
    expect(form.locator('[name=batch_episode]').nth(1)).to_have_value('2')
    confirm=page.get_by_role('button',name='确认整表方案')
    expect(confirm).to_be_disabled()
    page.get_by_role('button',name='预览完整整理表').click()
    expect(preview).to_contain_text('3 个入库文件')
    expect(preview).to_contain_text('Episode [67].zh-Hans.ass → Human library/Season 04/Human Example - S04E01.zh-Hans.ass')
    expect(confirm).to_be_enabled()
    page.get_by_role('button',name='查看对比记录').click()
    expect(page.get_by_text('没有已确认的人工记录；预览或取消不会生成样本。',exact=True)).to_be_visible()
    # Post-preview edit with no DOM event must not confirm the old review.
    writes=[];page.on('request',lambda r:writes.append(r.url) if r.method=='POST' and r.url.endswith('/batch-confirm') else None)
    page.locator('[name=batch_season]').evaluate('(e)=>e.value="5"')
    confirm.click();expect(confirm).to_be_disabled();assert not writes
    fill();page.get_by_role('button',name='预览完整整理表').click();expect(confirm).to_be_enabled()
    page.get_by_role('button',name='取消本次预览').click();expect(confirm).to_be_disabled();expect(preview).to_be_empty()
    expect(form.locator('[data-batch-status]')).to_contain_text('草稿保留')
    page.locator('[name=batch_mode]').select_option('manual')
    page.get_by_role('button',name='预览完整整理表').click();expect(confirm).to_be_enabled()
    form.locator('[name=batch_episode]').nth(1).evaluate('(e)=>e.value="9"')
    confirm.click();expect(confirm).to_be_disabled();assert not writes
    fixture('assert-preview')
    # Physical target conflicts come from the production file layer and recover locally.
    fill();fixture('conflict')
    page.get_by_role('button',name='预览完整整理表').click()
    expect(form.locator('[data-batch-status]')).to_contain_text('目标已存在')
    expect(page.locator('[name=batch_directory]')).to_have_value('Human library')
    fixture('clear-conflict')

    # Delay actual API success and error; stale UI cannot accept either.
    for outcome in ['success','error']:
        for change in ['input','silent','silent-row','cancel','navigate','reset','mode','sort']:
            fill()
            if outcome=='error':page.locator('[name=batch_season]').fill('0')
            held=[];page.route('**/api/batch-preview',lambda route:held.append(route))
            page.get_by_role('button',name='预览完整整理表').click()
            page.wait_for_timeout(60);assert len(held)==1
            if change=='input':page.locator('[name=batch_display_name]').fill('Replacement')
            elif change=='silent':page.locator('[name=batch_display_name]').evaluate('(e)=>e.value="Replacement"')
            elif change=='silent-row':form.locator('[name=batch_episode]').nth(0).evaluate('(e)=>e.value="6"')
            elif change=='cancel':page.get_by_role('button',name='取消本次预览').click()
            elif change=='reset':form.evaluate('(e)=>e.dispatchEvent(new Event("reset"))')
            elif change=='mode':page.locator('[name=batch_mode]').select_option('manual')
            elif change=='sort':form.get_by_role('button',name=re.compile(r'^下移 ')).nth(0).click()
            else:
                page.get_by_role('button',name='健康与接入',exact=True).click()
                expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
                page.get_by_role('button',name='任务与入库',exact=True).click()
                page.get_by_role('button',name='查看详情').click()
            response=held[0].fetch();assert (response.status==200)==(outcome=='success')
            held[0].fulfill(response=response);page.unroute('**/api/batch-preview');page.wait_for_timeout(80)
            expect(confirm).to_be_disabled();expect(preview).to_be_empty()
            assert page.locator('#message').inner_text()==''
            assert '预览未完成' not in form.locator('[data-batch-status]').inner_text()
            if change=='sort':form.get_by_role('button',name=re.compile(r'^上移 ')).nth(1).click()
    print('PASS batch snapshot: edit, silent change, cancel, navigation, delayed success/error')
    # Explicit sequence: no silent lexical allocation; move the actual table order.
    fill();page.locator('[name=batch_mode]').select_option('sequential')
    page.get_by_role('button',name='提取 / 展示连续编号').click()
    page.get_by_role('button',name='预览完整整理表').click()
    expect(form.locator('[data-batch-status]')).to_contain_text('排序确认')
    form.get_by_role('button',name=re.compile(r'^下移 ')).nth(0).focus()
    page.keyboard.press('Enter')
    page.get_by_role('button',name='提取 / 展示连续编号').click()
    expect(form.locator('tbody tr').nth(0)).to_contain_text('Episode [68].mp4')
    page.locator('[name=batch_order_confirmed]').check()
    page.get_by_role('button',name='预览完整整理表').click()
    expect(preview).to_contain_text('Episode [68].mp4 → Human library/Season 04/Human Example - S04E01.mp4')
    expect(confirm).to_be_enabled()
    page.locator('[name=batch_start]').evaluate('(e)=>e.value="2"')
    confirm.click();expect(confirm).to_be_disabled();expect(page.locator('[name=batch_order_confirmed]')).not_to_be_checked()
    page.locator('[name=batch_start]').fill('1')
    page.get_by_role('button',name='提取 / 展示连续编号').click()
    page.locator('[name=batch_order_confirmed]').check()
    page.get_by_role('button',name='预览完整整理表').click();expect(confirm).to_be_enabled()
    # Changing order after explicit approval invalidates preview and ordering approval.
    form.get_by_role('button',name=re.compile(r'^上移 ')).nth(1).click()
    expect(confirm).to_be_disabled();expect(page.locator('[name=batch_order_confirmed]')).not_to_be_checked()
    page.get_by_role('button',name='提取 / 展示连续编号').click()
    page.locator('[name=batch_order_confirmed]').check()
    page.get_by_role('button',name='预览完整整理表').click();expect(confirm).to_be_enabled()
    # Duplicate episodes fail as a full pack and recover by correcting the row.
    page.locator('[name=batch_mode]').select_option('manual')
    form.locator('[name=batch_episode]').nth(1).fill('1')
    page.get_by_role('button',name='预览完整整理表').click()
    expect(form.locator('[data-batch-status]')).to_contain_text('集号重复');expect(confirm).to_be_disabled()
    form.locator('[name=batch_episode]').nth(1).fill('2')
    posts=[];page.on('request',lambda r:posts.append(r.url) if r.method=='POST' and r.url.endswith('/batch-preview') else None)
    page.get_by_role('button',name='预览完整整理表').evaluate('(b)=>{b.click();b.click()}')
    expect(confirm).to_be_enabled();assert len(posts)==1
    # Confirmation failure retains draft and requires a fresh full preview.
    fixture('conflict');confirm.click()
    expect(form.locator('[data-batch-status]')).to_contain_text('目标已存在')
    expect(confirm).to_be_disabled();expect(page.locator('[name=batch_display_name]')).to_have_value('Human Example')
    fixture('clear-conflict');writes.clear()
    page.get_by_role('button',name='预览完整整理表').click();expect(confirm).to_be_enabled()
    page.screenshot(path='/tmp/asuka-batch-editor-desktop.png',full_page=True)
    page.set_viewport_size({'width':390,'height':844})
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'batch mobile overflow'
    page.screenshot(path='/tmp/asuka-batch-editor-mobile.png',full_page=True)
    expect(form.locator('[name=batch_episode]').nth(0)).to_be_visible()
    page.set_viewport_size({'width':1440,'height':1100})
    # A real successful confirmation may arrive after navigation. Only the server saves it;
    # the old UI response must never replace the new page or display a late success message.
    held_confirm=[];page.route('**/api/batch-confirm',lambda route:held_confirm.append(route))
    confirm.evaluate('(b)=>{b.click();b.click()}')
    page.wait_for_timeout(60);assert len(held_confirm)==1;assert len(writes)==1
    page.get_by_role('button',name='健康与接入',exact=True).click()
    expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
    response=held_confirm[0].fetch();assert response.status==200
    held_confirm[0].fulfill(response=response);page.unroute('**/api/batch-confirm');page.wait_for_timeout(80)
    expect(page.get_by_role('heading',name='本地服务与接入边界')).to_be_visible()
    assert page.locator('#message').inner_text()==''
    page.get_by_role('button',name='任务与入库',exact=True).click()
    page.get_by_role('button',name='查看详情').click()
    expect(page.get_by_text('计划已批准',exact=True)).to_be_visible();assert len(writes)==1
    page.get_by_role('button',name='查看对比记录').click()
    expect(page.get_by_text('人工意图已确认，尚未执行',exact=False)).to_be_visible()
    fixture('partial');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_role('button',name='恢复原冻结方案')).to_be_visible()
    expect(page.get_by_role('button',name='重新规划')).to_be_disabled()
    assert page.locator('[data-batch=true]').count()==0
    page.get_by_role('button',name='查看对比记录').click()
    expect(page.get_by_text('部分执行，未完整成功',exact=True)).to_be_visible()
    assert page.locator('.comparison-record .badge.ok').count()==0
    page.get_by_role('button',name='恢复原冻结方案').click()
    expect(page.locator('#message')).to_contain_text('操作已保存')
    fixture('recover');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_text('已硬链接',exact=True)).to_be_visible()
    assert page.get_by_text('全部发现 · 已就绪',exact=True).count()==0
    page.get_by_role('button',name='查看对比记录').click()
    expect(page.get_by_text('全部硬链接，等待全部 Emby 确认',exact=False)).to_be_visible()
    assert page.locator('.comparison-record .badge.ok').count()==0
    fixture('discover-one');page.get_by_role('button',name='刷新状态').click()
    assert page.get_by_text('全部发现 · 已就绪',exact=True).count()==0
    page.get_by_role('button',name='查看对比记录').click()
    expect(page.get_by_text('全部硬链接，等待全部 Emby 确认',exact=True)).to_be_visible()
    fixture('discover');page.get_by_role('button',name='刷新状态').click()
    expect(page.get_by_text('全部发现 · 已就绪',exact=True)).to_be_visible()
    page.get_by_role('button',name='查看对比记录').click()
    expect(page.get_by_text('整包完成',exact=False)).to_be_visible()
    expect(page.locator('.comparison-record .badge.ok')).to_have_count(1)
    assert 'hidden-secret' not in page.content()
    page.goto(os.environ['NAS_OPAQUE_URL'] + '#jobs');page.get_by_role('button',name='查看详情').click()
    page.get_by_role('button',name='查看对比记录').click()
    expect(page.get_by_text('季集未声明',exact=True)).to_be_visible()
    assert 'S00E00' not in page.locator('.comparison-record').inner_text()
    assert not exceptions,exceptions
    page.screenshot(path='/tmp/asuka-batch-organizer.png',full_page=True)
    print('PASS complete batch: explicit sorting, duplicate gate/recovery, double click, real hardlinks and all Emby confirmations, immutable intent vs execution')
    browser.close()
