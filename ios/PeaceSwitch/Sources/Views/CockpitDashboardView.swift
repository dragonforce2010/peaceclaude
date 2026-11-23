import SwiftUI

struct CockpitDashboardView: View {
    @EnvironmentObject private var store: CockpitStore

    private var gateOpen: Bool {
        store.state.gate.open && !store.state.gate.manualLock
    }

    private var probe: ProbeResult? {
        store.state.gate.lastProbe ?? store.state.gate.lastGood
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 14) {
                    if let err = store.errorMessage {
                        errorBanner(err)
                    }

                    gatePulseCard

                    emergencyControlsCard

                    pacShieldCard

                    auditGridCard
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
            }
            .background(PeaceTheme.paper.ignoresSafeArea())
            .refreshable {
                await store.refresh(forceProbe: true)
            }
            .navigationTitle(store.tr("PeaceSwitch 工作台", "PeaceSwitch Cockpit"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    HStack(spacing: 6) {
                        Text(platformLabel(store.state.platform))
                            .font(.caption2.weight(.semibold))
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(PeaceTheme.surfaceMuted, in: Capsule())
                            .foregroundStyle(PeaceTheme.ink)
                        doctorPill
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        Task { await store.refresh(forceProbe: true) }
                    } label: {
                        if store.isLoading {
                            ProgressView().controlSize(.small)
                        } else {
                            Image(systemName: "arrow.clockwise")
                        }
                    }
                }
            }
        }
    }

    private var doctorPill: some View {
        let score = store.state.doctorSummary.score
        let color: Color = score >= 90 ? PeaceTheme.emerald : (score >= 70 ? PeaceTheme.amber : PeaceTheme.crimson)
        return Text("Doctor \(score)")
            .font(.caption2.weight(.bold).monospacedDigit())
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(color.opacity(0.14), in: Capsule())
            .foregroundStyle(color)
    }

    private var gatePulseCard: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .center) {
                HStack(spacing: 8) {
                    Circle()
                        .fill(gateOpen ? PeaceTheme.emerald : PeaceTheme.crimson)
                        .frame(width: 11, height: 11)
                    Text(gateOpen
                         ? store.tr("静态出口已放行", "Egress Gate Open")
                         : store.tr("Fail-Closed 熔断锁止", "Fail-Closed Locked"))
                        .font(.headline)
                        .foregroundStyle(gateOpen ? PeaceTheme.emerald : PeaceTheme.crimson)
                }
                Spacer()
                if let ms = probe?.latencyMS, ms > 0 {
                    Text("\(ms) ms")
                        .font(.caption.weight(.semibold).monospacedDigit())
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(PeaceTheme.surfaceMuted, in: Capsule())
                        .foregroundStyle(PeaceTheme.inkMuted)
                }
            }

            if !gateOpen && !store.state.gate.lockReason.isEmpty {
                Text(store.state.gate.lockReason)
                    .font(.caption)
                    .foregroundStyle(PeaceTheme.crimson)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(PeaceTheme.crimson.opacity(0.08))
                    .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            }

            VStack(alignment: .leading, spacing: 4) {
                Text(store.tr("当前静态出口 IP", "Verified Static Egress IP"))
                    .font(.caption)
                    .foregroundStyle(PeaceTheme.inkMuted)
                Text((probe?.egressIP.isEmpty == false) ? (probe?.egressIP ?? "--") : "--")
                    .font(.system(size: 26, weight: .bold, design: .monospaced))
                    .foregroundStyle(PeaceTheme.ink)
            }

            HStack(spacing: 12) {
                infoTag(
                    icon: "globe.americas.fill",
                    text: locationText(probe)
                )
                if let asn = probe?.asn, asn > 0 {
                    infoTag(icon: "number", text: "AS\(asn)")
                }
            }

            Divider()

            HStack(spacing: 14) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(store.tr("宿主时区与语言环境", "Timezone & Locale"))
                        .font(.caption2)
                        .foregroundStyle(PeaceTheme.inkMuted)
                    Text("\(store.state.gate.effectiveTimezone.ifEmpty("--")) · \(store.state.gate.effectiveLanguage.ifEmpty("en-US"))")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(PeaceTheme.ink)
                }
                Spacer()
                VStack(alignment: .trailing, spacing: 2) {
                    Text(store.tr("上游物理路由", "Upstream Route"))
                        .font(.caption2)
                        .foregroundStyle(PeaceTheme.inkMuted)
                    Text(upstreamLabel(store.state.upstreamRoute))
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(store.state.upstreamRoute.boundPhysical ? PeaceTheme.emerald : PeaceTheme.ink)
                }
            }
        }
        .atelierCard(borderColor: gateOpen ? PeaceTheme.emerald.opacity(0.45) : PeaceTheme.crimson.opacity(0.55))
    }

    private var emergencyControlsCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(store.tr("实时闸门主控", "Master Switch"))
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(PeaceTheme.ink)

            HStack(spacing: 10) {
                Button {
                    Task { await store.toggleGateLock() }
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: gateOpen ? "hand.raised.slash.fill" : "lock.open.fill")
                        Text(gateOpen
                             ? store.tr("紧急熔断断网", "Emergency Lock")
                             : store.tr("恢复静默放行", "Unlock Gate"))
                            .fontWeight(.semibold)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 13)
                    .background(gateOpen ? PeaceTheme.crimson : PeaceTheme.emerald)
                    .foregroundStyle(.white)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                }
                .disabled(store.isBusyAction)
                .accessibilityLabel(gateOpen ? store.tr("紧急熔断断网", "Emergency Lock") : store.tr("恢复静默放行", "Unlock Gate"))
                .accessibilityHint(store.tr("切换静态 IP 门禁 Fail-Closed 状态", "Toggles fail-closed egress lock on your paired workstation"))

                Button {
                    Task { await store.checkGate() }
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "waveform.path.ecg")
                        Text(store.tr("重检出口", "Re-Probe"))
                            .fontWeight(.semibold)
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 13)
                    .background(PeaceTheme.surfaceMuted)
                    .foregroundStyle(PeaceTheme.ink)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                }
                .disabled(store.isBusyAction)
                .accessibilityLabel(store.tr("重新检测静态出口 IP", "Re-Probe Static Egress"))
            }
        }
        .atelierCard()
    }

    private var pacShieldCard: some View {
        let pacActive = store.state.browserPAC.matchesPeaceclaude
        return HStack(spacing: 14) {
            ZStack {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill((pacActive ? PeaceTheme.emerald : PeaceTheme.inkMuted).opacity(0.14))
                    .frame(width: 44, height: 44)
                Image(systemName: "safari")
                    .font(.system(size: 20, weight: .semibold))
                    .foregroundStyle(pacActive ? PeaceTheme.emerald : PeaceTheme.inkMuted)
            }

            VStack(alignment: .leading, spacing: 3) {
                Text(store.tr("本机浏览器 PAC 护航", "System Browser PAC Shield"))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(PeaceTheme.ink)
                Text(store.state.browserPAC.note.isEmpty
                     ? store.tr("仅代理 Claude / Anthropic 相关域名，其余网站直连",
                                "Proxies Claude domains only; general traffic stays direct")
                     : store.state.browserPAC.note)
                    .font(.caption)
                    .foregroundStyle(PeaceTheme.inkMuted)
                    .lineLimit(2)
            }

            Spacer()

            Toggle("", isOn: Binding(
                get: { pacActive },
                set: { _ in Task { await store.togglePAC() } }
            ))
            .labelsHidden()
            .tint(PeaceTheme.emerald)
            .disabled(store.isBusyAction)
            .accessibilityLabel(store.tr("本机浏览器 PAC 护航开关", "System Browser PAC Shield Toggle"))
        }
        .atelierCard()
    }

    private var auditGridCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(store.tr("实时防泄漏审计", "Leak & Audit Telemetry"))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(PeaceTheme.ink)
                Spacer()
                Text(store.tr("活跃连接 \(store.state.gate.activeStreams)",
                              "\(store.state.gate.activeStreams) active streams"))
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(PeaceTheme.inkMuted)
            }

            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10)], spacing: 10) {
                statTile(
                    title: store.tr("受控放行请求", "Proxied Requests"),
                    value: "\(store.state.stats.proxiedRequests)",
                    tint: PeaceTheme.ink
                )
                statTile(
                    title: store.tr("Fail-Closed 拦截", "Fail-Closed Blocked"),
                    value: "\(store.state.stats.blockedFailclosed)",
                    tint: PeaceTheme.amber
                )
                statTile(
                    title: store.tr("Telemetry 阻断", "Telemetry Blocked"),
                    value: "\(store.state.stats.blockedTelemetry)",
                    tint: PeaceTheme.vermilion
                )
                statTile(
                    title: store.tr("直连泄漏事件", "Leak Events"),
                    value: "\(store.state.stats.leakCount)",
                    tint: store.state.stats.leakCount == 0 ? PeaceTheme.emerald : PeaceTheme.crimson
                )
            }
        }
        .atelierCard()
    }

    private func statTile(title: String, value: String, tint: Color) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(.caption)
                .foregroundStyle(PeaceTheme.inkMuted)
            Text(value)
                .font(.title3.weight(.bold).monospacedDigit())
                .foregroundStyle(tint)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(PeaceTheme.surfaceMuted.opacity(0.7))
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private func infoTag(icon: String, text: String) -> some View {
        Label(text, systemImage: icon)
            .font(.caption)
            .foregroundStyle(PeaceTheme.inkMuted)
            .lineLimit(1)
    }

    private func errorBanner(_ message: String) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "wifi.exclamationmark")
                .foregroundStyle(PeaceTheme.crimson)
            Text(message)
                .font(.caption)
                .foregroundStyle(PeaceTheme.ink)
            Spacer()
            Button { store.clearError() } label: {
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(PeaceTheme.inkMuted)
            }
        }
        .padding(12)
        .background(PeaceTheme.crimson.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private func platformLabel(_ platform: String) -> String {
        switch platform.lowercased() {
        case "darwin": return "macOS"
        case "windows": return "Windows"
        case "linux": return "Ubuntu Linux"
        default: return platform.isEmpty ? "Host" : platform
        }
    }

    private func locationText(_ p: ProbeResult?) -> String {
        guard let p else { return "--" }
        let parts = [p.country, p.city].filter { !$0.isEmpty }
        return parts.isEmpty ? "--" : parts.joined(separator: " · ")
    }

    private func upstreamLabel(_ r: UpstreamRoute) -> String {
        if r.boundPhysical {
            let iface = r.activeInterface ?? r.configuredInterface ?? "en0"
            return store.tr("\(iface) 物理网卡直出", "\(iface) Physical Bypass")
        }
        return r.activeInterface ?? store.tr("系统默认路由", "System Default")
    }
}

private extension String {
    func ifEmpty(_ fallback: String) -> String {
        isEmpty ? fallback : self
    }
}
