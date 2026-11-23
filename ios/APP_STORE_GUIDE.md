# PeaceSwitch iOS 上架与签名指南（App Store Guide）

**PeaceSwitch** 是 PeaceClaude 的原生 SwiftUI 移动端伴侣应用，零第三方 SDK、零 WebView 包装，包含完整的离线互动演示模式（Interactive Demo Mode），确保在无桌面宿主机环境下也可直接完成 App Store Connect 审核流程。

## 1. 生成与打开 Xcode 工程

```bash
cd ios/PeaceSwitch
xcodegen generate
open PeaceSwitch.xcodeproj
```

> 仓库已提交生成好的 `PeaceSwitch.xcodeproj`，未安装 `xcodegen` 时也可直接打开。

## 2. 签名与 Bundle Identifier 设置

1. 打开 `PeaceSwitch.xcodeproj`，选中 Target `PeaceSwitch` -> `Signing & Capabilities`。
2. 勾选 `Automatically manage signing`，选择你的 Apple Developer `Team`。
3. 将 `Bundle Identifier` 从默认的 `com.peaceclaude.switch` 调整为你名下已注册的唯一标识符（例如 `com.yourname.peaceswitch`）。
4. 同步调整 `PeaceSwitchTests` Target 的 `Team` 或测试签名设置。

## 3. 应用图标与隐私清单核查

- **AppIcon**：`Resources/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png` 已校验为 `1024x1024`、无 Alpha 通道（24-bit RGB）满幅构图，满足 App Store Connect 提交规范。
- **PrivacyInfo.xcprivacy**：已声明仅使用受限范围内的 `UserDefaults`（原因码 `CA92.1`：保存本地语言偏好），`NSPrivacyTracking = false`。
- **ATS 网络安全**：`Info.plist` 仅开启 `NSAllowsLocalNetworking = true`（支持局域网 / Tailscale 直连宿主机），未开放 `NSAllowsArbitraryLoads`。
- **加密合规**：`ITSAppUsesNonExemptEncryption = NO`（仅使用系统标准 TLS / HTTPS）。

## 4. 真机打包与上传 TestFlight / App Store

1. 将 Xcode Run Destination 切至 `Any iOS Device (arm64)`。
2. 菜单栏执行 `Product -> Archive`。
3. 在 Organizer 中点击 `Distribute App -> App Store Connect -> Upload`。

## 5. 提交 App Review 的「Notes for Reviewer」模板（可直接复制）

> **How to test PeaceSwitch without external desktop hardware:**
> 1. Launch PeaceSwitch. On the onboarding screen, tap **"Launch Interactive Demo Mode"** (bottom card).
> 2. You can immediately explore all 3 tabs: **Cockpit** (real-time egress monitor, **Emergency Lock / Unlock** fail-closed switch, **Re-Probe**, and **Browser PAC Shield** toggle), **Accounts** (isolated multi-account switching, Pro/Max quota bars vs Free account boundary, Desktop process toggle), and **Security** (Web Console lock toggle, EN/ZH language toggle).
> 3. Camera QR scanning and LAN/Tailscale pairing are optional companion workflows when connecting to an existing PeaceClaude desktop daemon (`v0.2.0+`). No account login, third-party tracking SDK, or paid subscription inside the iOS app is required.
