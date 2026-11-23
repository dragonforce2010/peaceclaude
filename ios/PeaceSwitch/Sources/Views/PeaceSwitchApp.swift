import SwiftUI

@main
struct PeaceSwitchApp: App {
    @StateObject private var store = CockpitStore()

    var body: some Scene {
        WindowGroup {
            Group {
                switch store.mode {
                case .onboarding:
                    OnboardingPairView()
                        .transition(.opacity)
                case .demo, .connected:
                    MainTabView()
                        .transition(.opacity)
                }
            }
            .environmentObject(store)
            .animation(.easeInOut(duration: 0.22), value: store.mode)
        }
    }
}
