"""Real Chromium with simulated DTOs: UI/layout/ownership evidence only.
Does not certify Go, SQLite, file execution, model budgets or service protocol.
"""
import copy, json, os, shutil, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect
assets=Path(__file__).resolve().parents[1]/'internal/nas/web'
unsafe='<img src=x onerror="window.PWNED=1">'
sources=['Episode [67].mkv','Episode [67].zh-Hans.ass','Episode [68].mp4','notes.txt']
job={'id':'fixture_naming','version':7,'download':'completed','import':'needs_confirmation','discovery':'pending','refresh':'pending','seed':'seeding','seed_days':7,'sources':sources,'block_reason':'episode_unresolved'}
detail={'job':job,'ready':False,'contexts':[{'subscription':'fixture','title':'Example [67]'}], 'batch_hints':[{'source':sources[0],'relative':67},{'source':sources[2],'relative':68}]}
reviews=[];posts=[]
def suggest(state='suggested'):
    v=reviews[-1];v['state']=state;v['version']=2
    v['result']={'status':'suggestion' if state=='suggested' else 'unresolved','directory':'Human library','display_name':'Human Example','explanation':unsafe+' 人工季号与文件编号','uncertainties':['未来范围未验证 · '+unsafe], 'files':[{'file_id':str(i),'season':4 if i<3 else 0,'episode':1 if i<2 else 2 if i==2 else 0,'target':('Human library/Season 04/Human Example - S04E01'+('.mkv' if i==0 else '.zh-Hans.ass')) if i<2 else 'Human library/Season 04/Human Example - S04E02.mp4' if i==2 else '', 'basis':'人工季号 + 明确文件编号 '+unsafe,'uncertainties':[]} for i in range(4)]}
    if state=='unresolved':
        v['result']['directory']='';v['result']['display_name']=''
        for row in v['result']['files']:row.update(season=0,episode=0,target='')
def preview(b):
    return {'job_version':7,'digest':json.dumps(b,sort_keys=True), 'plan':{'version':8,'batch':b,'files':[{'source':r['source'],'target':f"{b['directory']}/Season {b['season']:02}/{b['display_name']} - S{b['season']:02}E{r['episode']:02}{Path(r['source']).suffix}",'fingerprint':'f'*64} for r in b['rows']]+[{'source':sources[1],'target':f"{b['directory']}/Season {b['season']:02}/{b['display_name']} - S{b['season']:02}E01.zh-Hans.ass",'fingerprint':'f'*64}], 'manifest':[{'source':s,'fingerprint':'f'*64} for s in sources]}}
