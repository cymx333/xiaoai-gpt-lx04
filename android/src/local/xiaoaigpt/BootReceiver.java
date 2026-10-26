// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
package local.xiaoaigpt;
import android.content.*;
public final class BootReceiver extends BroadcastReceiver {
 public void onReceive(Context context, Intent intent) {
  String a=intent.getAction();
  if (Intent.ACTION_BOOT_COMPLETED.equals(a)) context.getSharedPreferences("maintenance",0).edit().putLong("until",0).apply();
  if (Intent.ACTION_BOOT_COMPLETED.equals(a)||Intent.ACTION_MY_PACKAGE_REPLACED.equals(a))
   context.startForegroundService(new Intent(context,VoiceService.class));
 }
}
