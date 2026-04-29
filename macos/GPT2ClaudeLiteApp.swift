import SwiftUI
import AppKit

struct RuntimeConfig: Codable {
    var base_url: String
    var api_key: String
    var model: String
}

@main
struct GPT2ClaudeLiteApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
                .frame(minWidth: 760, minHeight: 640)
                .onAppear {
                    model.load()
                }
        }
        .commands {
            CommandGroup(replacing: .newItem) {}
        }
    }
}

@MainActor
final class AppModel: ObservableObject {
    @Published var upstreamBaseURL = ""
    @Published var upstreamAPIKey = ""
    @Published var apiKeySaved = false
    @Published var model = "gpt-5.5"
    @Published var fastModel = "gpt-5.5"
    @Published var subagentModel = "gpt-5.5"
    @Published var effortLevel = "max"
    @Published var port = "43501"
    @Published var status = "Idle"
    @Published var isRunning = false

    private var proxyProcess: Process?

    var localBaseURL: String {
        "http://127.0.0.1:\(port)"
    }

    var runtimeConfigURL: URL {
        home.appendingPathComponent(".gpt2claude-lite/config.json")
    }

    var claudeSettingsURL: URL {
        home.appendingPathComponent(".claude/settings.json")
    }

    private var home: URL {
        FileManager.default.homeDirectoryForCurrentUser
    }

    func load() {
        guard let data = try? Data(contentsOf: runtimeConfigURL),
              let config = try? JSONDecoder().decode(RuntimeConfig.self, from: data)
        else {
            return
        }
        upstreamBaseURL = config.base_url
        model = config.model.isEmpty ? "gpt-5.5" : config.model
        fastModel = model
        subagentModel = model
        apiKeySaved = !config.api_key.isEmpty
    }

    func oneClickConfigure() {
        do {
            try saveRuntimeConfig()
            let backup = try writeClaudeSettings()
            if !isRunning {
                try startProxy()
            }
            status = backup == nil
                ? "Configured Claude Code and started proxy"
                : "Configured Claude Code; backup: \(backup!.lastPathComponent)"
        } catch {
            status = "Error: \(error.localizedDescription)"
        }
    }

    func saveRuntimeConfig() throws {
        let keyToSave: String
        if upstreamAPIKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
           let existing = try? Data(contentsOf: runtimeConfigURL),
           let config = try? JSONDecoder().decode(RuntimeConfig.self, from: existing) {
            keyToSave = config.api_key
        } else {
            keyToSave = upstreamAPIKey.trimmingCharacters(in: .whitespacesAndNewlines)
        }

        let config = RuntimeConfig(
            base_url: upstreamBaseURL.trimmingCharacters(in: .whitespacesAndNewlines).trimmingCharacters(in: CharacterSet(charactersIn: "/")),
            api_key: keyToSave,
            model: model.trimmingCharacters(in: .whitespacesAndNewlines)
        )
        try FileManager.default.createDirectory(at: runtimeConfigURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        let data = try JSONEncoder.pretty.encode(config)
        try data.write(to: runtimeConfigURL, options: [.atomic])
        apiKeySaved = !keyToSave.isEmpty
        upstreamAPIKey = ""
    }

    @discardableResult
    func writeClaudeSettings() throws -> URL? {
        try FileManager.default.createDirectory(at: claudeSettingsURL.deletingLastPathComponent(), withIntermediateDirectories: true)

        var settings: [String: Any] = [:]
        var backupURL: URL?
        if FileManager.default.fileExists(atPath: claudeSettingsURL.path) {
            let existing = try Data(contentsOf: claudeSettingsURL)
            backupURL = claudeSettingsURL.deletingLastPathComponent()
                .appendingPathComponent("settings.json.bak-\(Self.timestamp())")
            try existing.write(to: backupURL!, options: [.atomic])
            if let obj = try JSONSerialization.jsonObject(with: existing) as? [String: Any] {
                settings = obj
            }
        }

        var env = settings["env"] as? [String: Any] ?? [:]
        env["ANTHROPIC_BASE_URL"] = localBaseURL
        env["ANTHROPIC_AUTH_TOKEN"] = "test"
        env["ANTHROPIC_MODEL"] = model
        env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = model
        env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = model
        env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = fastModel
        env["CLAUDE_CODE_SUBAGENT_MODEL"] = subagentModel
        env["CLAUDE_CODE_EFFORT_LEVEL"] = effortLevel
        settings["env"] = env
        settings["model"] = model
        settings["effortLevel"] = effortLevel

        let data = try JSONSerialization.data(withJSONObject: settings, options: [.prettyPrinted, .sortedKeys])
        try data.write(to: claudeSettingsURL, options: [.atomic])
        return backupURL
    }

    func startProxy() throws {
        if isRunning {
            return
        }
        try saveRuntimeConfig()
        guard let proxyURL = Bundle.main.url(forResource: "gpt2claude-lite", withExtension: nil) else {
            throw NSError(domain: "GPT2ClaudeLite", code: 1, userInfo: [NSLocalizedDescriptionKey: "Bundled proxy binary not found"])
        }
        let process = Process()
        process.executableURL = proxyURL
        process.arguments = ["--host", "127.0.0.1", "--port", port]
        process.environment = ProcessInfo.processInfo.environment
        process.terminationHandler = { [weak self] _ in
            Task { @MainActor in
                self?.isRunning = false
                self?.proxyProcess = nil
                self?.status = "Proxy stopped"
            }
        }
        try process.run()
        proxyProcess = process
        isRunning = true
        status = "Proxy running at \(localBaseURL)"
    }

    func stopProxy() {
        proxyProcess?.terminate()
        proxyProcess = nil
        isRunning = false
        status = "Proxy stopped"
    }

    func copyExports() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(shellExports, forType: .string)
        status = "Copied exports"
    }

