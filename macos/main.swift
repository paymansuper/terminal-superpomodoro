// superpomodoro-float: the floating mini window for SuperPomodoro (macOS 12+).
//
// Small and always on top in a corner while a phase runs. When a phase ends it moves
// to the center of the screen, grows and stays above everything until you react.
// It mirrors the terminal timer through a state file and sends commands back through
// a command file, so the terminal app stays the single source of truth.
//
// Build: swiftc -O -o superpomodoro-float macos/main.swift

import AppKit
import Carbon.HIToolbox
import SwiftUI

// MARK: - State shared with the terminal app

struct TimerState: Decodable {
    var pid: Int32
    var mini: Bool
    var phase: String
    var remaining: Double
    var total: Double
    var running: Bool
    var ready: Bool
    var event: Int
    var message: String
    var colorA: String
    var colorB: String
    var text: String
    var muted: String
    var surface: String
    var auto: Bool
    var image: String?
    var imageBrightness: Double?
    var imageFit: String?      // "cover" or "contain", resolved by the terminal app
    var imageScale: Double?
    var imagePosition: String?
    var imageShiftX: Double?   // fill mode: -1 ... 1, 0 is centered
    var imageShiftY: Double?
    var tasks: [TaskItem]?     // to-dos planned for this focus session
}

struct TaskItem: Decodable {
    var id: Int
    var text: String
    var done: Bool
}

extension Color {
    init(hex: String) {
        var s = hex.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("#") { s.removeFirst() }
        let v = UInt32(s, radix: 16) ?? 0
        self.init(.sRGB,
                  red: Double((v >> 16) & 0xff) / 255,
                  green: Double((v >> 8) & 0xff) / 255,
                  blue: Double(v & 0xff) / 255,
                  opacity: 1)
    }
}

func clockString(_ secs: Double) -> String {
    let t = max(0, Int(secs.rounded(.up)))
    if t >= 3600 { return String(format: "%d:%02d:%02d", t / 3600, (t / 60) % 60, t % 60) }
    return String(format: "%02d:%02d", t / 60, t % 60)
}

let maxTasks = 5

final class Model: ObservableObject {
    @Published var state: TimerState?
    @Published var expanded = false
    @Published var hovering = false
    @Published var image: NSImage?
    var imagePath: String?
}

// MARK: - View

/// Buttons of the big window. Drawn by hand so they stay readable on any theme,
/// any picture behind the window and in light or dark mode.
struct PillButtonStyle: ButtonStyle {
    var fill: [Color]
    var textColor: Color
    var prominent: Bool

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 13, weight: .semibold, design: .rounded))
            .foregroundColor(textColor)
            .padding(.horizontal, 16)
            .frame(height: 32)
            .background(
                Capsule().fill(LinearGradient(colors: fill, startPoint: .leading, endPoint: .trailing))
                    .opacity(configuration.isPressed ? 0.75 : 1))
            .overlay(Capsule().stroke(Color.white.opacity(prominent ? 0 : 0.22), lineWidth: 1))
            .contentShape(Capsule())
    }
}

/// Height of the window content, so the window always fits it exactly.
struct ContentHeightKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

/// Dark or light text, whichever reads better on the given color.
func readableText(on hex: String) -> Color {
    let c = NSColor(Color(hex: hex)).usingColorSpace(.sRGB) ?? .white
    let lum = 0.2126 * c.redComponent + 0.7152 * c.greenComponent + 0.0722 * c.blueComponent
    return lum > 0.55 ? Color(red: 0.07, green: 0.07, blue: 0.10) : .white
}

struct FloatView: View {
    @ObservedObject var model: Model
    let send: (String) -> Void
    let shrink: () -> Void
    let hide: () -> Void
    let dragChanged: () -> Void
    let dragEnded: () -> Void
    let contentHeight: (CGFloat) -> Void

