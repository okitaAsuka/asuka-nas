"""Simulated API layout/ownership checks. This does NOT certify NAS business behavior."""
import copy, json, os, shutil, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect

assets = Path(__file__).resolve().parents[1] / 'internal/nas/web'
unsafe = '<img src="https://invalid.test/payload" onerror="window.PWNED=1">'
ref = {'sample_id':'a'*64,'digest':'b'*64,'export_digest':'c'*64}
rule = {'id':'rp_example','revision':1,'series_id':'example','provider':'manual','order':'custom','subscription':'fixture','group':'','aliases':['Example'],'season':2,'directory':'Example','display_name':'Example','episodes':{'start':1,'end':1}}
proposal = {'id':'rp_example','revision':1,'version':1,'state':'pending','origin':'agent','created_at':1790000000,'input':{'protocol':1,'samples':[ref],'rule':rule,'explanation':unsafe,'uncertainties':['未观察其他篇章 · '+unsafe],'exceptions':[]}}
sample = {'ref':ref,'job_id':'job_fixture','job_version':7,'plan_version':8,'context_version':1,'parser_version':4,'contexts':[{'subscription':'fixture','title':'Example [01] '+unsafe}],'changes':[{'source':'Example [01].mkv '+unsafe,'target':'Example/Season 02/Example - S02E01.mkv','before_episode':1,'season':2,'episode':1,'changed_fields':['season','directory'],'semantics_resolved':True}]}
requests = []
class Handler(BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def reply(self,value,status=200):
        body=json.dumps(value).encode(); self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
    def do_GET(self):
        path=urlparse(self.path).path
        if not path.startswith('/api/'):
            name={'/':'index.html'}.get(path,path.lstrip('/'))
            if name not in ['index.html','app.js','style.css']:self.reply({'error':'not_found'},404);return
            body=(assets/name).read_bytes();self.send_response(200);self.send_header('Content-Type','text/javascript' if name.endswith('js') else 'text/css' if name.endswith('css') else 'text/html');self.send_header('Content-Security-Policy',"default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'");self.end_headers();self.wfile.write(body);return
        result={
            '/api/session':{'csrf':'fixture-nonce'},'/api/jobs':{'items':[],'next':''},
            '/api/rules':[], '/api/subscriptions':{'items':[],'next':''},'/api/epoch':{'epoch':0},'/api/audit':[],
            '/api/proposals':{'items':[proposal],'next':0},'/api/proposal':proposal,'/api/proposal-samples':{'items':[sample]},
            '/api/proposal-audit':{'items':[{'seq':1,'revision':proposal['revision'],'kind':'proposal_'+proposal['origin'],'created_at':1790000000}]},
        }
        self.reply(result.get(path,{'error':'not_found'}),200 if path in result else 404)
    def do_POST(self):
        body=json.loads(self.rfile.read(int(self.headers['Content-Length'])));requests.append((self.path,body))
        if self.path=='/api/proposal-revise':
            proposal['revision']+=1;proposal['input']=body['input'];proposal['input']['rule']['revision']=proposal['revision'];proposal['origin']='human'
            if 'scope_declaration' in body:proposal['scope_declaration']=body['scope_declaration']
            else:proposal.pop('scope_declaration',None)
            self.reply(proposal)
        elif self.path=='/api/proposal-preview':
            trial={'id':proposal['id'],'revision':proposal['revision'],'version':1,'epoch':9,'digest':'d'*64,'coverage':[dict(ref,sample_id=str(i).zfill(64)) for i in range(45)],'results':[{'sample_id':str(i).zfill(64),'source':'Example [01].mkv '+unsafe,'target':sample['changes'][0]['target'],'outcome':['matched','not_matched','out_of_scope'][i%3]} for i in range(45)],'conflicts':0,'limit':100,'truncated':True}
            if 'scope_declaration' in proposal:trial['scope_review']={'verified_episodes':[1],'declared_episodes':{'start':1,'end':2},'unverified_episodes':[2],'reason':proposal['scope_declaration']['reason']}
            self.reply(trial)
        elif self.path in ['/api/proposal-approve','/api/proposal-reject']:
            proposal['state']='approved' if self.path.endswith('approve') else 'rejected';proposal['version']=2;self.reply({'ok':True})
        else:self.reply({'error':'operation_failed'},409)
server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
threading.Thread(target=server.serve_forever,daemon=True).start()
try:
    with sync_playwright() as p:
        browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),args=['--no-sandbox'])
        page=browser.new_page(viewport={'width':1440,'height':1100});errors=[];external=[]
        page.on('pageerror',lambda e:errors.append(str(e)));page.on('request',lambda r:external.append(r.url) if 'invalid.test' in r.url else None)
        page.goto('http://127.0.0.1:'+str(server.server_port));page.get_by_role('button',name='命名规则',exact=True).click()
        page.get_by_role('button',name='审核建议').click()
        expect(page.locator('[name=proposal_explanation]')).to_have_value(unsafe)
        assert page.locator('.proposal-review img, .proposal-review script, .proposal-review a').count()==0
        assert page.evaluate('window.PWNED') is None and not external
        page.locator('[name=proposal_scope_declaration]').check();page.locator('[name=proposal_end]').fill('2');page.locator('[name=proposal_scope_reason]').fill('人工声明理由；第二集尚未验证')
        page.get_by_role('button',name='保存为新 revision').click()
        expect(page.get_by_text('revision 2 / version 1',exact=False)).to_be_visible()
        page.get_by_role('button',name='历史试算当前版本').click()
        output=page.locator('[data-proposal-trial]');expect(output).to_contain_text('覆盖有限：')
        expect(output).to_contain_text('人工声明但未验证 · 1 集：2')
        expect(output.locator('.review-pages').first.locator('tbody tr')).to_have_count(20)
        page.get_by_role('button',name='下一页结果').click();expect(output.locator('.review-pages').first).to_contain_text('21–40')
        page.get_by_role('button',name='下一页结果').focus();page.keyboard.press('Enter');expect(output.locator('.review-pages').first.locator('tbody tr')).to_have_count(5)
        expect(output.locator('.review-pages').first.locator('[role=status]')).to_be_focused()
        page.get_by_role('button',name='下一页覆盖').click();page.get_by_role('button',name='下一页覆盖').click()
        expect(output.locator('.review-pages').last.locator('tbody tr')).to_have_count(5)
        page.locator('[name=proposal_approval]').check();approve=page.get_by_role('button',name='人工批准并启用规则');expect(approve).to_be_disabled()
        page.locator('[name=proposal_scope_approval]').focus();page.keyboard.press('Space');expect(approve).to_be_enabled()
        page.screenshot(path='/tmp/asuka-rule-mock-desktop.png',full_page=True)
        page.set_viewport_size({'width':390,'height':844});assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
        page.screenshot(path='/tmp/asuka-rule-mock-mobile.png',full_page=True)
        # Silent edit and canceled in-flight response cannot retain authorization.
        page.locator('[name=proposal_explanation]').evaluate('(e)=>e.value="changed silently"');approve.click();expect(approve).to_be_disabled()
        assert not [r for r in requests if r[0]=='/api/proposal-approve']
        page.get_by_role('button',name='重新读取候选状态').click();expect(page.locator('[name=proposal_explanation]')).to_have_value(unsafe)
        held=[];page.route('**/api/proposal-preview',lambda r:held.append(r));page.get_by_role('button',name='历史试算当前版本').click();page.wait_for_timeout(60)
        page.get_by_role('button',name='取消本次审核 / 试算').click();held[0].fulfill(response=held[0].fetch());page.unroute('**/api/proposal-preview');page.wait_for_timeout(80)
        expect(output).to_be_empty();expect(approve).to_be_disabled()
        page.get_by_role('button',name='历史试算当前版本').click();expect(output).to_contain_text('完整试算覆盖集合')
        page.locator('[name=proposal_approval]').check();page.locator('[name=proposal_scope_approval]').check();approve.click()
        expect(page.get_by_text('已批准 · 规则已启用',exact=True)).to_be_visible()
        sent=[r[1] for r in requests if r[0]=='/api/proposal-approve'];assert len(sent)==1 and sent[0]=={'id':'rp_example','revision':2,'version':1,'epoch':9,'digest':'d'*64}
        assert not errors,errors
        assert not external
        print('PASS MOCK ONLY: desktop/390px, safe text DOM, complete result/coverage paging, scope second confirmation, snapshot/cancel, exact approval payload and persisted read')
        browser.close()
finally:server.shutdown();server.server_close()
