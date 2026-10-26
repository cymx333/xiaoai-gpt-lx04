// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
package local.xiaoaigpt;
import android.app.*;
import android.content.*;
import android.net.*;
import android.os.*;
import android.provider.Settings;
import java.io.*;
import java.net.InetAddress;
import java.util.*;

public final class VoiceService extends Service {
 private volatile boolean running;
 private volatile java.lang.Process child;
 private Thread worker;
 private PowerManager.WakeLock wake;
 private Handler maintenanceHandler;
 private final Runnable maintenanceCheck=new Runnable(){public void run(){
  long until=getSharedPreferences("maintenance",0).getLong("until",0);
  if(until<=System.currentTimeMillis())setAdb(false);
  if(maintenanceHandler!=null)maintenanceHandler.postDelayed(this,15000);
 }};
 private void setAdb(boolean enabled){
  try{if(Settings.Global.getInt(getContentResolver(),Settings.Global.ADB_ENABLED,0)!=(enabled?1:0))Settings.Global.putInt(getContentResolver(),Settings.Global.ADB_ENABLED,enabled?1:0);}catch(SecurityException ignored){}
 }

 public IBinder onBind(Intent i){return null;}
 public void onCreate(){
  super.onCreate();
  NotificationManager nm=(NotificationManager)getSystemService(NOTIFICATION_SERVICE);
  nm.createNotificationChannel(new NotificationChannel("voice","GPT 音箱连接",NotificationManager.IMPORTANCE_LOW));
  Notification n=new Notification.Builder(this,"voice").setContentTitle("小爱 GPT 正在运行").setContentText("音箱独立连接 GPT-6 Luna").setSmallIcon(android.R.drawable.stat_notify_sync).build();
  startForeground(101,n);
  PowerManager pm=(PowerManager)getSystemService(POWER_SERVICE);
  wake=pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK,"xiaoaigpt:voice");wake.acquire();
  maintenanceHandler=new Handler(Looper.getMainLooper());
  maintenanceHandler.postDelayed(maintenanceCheck,20000);
 }
 public synchronized int onStartCommand(Intent i,int flags,int id){
  if(i!=null&&"local.xiaoaigpt.MAINTENANCE".equals(i.getAction())){
   getSharedPreferences("maintenance",0).edit().putLong("until",System.currentTimeMillis()+600000).apply();setAdb(true);
  }
  if(i!=null&&"local.xiaoaigpt.CLOSE_MAINTENANCE".equals(i.getAction())){
   getSharedPreferences("maintenance",0).edit().putLong("until",0).apply();
   maintenanceHandler.postDelayed(()->setAdb(false),5000);
  }
  if(worker==null||!worker.isAlive()){
   running=true;worker=new Thread(()->runLoop(),"xiaoaigpt-supervisor");worker.start();
  }
  return START_STICKY;
 }
 private void status(String value){
  try(FileWriter f=new FileWriter(new File(getFilesDir(),"service-status.txt"))){f.write(System.currentTimeMillis()+" "+value+"\n");}catch(IOException ignored){}
 }
 private String dns(){
  ConnectivityManager cm=(ConnectivityManager)getSystemService(CONNECTIVITY_SERVICE);
  Network net=cm.getActiveNetwork();if(net==null)return null;
  LinkProperties lp=cm.getLinkProperties(net);if(lp==null)return null;
  for(InetAddress ip:lp.getDnsServers())if(!ip.getHostAddress().contains(":"))return ip.getHostAddress()+":53";
  for(InetAddress ip:lp.getDnsServers())return "["+ip.getHostAddress()+"]:53";
  return null;
 }
 private File installBinary()throws IOException{
  File out=new File(getFilesDir(),"lx04-gpt-agent");
  // Package updates replace the asset; copying is confined to this app's directory.
  File next=new File(getFilesDir(),"lx04-gpt-agent.new");
  try(InputStream in=getAssets().open("lx04-gpt-agent");OutputStream o=new FileOutputStream(next)){
   byte[] b=new byte[65536];int n;while((n=in.read(b))>0)o.write(b,0,n);
  }
  if(!next.setExecutable(true,true)||!next.renameTo(out))throw new IOException("binary installation failed");
  return out;
 }
 private void runLoop(){
  try{
   File binary=installBinary();
   File log=new File(getFilesDir(),"runtime.log");
   while(running){
    if(!new File(getFilesDir(),"chatgpt-account.json").isFile()||!new File(getFilesDir(),"speaker-config.json").isFile()){
     status("waiting_for_account_setup");Thread.sleep(3000);continue;
    }
    String resolver=dns();if(resolver==null){status("waiting_for_network");Thread.sleep(3000);continue;}
    if(log.length()>1024*1024){File previous=new File(getFilesDir(),"runtime.previous.log");previous.delete();log.renameTo(previous);}
    ProcessBuilder pb=new ProcessBuilder(binary.getAbsolutePath(),"-directory",getFilesDir().getAbsolutePath());
    Map<String,String> env=pb.environment();
    env.put("HOME",getFilesDir().getAbsolutePath());env.put("SSL_CERT_DIR","/system/etc/security/cacerts");env.put("XIAOBOT_DNS_SERVER",resolver);env.put("XIAOAI_AUDIO_PACKAGE",getPackageName());
    pb.redirectErrorStream(true);pb.redirectOutput(ProcessBuilder.Redirect.appendTo(log));
    child=pb.start();status("native_process_started");int result=child.waitFor();child=null;
    status("native_process_exited_"+result);if(running)Thread.sleep(5000);
   }
  }catch(Exception e){status("supervisor_failed_"+e.getClass().getSimpleName());}
 }
 public synchronized void onDestroy(){
  running=false;
  if(child!=null)child.destroy();
  if(worker!=null)worker.interrupt();
  if(maintenanceHandler!=null){maintenanceHandler.removeCallbacksAndMessages(null);maintenanceHandler=null;}
  setAdb(false);
  if(wake!=null&&wake.isHeld())wake.release();
  super.onDestroy();
 }
}