    var body: some View {
        ZStack {
            if let s = model.state {
                Group {
                    if model.expanded { expanded(s) } else { compact(s) }
                }
                .fixedSize(horizontal: false, vertical: true)
                .background(GeometryReader { g in
                    Color.clear.preference(key: ContentHeightKey.self, value: g.size.height)
                })
            }
        }
        .onPreferenceChange(ContentHeightKey.self) { contentHeight($0) }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(background) // a background never changes the layout, even for big images
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous)
            .stroke(Color.white.opacity(0.10), lineWidth: 1))
        .onHover { model.hovering = $0 }
        .gesture(DragGesture(minimumDistance: 3)
            .onChanged { _ in dragChanged() }
            .onEnded { _ in dragEnded() })
    }

    private var background: some View {
        ZStack {
            Color(hex: model.state?.surface ?? "#16161e")
            if let img = model.image {
                GeometryReader { g in
                    if model.state?.imageFit == "contain" {
                        // logos: the whole picture, scaled and positioned like in the terminal
                        let s = min(max(model.state?.imageScale ?? 0.8, 0.1), 1)
                        let pos = model.state?.imagePosition ?? "center"
                        let align: Alignment = pos == "top" ? .top : pos == "bottom" ? .bottom : .center
                        Image(nsImage: img)
                            .resizable()
                            .scaledToFit()
                            .frame(width: g.size.width * s, height: g.size.height * s)
                            .padding(.vertical, pos == "center" ? 0 : g.size.height * 0.06)
                            .frame(width: g.size.width, height: g.size.height, alignment: align)
                    } else {
                        // fill the window, the shift picks which part stays visible (same math as the terminal)
                        let W = g.size.width, H = g.size.height
                        let iw = max(img.size.width, 1), ih = max(img.size.height, 1)
                        let sc = max(W / iw, H / ih)
                        let fx = 0.5 + min(max(model.state?.imageShiftX ?? 0, -1), 1) / 2
                        let fy = 0.5 + min(max(model.state?.imageShiftY ?? 0, -1), 1) / 2
                        ZStack(alignment: .topLeading) {
                            Image(nsImage: img)
                                .resizable()
                                .frame(width: iw * sc, height: ih * sc)
                                .offset(x: (W - iw * sc) * fx, y: (H - ih * sc) * fy)
                        }
                        .frame(width: W, height: H, alignment: .topLeading)
                        .clipped()
                    }
                }
                // dim the picture so the timer stays readable
                Color.black.opacity(1 - min(max(model.state?.imageBrightness ?? 0.45, 0), 1))
            }
        }
    }

    private func secondaryStyle(_ s: TimerState) -> PillButtonStyle {
        PillButtonStyle(fill: [Color.white.opacity(0.14), Color.white.opacity(0.10)],
                        textColor: Color(hex: s.text), prominent: false)
    }

    private func gradient(_ s: TimerState) -> LinearGradient {
        LinearGradient(colors: [Color(hex: s.colorA), Color(hex: s.colorB)],
                       startPoint: .leading, endPoint: .trailing)
    }

    private func progress(_ s: TimerState, height: CGFloat) -> some View {
        let frac = s.total > 0 ? min(max(1 - s.remaining / s.total, 0), 1) : 0
        return GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.white.opacity(0.10))
                Capsule().fill(gradient(s))
                    .frame(width: max(height, g.size.width * CGFloat(frac)))
            }
        }
        .frame(height: height)
    }

    private func statusText(_ s: TimerState) -> String {
        if s.ready { return "ready" }
        if !s.running { return "paused" }
        return ""
    }

    private func compact(_ s: TimerState) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Circle().fill(Color(hex: s.colorA)).frame(width: 7, height: 7)
                Text(s.phase)
                    .font(.system(size: 10, weight: .semibold, design: .rounded))
                    .foregroundColor(Color(hex: s.muted))
                Text(statusText(s))
                    .font(.system(size: 10, weight: .regular, design: .rounded))
                    .foregroundColor(Color(hex: s.muted).opacity(0.8))
                Spacer(minLength: 4)
                if model.hovering { controls(s) }
            }
            Text(clockString(s.remaining))
                .font(.system(size: 30, weight: .bold, design: .rounded).monospacedDigit())
                .foregroundStyle(gradient(s))
                .opacity(s.running || s.ready ? 1 : 0.55)
            progress(s, height: 4)
            taskList(s, fontSize: 11)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
        // double-click goes back to the terminal
        .onTapGesture(count: 2) { send("restore") }
        // same keys as in the terminal once the window is clicked (key window)
        .background(
            ZStack {
                hiddenKey("w") { send("restore") }
                hiddenKey(" ") { send("toggle") }
                hiddenKey("s") { send("skip") }
            }
        )
    }

    /// The to-dos of this focus session, a click ticks one off in the terminal app too.
    @ViewBuilder
    private func taskList(_ s: TimerState, fontSize: CGFloat) -> some View {
        if let tasks = s.tasks, !tasks.isEmpty {
            VStack(alignment: .leading, spacing: 3) {
                ForEach(Array(tasks.prefix(maxTasks)), id: \.id) { t in
                    Button(action: { send("task:\(t.id)") }) {
                        HStack(spacing: 6) {
                            Image(systemName: t.done ? "checkmark.circle.fill" : "circle")
                                .font(.system(size: fontSize - 1))
                                .foregroundColor(t.done ? Color(hex: s.colorA) : Color(hex: s.muted))
                            Text(t.text)
                                .font(.system(size: fontSize, design: .rounded))
                                .foregroundColor(Color(hex: t.done ? s.muted : s.text))
                                .strikethrough(t.done)
                                .lineLimit(1)
                                .truncationMode(.tail)
                            Spacer(minLength: 0)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(t.done ? "Mark as open" : "Mark as done")
                }
                if tasks.count > maxTasks {
                    Text("+\(tasks.count - maxTasks) more")
                        .font(.system(size: fontSize - 1, design: .rounded))
                        .foregroundColor(Color(hex: s.muted))
                }
            }
            .padding(.top, 2)
        }
    }

    private func hiddenKey(_ key: Character, _ action: @escaping () -> Void) -> some View {
        Button("", action: action)
            .keyboardShortcut(KeyEquivalent(key), modifiers: [])
            .buttonStyle(.plain)
            .opacity(0)
            .frame(width: 0, height: 0)
    }

    private func controls(_ s: TimerState) -> some View {
        HStack(spacing: 9) {
            iconButton(s.running ? "pause.fill" : "play.fill", help: s.running ? "Pause" : "Start") { send("toggle") }
            iconButton("forward.end.fill", help: "Skip phase") { send("skip") }
            iconButton("macwindow", help: "Back to terminal (w or double-click)") { send("restore") }
            iconButton("minus", help: "Hide (⌃⌥P shows it again)") { hide() }
        }
    }

    private func iconButton(_ name: String, help: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: name).font(.system(size: 10, weight: .bold))
                .frame(width: 14, height: 14)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .foregroundColor(.white.opacity(0.75))
        .help(help)
    }

    private func expanded(_ s: TimerState) -> some View {
        let next = s.phase.lowercased()
        return VStack(spacing: 12) {
            Text(s.message.isEmpty ? "Phase complete" : s.message)
                .font(.system(size: 20, weight: .semibold, design: .rounded))
                .foregroundColor(Color(hex: s.text))
            Text(clockString(s.remaining))
                .font(.system(size: 58, weight: .bold, design: .rounded).monospacedDigit())
                .foregroundStyle(gradient(s))
            Text("next up · \(next)")
                .font(.system(size: 12, weight: .medium, design: .rounded))
                .foregroundColor(Color(hex: s.muted))
            if let tasks = s.tasks, !tasks.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Tick off what you finished")
                        .font(.system(size: 11, weight: .medium, design: .rounded))
                        .foregroundColor(Color(hex: s.muted))
                    taskList(s, fontSize: 13)
                }
                .frame(maxWidth: 340, alignment: .leading)
            }
            HStack(spacing: 10) {
                Button(s.running ? "Continue" : "Start \(next)") { send("start") }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(PillButtonStyle(fill: [Color(hex: s.colorA), Color(hex: s.colorB)],
                                                 textColor: readableText(on: s.colorA), prominent: true))
                Button("Later") { shrink() }
                    .keyboardShortcut(.cancelAction)
                    .buttonStyle(secondaryStyle(s))
                Button("Open terminal") { send("restore") }
                    .buttonStyle(secondaryStyle(s))
                // space starts as well, like in the terminal
                Button("") { send("start") }
                    .keyboardShortcut(.space, modifiers: [])
                    .opacity(0)
                    .frame(width: 0, height: 0)
            }
            .padding(.top, 4)
            Text("space or return to start · esc for later")
                .font(.system(size: 11, design: .rounded))
                .foregroundColor(Color(hex: s.muted).opacity(0.8))
        }
        .padding(24)
    }
}

