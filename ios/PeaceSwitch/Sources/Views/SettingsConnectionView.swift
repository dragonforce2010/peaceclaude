import SwiftUI

struct SettingsConnectionView: View {
    @EnvironmentObject private var store: CockpitStore
    @State private var confirmUnpair = false

    var body: some View {
        NavigationStack {
            List {
                Section(store.tr("配对宿主机", "Paired Workstation")) {
                    LabeledContent(store.tr("连接模式", "Mode")) {
                        Text(modeText)
                            .foregroundStyle(store.mode.isDemo ? PeaceTheme.amber : PeaceTheme.emerald)
                    }
                    if let record = store.mode.record {
                        LabeledContent(store.tr("控制地址", "Endpoint")) {
                            Text(record.baseURL)
                                .font(.caption.monospaced())
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                    }
                    LabeledContent(store.tr("宿主机名", "Hostname")) {
                        Text(store.state.hostName.isEmpty ? "--" : store.state.hostName)
                    }
                    LabeledContent(store.tr("操作系统", "OS Platform")) {
                        Text(platformTitle(store.state.platform))
                    }
                    LabeledContent(store.tr("Daemon 版本", "Daemon Version")) {
                        Text(store.state.version.isEmpty ? "--" : "v\(store.state.version)")
                            .monospacedDigit()
                    }
                }

                Section(store.tr("安全与交互控制", "Security & Controls")) {
                    Toggle(isOn: Binding(
                        get: { store.state.uiLocked },
                        set: { _ in Task { await store.toggleUILock() } }
                    )) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(store.tr("锁定桌面端操作面板", "Lock Desktop Web UI"))
                            Text(store.tr("开启后本地 Web 控制台只读展示，防止误改账号或策略",
                                          "Sets local web console read-only to avoid accidental edits"))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .tint(PeaceTheme.vermilion)
                    .disabled(store.isBusyAction)

                    Picker(store.tr("界面语言 / Language", "Language"), selection: $store.language) {
                        ForEach(AppLanguage.allCases) { lang in
                            Text(lang.label).tag(lang)
                        }
                    }
                }

                Section(store.tr("连接管理", "Session")) {
                    if store.mode.isDemo {
                        Button {
                            store.exitDemoToPair()
                        } label: {
                            Label(store.tr("退出演示模式并配对电脑端", "Exit Demo & Pair Workstation"), systemImage: "qrcode.viewfinder")
                        }
                    } else {
                        Button(role: .destructive) {
                            confirmUnpair = true
                        } label: {
                            Label(store.tr("解除当前配对并清除本地凭证", "Unpair & Clear Keychain Token"), systemImage: "trash")
                        }
                    }
                }

                Section(store.tr("隐私与透明说明", "Privacy & Open Source")) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("PeaceSwitch · v0.2.0")
                            .font(.subheadline.weight(.semibold))
                        Text(store.tr(
                            "本应用不包含任何第三方分析统计、广告 SDK 或后端云采集服务。所有设备配对凭证仅保存在 iOS 系统 Keychain 中，并通过您控制的局域网、Tailscale 或自建安全中继与您的桌面端通信。",
                            "Zero third-party analytics, zero ads, and no cloud telemetry. Device bearer tokens are stored strictly in iOS Keychain and communicate only with the PeaceClaude workstation you pair."
                        ))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                    .padding(.vertical, 4)
                }
            }
            .navigationTitle(store.tr("连接与安全", "Security & Session"))
            .navigationBarTitleDisplayMode(.inline)
            .alert(store.tr("确认解除配对？", "Unpair this workstation?"), isPresented: $confirmUnpair) {
                Button(store.tr("取消", "Cancel"), role: .cancel) {}
                Button(store.tr("解除配对", "Unpair"), role: .destructive) {
                    store.unpair()
                }
            } message: {
                Text(store.tr("解除后将移除 iOS Keychain 中保存的访问令牌，下次需要重新扫码。",
                              "This removes the stored Keychain bearer token from your iPhone."))
            }
        }
    }

    private var modeText: String {
        switch store.mode {
        case .demo: return store.tr("离线互动演示", "Interactive Demo")
        case .connected: return store.tr("已直连受控主机", "Live Connected")
        case .onboarding: return "--"
        }
    }

    private func platformTitle(_ raw: String) -> String {
        switch raw.lowercased() {
        case "darwin": return "macOS"
        case "windows": return "Windows"
        case "linux": return "Ubuntu Linux"
        default: return raw.isEmpty ? "--" : raw
        }
    }
}
