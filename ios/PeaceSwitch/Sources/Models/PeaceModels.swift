import Foundation

struct PairingRecord: Codable, Equatable, Sendable {
    var baseURL: String
    var bearerToken: String
    var deviceID: String
    var deviceName: String
    var pairedAt: Date
}

struct PairingQRPayload: Equatable, Sendable {
    let serverURL: String
    let pairCode: String
}

enum PairingQRParser {
    /// Parses QR strings emitted by PeaceClaude Desktop/Web Console:
    /// 1. `http://<host>:<port>/mobile#pair=PEACE-<secret>`
    /// 2. `PEACE1:{"r":"https://relay.example.com","c":"PEACE-<secret>"}`
    /// 3. `peaceclaude://pair?server=...&code=...` or `?code=...`
    /// 4. Raw JSON `{"server":"...","code":"..."}`
    static func parse(_ raw: String) -> PairingQRPayload? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }

        if trimmed.hasPrefix("PEACE1:") {
            let jsonPart = String(trimmed.dropFirst("PEACE1:".count))
            if let parsed = parseJSONPayload(jsonPart) {
                return parsed
            }
        }

        if trimmed.hasPrefix("{") {
            if let parsed = parseJSONPayload(trimmed) {
                return parsed
            }
        }

        guard let components = URLComponents(string: trimmed) else {
            return nil
        }

        if components.scheme == "peaceclaude" {
            let server = queryValue(components, names: ["server", "url", "r"])
            let code = queryValue(components, names: ["code", "pair", "c"])
                ?? fragmentPairCode(components.fragment)
            guard let server, let normServer = normalizeBaseURL(server),
                  let code, !code.isEmpty else {
                return nil
            }
            return PairingQRPayload(serverURL: normServer, pairCode: code)
        }

        guard let scheme = components.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              components.user == nil,
              components.password == nil,
              let host = components.host, !host.isEmpty else {
            return nil
        }

        let code = fragmentPairCode(components.fragment)
            ?? queryValue(components, names: ["pair", "code", "c"])
        guard let code, !code.isEmpty else {
            return nil
        }

        var origin = URLComponents()
        origin.scheme = scheme
        origin.host = host
        origin.port = components.port
        guard let originStr = origin.string, let normServer = normalizeBaseURL(originStr) else {
            return nil
        }
        return PairingQRPayload(serverURL: normServer, pairCode: code)
    }

    static func normalizeBaseURL(_ raw: String) -> String? {
        var s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !s.isEmpty else { return nil }
        if !s.contains("://") {
            s = "http://" + s
        }
        guard var comp = URLComponents(string: s),
              let scheme = comp.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              comp.user == nil,
              comp.password == nil,
              let host = comp.host, !host.isEmpty else {
            return nil
        }
        if let port = comp.port, (port <= 0 || port > 65535) {
            return nil
        }
        comp.fragment = nil
        comp.query = nil
        var path = comp.path
        if path.hasSuffix("/mobile") {
            path = String(path.dropLast("/mobile".count))
        }
        while path.hasSuffix("/") {
            path.removeLast()
        }
        comp.path = path
        return comp.string
    }

    /// Flags plaintext `http://` targeting non-private/non-Tailscale hosts so users can prefer HTTPS relays.
    static func isInsecurePublicHTTP(_ rawURL: String) -> Bool {
        guard let norm = normalizeBaseURL(rawURL),
              let comp = URLComponents(string: norm),
              comp.scheme?.lowercased() == "http",
              let host = comp.host?.lowercased() else {
            return false
        }
        if host == "localhost" || host == "127.0.0.1" || host == "::1" || host.hasSuffix(".local") {
            return false
        }
        let octets = host.split(separator: ".").compactMap { Int($0) }
        if octets.count == 4 {
            let (a, b) = (octets[0], octets[1])
            if a == 10 || a == 127 { return false }
            if a == 192 && b == 168 { return false }
            if a == 172 && (16...31).contains(b) { return false }
            if a == 100 && (64...127).contains(b) { return false } // Tailscale CGNAT 100.64.0.0/10
        }
        return true
    }

    private static func parseJSONPayload(_ jsonString: String) -> PairingQRPayload? {
        guard let data = jsonString.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return nil
        }
        let rawServer = (obj["r"] as? String) ?? (obj["server"] as? String) ?? (obj["url"] as? String)
        let rawCode = (obj["c"] as? String) ?? (obj["code"] as? String) ?? (obj["pair"] as? String)
        guard let rawServer, let normServer = normalizeBaseURL(rawServer),
              let rawCode else {
            return nil
        }
        let code = rawCode.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !code.isEmpty else { return nil }
        return PairingQRPayload(serverURL: normServer, pairCode: code)
    }

    private static func queryValue(_ comp: URLComponents, names: [String]) -> String? {
        guard let items = comp.queryItems else { return nil }
        for name in names {
            if let val = items.first(where: { $0.name == name })?.value?
                .trimmingCharacters(in: .whitespacesAndNewlines),
               !val.isEmpty {
                return val
            }
        }
        return nil
    }

    private static func fragmentPairCode(_ fragment: String?) -> String? {
        guard let fragment, !fragment.isEmpty else { return nil }
        for part in fragment.split(separator: "&") {
            let kv = part.split(separator: "=", maxSplits: 1).map(String.init)
            if kv.count == 2 && (kv[0] == "pair" || kv[0] == "code") {
                let value = kv[1].trimmingCharacters(in: .whitespacesAndNewlines)
                if !value.isEmpty {
                    return value
                }
            }
        }
        if fragment.hasPrefix("PEACE-") {
            return fragment
        }
        return nil
    }
}