class Handler(BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def reply(self,v,status=200):
        b=json.dumps(v).encode();self.send_response(status);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(b)
    def do_GET(self):
        path=urlparse(self.path).path
        if not path.startswith('/api/'):
            name={'/':'index.html'}.get(path,path.lstrip('/'))
            if name not in ['index.html','app.js','style.css']:self.reply({},404);return
            self.send_response(200);self.send_header('Content-Type','text/javascript' if name.endswith('js') else 'text/css' if name.endswith('css') else 'text/html');self.end_headers();self.wfile.write((assets/name).read_bytes());return
        data={'/api/session':{'csrf':'fixture-nonce'},'/api/jobs':{'items':[detail],'next':''},'/api/jobs/fixture_naming':detail,'/api/naming':{'items':reviews},'/api/rules':[],'/api/subscriptions':{'items':[{'id':'fixture','name':'Fixture'}],'next':''},'/api/epoch':{'epoch':0},'/api/audit':[],'/api/proposals':{'items':[],'next':0},'/api/health':{'management':'local'}}
        self.reply(data.get(path,{}))
    def do_POST(self):
        body=json.loads(self.rfile.read(int(self.headers['Content-Length'])));posts.append((self.path,body))
        if self.path=='/api/naming-request':
            reviews.append({'id':'naming_'+str(len(reviews)+1),'version':1,'state':'requested','snapshot':{'job_id':job['id'],'job_version':7,'hints':body['hints'],'files':[{'id':str(i),'name':s,'kind':'video' if i in [0,2] else 'subtitle' if i==1 else 'auxiliary'} for i,s in enumerate(sources)]}});self.reply(reviews[-1])
        elif self.path=='/api/naming-preview':
            b=body.get('organization') or {'directory':'Human library','display_name':'Human Example','season':4,'mode':'manual','offset':0,'start':0,'order_confirmed':False,'rows':[{'source':sources[0],'episode':1},{'source':sources[2],'episode':2}]}
            if not b['season'] or any(not r['episode'] for r in b['rows']):self.reply({'error':'invalid_batch'},409);return
            v=preview(b);reviews[-1]['preview']=v;self.reply(v)
        elif self.path=='/api/naming-confirm':
            reviews[-1]['state']='approved';reviews[-1]['version']+=1;job['plan']=dict(reviews[-1]['preview']['plan'],approved=True);job['import']='planned';self.reply({'ok':True})
        elif self.path=='/api/naming-reject':reviews[-1]['state']='rejected';reviews[-1]['version']+=1;self.reply({'ok':True})
        else:self.reply({'error':'operation_failed'},409)
server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
threading.Thread(target=server.serve_forever,daemon=True).start()
try:
    with sync_playwright() as p:
        browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),args=['--no-sandbox'])
        page=browser.new_page(viewport={'width':1440,'height':1100});errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
        page.goto(f'http://127.0.0.1:{server.server_port}');page.get_by_role('button',name='查看详情').click()
        expect(page.locator('[name=naming_season]')).to_have_value('')
        for name,value in [('naming_directory','Human library'),('naming_display_name','Human Example'),('naming_season','4')]:page.locator(f'[name={name}]').fill(value)
        page.get_by_role('button',name='请求命名建议').evaluate('(b)=>{b.click();b.click()}')
        expect(page.locator('[data-naming-state]')).to_contain_text('已请求 · 等待结果');assert len(posts)==1
        page.screenshot(path='/tmp/asuka-naming-mock-requested.png',full_page=True)
        # Preserve a manual draft when polling receives a suggestion.
        page.locator('[name=batch_directory]').fill('Draft preserved')
        suggest();page.get_by_role('button',name='重新读取建议状态').click()
        expect(page.locator('[name=batch_directory]')).to_have_value('Draft preserved')
        expect(page.locator('.naming-evidence tbody tr')).to_have_count(4)
        assert page.locator('.naming-evidence img, .naming-evidence script, .naming-evidence a').count()==0 and page.evaluate('window.PWNED') is None
        page.get_by_role('button',name='载入完整建议到编辑表').focus();page.keyboard.press('Enter')
        expect(page.locator('[name=batch_episode]').nth(0)).to_have_value('1')
        output=page.locator('[data-batch-preview=true]');confirm=page.get_by_role('button',name='人工批准本次命名方案')
        expect(confirm).to_be_disabled();expect(output).to_be_empty()
        def run_preview():page.get_by_role('button',name='预览完整整理表').click();expect(output).to_contain_text('3 个入库文件')
        run_preview();expect(confirm).to_be_disabled();page.locator('[name=naming_approval]').focus();page.keyboard.press('Space');expect(confirm).to_be_enabled()
        page.locator('[name=batch_display_name]').evaluate('(n)=>n.value="silent"');confirm.click();expect(confirm).to_be_disabled()
        assert not any(path=='/api/naming-confirm' for path,body in posts)
        page.locator('[name=batch_display_name]').fill('Human Example')
        for outcome in ['success','error']:
            for change in ['input','silent','cancel','reset','navigate']:
                assert page.locator('[name=batch_episode]').nth(0).input_value() == '1', (outcome,change)
                page.locator('[name=batch_season]').fill('0' if outcome=='error' else '4')
                held=[];page.route('**/api/naming-preview',lambda route:held.append(route))
                page.get_by_role('button',name='预览完整整理表').click();page.wait_for_timeout(60);assert len(held)==1
                if change=='input':page.locator('[name=batch_display_name]').fill('replacement')
                elif change=='silent':page.locator('[name=batch_display_name]').evaluate('(n)=>n.value="replacement"')
                elif change=='cancel':page.get_by_role('button',name='取消本次预览').click()
                elif change=='reset':page.locator('[data-batch=true]').evaluate('(n)=>n.dispatchEvent(new Event("reset"))')
                else:page.get_by_role('button',name='命名规则',exact=True).click();expect(page.get_by_role('heading',name='不可变版本规则')).to_be_visible()
                response=held[0].fetch();assert (response.status==200)==(outcome=='success')
                held[0].fulfill(response=response);page.unroute('**/api/naming-preview');page.wait_for_timeout(60)
                if change=='navigate':
                    page.get_by_role('button',name='任务与入库',exact=True).click();page.get_by_role('button',name='查看详情').click();page.get_by_role('button',name='载入完整建议到编辑表').click()
                    expect(page.locator('[name=batch_episode]').nth(0)).to_have_value('1')
                expect(confirm).to_be_disabled();expect(output).to_be_empty();assert page.locator('#message').inner_text()==''
                page.locator('[name=batch_display_name]').fill('Human Example')
        page.locator('[name=batch_season]').fill('4');run_preview();page.locator('[name=naming_approval]').check()
        page.screenshot(path='/tmp/asuka-naming-mock-desktop.png',full_page=True)
        page.locator('[data-naming=true]').scroll_into_view_if_needed();page.screenshot(path='/tmp/asuka-naming-mock-review-desktop.png')
        page.set_viewport_size({'width':390,'height':844});assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        page.screenshot(path='/tmp/asuka-naming-mock-mobile.png',full_page=True)
        page.locator('.naming-evidence h3').scroll_into_view_if_needed();page.screenshot(path='/tmp/asuka-naming-mock-review-mobile.png')
        page.set_viewport_size({'width':1440,'height':1100})
        confirm.evaluate('(b)=>{b.click();b.click()}');expect(page.get_by_role('button',name='复用已确认事实为规则草稿')).to_be_visible()
        assert len([x for x in posts if x[0]=='/api/naming-confirm'])==1
        page.get_by_role('button',name='复用已确认事实为规则草稿').click()
        expect(page.locator('[name=directory]')).to_have_value('Human library');expect(page.locator('[name=season]')).to_have_value('4')
        expect(page.locator('[name=aliases]')).to_have_value('');expect(page.locator('[name=end]')).to_have_value('')
        assert not any(path in ['/api/rules','/api/rule-state'] for path,body in posts)
        expect(page.get_by_role('button',name='保存草稿版本')).to_be_disabled()
        count=len(posts);page.get_by_role('button',name='预览命名',exact=True).click()
        expect(page.locator('#message')).to_contain_text('有限集数范围');assert len(posts)==count
        # Explicit unresolved supplement and reject, without automatic requeue.
        job.pop('plan',None);job['import']='needs_confirmation';reviews.clear()
        page.get_by_role('button',name='任务与入库',exact=True).click();page.get_by_role('button',name='查看详情').click()
        page.get_by_role('button',name='请求命名建议').click()
        expect(page.locator('[data-naming-state]')).to_contain_text('已请求')
        suggest('unresolved');reviews[-1]['result']['uncertainties']=['缺少目录、显示名与明确季号']
        page.get_by_role('button',name='重新读取建议状态').click()
        expect(page.locator('[data-naming-state]')).to_contain_text('依据不足')
        expect(page.locator('.naming-evidence')).to_contain_text('缺少目录、显示名与明确季号')
        for name,value in [('naming_directory','Human library'),('naming_display_name','Human Example'),('naming_season','4')]:page.locator(f'[name={name}]').fill(value)
        page.get_by_role('button',name='明确重新请求当前快照').click()
        expect(page.locator('[data-naming-state]')).to_contain_text('已请求')
        assert posts[-1][1]['retry'] is True and posts[-1][1]['hints']['season']==4
        suggest();page.get_by_role('button',name='重新读取建议状态').click()
        expect(page.get_by_role('button',name='人工拒绝本次建议')).to_be_disabled()
        page.locator('[name=naming_rejection]').check()
        page.get_by_role('button',name='人工拒绝本次建议').evaluate('(b)=>{b.click();b.click()}')
        expect(page.locator('[data-naming-state]')).to_contain_text('已人工拒绝')
        count=len(posts);page.wait_for_timeout(5200);assert len(posts)==count
        expect(page.get_by_role('button',name='预览完整整理表')).to_be_enabled()
        assert not errors,errors
        print('PASS MOCK ONLY: real Chromium request/suggestion/edit/double click/silent mutation/cancel/reset/navigation/safe text/keyboard/390px/rule draft')
        browser.close()
finally:server.shutdown();server.server_close()
