# XiaoAI GPT LX04

A standalone Android client for the Xiaomi LX04 touchscreen speaker, derived from [Goldloli/xiaoai-deepseek-lx04](https://github.com/Goldloli/xiaoai-deepseek-lx04).

The speaker calls GPT-6 Luna directly using an independently authorized ChatGPT account. A computer is required for initial configuration and building, but not everyday operation. Wake-word detection, speech recognition, speech synthesis, weather and device actions still use Xiaomi services. GPT inference runs in the cloud, not on the speaker.

See the **[complete setup guide in Chinese](README.md)** for requirements, APK compilation, Xiaomi login/verification, official ChatGPT authorization, identity-checked installation, boot tests, upgrades and maintenance.

Validated on one LX04 running Android 8.1 / ARMv7 / firmware 2.99.99. This is not a universal firmware or rooting package. Existing root ADB is required for initial setup; normal operation runs as an ordinary app and disables ADB. Other firmware and models are unverified.

The repository contains source only: no accounts, OAuth records, Xiaomi tokens, passwords, signing keys, personal device identifiers, firmware or prebuilt APK. Use your own accounts and keep private files in `.secrets/`. Run `make test` before publishing changes; see [SECURITY.md](SECURITY.md).

Default license: Apache-2.0. Upstream MIT and GPL-3.0-only components retain their respective licenses. See [NOTICE](NOTICE) and [THIRD_PARTY.md](THIRD_PARTY.md). This is an independent community project, not an official Xiaomi or OpenAI product.