struct RedeemRequest: Encodable {
    let code: String
    let deviceName: String
    let os: String

    enum CodingKeys: String, CodingKey {
        case code
        case deviceName = "device_name"
        case os
    }
}

struct RedeemResponse: Decodable {
    let ok: Bool
    let token: String?
    let device: PairedDeviceInfo?
    let error: String?
}

struct PairedDeviceInfo: Decodable, Equatable {
    let id: String
    let name: String
    let os: String
}

struct PeaceState: Codable, Equatable, Sendable {
    var version: String
    var platform: String
    var hostName: String
    var activeProfile: String
    var uiLocked: Bool
    var gate: GateState
    var stats: TrafficStats
    var upstreamRoute: UpstreamRoute
    var browserPAC: BrowserPAC
    var doctorSummary: DoctorSummary
    var accounts: [AccountProfile]

    enum CodingKeys: String, CodingKey {
        case version
        case platform
        case hostName = "host_name"
        case activeProfile = "active_profile"
        case uiLocked = "ui_locked"
        case gate
        case stats
        case upstreamRoute = "upstream_route"
        case browserPAC = "browser_pac"
        case doctorSummary = "doctor_summary"
        case accounts
    }

    init(
        version: String = "",
        platform: String = "",
        hostName: String = "",
        activeProfile: String = "",
        uiLocked: Bool = false,
        gate: GateState = GateState(),
        stats: TrafficStats = TrafficStats(),
        upstreamRoute: UpstreamRoute = UpstreamRoute(),
        browserPAC: BrowserPAC = BrowserPAC(),
        doctorSummary: DoctorSummary = DoctorSummary(),
        accounts: [AccountProfile] = []
    ) {
        self.version = version
        self.platform = platform
        self.hostName = hostName
        self.activeProfile = activeProfile
        self.uiLocked = uiLocked
        self.gate = gate
        self.stats = stats
        self.upstreamRoute = upstreamRoute
        self.browserPAC = browserPAC
        self.doctorSummary = doctorSummary
        self.accounts = accounts
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.decodeIfPresent(String.self, forKey: .version) ?? ""
        platform = try c.decodeIfPresent(String.self, forKey: .platform) ?? ""
        hostName = try c.decodeIfPresent(String.self, forKey: .hostName) ?? ""
        activeProfile = try c.decodeIfPresent(String.self, forKey: .activeProfile) ?? ""
        uiLocked = try c.decodeIfPresent(Bool.self, forKey: .uiLocked) ?? false
        gate = try c.decodeIfPresent(GateState.self, forKey: .gate) ?? GateState()
        stats = try c.decodeIfPresent(TrafficStats.self, forKey: .stats) ?? TrafficStats()
        upstreamRoute = try c.decodeIfPresent(UpstreamRoute.self, forKey: .upstreamRoute) ?? UpstreamRoute()
        browserPAC = try c.decodeIfPresent(BrowserPAC.self, forKey: .browserPAC) ?? BrowserPAC()
        doctorSummary = try c.decodeIfPresent(DoctorSummary.self, forKey: .doctorSummary) ?? DoctorSummary()
        accounts = try c.decodeIfPresent([AccountProfile].self, forKey: .accounts) ?? []
    }
}