    func openClaudeSettingsFolder() {
        NSWorkspace.shared.activateFileViewerSelecting([claudeSettingsURL])
    }

    var shellExports: String {
        """
        export ANTHROPIC_BASE_URL="\(localBaseURL)"
        export ANTHROPIC_AUTH_TOKEN="test"
        export ANTHROPIC_MODEL="\(model)"
        export ANTHROPIC_DEFAULT_OPUS_MODEL="\(model)"
        export ANTHROPIC_DEFAULT_SONNET_MODEL="\(model)"
        export ANTHROPIC_DEFAULT_HAIKU_MODEL="\(fastModel)"
        export CLAUDE_CODE_SUBAGENT_MODEL="\(subagentModel)"
        export CLAUDE_CODE_EFFORT_LEVEL="\(effortLevel)"
        """
    }

    private static func timestamp() -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyyMMdd-HHmmss"
        return formatter.string(from: Date())
    }
}

struct ContentView: View {
    @EnvironmentObject private var app: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("gpt2claude-lite")
                        .font(.title2.weight(.semibold))
                    Text(app.isRunning ? "Running at \(app.localBaseURL)" : "Ready")
                        .foregroundStyle(app.isRunning ? .green : .secondary)
                }
                Spacer()
                Button(app.isRunning ? "Stop Proxy" : "Start Proxy") {
                    if app.isRunning {
                        app.stopProxy()
                    } else {
                        do { try app.startProxy() } catch { app.status = "Error: \(error.localizedDescription)" }
                    }
                }
                .buttonStyle(.borderedProminent)
            }

            GroupBox("Upstream") {
                Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 12) {
                    GridRow {
                        Text("Base URL")
                        TextField("https://your-openai-compatible-host/v1", text: $app.upstreamBaseURL)
                            .textFieldStyle(.roundedBorder)
                    }
                    GridRow {
                        Text("API Key")
                        SecureField(app.apiKeySaved ? "Saved key present; leave blank to keep it" : "Paste your upstream API key", text: $app.upstreamAPIKey)
                            .textFieldStyle(.roundedBorder)
                    }
                    GridRow {
                        Text("Model")
                        TextField("gpt-5.5", text: $app.model)
                            .textFieldStyle(.roundedBorder)
                    }
                    GridRow {
                        Text("Port")
                        TextField("43501", text: $app.port)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 120)
                    }
                }
                .padding(.vertical, 6)
            }

            GroupBox("Claude Code") {
                Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 12) {
                    GridRow {
                        Text("Fast model")
                        TextField("gpt-5.5", text: $app.fastModel)
                            .textFieldStyle(.roundedBorder)
                    }
                    GridRow {
                        Text("Subagent model")
                        TextField("gpt-5.5", text: $app.subagentModel)
                            .textFieldStyle(.roundedBorder)
                    }
                    GridRow {
                        Text("Effort")
                        Picker("", selection: $app.effortLevel) {
                            ForEach(["max", "xhigh", "high", "medium", "low", "auto"], id: \.self) { value in
                                Text(value).tag(value)
                            }
                        }
                        .pickerStyle(.segmented)
                    }
                }
                .padding(.vertical, 6)
            }

            TextEditor(text: .constant(app.shellExports))
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 120)
                .padding(6)
                .background(Color(nsColor: .textBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 8))

            HStack {
                Button("One-click Configure") {
                    app.oneClickConfigure()
                }
                .buttonStyle(.borderedProminent)

                Button("Copy Exports") {
                    app.copyExports()
                }

                Button("Show Config") {
                    app.openClaudeSettingsFolder()
                }

                Spacer()
            }

            Text(app.status)
                .font(.footnote)
                .foregroundStyle(.secondary)

            Spacer(minLength: 0)
        }
        .padding(24)
    }
}

private extension JSONEncoder {
    static var pretty: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        return encoder
    }
}
