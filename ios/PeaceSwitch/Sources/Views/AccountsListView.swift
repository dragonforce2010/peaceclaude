import SwiftUI

struct AccountsListView: View {
    @EnvironmentObject private var store: CockpitStore

    private var sortedAccounts: [AccountProfile] {
        store.state.accounts.sorted { lhs, rhs in
            if lhs.active != rhs.active { return lhs.active }
            return lhs.name < rhs.name
        }
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 14) {
                    if sortedAccounts.isEmpty {
                        VStack(spacing: 10) {
                            Image(systemName: "person.crop.circle.badge.questionmark")
                                .font(.system(size: 38))
                                .foregroundStyle(PeaceTheme.inkMuted)
                            Text(store.tr("暂无隔离账号档案", "No isolated profiles found"))
                                .font(.headline)
                                .foregroundStyle(PeaceTheme.ink)
                        }
                        .frame(maxWidth: .infinity)
                        .padding(32)
                        .atelierCard()
                    } else {
                        ForEach(sortedAccounts) { account in
                            accountCard(account)
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
            }
            .background(PeaceTheme.paper.ignoresSafeArea())
            .refreshable {
                await store.refresh(forceProbe: false)
            }
            .navigationTitle(store.tr("隔离多账号", "Isolated Accounts"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        Task { await store.refresh(forceProbe: false) }
                    } label: {
                        Image(systemName: "arrow.clockwise")
                    }
                }
            }
        }
    }

    private func accountCard(_ account: AccountProfile) -> some View {
        let isRunning = !account.runningDesktopPID.isEmpty
        let title = account.displayName.isEmpty ? account.name : account.displayName
        let border = account.active ? PeaceTheme.vermilion : PeaceTheme.border

        return VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 12) {
                // 固定尺寸圆形头像防挤压右侧标签与按钮
                ZStack {
                    Circle()
                        .fill(account.active ? PeaceTheme.vermilion.opacity(0.16) : PeaceTheme.surfaceMuted)
                        .frame(width: 44, height: 44)
                    Text(String(title.prefix(1)).uppercased())
                        .font(.headline.weight(.bold))
                        .foregroundStyle(account.active ? PeaceTheme.vermilion : PeaceTheme.ink)
                }
                .frame(width: 44, height: 44)

                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text(title)
                            .font(.headline)
                            .foregroundStyle(PeaceTheme.ink)
                            .lineLimit(1)
                            .truncationMode(.tail)

                        if !account.planLabel.isEmpty {
                            Text(account.planLabel)
                                .font(.caption2.weight(.semibold))
                                .padding(.horizontal, 7)
                                .padding(.vertical, 2)
                                .background(PeaceTheme.surfaceMuted, in: Capsule())
                                .foregroundStyle(PeaceTheme.inkMuted)
                        }
                    }

                    Text(account.email.isEmpty ? account.name : account.email)
                        .font(.caption)
                        .foregroundStyle(PeaceTheme.inkMuted)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }

                Spacer(minLength: 6)

                if account.active {
                    Text(store.tr("当前使用", "ACTIVE"))
                        .font(.caption2.weight(.bold))
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(PeaceTheme.vermilion.opacity(0.14), in: Capsule())
                        .foregroundStyle(PeaceTheme.vermilion)
                }
            }

            if account.usage.hasPaidQuota {
                VStack(spacing: 8) {
                    if let fiveH = account.usage.fiveHour {
                        quotaMeter(
                            label: "5h Window",
                            utilization: fiveH.utilization,
                            resetText: fiveH.resetInText
                        )
                    }
                    if let weekly = account.usage.weekly {
                        quotaMeter(
                            label: "7d Weekly",
                            utilization: weekly.utilization,
                            resetText: weekly.resetInText
                        )
                    }
                }
                .padding(11)
                .background(PeaceTheme.surfaceMuted.opacity(0.65))
                .clipShape(RoundedRectangle(cornerRadius: 11, style: .continuous))
            } else {
                HStack(spacing: 8) {
                    Image(systemName: "info.circle")
                        .font(.caption)
                        .foregroundStyle(PeaceTheme.inkMuted)
                    Text(store.tr("免费账号 (Free) 无 5h / Weekly 付费配额",
                                  "Free plan has no 5h / Weekly paid quota buckets"))
                        .font(.caption)
                        .foregroundStyle(PeaceTheme.inkMuted)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(10)
                .background(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .strokeBorder(PeaceTheme.border, style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
                )
            }

            HStack(spacing: 10) {
                Button {
                    Task { await store.setActive(profile: account.name) }
                } label: {
                    Text(account.active
                         ? store.tr("当前已选定", "Selected")
                         : store.tr("设为当前使用", "Set Active"))
                        .font(.caption.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 9)
                        .background(account.active ? PeaceTheme.surfaceMuted : PeaceTheme.vermilion)
                        .foregroundStyle(account.active ? PeaceTheme.inkMuted : .white)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                }
                .disabled(account.active || store.isBusyAction)

                if isRunning {
                    Button {
                        Task { await store.stopDesktop(profile: account.name) }
                    } label: {
                        HStack(spacing: 5) {
                            Image(systemName: "stop.circle.fill")
                            Text(store.tr("停止 (PID \(account.runningDesktopPID))",
                                          "Stop (\(account.runningDesktopPID))"))
                                .monospacedDigit()
                        }
                        .font(.caption.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 9)
                        .background(PeaceTheme.crimson.opacity(0.12))
                        .foregroundStyle(PeaceTheme.crimson)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                    .disabled(store.isBusyAction)
                } else {
                    Button {
                        Task { await store.launchDesktop(profile: account.name) }
                    } label: {
                        HStack(spacing: 5) {
                            Image(systemName: "play.fill")
                            Text(store.tr("启动 Desktop", "Launch Desktop"))
                        }
                        .font(.caption.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 9)
                        .background(PeaceTheme.surfaceMuted)
                        .foregroundStyle(PeaceTheme.ink)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                    .disabled(store.isBusyAction)
                }
            }
        }
        .atelierCard(borderColor: border)
    }

    private func quotaMeter(label: String, utilization: Double, resetText: String?) -> some View {
        let clamped = min(max(utilization, 0), 100)
        let barColor: Color = clamped >= 85 ? PeaceTheme.crimson : (clamped >= 65 ? PeaceTheme.amber : PeaceTheme.vermilion)
        return VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(label)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(PeaceTheme.ink)
                if let resetText, !resetText.isEmpty {
                    Text("· \(resetText)")
                        .font(.caption2)
                        .foregroundStyle(PeaceTheme.inkMuted)
                }
                Spacer()
                Text("\(Int(clamped.rounded()))%")
                    .font(.caption.weight(.bold).monospacedDigit())
                    .foregroundStyle(barColor)
            }
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule()
                        .fill(PeaceTheme.border)
                        .frame(height: 6)
                    Capsule()
                        .fill(barColor)
                        .frame(width: max(4, geo.size.width * (clamped / 100.0)), height: 6)
                }
            }
            .frame(height: 6)
        }
    }
}