struct GateState: Codable, Equatable, Sendable {
    var open: Bool
    var manualLock: Bool
    var lockReason: String
    var activeStreams: Int
    var effectiveTimezone: String
    var effectiveLanguage: String
    var effectiveLocale: String
    var lastProbe: ProbeResult?
    var lastGood: ProbeResult?

    enum CodingKeys: String, CodingKey {
        case open
        case manualLock = "manual_lock"
        case lockReason = "lock_reason"
        case activeStreams = "active_streams"
        case effectiveTimezone = "effective_timezone"
        case effectiveLanguage = "effective_language"
        case effectiveLocale = "effective_locale"
        case lastProbe = "last_probe"
        case lastGood = "last_good"
    }

    init(
        open: Bool = false,
        manualLock: Bool = false,
        lockReason: String = "",
        activeStreams: Int = 0,
        effectiveTimezone: String = "",
        effectiveLanguage: String = "",
        effectiveLocale: String = "",
        lastProbe: ProbeResult? = nil,
        lastGood: ProbeResult? = nil
    ) {
        self.open = open
        self.manualLock = manualLock
        self.lockReason = lockReason
        self.activeStreams = activeStreams
        self.effectiveTimezone = effectiveTimezone
        self.effectiveLanguage = effectiveLanguage
        self.effectiveLocale = effectiveLocale
        self.lastProbe = lastProbe
        self.lastGood = lastGood
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        open = try c.decodeIfPresent(Bool.self, forKey: .open) ?? false
        manualLock = try c.decodeIfPresent(Bool.self, forKey: .manualLock) ?? false
        lockReason = try c.decodeIfPresent(String.self, forKey: .lockReason) ?? ""
        activeStreams = try c.decodeIfPresent(Int.self, forKey: .activeStreams) ?? 0
        effectiveTimezone = try c.decodeIfPresent(String.self, forKey: .effectiveTimezone) ?? ""
        effectiveLanguage = try c.decodeIfPresent(String.self, forKey: .effectiveLanguage) ?? ""
        effectiveLocale = try c.decodeIfPresent(String.self, forKey: .effectiveLocale) ?? ""
        lastProbe = try c.decodeIfPresent(ProbeResult.self, forKey: .lastProbe)
        lastGood = try c.decodeIfPresent(ProbeResult.self, forKey: .lastGood)
    }
}

struct ProbeResult: Codable, Equatable, Sendable {
    var checkedAt: String?
    var ok: Bool
    var egressIP: String
    var country: String
    var region: String
    var city: String
    var timezone: String
    var asn: Int
    var isp: String
    var latencyMS: Int64
    var failureReason: String

    enum CodingKeys: String, CodingKey {
        case checkedAt = "checked_at"
        case ok
        case egressIP = "egress_ip"
        case country
        case region
        case city
        case timezone
        case asn
        case isp
        case latencyMS = "latency_ms"
        case failureReason = "failure_reason"
    }

