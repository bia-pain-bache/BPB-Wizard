# 🎉 BPB Wizard Next Generation

Wizard has web and CLI editions now. Both editions deploy each BPB Panel with a unique hash to minimize Cloudflare errors. The web edition generates a `Private Link` after deployment which enables ONE-CLICK installations per Cloudflare account. However Wizard CLI stores your logins on your operating system and supports multiple accounts.

- Adapted BPB Panel next geneneration build flow.
- All deployments will have unique worker hash.
- Implemented script execution in order to prevent AVs False Positive. The script downloads the worker script on behalf of wizard now.
- Wizard Web fully rewrote in TS, using Cloudflare SDK.
- Wizard Web UI improvements.
- Sync CLI and Web terminal user experience.
- Fixed PowerShell version compatibility #76
- Fixes Termux DNS issues, now can be used with or without VPN #76 #75
- Fixed Fresh account missing workers.dev subdomain

> [!TIP]
> Wizard CLI version can only be used by scripts. Standalone execution is not permitted from now on. Windows users can only use `PowerShell` script to run the wizard.

## Web edition

To install the latest stable version of BPB Panel:

```url
https://wizard.bpb-panel.workers.dev
```

To install the latest version even if it's a pre-release:

```url
https://wizard.bpb-panel.workers.dev?pre-release=true
```

<br>

## CLI edition

### Android (Termux) - Linux - macOS

Make sure to install Termux from [Github source](https://github.com/termux/termux-app/releases/latest) not Google Play.

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/bia-pain-bache/BPB-Wizard/main/install.sh)
```

### Windows (PowerShell)

Windows users should use this script in PowerShell only.

```bash
irm https://raw.githubusercontent.com/bia-pain-bache/BPB-Wizard/main/install.ps1 | iex
```
