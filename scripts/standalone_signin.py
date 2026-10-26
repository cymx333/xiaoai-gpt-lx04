#!/usr/bin/env python3
# Copyright 2026 cymx333 and contributors.
# SPDX-License-Identifier: Apache-2.0
"""One-time official ChatGPT authorization for the standalone LX04 client.
Credentials stay in an owner-only local directory until deployment is verified.
Never reads Codex credentials; never logs tokens or callback query strings.
Run with: uv run --with 'PyJWT[crypto]>=2.10,<3' standalone_signin.py --directory ...
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import secrets
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer
import jwt

ISSUER = 'https://auth.openai.com'
RESOURCE = 'https://api.openai.com/v1'
SCOPE = 'openid profile email offline_access resource.invoke chatgpt.tokens.use.direct'

def atomic(path, data):
    temp = path.with_name(path.name + '.new')
    fd = os.open(temp, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
    with os.fdopen(fd, 'w') as out:
        json.dump(data, out, ensure_ascii=False)
        out.flush()
        os.fsync(out.fileno())
    os.replace(temp, path)
    os.chmod(path, 0o600)

def request_json(url, data=None, headers=None):
    req = urllib.request.Request(url, data=data, headers=headers or {})
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.loads(response.read(2 * 1024 * 1024))

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--directory', required=True)
    parser.add_argument('--device-file', required=True, help="private JSON with serial_number and/or mac")
    args = parser.parse_args()
    folder = Path(args.directory).resolve()
    folder.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(folder, 0o700)
    if (folder / 'chatgpt-account.json').exists():
        raise SystemExit('An account is already stored; refusing to overwrite it.')
    selected = json.loads(Path(args.device_file).read_text())
    selected = {key: str(selected.get(key, '')).strip() for key in ('serial_number', 'mac')}
    if not any(selected.values()):
        raise SystemExit('Device selector must contain serial_number and/or mac.')
    host_file = folder / 'host.json'
    if not host_file.exists():
        atomic(host_file, {'ext_agent_host_id': 'urn:uuid:' + str(uuid.uuid4()), 'device': selected})
    host = json.loads(host_file.read_text())
    if host.get("device") != selected:
        raise SystemExit("Host registration belongs to a different device selector.")
    pending = {}
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args): pass
        def page(self, message, status=200):
            body = ('<!doctype html><meta charset="utf-8"><title>小爱音箱独立运行授权</title>'
                    '<main style="max-width:650px;margin:70px auto;font:18px system-ui;line-height:1.7">'
                    + message + '</main>').encode()
            self.send_response(status)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.send_header('Cache-Control', 'no-store')
            self.send_header('Referrer-Policy', 'no-referrer')
            self.send_header('Content-Security-Policy', "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        def do_GET(self):
            if self.headers.get('Host') != f'127.0.0.1:{server.server_port}':
                return self.page('Invalid host', 400)
            parts = urllib.parse.urlsplit(self.path)
            if parts.path == '/':
                return self.page('<h1>小爱音箱：独立连接 GPT</h1><p>使用你的 ChatGPT 账号，为 XiaoAI GPT LX04 单独授权。模型保持 GPT-6 Luna，使用账号额度。</p><p>授权结果先保存在这台配置电脑的私密目录；尚未部署到音箱。电脑只用于首次配置。</p><p>请通过可信 USB 或已认证 ADB 配置自己的音箱。凭据不应通过开放的免认证网络调试端口传输。</p><p><a href="/start">Continue with ChatGPT</a></p>')
            if parts.path == '/start':
                if pending:
                    return self.page('已有授权流程，请完成已打开的页面；如已过期请重新启动配置程序。', 409)
                pending.update(state=secrets.token_urlsafe(32), nonce=secrets.token_urlsafe(32), verifier=secrets.token_urlsafe(64), created=time.time())
                challenge=base64.urlsafe_b64encode(hashlib.sha256(pending['verifier'].encode()).digest()).rstrip(b'=').decode()
                params=dict(client_id='dynamic_agent_client', agent_name_hint='XiaoAI GPT LX04', ext_agent_host_id=host['ext_agent_host_id'], response_type='code', redirect_uri=callback, scope=SCOPE, resource=RESOURCE, state=pending['state'], nonce=pending['nonce'], code_challenge_method='S256', code_challenge=challenge)
                self.send_response(302)
                self.send_header('Location', ISSUER+'/api/accounts/authorize?'+urllib.parse.urlencode(params))
                self.send_header('Cache-Control', 'no-store')
                self.end_headers()
                return
            if parts.path != '/auth/callback':
                return self.page('Not found',404)
            q=urllib.parse.parse_qs(parts.query)
            one=lambda name: q.get(name,[''])[0] if len(q.get(name,[]))==1 else ''
            if not pending or not secrets.compare_digest(one('state'),pending.get('state','')) or time.time()-pending['created']>1800:
                return self.page('授权状态无效或已过期。',400)
            attempt=dict(pending)
            pending.clear() # Consume once, even if exchange fails.
            if one('error'):
                atomic(folder/'status.json',{'status':'authorization_denied'})
                return self.page('授权未完成。没有保存账号凭据。',400)
            client_id=one('client_id')
            code=one('code')
            if not client_id.startswith('oaiapp_') or not code:
                return self.page('缺少有效的应用注册结果。',400)
            try:
                token=request_json(ISSUER+'/api/accounts/oauth/token', urllib.parse.urlencode(dict(grant_type='authorization_code',client_id=client_id,code=code,code_verifier=attempt['verifier'],redirect_uri=callback,resource=RESOURCE)).encode(), {'Content-Type':'application/x-www-form-urlencoded'})
                key=jwt.PyJWKClient(ISSUER+'/.well-known/jwks.json').get_signing_key_from_jwt(token['id_token'])
                identity=jwt.decode(token['id_token'],key.key,algorithms=['RS256'],audience=client_id,issuer=ISSUER,leeway=5,options={'require':['sub','exp','iat','nonce']})
                if not identity['sub'] or not secrets.compare_digest(identity['nonce'],attempt['nonce']):
                    raise ValueError('Invalid identity')
                scopes=token.get('scope','').split()
                if not set(['chatgpt.tokens.use.direct','resource.invoke','offline_access']).issubset(scopes) or token.get('token_type','').lower()!='bearer' or not token.get('refresh_token') or not token.get('access_token'):
                    raise ValueError('Missing required permission')
                record={k:token[k] for k in ('id_token','access_token','refresh_token','token_type','expires_in')}
                record.update(client_id=client_id,ext_agent_host_id=host['ext_agent_host_id'],subject=identity['sub'],issuer=ISSUER,email=identity.get('email',''),scopes=scopes,saved_at=time.time())
                atomic(folder/'chatgpt-account.json',record)
                atomic(folder/'status.json',{'status':'authorized','credentials_transferred_to_speaker':False})
            except Exception:
                atomic(folder/'status.json',{'status':'authorization_validation_failed'})
                return self.page('授权交换或身份验证失败。没有保存新凭据；请返回对话检查状态。',400)
            self.page('<h1>授权已完成</h1><p>凭据已安全保存在本机，还没有传入音箱。请返回 README，继续执行首次安装与语音验收。</p>')
            threading.Thread(target=server.shutdown,daemon=True).start()
    server=HTTPServer(('127.0.0.1',0),Handler)
    callback=f'http://127.0.0.1:{server.server_port}/auth/callback'
    atomic(folder/'signin-page.json',{'url':f'http://127.0.0.1:{server.server_port}/'})
    atomic(folder/'status.json',{'status':'waiting_for_authorization'})
    print(f'Open this local page: http://127.0.0.1:{server.server_port}/', flush=True)
    timer=threading.Timer(3600,server.shutdown);timer.daemon=True;timer.start()
    try: server.serve_forever()
    finally: timer.cancel();server.server_close()
if __name__=='__main__': main()