    init(
        checkedAt: String? = nil,
        ok: Bool = false,
        egressIP: String = "",
        country: String = "",
        region: String = "",
        city: String = "",
        timezone: String = "",
        asn: Int = 0,
        isp: String = "",
        latencyMS: Int64 = 0,
        failureReason: String = ""
    ) {
        self.checkedAt = checkedAt
        self.ok = ok
        self.egressIP = egressIP
        self.country = country
        self.region = region
        self.city = city
        self.timezone = timezone
        self.asn = asn
        self.isp = isp
        self.latencyMS = latencyMS
        self.failureReason = failureReason
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        checkedAt = try c.decodeIfPresent(String.self, forKey: .checkedAt)
        ok = try c.decodeIfPresent(Bool.self, forKey: .ok) ?? false
        egressIP = try c.decodeIfPresent(String.self, forKey: .egressIP) ?? ""
        country = try c.decodeIfPresent(String.self, forKey: .country) ?? ""
        region = try c.decodeIfPresent(String.self, forKey: .region) ?? ""
        city = try c.decodeIfPresent(String.self, forKey: .city) ?? ""
        timezone = try c.decodeIfPresent(String.self, forKey: .timezone) ?? ""
        asn = try c.decodeIfPresent(Int.self, forKey: .asn) ?? 0
        isp = try c.decodeIfPresent(String.self, forKey: .isp) ?? ""
        latencyMS = try c.decodeIfPresent(Int64.self, forKey: .latencyMS) ?? 0
        failureReason = try c.decodeIfPresent(String.self, forKey: .failureReason) ?? ""
    }
}

struct TrafficStats: Codable, Equatable, Sendable {
    var proxiedRequests: Int
    var blockedFailclosed: Int
    var blockedTelemetry: Int
    var leakCount: Int

    enum CodingKeys: String, CodingKey {
        case proxiedRequests = "proxied_requests"
        case blockedFailclosed = "blocked_failclosed"
        case blockedTelemetry = "blocked_telemetry"
        case leakCount = "leak_count"
    }

    init(
        proxiedRequests: Int = 0,
        blockedFailclosed: Int = 0,
        blockedTelemetry: Int = 0,
        leakCount: Int = 0
    ) {
        self.proxiedRequests = proxiedRequests
        self.blockedFailclosed = blockedFailclosed
        self.blockedTelemetry = blockedTelemetry
        self.leakCount = leakCount
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        proxiedRequests = try c.decodeIfPresent(Int.self, forKey: .proxiedRequests) ?? 0
        blockedFailclosed = try c.decodeIfPresent(Int.self, forKey: .blockedFailclosed) ?? 0
        blockedTelemetry = try c.decodeIfPresent(Int.self, forKey: .blockedTelemetry) ?? 0
        leakCount = try c.decodeIfPresent(Int.self, forKey: .leakCount) ?? 0
    }
}

struct UpstreamRoute: Codable, Equatable, Sendable {
    var bypassEnabled: Bool
    var configuredInterface: String?
    var activeInterface: String?
    var boundPhysical: Bool
    var usingFallback: Bool?
    var fallbackReason: String?
    var skippedReason: String?

    enum CodingKeys: String, CodingKey {
        case bypassEnabled = "bypass_enabled"
        case configuredInterface = "configured_interface"
        case activeInterface = "active_interface"
        case boundPhysical = "bound_physical"
        case usingFallback = "using_fallback"
        case fallbackReason = "fallback_reason"
        case skippedReason = "skipped_reason"
    }

    init(
        bypassEnabled: Bool = false,
        configuredInterface: String? = nil,
        activeInterface: String? = nil,
        boundPhysical: Bool = false,
        usingFallback: Bool? = nil,
        fallbackReason: String? = nil,
        skippedReason: String? = nil
    ) {
        self.bypassEnabled = bypassEnabled
        self.configuredInterface = configuredInterface
        self.activeInterface = activeInterface
        self.boundPhysical = boundPhysical
        self.usingFallback = usingFallback
        self.fallbackReason = fallbackReason
        self.skippedReason = skippedReason
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        bypassEnabled = try c.decodeIfPresent(Bool.self, forKey: .bypassEnabled) ?? false
        configuredInterface = try c.decodeIfPresent(String.self, forKey: .configuredInterface)
        activeInterface = try c.decodeIfPresent(String.self, forKey: .activeInterface)
        boundPhysical = try c.decodeIfPresent(Bool.self, forKey: .boundPhysical) ?? false
        usingFallback = try c.decodeIfPresent(Bool.self, forKey: .usingFallback)
        fallbackReason = try c.decodeIfPresent(String.self, forKey: .fallbackReason)
        skippedReason = try c.decodeIfPresent(String.self, forKey: .skippedReason)
    }
}

