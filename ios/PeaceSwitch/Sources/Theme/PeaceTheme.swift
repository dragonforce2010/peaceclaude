import SwiftUI

enum PeaceTheme {
    // 暖瑞士工坊（Warm Swiss Atelier）与桌面控制台、独立官网保持一致的视觉语言
    static let paper = Color(lightHex: 0xF9F7F4, darkHex: 0x161513)
    static let surface = Color(lightHex: 0xFFFFFF, darkHex: 0x201E1B)
    static let surfaceMuted = Color(lightHex: 0xF1ECE4, darkHex: 0x2A2723)
    static let border = Color(lightHex: 0xE6E0D6, darkHex: 0x3A352F)
    static let ink = Color(lightHex: 0x161615, darkHex: 0xF5F2EC)
    static let inkMuted = Color(lightHex: 0x625D55, darkHex: 0xA8A095)

    static let vermilion = Color(lightHex: 0xC15A32, darkHex: 0xE07449)
    static let emerald = Color(lightHex: 0x1D7A4E, darkHex: 0x35B276)
    static let amber = Color(lightHex: 0xB45309, darkHex: 0xF59E0B)
    static let crimson = Color(lightHex: 0xB91C1C, darkHex: 0xEF4444)
    static let navy = Color(lightHex: 0x1C2B48, darkHex: 0x293E66)
}

extension Color {
    init(lightHex: UInt32, darkHex: UInt32) {
        self.init(UIColor { traits in
            let hex = traits.userInterfaceStyle == .dark ? darkHex : lightHex
            let r = CGFloat((hex >> 16) & 0xFF) / 255.0
            let g = CGFloat((hex >> 8) & 0xFF) / 255.0
            let b = CGFloat(hex & 0xFF) / 255.0
            return UIColor(red: r, green: g, blue: b, alpha: 1.0)
        })
    }
}

struct AtelierCardModifier: ViewModifier {
    var borderColor: Color = PeaceTheme.border
    func body(content: Content) -> some View {
        content
            .padding(16)
            .background(PeaceTheme.surface)
            .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .stroke(borderColor, lineWidth: 1)
            )
    }
}

extension View {
    func atelierCard(borderColor: Color = PeaceTheme.border) -> some View {
        modifier(AtelierCardModifier(borderColor: borderColor))
    }
}
