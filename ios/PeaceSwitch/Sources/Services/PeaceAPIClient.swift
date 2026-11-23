import Foundation

enum PeaceAPIError: LocalizedError, Equatable {
    case invalidURL
    case unauthorized
    case serverError(String)
    case transportError(String)
    case decodingError

    var errorDescription: String? {
        switch self {
        case .invalidURL:
            return "Invalid PeaceClaude daemon URL."
        case .unauthorized:
            return "Device token expired or revoked. Please re-pair."
        case .serverError(let msg):
            return msg
        case .transportError(let msg):
            return msg
        case .decodingError:
            return "Unexpected response format from daemon."
        }
    }
}

final class PeaceAPIClient: Sendable {
    private let session: URLSession

    init(timeout: TimeInterval = 8) {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = timeout
        config.timeoutIntervalForResource = timeout + 4
        config.waitsForConnectivity = false
        self.session = URLSession(configuration: config)
    }

    func redeem(serverURL: String, code: String, deviceName: String) async throws -> PairingRecord {
        guard let norm = PairingQRParser.normalizeBaseURL(serverURL),
              let url = URL(string: norm + "/mobile/api/redeem") else {
            throw PeaceAPIError.invalidURL
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(
            RedeemRequest(code: code, deviceName: deviceName, os: "iOS")
        )

        let (data, http) = try await perform(request)
        if !(200...299).contains(http.statusCode) {
            throw parseServerError(data: data, statusCode: http.statusCode)
        }
        guard let decoded = try? JSONDecoder().decode(RedeemResponse.self, from: data),
              decoded.ok, let token = decoded.token, !token.isEmpty else {
            throw PeaceAPIError.serverError(
                (try? JSONDecoder().decode(RedeemResponse.self, from: data).error)
                    ?? "Pairing failed"
            )
        }
        return PairingRecord(
            baseURL: norm,
            bearerToken: token,
            deviceID: decoded.device?.id ?? UUID().uuidString,
            deviceName: decoded.device?.name ?? deviceName,
            pairedAt: Date()
        )
    }

    func fetchState(record: PairingRecord, forceProbe: Bool = false) async throws -> PeaceState {
        let suffix = forceProbe ? "/mobile/api/state?refresh=1" : "/mobile/api/state"
        guard let url = URL(string: record.baseURL + suffix) else {
            throw PeaceAPIError.invalidURL
        }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("Bearer \(record.bearerToken)", forHTTPHeaderField: "Authorization")

        let (data, http) = try await perform(request)
        if http.statusCode == 401 || http.statusCode == 403 {
            throw PeaceAPIError.unauthorized
        }
        if !(200...299).contains(http.statusCode) {
            throw parseServerError(data: data, statusCode: http.statusCode)
        }
        guard let state = try? JSONDecoder().decode(PeaceState.self, from: data) else {
            throw PeaceAPIError.decodingError
        }
        return state
    }

    /// Posts a control action and immediately refreshes full `/mobile/api/state`
    /// because Go `handleMobileAction` returns concise confirmation payloads (`{"ok":true,...}`).
    func performAction(record: PairingRecord, payload: ActionRequest) async throws -> PeaceState {
        guard let url = URL(string: record.baseURL + "/mobile/api/action") else {
            throw PeaceAPIError.invalidURL
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(record.bearerToken)", forHTTPHeaderField: "Authorization")
        request.httpBody = try JSONEncoder().encode(payload)

        let (data, http) = try await perform(request)
        if http.statusCode == 401 || http.statusCode == 403 {
            throw PeaceAPIError.unauthorized
        }
        if !(200...299).contains(http.statusCode) {
            throw parseServerError(data: data, statusCode: http.statusCode)
        }
        return try await fetchState(record: record, forceProbe: false)
    }

    private func perform(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        do {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                throw PeaceAPIError.transportError("Non-HTTP response")
            }
            return (data, http)
        } catch let err as PeaceAPIError {
            throw err
        } catch {
            throw PeaceAPIError.transportError(error.localizedDescription)
        }
    }

    private func parseServerError(data: Data, statusCode: Int) -> PeaceAPIError {
        if let msg = try? JSONDecoder().decode(APIErrorPayload.self, from: data).error,
           !msg.isEmpty {
            return .serverError(msg)
        }
        return .serverError("Daemon returned HTTP \(statusCode)")
    }
}
