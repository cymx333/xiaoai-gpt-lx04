// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
package local.xiaoaigpt;
import android.app.Activity;
import android.app.AlertDialog;
import android.os.Bundle;
import android.content.Intent;
import android.widget.*;
public final class MainActivity extends Activity {
 public void onCreate(Bundle b) {
  super.onCreate(b);
  LinearLayout box=new LinearLayout(this);box.setOrientation(LinearLayout.VERTICAL);box.setPadding(24,24,24,24);
  TextView title=new TextView(this);title.setText("小爱 GPT · GPT-6 Luna\n连接服务在音箱本机运行，使用你的 ChatGPT 账号额度。\n天气和设备操作仍交给原生小爱。");box.addView(title);
  Button start=new Button(this);start.setText("启动 GPT");start.setOnClickListener(v->startForegroundService(new Intent(this,VoiceService.class)));box.addView(start);
  Button stop=new Button(this);stop.setText("暂停 GPT（重启后恢复）");stop.setOnClickListener(v->stopService(new Intent(this,VoiceService.class)));box.addView(stop);
  TextView note=new TextView(this);note.setText("运行状态保存在本机应用目录。不会保存问题和回答正文。\n此版本为实机验证版。");box.addView(note);
  Button maintenance=new Button(this);maintenance.setText("临时开启维护连接");maintenance.setOnClickListener(v->new AlertDialog.Builder(this).setTitle("开启维护连接？").setMessage("仅在需要维护时开启，10分钟后自动关闭。开启期间，同一局域网可访问具有设备管理权限的调试接口。").setNegativeButton("取消",null).setPositiveButton("开启10分钟",(dialog,which)->startForegroundService(new Intent(this,VoiceService.class).setAction("local.xiaoaigpt.MAINTENANCE"))).show());box.addView(maintenance);
  setContentView(box);
  startForegroundService(new Intent(this,VoiceService.class));
 }
}
