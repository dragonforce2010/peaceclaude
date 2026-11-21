import Cocoa
import WebKit

private let fallbackControlURL = "http://127.0.0.1:18899"

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKUIDelegate, WKNavigationDelegate {
    private var window: NSWindow!
    private var webView: WKWebView!
    private var controlURL: String = fallbackControlURL
    private var targetURL: URL { URL(string: "\(controlURL)/#accounts")! }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        controlURL = resolveControlURL()
        buildMainMenu()
        buildWindow()
        showLoadingHTML()
        ensureDaemonAndLoad()
        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag {
            window.makeKeyAndOrderFront(nil)
        }
        NSApp.activate(ignoringOtherApps: true)
        return true
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return true
    }

    private func resolveControlURL() -> String {
        let envHome = ProcessInfo.processInfo.environment["PEACECLAUDE_HOME"] ?? ""
        let baseDir = envHome.isEmpty ? (NSHomeDirectory() + "/.peaceclaude") : envHome
        let cfgPath = baseDir + "/config.json"
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: cfgPath)),
              let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let listen = root["listen"] as? [String: Any] else {
            return fallbackControlURL
        }
        let bind = (listen["bind_addr"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "127.0.0.1"
        let port = (listen["control_port"] as? Int) ?? 18899
        return "http://\(bind):\(port)"
    }

    private func buildWindow() {
        let rect = NSRect(x: 0, y: 0, width: 1260, height: 840)
        window = NSWindow(
            contentRect: rect,
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = "PeaceClaude — 多账号安心工作台"
        window.minSize = NSSize(width: 960, height: 640)
        window.backgroundColor = NSColor(calibratedRed: 0.059, green: 0.078, blue: 0.098, alpha: 1.0)
        window.center()
        window.delegate = self

        let config = WKWebViewConfiguration()
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        webView = WKWebView(frame: rect, configuration: config)
        webView.uiDelegate = self
        webView.navigationDelegate = self
        webView.setValue(false, forKey: "drawsBackground")
        window.contentView = webView
        window.makeKeyAndOrderFront(nil)
    }

    private func buildMainMenu() {
        let mainMenu = NSMenu()

        // App Menu
        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "关于 PeaceClaude", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(NSMenuItem.separator())
        appMenu.addItem(withTitle: "隐藏 PeaceClaude", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        appMenu.addItem(withTitle: "退出 PeaceClaude", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        mainMenu.addItem(appItem)

        // Edit Menu (essential so Cmd+C / Cmd+V / Cmd+A work in WKWebView inputs)
        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "编辑")
        editMenu.addItem(withTitle: "撤销", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "重做", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(NSMenuItem.separator())
        editMenu.addItem(withTitle: "剪切", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "拷贝", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu
        mainMenu.addItem(editItem)

        // View Menu
        let viewItem = NSMenuItem()
        let viewMenu = NSMenu(title: "视图")
        viewMenu.addItem(withTitle: "刷新页面", action: #selector(reloadPage), keyEquivalent: "r")
        viewMenu.addItem(withTitle: "在默认浏览器中打开", action: #selector(openInBrowser), keyEquivalent: "o")
        viewItem.submenu = viewMenu
        mainMenu.addItem(viewItem)

        // Window Menu
        let winItem = NSMenuItem()
        let winMenu = NSMenu(title: "窗口")
        winMenu.addItem(withTitle: "最小化", action: #selector(NSWindow.miniaturize(_:)), keyEquivalent: "m")
        winMenu.addItem(withTitle: "缩放", action: #selector(NSWindow.zoom(_:)), keyEquivalent: "")
        winMenu.addItem(withTitle: "关闭窗口", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        winItem.submenu = winMenu
        mainMenu.addItem(winItem)

        NSApp.mainMenu = mainMenu
    }

    @objc private func reloadPage() {
        if webView.url == nil || webView.url?.scheme == "about" {
            ensureDaemonAndLoad()
        } else {
            webView.reload()
        }
    }

    @objc private func openInBrowser() {
        NSWorkspace.shared.open(targetURL)
    }

    private func showLoadingHTML(_ message: String = "正在连接 PeaceClaude 安心工作台与本地守护网关…") {
        let safeMsg = message
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
        let html = """
        <!doctype html><html><head><meta charset="utf-8"><style>
        body{margin:0;height:100vh;display:flex;align-items:center;justify-content:center;
        background:#0f1419;color:#e6edf3;font-family:-apple-system,BlinkMacSystemFont,sans-serif;}
        .box{text-align:center;max-width:460px;padding:32px;border:1px solid #25303b;border-radius:16px;background:#161d24;}
        h2{margin:0 0 10px;font-size:18px;font-weight:600;color:#e07a55;}
        p{margin:0;font-size:13.5px;color:#9fb0c0;line-height:1.6;}
        </style></head><body><div class="box"><h2>PeaceClaude</h2><p>\(safeMsg)</p></div></body></html>
        """
        webView.loadHTMLString(html, baseURL: nil)
    }

    private func ensureDaemonAndLoad() {
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self = self else { return }
            if !self.isDaemonHealthy() {
                self.startDaemonCLI()
                for _ in 0..<25 {
                    if self.isDaemonHealthy() { break }
                    Thread.sleep(forTimeInterval: 0.25)
                }
            }
            DispatchQueue.main.async {
                if self.isDaemonHealthy() {
                    self.webView.load(URLRequest(url: self.targetURL))
                } else {
                    self.showLoadingHTML("无法连接本地后台网关 (\(self.controlURL))，请按 Cmd+R 重试或运行 `peaceclaude service start`。")
                }
            }
        }
    }

    private func isDaemonHealthy() -> Bool {
        guard let url = URL(string: "\(controlURL)/api/health") else { return false }
        var req = URLRequest(url: url)
        req.timeoutInterval = 1.2
        let sem = DispatchSemaphore(value: 0)
        var ok = false
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            if let http = resp as? HTTPURLResponse, http.statusCode == 200 {
                ok = true
            }
            sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + 1.5)
        return ok
    }

    private func bundledCLIBinary() -> String? {
        guard let resURL = Bundle.main.resourceURL else { return nil }
        let path = resURL.appendingPathComponent("bin/peaceclaude").path
        return FileManager.default.isExecutableFile(atPath: path) ? path : nil
    }

    private func configPath() -> String {
        let envHome = ProcessInfo.processInfo.environment["PEACECLAUDE_HOME"] ?? ""
        let baseDir = envHome.isEmpty ? (NSHomeDirectory() + "/.peaceclaude") : envHome
        return baseDir + "/config.json"
    }

    private func startDaemonCLI() {
        let candidates = [
            NSHomeDirectory() + "/.local/bin/peaceclaude",
            "/opt/homebrew/bin/peaceclaude",
            "/usr/local/bin/peaceclaude"
        ]
        guard let bin = candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) }) ?? bundledCLIBinary() else {
            return
        }
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: bin)
        if FileManager.default.fileExists(atPath: configPath()) {
            proc.arguments = ["service", "start"]
        } else {
            proc.arguments = ["setup", "--no-app"]
        }
        try? proc.run()
        proc.waitUntilExit()
    }

    // Standard JS alert/confirm handlers
    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let alert = NSAlert()
        alert.messageText = "PeaceClaude"
        alert.informativeText = message
        alert.addButton(withTitle: "确定")
        alert.runModal()
        completionHandler()
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        let alert = NSAlert()
        alert.messageText = "PeaceClaude"
        alert.informativeText = message
        alert.addButton(withTitle: "确定")
        alert.addButton(withTitle: "取消")
        completionHandler(alert.runModal() == .alertFirstButtonReturn)
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