// MARK: - Window

final class Panel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    static var shared: AppDelegate?

    let model = Model()
    var panel: Panel!
    var statePath = ""
    var cmdPath = ""
    var lastEvent = -1
    var hiddenByUser = false
    var compactOrigin: NSPoint?
    var dragMouseStart: NSPoint?
    var dragWindowStart: NSPoint?
    var missingSince: Date?
    var timer: Timer?
    var hotKeyRef: EventHotKeyRef?

    var taskRows: Int {
        let n = model.state?.tasks?.count ?? 0
        return n == 0 ? 0 : min(n, maxTasks) + (n > maxTasks ? 1 : 0)
    }
    // measured content heights, the estimates are only used before the first layout
    var measuredCompact: CGFloat = 0
    var measuredExpanded: CGFloat = 0
    var compactSize: NSSize {
        let guess = 88 + (taskRows > 0 ? CGFloat(taskRows) * 19 + 6 : 0)
        return NSSize(width: 236, height: measuredCompact > 0 ? measuredCompact : guess)
    }
    var expandedSize: NSSize {
        let guess = 300 + (taskRows > 0 ? CGFloat(taskRows) * 22 + 30 : 0)
        return NSSize(width: 500, height: measuredExpanded > 0 ? measuredExpanded : guess)
    }

    /// SwiftUI reports how tall the content is, the window follows.
    fileprivate func contentHeightChanged(_ h: CGFloat) {
        guard h > 20 else { return }
        let height = ceil(h)
        if model.expanded {
            guard abs(height - measuredExpanded) > 1 else { return }
            measuredExpanded = height
            panel.setFrame(centerFrame(), display: true, animate: false)
        } else {
            guard abs(height - measuredCompact) > 1 else { return }
            measuredCompact = height
            guard panel.isVisible else { return }
            // keep the top edge where it is
            let old = panel.frame
            let frame = NSRect(x: old.minX, y: old.maxY - height, width: compactSize.width, height: height)
            panel.setFrame(frame, display: true, animate: false)
            compactOrigin = frame.origin
        }
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        AppDelegate.shared = self
        parseArguments()
        NSApp.setActivationPolicy(.accessory) // no dock icon

        panel = Panel(contentRect: NSRect(origin: .zero, size: compactSize),
                      styleMask: [.borderless, .nonactivatingPanel],
                      backing: .buffered, defer: false)
        panel.isFloatingPanel = true
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        panel.hidesOnDeactivate = false
        panel.appearance = NSAppearance(named: .darkAqua) // readable controls in light mode too

        let view = FloatView(
            model: model,
            send: { [weak self] in self?.send($0) },
            shrink: { [weak self] in self?.shrink() },
            hide: { [weak self] in self?.hideByUser() },
            dragChanged: { [weak self] in self?.dragChanged() },
            dragEnded: { [weak self] in self?.dragEnded() },
            contentHeight: { [weak self] in self?.contentHeightChanged($0) })
        // a solid dark backing in AppKit, so the window is never see through,
        // whatever SwiftUI draws on top
        let backing = NSView()
        backing.wantsLayer = true
        backing.layer?.backgroundColor = NSColor(srgbRed: 0.07, green: 0.07, blue: 0.10, alpha: 1).cgColor
        backing.layer?.cornerRadius = 16
        backing.layer?.masksToBounds = true
        let hosting = NSHostingView(rootView: view)
        if #available(macOS 13.0, *) { hosting.sizingOptions = [] } // the window size is ours
        hosting.frame = backing.bounds
        hosting.autoresizingMask = [.width, .height]
        backing.addSubview(hosting)
        panel.contentView = backing
        panel.setFrame(compactFrame(), display: true)
        panel.orderFrontRegardless()

        registerHotKey()
        timer = Timer.scheduledTimer(withTimeInterval: 0.2, repeats: true) { [weak self] _ in self?.poll() }
        poll()
    }

    private func parseArguments() {
        let args = CommandLine.arguments
        var i = 1
        while i < args.count {
            if args[i] == "--state", i + 1 < args.count { statePath = args[i + 1]; i += 1 }
            else if args[i] == "--cmd", i + 1 < args.count { cmdPath = args[i + 1]; i += 1 }
            i += 1
        }
        if statePath.isEmpty || cmdPath.isEmpty {
            let base = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Caches/superpomodoro")
            if statePath.isEmpty { statePath = base.appendingPathComponent("state.json").path }
            if cmdPath.isEmpty { cmdPath = base.appendingPathComponent("cmd").path }
        }
    }

    // MARK: polling

    private func poll() {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: statePath)),
              let s = try? JSONDecoder().decode(TimerState.self, from: data) else {
            if missingSince == nil { missingSince = Date() }
            if let since = missingSince, Date().timeIntervalSince(since) > 5 { NSApp.terminate(nil) }
            return
        }
        missingSince = nil
        // the terminal turned the mini window off, quit or crashed
        if !s.mini || kill(s.pid, 0) != 0 {
            NSApp.terminate(nil)
            return
        }
        model.state = s
        if s.image != model.imagePath {
            model.imagePath = s.image
            model.image = s.image.flatMap { NSImage(contentsOfFile: $0) }
        }
        if lastEvent < 0 {
            lastEvent = s.event
            return
        }
        if s.event != lastEvent {
            lastEvent = s.event
            expand()
        }
    }

    private func send(_ command: String) {
        let line = Data((command + "\n").utf8)
        if let handle = FileHandle(forWritingAtPath: cmdPath) {
            handle.seekToEndOfFile()
            handle.write(line)
            handle.closeFile()
        } else {
            FileManager.default.createFile(atPath: cmdPath, contents: line)
        }
        if command == "start" { shrink() }
    }

    // MARK: layout

    private var screen: NSScreen {
        panel.screen ?? NSScreen.main ?? NSScreen.screens[0]
    }

    private func compactFrame() -> NSRect {
        if let origin = compactOrigin { return NSRect(origin: origin, size: compactSize) }
        let vf = screen.visibleFrame
        return NSRect(x: vf.maxX - compactSize.width - 16,
                      y: vf.maxY - compactSize.height - 16,
                      width: compactSize.width, height: compactSize.height)
    }

    private func centerFrame() -> NSRect {
        let vf = screen.visibleFrame
        return NSRect(x: vf.midX - expandedSize.width / 2,
                      y: vf.midY - expandedSize.height / 2,
                      width: expandedSize.width, height: expandedSize.height)
    }

    /// Phase ended: come to the center, grow, stay above everything.
    private func expand() {
        if !model.expanded && panel.isVisible { compactOrigin = panel.frame.origin }
        model.expanded = true
        panel.level = .screenSaver
        if !panel.isVisible { panel.setFrame(compactFrame(), display: false) }
        panel.orderFrontRegardless()
        panel.setFrame(centerFrame(), display: true, animate: true)
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKey()
        // with auto-start on, the next phase is already running: tuck away by itself
        if model.state?.auto == true {
            let ev = lastEvent
            DispatchQueue.main.asyncAfter(deadline: .now() + 6) { [weak self] in
                if self?.lastEvent == ev, self?.model.expanded == true { self?.shrink() }
            }
        }
    }

    /// Back to the corner (or out of sight, if the user had hidden it).
    private func shrink() {
        model.expanded = false
        panel.level = .floating
        panel.setFrame(compactFrame(), display: true, animate: true)
        if hiddenByUser { panel.orderOut(nil) }
    }

    private func hideByUser() {
        hiddenByUser = true
        panel.orderOut(nil)
    }

    fileprivate func toggleVisible() {
        if panel.isVisible && !model.expanded {
            hideByUser()
        } else if !panel.isVisible {
            hiddenByUser = false
            panel.setFrame(compactFrame(), display: true)
            panel.orderFrontRegardless()
        }
    }

    // MARK: dragging (screen coordinates, so the window never jitters)

    private func dragChanged() {
        let mouse = NSEvent.mouseLocation
        if dragMouseStart == nil {
            dragMouseStart = mouse
            dragWindowStart = panel.frame.origin
        }
        guard let m0 = dragMouseStart, let w0 = dragWindowStart else { return }
        panel.setFrameOrigin(NSPoint(x: w0.x + mouse.x - m0.x, y: w0.y + mouse.y - m0.y))
    }

    private func dragEnded() {
        dragMouseStart = nil
        dragWindowStart = nil
        if !model.expanded { compactOrigin = panel.frame.origin }
    }

    // MARK: global hotkey ⌃⌥P shows or hides the mini window

    private func registerHotKey() {
        var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard),
                                 eventKind: UInt32(kEventHotKeyPressed))
        InstallEventHandler(GetApplicationEventTarget(), { _, _, _ in
            DispatchQueue.main.async { AppDelegate.shared?.toggleVisible() }
            return OSStatus(noErr)
        }, 1, &spec, nil, nil)
        let id = EventHotKeyID(signature: OSType(0x5350_4D46), id: 1) // "SPMF"
        RegisterEventHotKey(UInt32(kVK_ANSI_P), UInt32(controlKey | optionKey),
                            id, GetApplicationEventTarget(), 0, &hotKeyRef)
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