struct BrowserPAC: Codable, Equatable, Sendable {
    var pacURL: String
    var systemPACEnabled: Bool
    var currentPACURL: String?
    var activeServices: [String]?
    var matchesPeaceclaude: Bool
    var note: String

    enum CodingKeys: String, CodingKey {
        case pacURL = "pac_url"
        case systemPACEnabled = "system_pac_enabled"
        case currentPACURL = "current_pac_url"
        case activeServices = "active_services"
        case matchesPeaceclaude = "matches_peaceclaude"
        case note
    }

    init(
        pacURL: String = "",
        systemPACEnabled: Bool = false,
        currentPACURL: String? = nil,
        activeServices: [String]? = nil,
        matchesPeaceclaude: Bool = false,
        note: String = ""
    ) {
        self.pacURL = pacURL
        self.systemPACEnabled = systemPACEnabled
        self.currentPACURL = currentPACURL
        self.activeServices = activeServices
        self.matchesPeaceclaude = matchesPeaceclaude
        self.note = note
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pacURL = try c.decodeIfPresent(String.self, forKey: .pacURL) ?? ""
        systemPACEnabled = try c.decodeIfPresent(Bool.self, forKey: .systemPACEnabled) ?? false
        currentPACURL = try c.decodeIfPresent(String.self, forKey: .currentPACURL)
        activeServices = try c.decodeIfPresent([String].self, forKey: .activeServices)
        matchesPeaceclaude = try c.decodeIfPresent(Bool.self, forKey: .matchesPeaceclaude) ?? false
        note = try c.decodeIfPresent(String.self, forKey: .note) ?? ""
    }
}

struct DoctorSummary: Codable, Equatable, Sendable {
    var score: Int
    var grade: String
    var criticalPassed: Int
    var criticalTotal: Int

    enum CodingKeys: String, CodingKey {
        case score
        case grade
        case criticalPassed = "critical_passed"
        case criticalTotal = "critical_total"
    }

    init(
        score: Int = 0,
        grade: String = "safe",
        criticalPassed: Int = 0,
        criticalTotal: Int = 0
    ) {
        self.score = score
        self.grade = grade
        self.criticalPassed = criticalPassed
        self.criticalTotal = criticalTotal
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        score = try c.decodeIfPresent(Int.self, forKey: .score) ?? 0
        grade = try c.decodeIfPresent(String.self, forKey: .grade) ?? "safe"
        criticalPassed = try c.decodeIfPresent(Int.self, forKey: .criticalPassed) ?? 0
        criticalTotal = try c.decodeIfPresent(Int.self, forKey: .criticalTotal) ?? 0
    }
}

struct AccountProfile: Codable, Equatable, Identifiable, Sendable {
    var id: String { name }
    var name: String
    var displayName: String
    var active: Bool
    var activated: Bool
    var runningDesktopPID: String
    var planLabel: String
    var email: String
    var accountAgeText: String
    var usage: UsageData

    enum CodingKeys: String, CodingKey {
        case name
        case displayName = "display_name"
        case active
        case activated
        case runningDesktopPID = "running_desktop_pid"
        case planLabel = "plan_label"
        case email
        case accountAgeText = "account_age_text"
        case usage
    }

    init(
        name: String = "",
        displayName: String = "",
        active: Bool = false,
        activated: Bool = false,
        runningDesktopPID: String = "",
        planLabel: String = "",
        email: String = "",
        accountAgeText: String = "",
        usage: UsageData = UsageData()
    ) {
        self.name = name
        self.displayName = displayName
        self.active = active
        self.activated = activated
        self.runningDesktopPID = runningDesktopPID
        self.planLabel = planLabel
        self.email = email
        self.accountAgeText = accountAgeText
        self.usage = usage
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        displayName = try c.decodeIfPresent(String.self, forKey: .displayName) ?? ""
        active = try c.decodeIfPresent(Bool.self, forKey: .active) ?? false
        activated = try c.decodeIfPresent(Bool.self, forKey: .activated) ?? false
        runningDesktopPID = try c.decodeIfPresent(String.self, forKey: .runningDesktopPID) ?? ""
        planLabel = try c.decodeIfPresent(String.self, forKey: .planLabel) ?? ""
        email = try c.decodeIfPresent(String.self, forKey: .email) ?? ""
        accountAgeText = try c.decodeIfPresent(String.self, forKey: .accountAgeText) ?? ""
        usage = try c.decodeIfPresent(UsageData.self, forKey: .usage) ?? UsageData()
    }
}

