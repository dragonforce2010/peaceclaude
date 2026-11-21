package remote

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"peaceclaude/internal/config"
)

func TestQRRenderContainsFinderPatternsAndValidSVG(t *testing.T) {
	svg, err := RenderQRSVG("http://192.168.1.25:18900/mobile#pair=PEACE-testtoken1234567890")
	if err != nil {
		t.Fatalf("RenderQRSVG failed: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg ") || !strings.Contains(svg, `viewBox="0 0 `) || !strings.Contains(svg, "<path ") {
		t.Fatalf("unexpected SVG markup: %.120s", svg)
	}
	mat, err := EncodeQRMatrix("PEACE-hello-world-2026")
	if err != nil {
		t.Fatalf("EncodeQRMatrix failed: %v", err)
	}
	n := len(mat)
	if n != 25 && n != 21 && n != 29 {
		t.Fatalf("unexpected QR matrix size %d", n)
	}
	// Check top-left 7x7 finder outer border is all true (dark)
	for i := 0; i < 7; i++ {
		if !mat[0][i] || !mat[6][i] || !mat[i][0] || !mat[i][6] {
			t.Fatalf("finder border corrupted at %d", i)
		}
		if !mat[0][n-7+i] || !mat[n-7+i][0] {
			t.Fatalf("secondary finder border corrupted at %d", i)
		}
	}
}

func TestPairTicketRedeemIsSingleUseAndRevocable(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Remote.RelayMode = "lan"
	cfg.Remote.MobilePort = 18900

	mgr := NewManager()
	ticket, err := mgr.CreatePairTicket(context.Background(), cfg, "192.168.10.22")
	if err != nil {
		t.Fatalf("CreatePairTicket failed: %v", err)
	}
	if !strings.HasPrefix(ticket.Code, "PEACE-") {
		t.Fatalf("unexpected code: %q", ticket.Code)
	}
	if !strings.Contains(ticket.URL, "http://192.168.10.22:18900/mobile#pair="+ticket.Code) {
		t.Fatalf("unexpected QR URL: %q", ticket.URL)
	}
	if !strings.Contains(ticket.SVG, "<svg ") {
		t.Fatalf("expected SVG QR code")
	}

	tok, dev, err := mgr.RedeemPair(ticket.Code, "iPhone Test", "iOS")
	if err != nil {
		t.Fatalf("RedeemPair failed: %v", err)
	}
	if tok == "" || dev == nil || dev.Name != "iPhone Test" {
		t.Fatalf("unexpected paired device: %+v", dev)
	}

	// Single-use guarantee
	if _, _, err := mgr.RedeemPair(ticket.Code, "Attacker", "Android"); err == nil {
		t.Fatalf("expected second redemption of one-time code to fail")
	}

	// Auth works after reload from disk
	mgr2 := NewManager()
	if _, ok := mgr2.AuthenticateBearer(tok); !ok {
		t.Fatalf("expected persisted bearer auth to succeed across manager reload")
	}

	// Revocation invalidates bearer immediately
	if count := mgr2.RevokeAll(); count != 1 {
		t.Fatalf("expected 1 revoked device, got %d", count)
	}
	if _, ok := mgr2.AuthenticateBearer(tok); ok {
		t.Fatalf("expected revoked bearer token to fail auth")
	}
}

func TestCustomRelayRequiresReachableHealthz(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Remote.RelayMode = "custom"
	cfg.Remote.CustomRelayURL = srv.URL

	mgr := NewManager()
	ticket, err := mgr.CreatePairTicket(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("expected healthy loopback custom relay to succeed: %v", err)
	}
	if !strings.HasPrefix(ticket.URL, "PEACE1:") {
		t.Fatalf("expected PEACE1 payload for custom relay, got %q", ticket.URL)
	}

	// Official mode honestly refuses rather than faking a relay server
	cfg.Remote.RelayMode = "official"
	if _, err := mgr.CreatePairTicket(context.Background(), cfg, ""); err == nil {
		t.Fatalf("expected official relay mode to refuse with clear explanation")
	}
}

func TestQRDecodesCleanlyWithMacOSCoreImage(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("CoreImage QR scanner test is macOS-only")
	}
	if _, err := exec.LookPath("swift"); err != nil {
		t.Skip("swift not installed")
	}
	payloads := []string{
		"PEACE-short-123",
		"http://192.168.1.88:18900/mobile#pair=PEACE-verify-coreimage-2026-medium-url-token",
		`PEACE1:{"r":"https://relay.example.com:8443","c":"PEACE-long-version7-two-group-ecc-test-payload-0123456789abcdef-ghijklmnopqrstuvwxyz-0987654321"}`,
	}
	dir := t.TempDir()
	for idx, want := range payloads {
		m, err := EncodeQRMatrix(want)
		if err != nil {
			t.Fatalf("EncodeQRMatrix(%d) failed: %v", idx, err)
		}
		const scale, quiet = 8, 4
		n := len(m)
		sz := (n + quiet*2) * scale
		img := image.NewGray(image.Rect(0, 0, sz, sz))
		for y := 0; y < sz; y++ {
			for x := 0; x < sz; x++ {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
		for r := 0; r < n; r++ {
			for c := 0; c < n; c++ {
				if m[r][c] {
					for dy := 0; dy < scale; dy++ {
						for dx := 0; dx < scale; dx++ {
							img.SetGray((c+quiet)*scale+dx, (r+quiet)*scale+dy, color.Gray{Y: 0})
						}
					}
				}
			}
		}
		pngPath := filepath.Join(dir, "qr.png")
		f, err := os.Create(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		_ = f.Close()

		swiftSrc := `import CoreImage
import Foundation
let url = URL(fileURLWithPath: CommandLine.arguments[1])
let ci = CIImage(contentsOf: url)!
let det = CIDetector(ofType: CIDetectorTypeQRCode, context: nil, options: [CIDetectorAccuracy: CIDetectorAccuracyHigh])!
let feats = det.features(in: ci) as? [CIQRCodeFeature] ?? []
print(feats.first?.messageString ?? "")`
		out, err := exec.Command("swift", "-e", swiftSrc, pngPath).CombinedOutput()
		if err != nil {
			t.Fatalf("swift CoreImage scan failed: %v (%s)", err, string(out))
		}
		got := strings.TrimSpace(string(out))
		if got != want {
			t.Fatalf("CoreImage decoded QR[%d] = %q, want %q", idx, got, want)
		}
	}
}

func TestRemoteSecurityHardening(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Remote.RelayMode = "lan"
	mgr := NewManager()

	ticket, err := mgr.CreatePairTicket(context.Background(), cfg, "192.168.1.50")
	if err != nil {
		t.Fatalf("CreatePairTicket: %v", err)
	}

	// 5 failed guesses MUST burn the active one-time code immediately.
	for i := 0; i < maxPairFailCount; i++ {
		if _, _, err := mgr.RedeemPair("PEACE-wrong-guess", "Bad", "iOS"); err == nil {
			t.Fatalf("attempt %d should fail", i+1)
		}
	}
	if _, _, err := mgr.RedeemPair(ticket.Code, "LateValid", "iOS"); err == nil {
		t.Fatal("real code MUST be burned after 5 wrong brute-force attempts")
	}

	// ValidateRelayURL MUST reject cloud metadata / private IPs (SSRF protection).
	for _, bad := range []string{
		"https://169.254.169.254",
		"https://10.0.0.1",
		"https://192.168.1.1",
		"http://198.51.100.10",
	} {
		if _, err := ValidateRelayURL(bad); err == nil {
			t.Fatalf("ValidateRelayURL should reject %q", bad)
		}
	}

	// Valid pairing -> Status() preserves in-memory LastSeenAt instead of overwriting via ReloadState().
	ticket2, err := mgr.CreatePairTicket(context.Background(), cfg, "192.168.1.50")
	if err != nil {
		t.Fatal(err)
	}
	tok, dev, err := mgr.RedeemPair(ticket2.Code, "Phone", "iOS")
	if err != nil {
		t.Fatal(err)
	}
	beforeSeen := dev.LastSeenAt
	devAuthed, ok := mgr.AuthenticateBearer(tok)
	if !ok {
		t.Fatal("AuthenticateBearer failed")
	}
	st := mgr.Status(cfg, "0.0.0.0:18900")
	if len(st.PairedDevices) != 1 || st.PairedDevices[0].LastSeenAt.Before(beforeSeen) || !st.PairedDevices[0].LastSeenAt.Equal(devAuthed.LastSeenAt) {
		t.Fatalf("Status() clobbered LastSeenAt: %+v vs %v", st.PairedDevices, devAuthed.LastSeenAt)
	}
}
