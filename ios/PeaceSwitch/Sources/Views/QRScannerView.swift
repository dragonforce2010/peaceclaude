import AVFoundation
import SwiftUI

struct QRScannerView: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var store: CockpitStore

    let onScanned: (String) -> Void

    @State private var authStatus: AVAuthorizationStatus = AVCaptureDevice.authorizationStatus(for: .video)
    @State private var torchOn = false
    @State private var cameraUnavailable = false

    var body: some View {
        NavigationStack {
            ZStack {
                Color.black.ignoresSafeArea()

                if cameraUnavailable || !UIImagePickerController.isSourceTypeAvailable(.camera) {
                    VStack(spacing: 16) {
                        Image(systemName: "camera.metering.unknown")
                            .font(.system(size: 44))
                            .foregroundStyle(.white.opacity(0.8))
                        Text(store.tr("当前设备或模拟器不支持摄像头扫码", "Camera scanning is unavailable on this device or simulator"))
                            .font(.headline)
                            .foregroundStyle(.white)
                            .multilineTextAlignment(.center)
                        Text(store.tr("请返回上一页粘贴电脑端生成的配对链接，或先进入「互动演示模式」。",
                                      "Go back to paste the pairing URL from your desktop, or enter Interactive Demo Mode."))
                            .font(.subheadline)
                            .foregroundStyle(.white.opacity(0.7))
                            .multilineTextAlignment(.center)
                    }
                    .padding(32)
                } else if authStatus == .authorized {
                    ScannerCaptureRepresentable(
                        torchOn: $torchOn,
                        onUnavailable: { cameraUnavailable = true },
                        onCodeDetected: { code in
                            onScanned(code)
                            dismiss()
                        }
                    )
                    .ignoresSafeArea()

                    VStack {
                        Spacer()
                        RoundedRectangle(cornerRadius: 22, style: .continuous)
                            .stroke(PeaceTheme.vermilion, lineWidth: 2.5)
                            .frame(width: 240, height: 240)
                            .background(Color.clear)
                        Spacer()
                        Text(store.tr("将电脑控制台生成的配对二维码放入框内",
                                      "Align the PeaceClaude desktop pairing QR inside the frame"))
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 18)
                            .padding(.vertical, 10)
                            .background(.ultraThinMaterial, in: Capsule())
                            .padding(.bottom, 32)
                    }
                } else if authStatus == .notDetermined {
                    ProgressView()
                        .tint(.white)
                        .task {
                            let granted = await AVCaptureDevice.requestAccess(for: .video)
                            authStatus = granted ? .authorized : .denied
                        }
                } else {
                    VStack(spacing: 16) {
                        Image(systemName: "lock.shield")
                            .font(.system(size: 42))
                            .foregroundStyle(PeaceTheme.vermilion)
                        Text(store.tr("需要相机权限以扫描配对二维码", "Camera access required for QR pairing"))
                            .font(.headline)
                            .foregroundStyle(.white)
                        Button(store.tr("打开系统设置", "Open Settings")) {
                            if let url = URL(string: UIApplication.openSettingsURLString) {
                                UIApplication.shared.open(url)
                            }
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(PeaceTheme.vermilion)
                    }
                    .padding(32)
                }
            }
            .navigationTitle(store.tr("扫码配对", "Scan QR Code"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button(store.tr("关闭", "Close")) {
                        dismiss()
                    }
                    .foregroundStyle(.white)
                }
                if authStatus == .authorized && !cameraUnavailable {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            torchOn.toggle()
                        } label: {
                            Image(systemName: torchOn ? "flashlight.on.fill" : "flashlight.off.fill")
                                .foregroundStyle(.white)
                        }
                    }
                }
            }
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbarBackground(Color.black.opacity(0.65), for: .navigationBar)
        }
    }
}

private struct ScannerCaptureRepresentable: UIViewControllerRepresentable {
    @Binding var torchOn: Bool
    let onUnavailable: () -> Void
    let onCodeDetected: (String) -> Void

    func makeUIViewController(context: Context) -> ScannerViewController {
        let vc = ScannerViewController()
        vc.onUnavailable = onUnavailable
        vc.onCodeDetected = onCodeDetected
        return vc
    }

    func updateUIViewController(_ uiViewController: ScannerViewController, context: Context) {
        uiViewController.setTorch(torchOn)
    }
}

private final class ScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onUnavailable: (() -> Void)?
    var onCodeDetected: ((String) -> Void)?

    private let session = AVCaptureSession()
    private var previewLayer: AVCaptureVideoPreviewLayer?
    private var didEmit = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black

        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else {
            DispatchQueue.main.async { [weak self] in
                self?.onUnavailable?()
            }
            return
        }
        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else {
            DispatchQueue.main.async { [weak self] in
                self?.onUnavailable?()
            }
            return
        }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        if output.availableMetadataObjectTypes.contains(.qr) {
            output.metadataObjectTypes = [.qr]
        }

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        preview.frame = view.bounds
        view.layer.addSublayer(preview)
        previewLayer = preview
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        previewLayer?.frame = view.bounds
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        didEmit = false
        let capSession = session
        DispatchQueue.global(qos: .userInitiated).async {
            if !capSession.isRunning {
                capSession.startRunning()
            }
        }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        setTorch(false)
        let capSession = session
        DispatchQueue.global(qos: .utility).async {
            if capSession.isRunning {
                capSession.stopRunning()
            }
        }
    }

    func setTorch(_ on: Bool) {
        guard let device = AVCaptureDevice.default(for: .video),
              device.hasTorch else { return }
        try? device.lockForConfiguration()
        device.torchMode = on ? .on : .off
        device.unlockForConfiguration()
    }

    func metadataOutput(
        _ output: AVCaptureMetadataOutput,
        didOutput metadataObjects: [AVMetadataObject],
        from connection: AVCaptureConnection
    ) {
        guard !didEmit,
              let readable = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              let value = readable.stringValue,
              !value.isEmpty else {
            return
        }
        didEmit = true
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        onCodeDetected?(value)
    }
}