struct UsageWindow: Codable, Equatable, Sendable {
    var key: String
    var label: String
    var utilization: Double
    var resetsAt: String?
    var resetInText: String?

    enum CodingKeys: String, CodingKey {
        case key
        case label
        case utilization
        case resetsAt = "resets_at"
        case resetInText = "reset_in_text"
    }

    init(
        key: String = "",
        label: String = "",
        utilization: Double = 0,
        resetsAt: String? = nil,
        resetInText: String? = nil
    ) {
        self.key = key
        self.label = label
        self.utilization = utilization
        self.resetsAt = resetsAt
        self.resetInText = resetInText
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.decodeIfPresent(String.self, forKey: .key) ?? ""
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
        utilization = try c.decodeIfPresent(Double.self, forKey: .utilization) ?? 0
        resetsAt = try c.decodeIfPresent(String.self, forKey: .resetsAt)
        resetInText = try c.decodeIfPresent(String.self, forKey: .resetInText)
    }
}

struct UsageData: Codable, Equatable, Sendable {
    var profile: String
    var status: String
    var source: String
    var subscriptionType: String?
    var fetchedAt: String?
    var fiveHour: UsageWindow?
    var weekly: UsageWindow?
    var weeklySonnet: UsageWindow?
    var weeklyOpus: UsageWindow?
    var error: String?
    var hint: String?

    var hasPaidQuota: Bool {
        fiveHour != nil || weekly != nil
    }

    enum CodingKeys: String, CodingKey {
        case profile
        case status
        case source
        case subscriptionType = "subscription_type"
        case fetchedAt = "fetched_at"
        case fiveHour = "five_hour"
        case weekly
        case weeklySonnet = "weekly_sonnet"
        case weeklyOpus = "weekly_opus"
        case error
        case hint
    }

    init(
        profile: String = "",
        status: String = "history",
        source: String = "none",
        subscriptionType: String? = nil,
        fetchedAt: String? = nil,
        fiveHour: UsageWindow? = nil,
        weekly: UsageWindow? = nil,
        weeklySonnet: UsageWindow? = nil,
        weeklyOpus: UsageWindow? = nil,
        error: String? = nil,
        hint: String? = nil
    ) {
        self.profile = profile
        self.status = status
        self.source = source
        self.subscriptionType = subscriptionType
        self.fetchedAt = fetchedAt
        self.fiveHour = fiveHour
        self.weekly = weekly
        self.weeklySonnet = weeklySonnet
        self.weeklyOpus = weeklyOpus
        self.error = error
        self.hint = hint
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        profile = try c.decodeIfPresent(String.self, forKey: .profile) ?? ""
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? "none"
        source = try c.decodeIfPresent(String.self, forKey: .source) ?? "none"
        subscriptionType = try c.decodeIfPresent(String.self, forKey: .subscriptionType)
        fetchedAt = try c.decodeIfPresent(String.self, forKey: .fetchedAt)
        fiveHour = try c.decodeIfPresent(UsageWindow.self, forKey: .fiveHour)
        weekly = try c.decodeIfPresent(UsageWindow.self, forKey: .weekly)
        weeklySonnet = try c.decodeIfPresent(UsageWindow.self, forKey: .weeklySonnet)
        weeklyOpus = try c.decodeIfPresent(UsageWindow.self, forKey: .weeklyOpus)
        error = try c.decodeIfPresent(String.self, forKey: .error)
        hint = try c.decodeIfPresent(String.self, forKey: .hint)
    }
}

struct ActionRequest: Encodable {
    let action: String
    var profile: String?
    var reason: String?
    var locked: Bool?
    var enabled: Bool?
}

struct APIErrorPayload: Decodable {
    let error: String?
}
