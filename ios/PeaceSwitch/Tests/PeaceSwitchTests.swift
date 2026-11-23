import XCTest
@testable import PeaceSwitch

final class PeaceSwitchTests: XCTestCase {
    func testPairingQRParserSupportsLANURLWithFragment() {
        let raw = "http://192.168.1.42:18900/mobile#pair=PEACE-secretToken_123"
        let parsed = PairingQRParser.parse(raw)
        XCTAssertEqual(parsed?.serverURL, "http://192.168.1.42:18900")
        XCTAssertEqual(parsed?.pairCode, "PEACE-secretToken_123")
    }

    func testPairingQRParserSupportsCustomRelayPEACE1JSON() {
        let raw = #"PEACE1:{"r":"https://relay.example.com/","c":"PEACE-xyz999"}"#
        let parsed = PairingQRParser.parse(raw)
        XCTAssertEqual(parsed?.serverURL, "https://relay.example.com")
        XCTAssertEqual(parsed?.pairCode, "PEACE-xyz999")
    }

    func testPairingQRParserSupportsCustomSchemeAndRejectsInvalidInput() {
        let custom = "peaceclaude://pair?server=http%3A%2F%2F100.64.0.5%3A18900&code=PEACE-abc"
        let parsed = PairingQRParser.parse(custom)
        XCTAssertEqual(parsed?.serverURL, "http://100.64.0.5:18900")
        XCTAssertEqual(parsed?.pairCode, "PEACE-abc")

        XCTAssertNil(PairingQRParser.parse("https://example.com/no-pair-code"))
        XCTAssertNil(PairingQRParser.parse("not-a-valid-payload"))
    }

    func testDecodesRealGoBackendMobileStateJSONWithOmitEmptyFields() throws {
        let goJSON = """
        {
          "version": "0.2.0",
          "platform": "darwin",
          "host_name": "macbook.local",
          "active_profile": "pro-work",
          "ui_locked": false,
          "gate": {
            "open": true,
            "manual_lock": false,
            "active_streams": 2,
            "effective_timezone": "America/Denver",
            "effective_language": "en-US",
            "effective_locale": "en_US",
            "last_probe": {
              "checked_at": "2026-11-23T02:00:00Z",
              "ok": true,
              "egress_ip": "198.51.100.42",
              "country": "United States",
              "city": "Cheyenne",
              "asn": 396982,
              "latency_ms": 84
            },
            "last_good": {
              "checked_at": "2026-11-23T02:00:00Z",
              "ok": true,
              "egress_ip": "198.51.100.42",
              "latency_ms": 84
            }
          },
          "stats": {
            "proxied_requests": 120,
            "blocked_failclosed": 1,
            "blocked_telemetry": 5,
            "leak_count": 0
          },
          "upstream_route": {
            "bypass_enabled": true,
            "configured_interface": "en0",
            "active_interface": "en0",
            "bound_physical": true
          },
          "browser_pac": {
            "pac_url": "http://127.0.0.1:18899/peaceclaude.pac",
            "system_pac_enabled": true,
            "matches_peaceclaude": true,
            "note": "ok"
          },
          "doctor_summary": {
            "score": 100,
            "grade": "safe",
            "critical_passed": 11,
            "critical_total": 11
          },
          "accounts": [
            {
              "name": "pro-work",
              "display_name": "Work Pro",
              "active": true,
              "activated": true,
              "running_desktop_pid": "4100",
              "plan_label": "Pro",
              "email": "pro@example.test",
              "account_age_text": "11m",
              "usage": {
                "profile": "pro-work",
                "status": "ok",
                "source": "oauth_api",
                "five_hour": {
                  "key": "five_hour",
                  "label": "5-Hour",
                  "utilization": 42.5,
                  "reset_in_text": "2h 10m"
                },
                "weekly": {
                  "key": "weekly",
                  "label": "Weekly",
                  "utilization": 65.0
                }
              }
            },
            {
              "name": "free-slot",
              "display_name": "Free Slot",
              "active": false,
              "activated": true,
              "running_desktop_pid": "",
              "plan_label": "Free",
              "email": "free@example.test",
              "account_age_text": "",
              "usage": {
                "profile": "free-slot",
                "status": "history",
                "source": "none"
              }
            }
          ]
        }
        """

        let data = try XCTUnwrap(goJSON.data(using: .utf8))
        let state = try JSONDecoder().decode(PeaceState.self, from: data)

        XCTAssertEqual(state.platform, "darwin")
        XCTAssertEqual(state.doctorSummary.score, 100)
        XCTAssertEqual(state.gate.lastProbe?.egressIP, "198.51.100.42")
        XCTAssertEqual(state.accounts.count, 2)
        XCTAssertTrue(state.accounts[0].usage.hasPaidQuota)
        XCTAssertEqual(state.accounts[0].usage.fiveHour?.utilization, 42.5)
        XCTAssertFalse(state.accounts[1].usage.hasPaidQuota)
    }

    func testDemoStateEngineTransitionsGateAccountUIAndPAC() {
        var state = DemoStateEngine.initialState()
        XCTAssertTrue(state.gate.open)
        XCTAssertFalse(state.gate.manualLock)

        state = DemoStateEngine.apply(action: ActionRequest(action: "lock_gate"), to: state)
        XCTAssertFalse(state.gate.open)
        XCTAssertTrue(state.gate.manualLock)

        state = DemoStateEngine.apply(action: ActionRequest(action: "unlock_gate"), to: state)
        XCTAssertTrue(state.gate.open)
        XCTAssertFalse(state.gate.manualLock)

        state = DemoStateEngine.apply(action: ActionRequest(action: "set_active", profile: "beta-research"), to: state)
        XCTAssertEqual(state.activeProfile, "beta-research")
        XCTAssertEqual(state.accounts.first(where: { $0.active })?.name, "beta-research")

        state = DemoStateEngine.apply(action: ActionRequest(action: "lock_ui", locked: true), to: state)
        XCTAssertTrue(state.uiLocked)

        state = DemoStateEngine.apply(action: ActionRequest(action: "toggle_pac", enabled: false), to: state)
        XCTAssertFalse(state.browserPAC.matchesPeaceclaude)
    }

    func testPairingParserRejectsUserinfoSpoofingAndFlagsPublicHTTP() {
        XCTAssertNil(PairingQRParser.parse("http://trusted.local@evil.example:18900/mobile#pair=PEACE-1"))
        XCTAssertNil(PairingQRParser.normalizeBaseURL("http://user:pass@192.168.1.2:18900"))
        XCTAssertNil(PairingQRParser.normalizeBaseURL("http://192.168.1.2:99999"))

        XCTAssertFalse(PairingQRParser.isInsecurePublicHTTP("http://192.168.1.10:18900"))
        XCTAssertFalse(PairingQRParser.isInsecurePublicHTTP("http://100.64.0.9:18900"))
        XCTAssertFalse(PairingQRParser.isInsecurePublicHTTP("http://localhost:18900"))
        XCTAssertFalse(PairingQRParser.isInsecurePublicHTTP("https://relay.example.com"))
        XCTAssertTrue(PairingQRParser.isInsecurePublicHTTP("http://relay.example.com:18900"))
    }
}
