#!/usr/bin/env python3
# Copyright 2026 cymx333 and contributors.
# SPDX-License-Identifier: Apache-2.0
"""Build the Android 8.1 foreground-service wrapper using isolated SDK/JDK."""
import argparse,os,secrets,shutil,subprocess,tempfile,zipfile
from pathlib import Path
p=argparse.ArgumentParser()
p.add_argument('--tools',help='optional legacy extracted tool layout');p.add_argument('--java-home');p.add_argument('--build-tools');p.add_argument('--android-jar');p.add_argument('--binary',required=True);p.add_argument('--keystore',required=True);p.add_argument('--output',required=True)
a=p.parse_args()
root=Path(__file__).resolve().parents[1]
if a.tools:
 tools=Path(a.tools).resolve();java=tools/'amazon-corretto-17.jdk/Contents/Home/bin';bt=tools/'android-15';jar=tools/'android-8.1.0/android.jar'
else:
 if not all((a.java_home,a.build_tools,a.android_jar)):
  p.error('provide --java-home, --build-tools and --android-jar (or --tools for the legacy layout)')
 java=Path(a.java_home).resolve()/'bin';bt=Path(a.build_tools).resolve();jar=Path(a.android_jar).resolve()
for required in (java/'java',java/'javac',java/'keytool',bt/'aapt2',bt/'zipalign',bt/'lib/d8.jar',bt/'lib/apksigner.jar',jar):
 if not required.is_file():p.error('missing build dependency: '+str(required))

key=Path(a.keystore).resolve();key.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
password_file=key.with_suffix('.password')
if not key.exists():
 password=secrets.token_urlsafe(32)
 fd=os.open(password_file,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:f.write(password)
 env={**os.environ,'XIAOAI_SIGN_PASSWORD':password}
 subprocess.run([str(java/'keytool'),'-genkeypair','-keystore',str(key),'-storepass:env','XIAOAI_SIGN_PASSWORD','-keypass:env','XIAOAI_SIGN_PASSWORD','-alias','xiaoaigpt','-keyalg','RSA','-keysize','3072','-validity','3650','-dname','CN=XiaoAI GPT Local Build','-storetype','PKCS12'],env=env,check=True)
 os.chmod(key,0o600)
else:password=password_file.read_text()
for name in ['aapt2','zipalign']:os.chmod(bt/name,0o755)
with tempfile.TemporaryDirectory(prefix='xiaoaigpt-apk-') as temp:
 temp=Path(temp);classes=temp/'classes';classes.mkdir();dex=temp/'dex';dex.mkdir()
 sources=sorted((root/'android/src').rglob('*.java'))
 subprocess.run([str(java/'javac'),'-encoding','UTF-8','--release','8','-classpath',str(jar),'-d',str(classes),*map(str,sources)],check=True)
 subprocess.run([str(java/'java'),'-cp',str(bt/'lib/d8.jar'),'com.android.tools.r8.D8','--min-api','26','--lib',str(jar),'--output',str(dex),*map(str,classes.rglob('*.class'))],check=True)
 unsigned=temp/'unsigned.apk'
 subprocess.run([str(bt/'aapt2'),'link','-I',str(jar),'--manifest',str(root/'android/AndroidManifest.xml'),'-o',str(unsigned)],check=True)
 with zipfile.ZipFile(unsigned,'a',compression=zipfile.ZIP_DEFLATED) as z:
  z.write(dex/'classes.dex','classes.dex')
  z.write(a.binary,'assets/lx04-gpt-agent')
 aligned=temp/'aligned.apk'
 subprocess.run([str(bt/'zipalign'),'-f','4',str(unsigned),str(aligned)],check=True)
 out=Path(a.output).resolve();out.parent.mkdir(exist_ok=True,parents=True)
 env={**os.environ,'XIAOAI_SIGN_PASSWORD':password}
 subprocess.run([str(java/'java'),'-jar',str(bt/'lib/apksigner.jar'),'sign','--ks',str(key),'--ks-key-alias','xiaoaigpt','--ks-pass','env:XIAOAI_SIGN_PASSWORD','--out',str(out),str(aligned)],env=env,check=True)
 subprocess.run([str(java/'java'),'-jar',str(bt/'lib/apksigner.jar'),'verify','--verbose',str(out)],check=True)
 print('APK built:',out)
