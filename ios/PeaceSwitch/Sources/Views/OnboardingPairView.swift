import SwiftUI

struct OnboardingPairView: View {
    @EnvironmentObject private var store: CockpitStore

    @State private var showScanner = false
    @State private var showManualSheet = false
    @State private var pastedPayload = ""
    @State private var serverAddress = ""
    @State private var pairCode = ""

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    headerCard

                    if let err = store.errorMessage {
                        HStack(alignment: .top, spacing: 10) {
                            Image(systemName: "exclamationmark.triangle.fill")
                                .foregroundStyle(PeaceTheme.crimson)
                            Text(err)
                                .font(.footnote)
                                .foregroundStyle(PeaceTheme.ink)
                            Spacer()
                            Button {
                                store.clearError()
                            } label: {
                                Image(systemName: "xmark")
                                    .font(.caption)
                                    .foregroundStyle(PeaceTheme.inkMuted)
                            }
                        }
                        .padding(14)
                        .background(PeaceTheme.crimson.opacity(0.08))
                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                        .overlay(
                            RoundedRectangle(cornerRadius: 12, style: .continuous)
                                .stroke(PeaceTheme.crimson.opacity(0.35), lineWidth: 1)
                        )
                    }

                    scanCard

                    manualCard

                    demoCard
                }
                .padding(.horizontal, 18)
                .padding(.vertical, 20)
            }
            .background(PeaceTheme.paper.ignoresSafeArea())
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        store.language = (store.language == .zh) ? .en : .zh
                    } label: {
                        Text(store.language == .zh ? "EN" : "中文")
                            .font(.caption.weight(.semibold))
                            .padding(.horizontal, 10)
                            .padding(.vertical, 5)
                            .background(PeaceTheme.surfaceMuted, in: Capsule())
                            .foregroundStyle(PeaceTheme.ink)
                    }
                }
            }
            .sheet(isPresented: $showScanner) {
                QRScannerView { scanned in
                    pastedPayload = scanned
                    Task {
                        await store.pair(rawInput: scanned)
                    }
                }
            }
        }
    }

    private var headerCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                ZStack {
                    RoundedRectangle(cornerRadius: 14, style: .continuous)
                        .fill(PeaceTheme.navy)
                        .frame(width: 52, height: 52)
                    Image(systemName: "shield.lefthalf.filled")
                        .font(.system(size: 26, weight: .semibold))
                        .foregroundStyle(PeaceTheme.vermilion)
                }
                VStack(alignment: .leading, spacing: 3) {
                    Text("PeaceSwitch")
                        .font(.title2.weight(.bold))
                        .foregroundStyle(PeaceTheme.ink)
                    Text(store.tr("PeaceClaude 静态出口移动端伴侣控制台",
                                  "Static-IP Companion Console for PeaceClaude"))
                        .font(.subheadline)
                        .foregroundStyle(PeaceTheme.inkMuted)
                }
            }

            Text(store.tr(
                "通过同一局域网、Tailscale 或自建安全中继，远程监看桌面端静态出口健康度、切换隔离账号或一键触发 Fail-Closed 紧急熔断。",
                "Monitor egress health, switch isolated accounts, and trigger fail-closed emergency lock from your iPhone over LAN, Tailscale, or your private relay."
            ))
            .font(.footnote)
            .foregroundStyle(PeaceTheme.inkMuted)
            .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.top, 4)
    }

    private var scanCard: some View {
        VStack(alignment: .leading, spacing: 14) {
            Label(
                store.tr("1. 扫描电脑端配对二维码", "1. Scan Desktop Pairing QR"),
                systemImage: "qrcode.viewfinder"
            )
            .font(.headline)
            .foregroundStyle(PeaceTheme.ink)

            Text(store.tr(
                "打开电脑版 PeaceClaude 控制台 ->「远程配对」生成一次性的 120 秒配对码，再用手机摄像头扫描。",
                "Open PeaceClaude Desktop -> Remote Pairing to generate a single-use 120s QR code."
            ))
            .font(.footnote)
            .foregroundStyle(PeaceTheme.inkMuted)

            Button {
                showScanner = true
            } label: {
                HStack {
                    Image(systemName: "camera.viewfinder")
                    Text(store.tr("立即扫码配对", "Scan QR Code"))
                        .fontWeight(.semibold)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 13)
                .background(PeaceTheme.vermilion)
                .foregroundStyle(.white)
                .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            }
            .disabled(store.isLoading)
        }
        .atelierCard()
    }

    private var manualCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    showManualSheet.toggle()
                }
            } label: {
                HStack {
                    Label(
                        store.tr("2. 手动粘贴链接或局域网地址", "2. Manual URL & Pair Code"),
                        systemImage: "link"
                    )
                    .font(.headline)
                    .foregroundStyle(PeaceTheme.ink)
                    Spacer()
                    Image(systemName: showManualSheet ? "chevron.up" : "chevron.down")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(PeaceTheme.inkMuted)
                }
            }

            if showManualSheet {
                VStack(alignment: .leading, spacing: 10) {
                    Text(store.tr("直接粘贴二维码内容（如 http://100.x.x.x:18900/mobile#pair=PEACE-...）",
                                  "Paste pairing URL or code payload"))
                        .font(.caption)
                        .foregroundStyle(PeaceTheme.inkMuted)

                    TextField("http://192.168.1.10:18900/mobile#pair=PEACE-...", text: $pastedPayload)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.system(.footnote, design: .monospaced))
                        .padding(10)
                        .background(PeaceTheme.surfaceMuted)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))

                    Divider().padding(.vertical, 2)

                    Text(store.tr("或分别填写电脑端地址与一次性配对码：", "Or enter server host and pair code separately:"))
                        .font(.caption)
                        .foregroundStyle(PeaceTheme.inkMuted)

                    TextField("http://192.168.1.10:18900", text: $serverAddress)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .font(.system(.footnote, design: .monospaced))
                        .padding(10)
                        .background(PeaceTheme.surfaceMuted)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))

                    TextField("PEACE-...", text: $pairCode)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.system(.footnote, design: .monospaced))
                        .padding(10)
                        .background(PeaceTheme.surfaceMuted)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))

                    Button {
                        Task {
                            await store.pair(
                                rawInput: pastedPayload,
                                manualServerURL: serverAddress,
                                manualCode: pairCode
                            )
                        }
                    } label: {
                        HStack {
                            if store.isLoading {
                                ProgressView().tint(PeaceTheme.ink)
                            }
                            Text(store.tr("验证并配对设备", "Verify & Pair Device"))
                                .fontWeight(.semibold)
                        }
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 11)
                        .background(PeaceTheme.surfaceMuted)
                        .foregroundStyle(PeaceTheme.ink)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                    .disabled(store.isLoading)
                }
            }
        }
        .atelierCard()
    }

    private var demoCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Label(
                    store.tr("免配对交互体验", "No Workstation Handy?"),
                    systemImage: "sparkles.rectangle.stack"
                )
                .font(.headline)
                .foregroundStyle(PeaceTheme.ink)
                Spacer()
                Text("DEMO")
                    .font(.caption2.weight(.bold))
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(PeaceTheme.amber.opacity(0.16), in: Capsule())
                    .foregroundStyle(PeaceTheme.amber)
            }

            Text(store.tr(
                "在没有运行桌面端网关时，也可以立即进入离线拟真沙盒，完整体验出口检测、紧急熔断、账号切换与 PAC 开关。",
                "Try every feature in an offline interactive sandbox—test emergency gate lock, profile switching, and quota meters without a running desktop."
            ))
            .font(.footnote)
            .foregroundStyle(PeaceTheme.inkMuted)

            Button {
                store.enterDemoMode()
            } label: {
                HStack {
                    Image(systemName: "play.circle.fill")
                    Text(store.tr("进入互动演示模式", "Launch Interactive Demo Mode"))
                        .fontWeight(.semibold)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 12)
                .background(PeaceTheme.navy)
                .foregroundStyle(.white)
                .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            }
        }
        .atelierCard(borderColor: PeaceTheme.navy.opacity(0.25))
    }
}
