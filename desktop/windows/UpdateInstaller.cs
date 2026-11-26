using System.Diagnostics;
using System.Security.Cryptography;
using System.Text.Json;

namespace Cage.Windows;

// The window of an update, as the helper drives it; see UpdateProgress.
// Texts are native keys with {0} for a version, so the window translates.
internal interface IUpdateProgressView : IDisposable
{
    void Step(int index, int? percent = null);
    void Status(string format, string version);
    // The update ended without the new version; the window keeps the result
    // until it is closed. canOpen: the installation may be started.
    void Fail(string format, string version, string? detail, string target, bool canOpen);
    // In front again once the App has handed over and closed its window.
    void Raise();
    Task Closed { get; }
}

internal static class UpdateSteps
{
    public const int Backup = 0, StopEnvironment = 1, Install = 2, StartNew = 3;
}

internal static class UpdateInstaller
{
    [System.Runtime.InteropServices.DllImport("user32.dll", CharSet = System.Runtime.InteropServices.CharSet.Unicode)]
    private static extern int MessageBoxW(IntPtr owner, string text, string title, uint flags);
    public static void VerifyArchive(string path, string hash, string version, string publisher)
    {
        UpdatePolicy.DownloadPath(path, AppUpdater.Root); UpdatePolicy.PlainPath(path);
        if (new FileInfo(path).Length is <= 0 or > (2L << 30)) throw new IOException("安装包大小无效");
        using var stream = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.Read);
        var actual = Convert.ToHexString(SHA256.HashData(stream));
        if (hash.Length != 64 || !string.Equals(actual, hash, StringComparison.OrdinalIgnoreCase))
            throw new IOException("安装包下载后发生变化，请重新下载");
        Authenticode.SamePublisher(path, publisher);
        var metadata = FileVersionInfo.GetVersionInfo(path);
        // Installers built before the Cage lite rename name the product "Cage".
        if (metadata.ProductName is not ("Cage lite" or "Cage") || metadata.ProductVersion != version)
            throw new IOException("安装包产品身份或版本不匹配");
    }

    // openProgress gets the versions and the installation folder, and may
    // give no window: the update then runs without one, as before 0.2.3.
    public static async Task<int> Run(string? installerDiagnostics = null, Func<string, string, IUpdateProgressView?>? openProgress = null)
    {
        InstallPlan? plan = null;
        string? backup = null;
        var installerStarted = false;
        var committed = false;
        var succeeded = false;
        Process? launched = null;
        IUpdateProgressView? progress = null;
        var previousVersion = "";
        // A failure shown in the window stays until it is closed.
        var keepWindow = false;
        try
        {
            using var inputTimeout = new CancellationTokenSource(TimeSpan.FromMinutes(5));
            var line = await Console.In.ReadLineAsync(inputTimeout.Token);
            if (line == null || line.Length > 16384) return 1;
            plan = JsonSerializer.Deserialize<InstallPlan>(line) ?? throw new IOException("安装计划无效");
            if (!Guid.TryParse(plan.Confirmation, out _) || !Path.IsPathFullyQualified(plan.Target) || plan.Target.Contains('"') ||
                plan.Target.TrimEnd(Path.DirectorySeparatorChar) != plan.Target ||
                string.Equals(Path.GetPathRoot(plan.Target), plan.Target, StringComparison.OrdinalIgnoreCase))
                throw new IOException("安装目标无效");
            UpdatePolicy.PlainPath(plan.Target); UpdatePolicy.PlainPath(AppUpdater.Root);
            // Also release an inherited install-directory lock when this helper
            // is invoked by a launcher that did not choose a safe directory.
            Directory.SetCurrentDirectory(AppUpdater.Root);
            if (File.ReadAllText(Path.Combine(plan.Target, "Cage.install-id")).Trim() != "io.github.lee-xeo.claude-cage")
                throw new IOException("请先使用 Cage lite 安装程序安装应用，再自动更新");
            await HealPendingTransaction(plan.Target);
            var original = Path.Combine(plan.Target, "Cage.exe");
            string targetPublisher;
            if (plan.UnsignedSource)
            {
                if (!string.Equals(plan.Publisher, Authenticode.OfficialPublisherKey, StringComparison.Ordinal))
                    throw new IOException("安装计划发布者不匹配");
                targetPublisher = Authenticode.OfficialPublisherKey;
            }
            else
            {
                var ownPublisher = Authenticode.OwnPublisher();
                if (plan.Publisher != ownPublisher) throw new IOException("安装计划发布者不匹配");
                Authenticode.SamePublisher(original, ownPublisher);
                targetPublisher = ownPublisher;
            }
            previousVersion = FileVersionInfo.GetVersionInfo(original).ProductVersion ?? "";
            if (!UpdatePolicy.IsNewer(plan.Version, previousVersion))
                throw new IOException("更新版本必须高于当前安装版本");
            VerifyArchive(plan.Archive, plan.Digest, plan.Version, targetPublisher);
            // Hold the verified file open without write/delete sharing until the
            // installer exits. Another process cannot swap it after verification.
            await using var archiveLock = new FileStream(plan.Archive, FileMode.Open, FileAccess.Read, FileShare.Read);
            VerifyArchive(plan.Archive, plan.Digest, plan.Version, targetPublisher);
            using var parent = Process.GetProcessById(plan.ParentPid);
            if (parent.StartTime.ToUniversalTime().Ticks != plan.ParentStartTicks ||
                !string.Equals(parent.MainModule?.FileName, original, StringComparison.OrdinalIgnoreCase))
                throw new IOException("更新请求进程身份不匹配");
            backup = Path.Combine(AppUpdater.Root, "previous-" + plan.Confirmation);
            if (Directory.Exists(backup)) throw new IOException("更新备份目录已存在");
            // From here to the new version's confirmation. Before the commit the
            // App still shows its own state; a start of it meanwhile brings
            // this window forward instead of holding the program folder.
            try { progress = openProgress?.Invoke(previousVersion + "  →  " + plan.Version, plan.Target); }
            catch (Exception error) when (error is IOException or InvalidOperationException or UnauthorizedAccessException) { }
            progress?.Step(UpdateSteps.Backup);
            // Preparation is reversible and completes before the controller stops.
            CopyTree(plan.Target, backup);
            if (plan.UnsignedSource)
            {
                var backupBackend = UpdateRecovery.BackendPath(backup);
                UpdatePolicy.PlainPath(backupBackend);
                if (!File.Exists(backupBackend)) throw new IOException("原应用备份缺少必要组件");
            }
            else
            {
                Authenticode.SamePublisher(UpdateRecovery.BackendPath(backup), targetPublisher);
            }
            progress?.Step(UpdateSteps.StopEnvironment);
            Console.WriteLine("READY"); await Console.Out.FlushAsync();
            if (await Console.In.ReadLineAsync(inputTimeout.Token) != "COMMIT") return 1;
            committed = true;
            progress?.Step(UpdateSteps.Install, 0);
            await parent.WaitForExitAsync().WaitAsync(TimeSpan.FromSeconds(45));
            progress?.Raise();
            VerifyArchive(plan.Archive, plan.Digest, plan.Version, targetPublisher);
            // NSIS requires /D= to be the final, unquoted argument; the executable
            // path is passed separately and no command shell is involved.
            // The installer writes its phase there, which the window follows.
            var diagnostics = installerDiagnostics ?? Path.Combine(AppUpdater.Root, "install-" + plan.Confirmation + ".json");
            using (var installer = Process.Start(new ProcessStartInfo(plan.Archive) {
                UseShellExecute = false, CreateNoWindow = true,
                Arguments = "/S /UPDATE " + $"/DIAGNOSTICS=\"{diagnostics}\" " + "/D=" + plan.Target,
                WorkingDirectory = Path.GetDirectoryName(plan.Archive),
            }) ?? throw new IOException("无法启动 Windows 安装器"))
            {
                installerStarted = true;
                var installDeadline = DateTime.UtcNow.AddMinutes(10);
                var exited = installer.WaitForExitAsync();
                while (!exited.IsCompleted)
                {
                    if (DateTime.UtcNow >= installDeadline)
                    {
                        if (!installer.HasExited) installer.Kill(true);
                        await installer.WaitForExitAsync();
                        throw new TimeoutException("Windows 安装程序超过 10 分钟未完成");
                    }
                    progress?.Step(UpdateSteps.Install, InstallPercent(InstallerPhase(diagnostics)));
                    await Task.WhenAny(exited, Task.Delay(500));
                }
                if (installer.ExitCode != 0)
                    throw new IOException($"Windows 安装程序未完成安装（退出码 {installer.ExitCode}，阶段 {InstallerPhase(diagnostics) ?? "未知"}）");
            }
            progress?.Step(UpdateSteps.Install, 100);
            EnsureInstallationComplete(plan.Target);
            Authenticode.SamePublisher(original, targetPublisher);
            if (FileVersionInfo.GetVersionInfo(original).ProductVersion != plan.Version)
                throw new IOException("安装后版本不匹配");
            progress?.Step(UpdateSteps.StartNew);
            var marker = Path.Combine(AppUpdater.Root, "launch-" + plan.Confirmation + ".json");
            if (File.Exists(marker)) File.Delete(marker);
            var launch = new ProcessStartInfo(original) { UseShellExecute = false, WorkingDirectory = plan.Target };
            launch.Environment["CAGE_UPDATE_CONFIRMATION"] = plan.Confirmation;
            launched = Process.Start(launch) ?? throw new IOException("新版 Cage lite 无法启动");
            var deadline = DateTime.UtcNow.AddSeconds(120);
            var ready = false;
            while (DateTime.UtcNow < deadline && !launched.HasExited)
            {
                if (File.Exists(marker) && new FileInfo(marker).Length < 16384)
                {
                    try
                    {
                        var confirmation = JsonDocument.Parse(await File.ReadAllTextAsync(marker)).RootElement;
                        ready = confirmation.Text("version") == plan.Version;
                    }
                    catch (IOException) { }
                    catch (JsonException) { }
                    if (ready) break;
                }
                await Task.Delay(500);
            }
            if (!ready) throw new IOException("新版工作台未能完成启动确认");
            succeeded = true;
            Result(true, "Cage lite 已更新至 " + plan.Version);
            // The new version's window shows now: this one closes before the
            // backup is deleted below, which takes seconds for a large folder.
            // Meanwhile a start of the App must reach the new version, not
            // bring this window forward.
            progress?.Dispose(); progress = null;
            // The currently executing helper cannot delete itself on Windows.
            // Keep this small signed file; the next successful update may remove it.
            return 0;
        }
        catch (Exception error)
        {
            if (succeeded) return 0;
            var detail = error.Message;
            var recoveryIncomplete = false;
            if (committed && plan != null && backup != null)
            {
                progress?.Status("正在恢复 {0}", previousVersion);
                try
                {
                    // NSIS and this helper both require a newly created named
                    // object and retain its handle for the entire transaction.
                    using var installationLock = new Mutex(false, "Global\\Cage.Installation", out var ownsInstallation);
                    if (!ownsInstallation) throw new IOException("另一个安装或卸载正在进行，已保留更新备份，请等待完成后恢复。");
                    EnsureInstallationComplete(plan.Target);
                    if (UpdateRecovery.OriginalIntact(plan.Target, backup))
                    {
                        if (!plan.UnsignedSource) Authenticode.SamePublisher(Path.Combine(plan.Target, "Cage.exe"), plan.Publisher);
                        Result(false, "更新未完成，原应用已保留：" + detail);
                        UpdateRecovery.CleanupDirectory(backup); backup = null;
                        keepWindow = Finish(progress, plan.Target, "已保留 {0}", previousVersion, detail);
                        return 1;
                    }
                    if (!installerStarted) throw new IOException("原应用在安装前发生变化，备份已保留，请核对安装目录后重试。");
                    // Stop the exact new controller through its lifecycle API before
                    // restoring files. Never kill other Cage instances by name.
                    if (!plan.UnsignedSource) Authenticode.SamePublisher(UpdateRecovery.BackendPath(backup), plan.Publisher);
                    await UpdateRecovery.PrepareRollback(backup);
                    var appStopped = false;
                    if (launched is { HasExited: false }) { launched.Kill(); await launched.WaitForExitAsync(); appStopped = true; }
                    // A stopped App cannot restart the controller; stop the one it
                    // may have started since the first lifecycle request.
                    if (await UpdateRecovery.StopInstalledApps(plan.Target) || appStopped) await UpdateRecovery.PrepareRollback(backup);
                    await UpdateRecovery.WaitForInstalledProcesses(plan.Target, TimeSpan.FromSeconds(30));
                    var restored = plan.Target + ".restore-" + plan.Confirmation;
                    var failed = plan.Target + ".failed-" + plan.Confirmation;
                    CopyTree(backup, restored);
                    var receipt = plan.Target + ".cage-installation.json";
                    await PreserveFiles("prepare", plan.Target, restored, receipt, failed);
                    // The installation folder and its CageData stay in place,
                    // as in NSIS: only the other top-level entries move.
                    if (!await Detach(plan.Target, backup, failed, receipt))
                    {
                        // Each failed detach put every entry back: the folder holds
                        // the new version as installed. Finishing the receipt keeps
                        // later installations from stopping at it (customer report,
                        // 0.2.2); the previous App stays in the backup.
                        await PreserveFiles("complete", "", plan.Target, receipt);
                        UpdateRecovery.CleanupDirectory(restored);
                        detail += "。原应用无法恢复，已保留新版，程序备份保留在 " + backup;
                        Result(false, "更新未完成：" + detail);
                        backup = null;
                        keepWindow = Finish(progress, plan.Target, "已保留 {0}", plan.Version, detail);
                        return 1;
                    }
                    try
                    {
                        await PreserveFiles("move", failed, restored, receipt);
                        await PreserveFiles("attach", restored, plan.Target, receipt);
                    }
                    catch
                    {
                        await PreserveFiles("restore", restored, failed, receipt);
                        await PreserveFiles("attach", failed, plan.Target, receipt);
                        await PreserveFiles("complete", "", plan.Target, receipt);
                        throw;
                    }
                    await UpdateRecovery.StopDetachedCandidate(backup, failed, TimeSpan.FromSeconds(30));
                    await PreserveFiles("cleanup", failed, plan.Target, receipt);
                    EnsureInstallationComplete(plan.Target);
                    UpdateRecovery.CleanupDirectory(backup); backup = null;
                    Result(false, "更新未完成，已恢复原应用：" + detail);
                    keepWindow = Finish(progress, plan.Target, "已恢复 {0}", previousVersion, detail);
                    return 1;
                }
                catch (Exception recovery)
                {
                    recoveryIncomplete = true;
                    detail += "。自动恢复未完成。" + recovery.Message;
                    if (File.Exists(plan.Target + ".cage-installation.json"))
                        detail += "。恢复目录记录在 " + plan.Target + ".cage-installation.json";
                    if (backup != null) detail += "。程序备份保留在 " + backup;
                }
            }
            Result(false, "更新未完成：" + detail);
            if (recoveryIncomplete && progress != null && plan != null)
            {
                progress.Fail("恢复未完成", "", detail, plan.Target, false);
                keepWindow = true;
            }
            else if (recoveryIncomplete && OperatingSystem.IsWindows()) MessageBoxW(IntPtr.Zero, detail, "Cage lite 更新需要恢复", 0x00010010);
            return 1;
        }
        finally
        {
            launched?.Dispose();
            if (succeeded && plan != null && backup != null)
                UpdateRecovery.CleanupAfterSuccess(backup, Path.GetDirectoryName(plan.Archive)!, Path.Combine(AppUpdater.Root, "launch-" + plan.Confirmation + ".json"));
            else if (!committed) UpdateRecovery.CleanupDirectory(backup);
            if (progress != null)
            {
                if (keepWindow) await progress.Closed;
                progress.Dispose();
            }
        }
    }

    // The result is shown in the window, which offers to start the App;
    // without a window the App starts at once, as before. True when the
    // window shows it.
    private static bool Finish(IUpdateProgressView? progress, string target, string format, string version, string detail)
    {
        if (progress != null)
        {
            progress.Fail(format, version, detail, target, true);
            return true;
        }
        Process.Start(new ProcessStartInfo(Path.Combine(target, "Cage.exe")) { UseShellExecute = false, WorkingDirectory = target });
        return false;
    }

    // A start of the App since its processes were stopped holds the program
    // folder (customer report, 0.2.2). Stop it and try again; false when
    // the entries still cannot move.
    private static async Task<bool> Detach(string target, string backup, string failed, string receipt)
    {
        for (var attempt = 1; ; attempt++)
        {
            try
            {
                await PreserveFiles("detach", target, failed, receipt);
                return true;
            }
            catch (IOException) when (attempt < 3)
            {
                try
                {
                    if (await UpdateRecovery.StopInstalledApps(target)) await UpdateRecovery.PrepareRollback(backup);
                    await UpdateRecovery.WaitForInstalledProcesses(target, TimeSpan.FromSeconds(30));
                }
                catch (IOException) { return false; }
            }
            catch (IOException) { return false; }
        }
    }

    // An earlier rollback that moved nothing left its receipt: archive it,
    // as the installer does, before refusing a receipt that still needs one.
    public static async Task HealPendingTransaction(string target)
    {
        var receipt = target + ".cage-installation.json";
        if (!File.Exists(receipt)) return;
        try { await PreserveFiles("heal", "", target, receipt); }
        catch (IOException) { }
        UpdateRecovery.EnsureNoPendingTransaction(target);
    }

    private static string? InstallerPhase(string diagnostics)
    {
        try
        {
            if (!File.Exists(diagnostics) || new FileInfo(diagnostics).Length > 4096) return null;
            using var stream = new FileStream(diagnostics, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete);
            using var document = JsonDocument.Parse(stream);
            return document.RootElement.Text("phase") is { Length: > 0 } phase ? phase : null;
        }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException or JsonException) { return null; }
    }

    // The installer's phases (Cage.nsi InstallPhase) in their order; unpacking
    // the payload takes most of the time.
    internal static int? InstallPercent(string? phase) => phase switch
    {
        null or "initializing" or "acquiring_installation_lock" or "healing_transaction_record" => 2,
        "checking_destination" => 5,
        "unpacking_application" => 10,
        "detecting_webview_runtime" => 45,
        "installing_webview_runtime" => 50,
        "closing_reopened_application" => 58,
        "preparing_file_transaction" => 60,
        "moving_previous_application" => 70,
        "moving_personal_files" => 75,
        "publishing_application" => 80,
        "cleaning_previous_application" or "completing_file_transaction" => 88,
        "registering_installation" => 94,
        "trusting_publisher" or "adding_user_path" => 97,
        "completed" => 100,
        _ => null,
    };

    private static void Result(bool success, string message)
    {
        try
        {
            Directory.CreateDirectory(AppUpdater.Root); UpdatePolicy.PlainPath(AppUpdater.Root);
            File.WriteAllText(Path.Combine(AppUpdater.Root, "last-result.json"), JsonSerializer.Serialize(new { success, message }));
        }
        catch (IOException) { }
        catch (UnauthorizedAccessException) { }
    }

    private static void EnsureInstallationComplete(string target)
    {
        var receipt = target + ".cage-installation.json";
        if (!File.Exists(receipt) && Directory.Exists(target)) return;
        var message = "安装目录或个人文件仍需恢复，已停止自动启动。请保留并检查 " + receipt;
        if (File.Exists(receipt) && new FileInfo(receipt).Length <= 16 * 1024 * 1024)
        {
            try
            {
                using var document = JsonDocument.Parse(File.ReadAllText(receipt));
                foreach (var name in new[] { "stagePath", "backupPath" })
                    if (document.RootElement.Text(name) is { Length: > 0 } path) message += "。保留目录 " + path;
            }
            catch (Exception error) when (error is IOException or JsonException or InvalidOperationException) { }
        }
        throw new IOException(message);
    }

    private static async Task PreserveFiles(string operation, string source, string target, string receipt, string? backupPath = null)
    {
        var directory = receipt + ".helper-" + Guid.NewGuid().ToString("N");
        Directory.CreateDirectory(directory);
        var script = Path.Combine(directory, "installation-files.ps1");
        try
        {
            await using (var resource = typeof(UpdateInstaller).Assembly.GetManifestResourceStream("Cage.InstallationFiles.ps1")
                ?? throw new IOException("缺少安装文件保留组件"))
            await using (var output = File.Create(script)) { await resource.CopyToAsync(output); }
            await using (var resource = typeof(UpdateInstaller).Assembly.GetManifestResourceStream("Cage.VerifiedFileDeletion.cs")
                ?? throw new IOException("缺少安装文件校验组件"))
            await using (var output = File.Create(Path.Combine(directory, "verified-file-deletion.cs"))) { await resource.CopyToAsync(output); }
            using var deadline = new CancellationTokenSource(TimeSpan.FromMinutes(10));
            var executable = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.System), "WindowsPowerShell", "v1.0", "powershell.exe");
            var arguments = new List<string> { "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
                "-Operation", operation, "-Source", source, "-Target", target, "-Receipt", receipt, "-LegacyTarget", "-KeepInPlace", "CageData" };
            if (backupPath != null) { arguments.Add("-Backup"); arguments.Add(backupPath); }
            // Which step and kind of entry stopped it, never a path or name.
            var diagnostic = Path.Combine(directory, "diagnostic.json");
            arguments.Add("-Diagnostics"); arguments.Add(diagnostic);
            using var process = new Process { StartInfo = Backend.StartInfo(executable, arguments.ToArray()) };
            if (!process.Start()) throw new IOException("无法启动安装文件保留组件");
            using var abort = deadline.Token.Register(() => { try { if (!process.HasExited) process.Kill(); } catch (InvalidOperationException) { } });
            var outputText = Backend.ReadLimited(process.StandardOutput, 65536, deadline.Token);
            var errorText = Backend.ReadLimited(process.StandardError, 65536, deadline.Token);
            await process.WaitForExitAsync(deadline.Token);
            await outputText;
            var error = await errorText;
            if (process.ExitCode != 0)
            {
                var reason = "";
                try { if (File.Exists(diagnostic) && new FileInfo(diagnostic).Length <= 8192) reason = " " + File.ReadAllText(diagnostic).Trim(); }
                catch (Exception readError) when (readError is IOException or UnauthorizedAccessException) { }
                throw new IOException("安装文件保留未完成：" + error.Trim() + reason);
            }
        }
        finally
        {
            UpdateRecovery.CleanupDirectory(directory);
        }
    }

    private static void CopyTree(string source, string target)
    {
        source = Path.GetFullPath(source).TrimEnd(Path.DirectorySeparatorChar);
        target = Path.GetFullPath(target).TrimEnd(Path.DirectorySeparatorChar);
        var comparison = OperatingSystem.IsWindows() ? StringComparison.OrdinalIgnoreCase : StringComparison.Ordinal;
        if (string.Equals(source, target, comparison) || target.StartsWith(source + Path.DirectorySeparatorChar, comparison))
            throw new InvalidDataException("更新备份目录位于程序目录内，无法安全备份。请使用完整安装包手动更新。");
        UpdatePolicy.PlainPath(source); UpdatePolicy.PlainPath(target); Directory.CreateDirectory(target);
        HashSet<string>? owned = null;
        var manifest = Path.Combine(source, "Cage.install-files.json");
        if (File.Exists(manifest) && (File.GetAttributes(manifest) & FileAttributes.ReparsePoint) == 0)
        {
            try
            {
                using var document = JsonDocument.Parse(File.ReadAllText(manifest));
                if (document.RootElement.GetProperty("version").GetInt32() == 1)
                {
                    var paths = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
                    foreach (var entry in document.RootElement.GetProperty("files").EnumerateArray())
                    {
                        var path = entry.GetProperty("path").GetString();
                        var digest = entry.GetProperty("sha256").GetString();
                        if (string.IsNullOrEmpty(path) || path.StartsWith('/') || path.Contains('\\') || path.Contains(':') ||
                            path.Split('/').Any(part => part is "." or "..") || digest?.Length != 64 || !digest.All(char.IsAsciiHexDigit))
                            throw new JsonException("Invalid installation file record");
                        paths.Add(path);
                    }
                    if (paths.Count > 0) { paths.Add("Cage.install-files.json"); owned = paths; }
                }
            }
            catch (Exception error) when (error is IOException or JsonException or InvalidOperationException or KeyNotFoundException or FormatException) { }
        }
        long size = 0; var count = 0;
        var pending = new Queue<string>(); pending.Enqueue(source);
        while (pending.TryDequeue(out var directory))
        foreach (var entry in Directory.EnumerateFileSystemEntries(directory))
        {
            var attributes = File.GetAttributes(entry);
            if (++count > 100000) throw new IOException("安装目录文件过多，无法安全备份");
            // User links remain in the installed tree. NSIS and rollback move
            // them with their user subtree, preserving link identity and targets.
            if ((attributes & FileAttributes.ReparsePoint) != 0) continue;
            var relative = Path.GetRelativePath(source, entry);
            var key = relative.Replace(Path.DirectorySeparatorChar, '/');
            if (owned != null && !owned.Contains(key) && !owned.Any(path => path.StartsWith(key + "/", StringComparison.OrdinalIgnoreCase))) continue;
            var destination = Path.Combine(target, relative);
            if ((attributes & FileAttributes.Directory) != 0) { Directory.CreateDirectory(destination); pending.Enqueue(entry); }
            else
            {
                size += new FileInfo(entry).Length;
                if (size > (4L << 30)) throw new IOException("安装目录超过允许的备份大小");
                File.Copy(entry, destination, false);
            }
        }
    }
}
