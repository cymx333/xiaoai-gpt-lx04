# XiaoAI GPT LX04

让小爱触屏音箱 LX04 **独立连接 GPT-6 Luna**：日常无需 Mac、电脑、NAS 或树莓派常开，开机后自动恢复服务。音箱运行客户端，模型推理仍在 OpenAI 云端，唤醒、语音识别及播报仍依赖小米服务。

本项目基于 [Goldloli/xiaoai-deepseek-lx04](https://github.com/Goldloli/xiaoai-deepseek-lx04) 改造，保留上游来源及许可证。不是小米或 OpenAI 官方产品。

[English](README.en.md) · [安全与隐私](SECURITY.md) · [许可证与来源](THIRD_PARTY.md)

## 功能与验证范围

- 普通问答直接调用固定模型 `gpt-6-luna`，使用**使用者自己的 ChatGPT 账号独立授权**，不读取 Codex 登录文件，也不要求创建 API Key。
- 天气、闹钟、音乐和设备操作按本地规则交回原生小爱。
- 在识别中间结果出现时提前静音，等匹配的 GPT 播报事件到达后恢复，减少原生抢答。
- Android 前台服务负责开机启动和进程恢复；客户端包含超时及音频恢复保护。
- 默认不保存问答正文，不在 ChatGPT/Codex 侧边栏创建持久对话；当前没有多轮记忆或联网搜索。
- 日常关闭 ADB，应用内可临时开启约十分钟维护。首次配置和安装仍需要电脑及**已有 root ADB**。

**已验证：一台 LX04 / Android 8.1（SDK 27）/ ARMv7 / 固件 2.99.99。** 已完成普通问答、天气、授权自动续期、主进程恢复、系统重启和实际断电重启测试。少量实测模型响应约 2～3 秒，不代表所有网络、账号或问题的延迟保证。其他机型、固件以及音频 Binder 事务号没有验证。

本方案不刷写 boot/system/vendor，不安装 Magisk，也不会为未开放 root 的设备自动解锁。上游 DeepSeek 与刷机资料保留在仓库中，**不属于本 README 的 GPT 安装步骤**。

## 配置前准备

推荐在 macOS 配置和构建（已验证）；Linux 可参考相同命令，但 APK 部署脚本的完整流程尚未在第二台设备验证。Windows 用户可使用合适的构建环境自行适配，不要照搬路径。

需要：

1. 已有 root ADB 的 LX04，优先使用 USB 或已认证 ADB。必须知道自己音箱的序列号或 MAC。
2. 音箱能够访问 OpenAI 和小米服务。电脑能访问不代表音箱也能访问。
3. 绑定该音箱的小米账号，以及允许独立客户端使用套餐额度、且模型目录包含 `gpt-6-luna` 的 ChatGPT 账号。账号资格、配额和支持模型以服务端为准，不可用时程序报错，不自动换模型或改用付费 API Key。
4. [.go-version](.go-version) 指定的 Go、Python 3.11+、[uv](https://docs.astral.sh/uv/)、JDK 17、[Android SDK / Platform-Tools](https://developer.android.com/studio)。
5. Android SDK Platform 27 和 Build Tools 35.0.0。SDK Manager 可安装：

```sh
sdkmanager "platforms;android-27" "build-tools;35.0.0" "platform-tools"
```

这是源码发布，仓库不包含预编译 APK、厂商固件、任何人的账号、令牌或签名密钥。请自行构建并保管自己的签名密钥，后续升级需要同一把密钥。

## 1. 获取源码与创建私密目录

```sh
git clone https://github.com/cymx333/xiaoai-gpt-lx04.git
cd xiaoai-gpt-lx04
mkdir -p .secrets dist
chmod 700 .secrets
cp speaker-config.gpt.example.json .secrets/speaker-config.json
cp device-match.example.json .secrets/device-match.json
chmod 600 .secrets/*.json
```

在本地编辑器填写下列文件。不要把密码放进命令行、截图、Issue 或 Git 提交中。

| 文件 | 需要填写 |
| --- | --- |
| `.secrets/speaker-config.json` | `account`、`password`：绑定音箱的小米账号与密码。`hardware` 保持 `LX04`，`use_command` 保持 `true`。`mi_did` 初次留空，由小米初始化程序填写。 |
| `.secrets/device-match.json` | `serial_number` 或 `mac` 至少填一项；两项都填时必须同时精确匹配。填写自己的设备信息。 |
| `.secrets/chatgpt-account.json` | **不要手动填写**；第 4 步通过官方授权生成。 |

GPT 模式的 `openai_key` 和 `openai_backend` 保持空字符串。DeepSeek 示例文件 `speaker-config.example.json` 供旧版代码使用，不要和 GPT 示例混用。

## 2. 构建客户端与 APK

先设置自己安装的 JDK 和 Android SDK 路径，例如通过系统工具查询。下列变量必须指向真实安装目录，不能直接使用占位符：

```sh
export JAVA_HOME="<你的 JDK 17 根目录>"
export ANDROID_HOME="<你的 Android SDK 根目录>"
```

然后构建：

```sh
make test
(
  cd client
  CGO_ENABLED=0 go build -trimpath -o ../dist/lx04-bootstrap-host ./cmd/lx04-bootstrap
)
python3 scripts/build-android-agent.py \
  --go-root "$(go env GOROOT)" \
  --source client \
  --output dist/lx04-gpt-agent-android
python3 scripts/build-android.py \
  --java-home "$JAVA_HOME" \
  --build-tools "$ANDROID_HOME/build-tools/35.0.0" \
  --android-jar "$ANDROID_HOME/platforms/android-27/android.jar" \
  --binary dist/lx04-gpt-agent-android \
  --keystore .secrets/android-signing.p12 \
  --output dist/xiaoai-gpt.apk
```

Android 8 的应用限制会终止 `pidfd_open` 探测。构建脚本通过只影响该产物的 Go overlay 使用旧进程管理路径，不关闭设备 SELinux/seccomp，也不改全局 Go 安装。不要把普通 Linux/ARM 构建直接替代这个产物。

签名工具首次运行会生成 `.secrets/android-signing.p12` 和对应 `.password` 文件。两者都不能上传；不要在升级时重新生成签名密钥。

## 3. 登录小米并精确匹配音箱

在仓库根目录运行初始化程序。显式移除旧环境变量覆盖，避免把其他项目的账号或 Key 写入本配置：

```sh
env -u MI_USER -u MI_PASS -u MI_DID \
  -u OPENAI_API_KEY -u OPENAI_MODEL -u OPENAI_BASE_URL \
  ./dist/lx04-bootstrap-host \
  -config .secrets/speaker-config.json \
  -device-file .secrets/device-match.json
```

如果需要短信或邮箱验证码，程序会在 `.secrets/bootstrap-status.json` 写入 `verification_required` 并等待约十分钟。保持当前进程运行，在另一个窗口用本地编辑器将验证码写入 `.secrets/otp.txt`（仅验证码），再设为 `chmod 600 .secrets/otp.txt`。程序读取后删除它。

成功标志：`bootstrap-status.json` 中 `status` 为 `ready`、`exact_matches` 为 `1`，并生成 `.secrets/.mi.token`、回填 `speaker-config.json` 的 `mi_did`。匹配失败时修正自己的设备选择文件，不要随便选择列表第一台设备。遇到风控、图片验证码或验证失败时停止重试并查看脱敏状态；不要反复发送短信。

## 4. 用自己的 ChatGPT 账号授权

```sh
uv run --no-project --with 'PyJWT[crypto]>=2.10,<3' \
  python scripts/standalone_signin.py \
  --directory .secrets \
  --device-file .secrets/device-match.json
```

打开终端输出的 `http://127.0.0.1:端口/` 页面，点击 **Continue with ChatGPT**，登录自己的账号并审阅 `XiaoAI GPT LX04` 的套餐额度权限。授权完成后生成 `.secrets/chatgpt-account.json`；等待配置程序退出。

这是[官方独立客户端授权流程](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)。程序校验 PKCE、state、nonce、签名、账号身份和所授予权限，再以 0600 权限保存。请不要复制浏览器 Cookie、他人的 Token 或 Codex 的 `auth.json`。本地页面只用于首次配置，日常问答不经过它。

现有授权文件不会被覆盖。如更换账号或设备，使用另一个私密配置目录重新授权；不要把两个设备的注册和刷新令牌混用。

## 5. 首次安装到自己的音箱

确保其他 Mac 伴随服务或旧版 agent 已停止，避免同一句话被处理两次。安装工具不负责自动停止其他项目的服务。

```sh
adb devices -l
export SPEAKER_ADB="<adb devices 中自己音箱的标识>"
adb -s "$SPEAKER_ADB" shell id
```

`id` 必须显示 root；不是 root 就停止，本工具不会尝试提权或刷机。无线 ADB 需先自行 `adb connect <自己的音箱IP>:<端口>`，不能使用别人的 IP。

先只读检查，确认匹配正确再安装：

```sh
python3 scripts/install-android.py \
  --device "$SPEAKER_ADB" \
  --device-file .secrets/device-match.json \
  --private-dir .secrets \
  --apk dist/xiaoai-gpt.apk \
  --dry-run

python3 scripts/install-android.py \
  --device "$SPEAKER_ADB" \
  --device-file .secrets/device-match.json \
  --private-dir .secrets \
  --apk dist/xiaoai-gpt.apk
```

脚本检查机型、SDK、root、精确设备身份和私密配置；不覆盖已有账号。它会安装应用、授予 `READ_LOGS` 和 `WRITE_SECURE_SETTINGS`、按系统实际分配的 UID 配置 0600 私有文件、添加本应用的省电豁免并启动服务。不会硬编码某台音箱的 UID、IP、序列号或账号。

默认拒绝未经认证的网络 ADB。只有确认网络隔离且接受传输风险时，才能自行增加 `--allow-unauthenticated-network`。此选项不会加密 ADB，也不是安全认证的替代；优先 USB。

## 6. 验收与关闭维护

安装后约十分钟内可以查看**状态日志**：

```sh
adb -s "$SPEAKER_ADB" shell \
  'tail -n 20 /data/user/0/local.xiaoaigpt/files/runtime.log'
```

应出现 `ready: standalone_gpt-6-luna`。依次检查：

1. 问一个知识问题，确认 GPT 播报正常、没有明显原生抢答。
2. 问天气，检查一条自己常用的设备指令。
3. 关闭电脑上的伴随程序，确认音箱仍可使用。
4. 实际断电约十秒后重新接电，等待联网，再检查 GPT 和天气。

完成后关闭维护：

```sh
adb -s "$SPEAKER_ADB" shell am start-foreground-service \
  -n local.xiaoaigpt/.VoiceService \
  -a local.xiaoaigpt.CLOSE_MAINTENANCE
```

数秒后 ADB 断开是预期行为，**GPT 问答仍应正常**。此开关也可能断开 USB 调试。开机会清除维护状态；以后需要维护时，在音箱的“小爱 GPT”应用中手动开启十分钟维护。请在关闭调试前确认自己能在音箱上打开该应用。

## 升级、停用与排障

- **升级**：保留同一签名密钥构建新 APK。在音箱开启维护后，运行安装命令并以 `--update-only` 替代 `--private-dir .secrets`；建议先加 `--dry-run`。升级保留设备端凭据。不要把电脑上的旧刷新令牌覆盖回已经续期的音箱。
- **暂停**：应用内“暂停 GPT（重启后恢复）”；“启动 GPT”可恢复。卸载应用会删除其私有配置，请先确认音箱未处于临时静音。
- **授权撤销/额度不足**：重新授权或等账号额度恢复；程序不会自动购买额度、改 API Key 或替换模型。可在 ChatGPT 的已授权应用管理中撤销此客户端。
- **没有 GPT 回答**：检查音箱网络、是否有 `ready`、账号是否允许该模型、是否同时运行旧程序，以及 READ_LOGS 权限。不要将完整令牌或原始设备日志上传到 Issue。
- **出现系统调用崩溃**：确认用的是 `build-android-agent.py` 的兼容构建，不是普通 Linux/ARM 二进制。
- **异常静音**：保护子进程可处理主进程异常，但无法保证覆盖整包强制停止或系统同时杀掉同 UID 全部进程。先重新启动应用；必要时重新接电。维护期间不要强制停止整个应用。

源码包含可用于诊断的 `client/cmd/lx04-gptcheck`，但不要在两个进程或两台设备同时使用同一份可续期账号记录。

## 来源与许可


上游固定来源：`Goldloli/xiaoai-deepseek-lx04` 的 `c0f85e9886fc57f4dc04fbfab844c4a9885cf7f9`。本分支新增 Android 独立运行、ChatGPT OAuth/Responses 接入及配置工具；保留的旧 Mac/DeepSeek 代码不是默认运行路径。

默认 [Apache-2.0](LICENSE)；MiService 的 MIT 许可和上游硬件脚本的 GPL-3.0-only 许可保持不变，详见 [NOTICE](NOTICE) 与 [THIRD_PARTY.md](THIRD_PARTY.md)。
