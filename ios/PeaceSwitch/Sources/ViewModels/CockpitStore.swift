import Combine
import Foundation
import SwiftUI
import UIKit

enum ConnectionMode: Equatable {
    case onboarding
    case demo
    case connected(PairingRecord)

    var isDemo: Bool {
        if case .demo = self { return true }
        return false
    }

    var record: PairingRecord? {
        if case .connected(let rec) = self { return rec }
        return nil
    }
}

enum AppLanguage: String, CaseIterable, Identifiable {
    case zh = "zh"
    case en = "en"

    var id: String { rawValue }

    var label: String {
        switch self {
        case .zh: return "简体中文"
        case .en: return "English"
        }
    }
}

@MainActor
final class CockpitStore: ObservableObject {
    @Published private(set) var mode: ConnectionMode
    @Published private(set) var state: PeaceState = DemoStateEngine.initialState()
    @Published private(set) var isLoading: Bool = false
    @Published private(set) var isBusyAction: Bool = false
    @Published private(set) var isOffline: Bool = false
    @Published private(set) var lastSyncedAt: Date?
    @Published var errorMessage: String?
    @Published var toastMessage: String?
    @Published var language: AppLanguage {
        didSet {
            UserDefaults.standard.set(language.rawValue, forKey: Self.langDefaultsKey)
        }
    }

    private static let langDefaultsKey = "peace_switch_lang"
    private let apiClient = PeaceAPIClient()
    private var pollTask: Task<Void, Never>?
    private var toastTask: Task<Void, Never>?
    private var stateRevision: Int = 0
    private var consecutivePollFails: Int = 0

    init() {
        if let savedLang = UserDefaults.standard.string(forKey: Self.langDefaultsKey),
           let parsedLang = AppLanguage(rawValue: savedLang) {
            self.language = parsedLang
        } else {
            let preferred = Locale.preferredLanguages.first ?? "en"
            self.language = preferred.lowercased().hasPrefix("zh") ? .zh : .en
        }

        if let record = KeychainStore.load() {
            self.mode = .connected(record)
        } else {
            self.mode = .onboarding
        }
    }

    func tr(_ zh: String, _ en: String) -> String {
        language == .zh ? zh : en
    }

