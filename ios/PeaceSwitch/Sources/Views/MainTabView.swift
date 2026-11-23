import SwiftUI

struct MainTabView: View {
    @EnvironmentObject private var store: CockpitStore
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        TabView {
            CockpitDashboardView()
                .tabItem {
                    Label(store.tr("工作台", "Cockpit"), systemImage: "shield.checkered")
                }

            AccountsListView()
                .tabItem {
                    Label(store.tr("多账号", "Accounts"), systemImage: "person.2.badge.key")
                }

            SettingsConnectionView()
                .tabItem {
                    Label(store.tr("连接与安全", "Security"), systemImage: "slider.horizontal.3")
                }
        }
        .tint(PeaceTheme.vermilion)
        .safeAreaInset(edge: .top, spacing: 0) {
            VStack(spacing: 6) {
                if store.mode.isDemo {
                    HStack(spacing: 8) {
                        Image(systemName: "sparkles")
                            .font(.caption.weight(.bold))
                        Text(store.tr("互动演示模式 · 状态修改仅在本地沙盒生效",
                                      "Interactive Demo Mode · Local sandbox state"))
                            .font(.caption.weight(.medium))
                            .lineLimit(1)
                        Spacer()
                        Button(store.tr("去配对", "Pair PC")) {
                            store.exitDemoToPair()
                        }
                        .font(.caption.weight(.bold))
                        .padding(.horizontal, 10)
                        .padding(.vertical, 4)
                        .background(.white.opacity(0.22), in: Capsule())
                    }
                    .foregroundStyle(.white)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .background(PeaceTheme.navy)
                }

                if let toast = store.toastMessage {
                    HStack(spacing: 8) {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(PeaceTheme.emerald)
                        Text(toast)
                            .font(.caption.weight(.medium))
                            .foregroundStyle(PeaceTheme.ink)
                        Spacer()
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .background(PeaceTheme.surface)
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                    .overlay(
                        RoundedRectangle(cornerRadius: 12, style: .continuous)
                            .stroke(PeaceTheme.emerald.opacity(0.4), lineWidth: 1)
                    )
                    .shadow(color: .black.opacity(0.08), radius: 8, y: 3)
                    .padding(.horizontal, 14)
                    .padding(.top, 4)
                    .transition(.move(edge: .top).combined(with: .opacity))
                }
            }
            .animation(.spring(response: 0.3, dampingFraction: 0.85), value: store.toastMessage)
        }
        .onAppear {
            store.startPollingIfNeeded()
        }
        .onDisappear {
            store.stopPolling()
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active {
                store.startPollingIfNeeded()
            } else if phase == .background {
                store.stopPolling()
            }
        }
    }
}
