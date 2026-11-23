import Foundation

/// Interactive offline simulation engine so App Store reviewers and new users
/// can test every PeaceSwitch workflow without pairing a live workstation.
struct DemoStateEngine {
    static func initialState() -> PeaceState {
        let nowISO = ISO8601DateFormatter().string(from: Date())
        let sampleProbe = ProbeResult(
            checkedAt: nowISO,
            ok: true,
            egressIP: "198.51.100.42",
            country: "United States",
            region: "Wyoming",
            city: "Cheyenne",
            timezone: "America/Denver",
            asn: 396982,
            isp: "Static Residential ISP (Demo)",
            latencyMS: 86,
            failureReason: ""
        )
        return PeaceState(
            version: "0.2.0",
            platform: "darwin",
            hostName: "studio-macbook.local (Demo)",
            activeProfile: "alpha-studio",
            uiLocked: false,
            gate: GateState(
                open: true,
                manualLock: false,
                lockReason: "",
                activeStreams: 3,
                effectiveTimezone: "America/Denver",
                effectiveLanguage: "en-US",
                effectiveLocale: "en_US",
                lastProbe: sampleProbe,
                lastGood: sampleProbe
            ),
            stats: TrafficStats(
                proxiedRequests: 482,
                blockedFailclosed: 2,
                blockedTelemetry: 19,
                leakCount: 0
            ),
            upstreamRoute: UpstreamRoute(
                bypassEnabled: true,
                configuredInterface: "en0",
                activeInterface: "en0",
                boundPhysical: true,
                usingFallback: false,
                fallbackReason: nil,
                skippedReason: nil
            ),
            browserPAC: BrowserPAC(
                pacURL: "http://127.0.0.1:18899/peaceclaude.pac",
                systemPACEnabled: true,
                currentPACURL: "http://127.0.0.1:18899/peaceclaude.pac",
                activeServices: ["Wi-Fi"],
                matchesPeaceclaude: true,
                note: "Browser PAC active for scoped Claude domains only."
            ),
            doctorSummary: DoctorSummary(
                score: 100,
                grade: "safe",
                criticalPassed: 11,
                criticalTotal: 11
            ),
            accounts: [
                AccountProfile(
                    name: "alpha-studio",
                    displayName: "Alpha Studio",
                    active: true,
                    activated: true,
                    runningDesktopPID: "41221",
                    planLabel: "Pro",
                    email: "alpha.demo@example.test",
                    accountAgeText: "Registered 11mo ago (isolated slot)",
                    usage: UsageData(
                        profile: "alpha-studio",
                        status: "ok",
                        source: "oauth_api",
                        subscriptionType: "Pro",
                        fetchedAt: nowISO,
                        fiveHour: UsageWindow(
                            key: "five_hour",
                            label: "5-Hour Window",
                            utilization: 38,
                            resetsAt: nil,
                            resetInText: "2h 40m"
                        ),
                        weekly: UsageWindow(
                            key: "weekly",
                            label: "Weekly Quota",
                            utilization: 54,
                            resetsAt: nil,
                            resetInText: "Thu 14:00"
                        )
                    )
                ),
                AccountProfile(
                    name: "beta-research",
                    displayName: "Beta Research",
                    active: false,
                    activated: true,
                    runningDesktopPID: "",
                    planLabel: "Max 5x",
                    email: "beta.demo@example.test",
                    accountAgeText: "Registered 4mo ago (isolated slot)",
                    usage: UsageData(
                        profile: "beta-research",
                        status: "ok",
                        source: "oauth_api",
                        subscriptionType: "Max 5x",
                        fetchedAt: nowISO,
                        fiveHour: UsageWindow(
                            key: "five_hour",
                            label: "5-Hour Window",
                            utilization: 16,
                            resetsAt: nil,
                            resetInText: "4h 12m"
                        ),
                        weekly: UsageWindow(
                            key: "weekly",
                            label: "Weekly Quota",
                            utilization: 29,
                            resetsAt: nil,
                            resetInText: "Mon 09:00"
                        )
                    )
                ),
                AccountProfile(
                    name: "gamma-free",
                    displayName: "Gamma Sandbox",
                    active: false,
                    activated: true,
                    runningDesktopPID: "",
                    planLabel: "Free",
                    email: "gamma.demo@example.test",
                    accountAgeText: "Free isolated profile",
                    usage: UsageData(
                        profile: "gamma-free",
                        status: "history",
                        source: "none",
                        subscriptionType: "Free",
                        fetchedAt: nowISO,
                        fiveHour: nil,
                        weekly: nil,
                        hint: "Free 账号不含 5h / Weekly 付费配额"
                    )
                )
            ]
        )
    }

    static func apply(action: ActionRequest, to state: PeaceState) -> PeaceState {
        var next = state
        switch action.action {
        case "set_active":
            if let profile = action.profile,
               next.accounts.contains(where: { $0.name == profile }) {
                next.activeProfile = profile
                for i in next.accounts.indices {
                    next.accounts[i].active = (next.accounts[i].name == profile)
                }
            }
        case "launch_desktop":
            if let profile = action.profile,
               let idx = next.accounts.firstIndex(where: { $0.name == profile }) {
                next.accounts[idx].runningDesktopPID = "48\(10 + idx)9"
                next.gate.activeStreams = max(1, next.gate.activeStreams + 1)
            }
        case "stop_desktop":
            if let profile = action.profile,
               let idx = next.accounts.firstIndex(where: { $0.name == profile }) {
                next.accounts[idx].runningDesktopPID = ""
            }
        case "check_gate":
            let delta = Int64.random(in: -8...12)
            let lat = max(48, (next.gate.lastProbe?.latencyMS ?? 86) + delta)
            let nowISO = ISO8601DateFormatter().string(from: Date())
            if !next.gate.manualLock {
                next.gate.open = true
                next.gate.lastProbe?.checkedAt = nowISO
                next.gate.lastProbe?.latencyMS = lat
                next.stats.proxiedRequests += 1
            }
        case "lock_gate":
            next.gate.manualLock = true
            next.gate.open = false
            next.gate.lockReason = action.reason ?? "Manual emergency lock from PeaceSwitch iOS"
            next.stats.blockedFailclosed += 1
        case "unlock_gate":
            next.gate.manualLock = false
            next.gate.open = true
            next.gate.lockReason = ""
            next.stats.proxiedRequests += 1
        case "lock_ui":
            if let explicit = action.locked {
                next.uiLocked = explicit
            } else {
                next.uiLocked.toggle()
            }
        case "toggle_pac":
            let nextEnabled = action.enabled ?? !next.browserPAC.systemPACEnabled
            next.browserPAC.systemPACEnabled = nextEnabled
            next.browserPAC.matchesPeaceclaude = nextEnabled
            next.browserPAC.currentPACURL = nextEnabled ? next.browserPAC.pacURL : ""
            next.browserPAC.note = nextEnabled
                ? "Browser PAC active for scoped Claude domains only."
                : "System PAC disabled in demo mode."
        default:
            break
        }
        return next
    }
}