    func startPollingIfNeeded() {
        stopPolling()
        guard case .connected = mode else { return }
        pollTask = Task { [weak self] in
            guard let self else { return }
            await self.refresh(forceProbe: false, silent: false)
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(6))
                if Task.isCancelled { break }
                await self.refresh(forceProbe: false, silent: true)
            }
        }
    }

    func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    func enterDemoMode() {
        stopPolling()
        errorMessage = nil
        state = DemoStateEngine.initialState()
        mode = .demo
        hapticSuccess()
        showToast(tr("已进入互动演示模式（无需桌面端即可体验）", "Entered Interactive Demo Mode"))
    }

    func exitDemoToPair() {
        stopPolling()
        errorMessage = nil
        mode = .onboarding
    }

    func pair(rawInput: String, manualServerURL: String = "", manualCode: String = "") async {
        errorMessage = nil
        let resolved: PairingQRPayload?
        if let parsed = PairingQRParser.parse(rawInput) {
            resolved = parsed
        } else if let normURL = PairingQRParser.normalizeBaseURL(manualServerURL),
                  !manualCode.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            resolved = PairingQRPayload(
                serverURL: normURL,
                pairCode: manualCode.trimmingCharacters(in: .whitespacesAndNewlines)
            )
        } else {
            resolved = nil
        }

        guard let payload = resolved else {
            errorMessage = tr(
                "未能识别配对信息，请扫描电脑端二维码或填写正确的局域网/Tailscale 地址与配对码。",
                "Invalid pairing info. Scan the desktop QR code or enter a valid server URL and pair code."
            )
            hapticError()
            return
        }

        isLoading = true
        defer { isLoading = false }

        do {
            let deviceName = UIDevice.current.name
            let record = try await apiClient.redeem(
                serverURL: payload.serverURL,
                code: payload.pairCode,
                deviceName: deviceName
            )
            guard KeychainStore.save(record) else {
                errorMessage = tr(
                    "无法将设备令牌安全写入系统 Keychain，请检查设备解锁状态后重试。",
                    "Failed to save pairing token into iOS Keychain."
                )
                hapticError()
                return
            }
            if let fetched = try? await apiClient.fetchState(record: record, forceProbe: false) {
                state = fetched
                isOffline = false
                lastSyncedAt = Date()
            } else {
                // 配对令牌已生效，若初次拉取瞬断则直接进入工作台并触发重试横幅
                isOffline = true
            }
            stateRevision &+= 1
            mode = .connected(record)
            startPollingIfNeeded()
            hapticSuccess()
            showToast(tr("已成功配对 \(record.deviceName)", "Paired with workstation"))
        } catch {
            errorMessage = error.localizedDescription
            hapticError()
        }
    }

    func unpair() {
        stopPolling()
        KeychainStore.clear()
        errorMessage = nil
        isOffline = false
        consecutivePollFails = 0
        lastSyncedAt = nil
        stateRevision &+= 1
        state = DemoStateEngine.initialState()
        mode = .onboarding
        showToast(tr("已解除本地配对", "Unpaired from workstation"))
    }

    func refresh(forceProbe: Bool = false, silent: Bool = false) async {
        switch mode {
        case .onboarding:
            return
        case .demo:
            if forceProbe {
                isLoading = true
                try? await Task.sleep(for: .milliseconds(250))
                stateRevision &+= 1
                state = DemoStateEngine.apply(action: ActionRequest(action: "check_gate"), to: state)
                lastSyncedAt = Date()
                isLoading = false
                hapticTap()
                showToast(tr("演示出口测速完成", "Demo egress probe finished"))
            }
        case .connected(let record):
            if silent && isBusyAction { return }
            let startRevision = stateRevision
            if !silent { isLoading = true }
            defer {
                if !silent { isLoading = false }
            }
            do {
                let latest = try await apiClient.fetchState(record: record, forceProbe: forceProbe)
                // 防止过期轮询覆盖刚刚触发的操作响应
                if silent && startRevision != stateRevision { return }
                state = latest
                isOffline = false
                consecutivePollFails = 0
                lastSyncedAt = Date()
                if !silent { errorMessage = nil }
                if forceProbe {
                    hapticTap()
                    showToast(tr("出口状态已刷新", "Egress state refreshed"))
                }
            } catch PeaceAPIError.unauthorized {
                stopPolling()
                KeychainStore.clear()
                isOffline = false
                mode = .onboarding
                errorMessage = tr(
                    "设备令牌已被电脑端撤销或失效，请重新扫码配对。",
                    "Device authorization expired or was revoked. Please re-pair."
                )
                hapticError()
            } catch {
                consecutivePollFails += 1
                if !silent || consecutivePollFails >= 2 {
                    isOffline = true
                }
                if !silent {
                    errorMessage = error.localizedDescription
                }
            }
        }
    }

    func setActive(profile: String) async {
        await runAction(
            ActionRequest(action: "set_active", profile: profile),
            toast: tr("已切换当前账号：\(profile)", "Switched active account: \(profile)")
        )
    }

    func launchDesktop(profile: String) async {
        await runAction(
            ActionRequest(action: "launch_desktop", profile: profile),
            toast: tr("正在唤醒 \(profile) 隔离桌面端", "Launched isolated Desktop: \(profile)")
        )
    }

    func stopDesktop(profile: String) async {
        await runAction(
            ActionRequest(action: "stop_desktop", profile: profile),
            toast: tr("已停止 \(profile) 桌面端进程", "Stopped Desktop process: \(profile)")
        )
    }

    func checkGate() async {
        await runAction(
            ActionRequest(action: "check_gate"),
            toast: tr("已完成静态出口复检", "Egress gate verified")
        )
    }

    func toggleGateLock() async {
        if state.gate.manualLock || !state.gate.open {
            await runAction(
                ActionRequest(action: "unlock_gate"),
                toast: tr("已恢复静默放行，直连流量继续受控", "Gate unlocked and egress opened")
            )
        } else {
            await runAction(
                ActionRequest(action: "lock_gate", reason: "Emergency lock from PeaceSwitch iOS"),
                toast: tr("已触发紧急熔断并断开受控长连接", "Emergency fail-closed lock engaged")
            )
        }
    }

    func toggleUILock() async {
        let target = !state.uiLocked
        await runAction(
            ActionRequest(action: "lock_ui", locked: target),
            toast: target
                ? tr("已锁定桌面端操作面板", "Desktop console UI locked")
                : tr("已解锁桌面端操作面板", "Desktop console UI unlocked")
        )
    }

    func togglePAC() async {
        let target = !state.browserPAC.matchesPeaceclaude
        await runAction(
            ActionRequest(action: "toggle_pac", enabled: target),
            toast: target
                ? tr("已开启本机浏览器域内 PAC 护航", "Browser domain-scoped PAC enabled")
                : tr("已暂停本机浏览器系统 PAC", "Browser system PAC paused")
        )
    }

    func clearError() {
        errorMessage = nil
    }

    private func runAction(_ payload: ActionRequest, toast: String) async {
        errorMessage = nil
        switch mode {
        case .onboarding:
            return
        case .demo:
            isBusyAction = true
            stateRevision &+= 1
            try? await Task.sleep(for: .milliseconds(180))
            state = DemoStateEngine.apply(action: payload, to: state)
            lastSyncedAt = Date()
            isBusyAction = false
            hapticTap()
            showToast(toast)
        case .connected(let record):
            isBusyAction = true
            stateRevision &+= 1
            defer { isBusyAction = false }
            do {
                let updated = try await apiClient.performAction(record: record, payload: payload)
                stateRevision &+= 1
                state = updated
                isOffline = false
                consecutivePollFails = 0
                lastSyncedAt = Date()
                hapticSuccess()
                showToast(toast)
            } catch PeaceAPIError.unauthorized {
                stopPolling()
                KeychainStore.clear()
                mode = .onboarding
                errorMessage = tr(
                    "设备令牌已被撤销或失效，请重新扫码配对。",
                    "Device authorization expired or was revoked. Please re-pair."
                )
                hapticError()
            } catch {
                errorMessage = error.localizedDescription
                hapticError()
            }
        }
    }

    private func showToast(_ text: String) {
        toastTask?.cancel()
        toastMessage = text
        toastTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(2.8))
            if !Task.isCancelled {
                self?.toastMessage = nil
            }
        }
    }

    private func hapticTap() {
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
    }

    private func hapticSuccess() {
        UINotificationFeedbackGenerator().notificationOccurred(.success)
    }

    private func hapticError() {
        UINotificationFeedbackGenerator().notificationOccurred(.error)
    }
}
