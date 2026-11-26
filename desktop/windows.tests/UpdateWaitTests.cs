using System.Diagnostics;
using System.IO.Pipes;
using System.Runtime.ExceptionServices;
using Cage.Windows;

// A start of the App during an update (customer report, 0.2.2): the update's
// own start found it running, handed over without its token and ended, so
// the helper never saw the confirmation and rolled the update back.
internal static class UpdateWaitTests
{
    private static int assertions;

    private static void Check(bool value, string message)
    {
        assertions++;
        if (!value) throw new Exception(message);
    }

    public static async Task Run()
    {
        // Never the real profile: AppUpdater.Root follows CLAUDE_CAGE_CONFIG.
        var profile = Path.Combine(Path.GetTempPath(), "cage-wait-profile-" + Guid.NewGuid().ToString("N"));
        var savedProfile = Environment.GetEnvironmentVariable("CLAUDE_CAGE_CONFIG");
        Directory.CreateDirectory(profile);
        Environment.SetEnvironmentVariable("CLAUDE_CAGE_CONFIG", Path.Combine(profile, "config.json"));
        try { await RunInProfile(); }
        finally
        {
            Environment.SetEnvironmentVariable("CLAUDE_CAGE_CONFIG", savedProfile);
            Directory.Delete(profile, true);
        }
    }

    private static async Task RunInProfile()
    {
        await InstanceMessages();
        RollingBack();
        Confirmation();
        Results();
        await HelperRecovery();
        InstallerPhases();
        Exception? failure = null;
        var thread = new Thread(() =>
        {
            try { Waiting(); }
            catch (Exception error) { failure = error; }
        }) { IsBackground = true, Name = "Cage update wait regression" };
        thread.SetApartmentState(ApartmentState.STA);
        thread.Start();
        if (!thread.Join(TimeSpan.FromSeconds(30))) throw new TimeoutException("Update wait regression did not complete");
        if (failure != null) ExceptionDispatchInfo.Capture(failure).Throw();
        Console.WriteLine($"Windows update wait: {assertions} assertions passed.");
    }

    private static async Task InstanceMessages()
    {
        foreach (var token in new Guid?[] { null, Guid.NewGuid() })
        {
            var name = "cage-test-" + Guid.NewGuid().ToString("N");
            await using var server = new NamedPipeServerStream(name, PipeDirection.In, 1, PipeTransmissionMode.Byte, PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);
            var accepted = server.WaitForConnectionAsync();
            using var client = new NamedPipeClientStream(".", name, PipeDirection.Out, PipeOptions.CurrentUserOnly);
            client.Connect(3000);
            await accepted;
            // As in the App: the pipe has no buffer, its reader already waits.
            using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(10));
            var reading = InstanceMessage.Read(server, deadline.Token);
            await Task.Run(() => InstanceMessage.Write(client, token)).WaitAsync(TimeSpan.FromSeconds(10));
            client.Dispose();
            var message = await reading;
            Check(message is { Restore: true } && message.Value.Confirmation == token, $"second start sent {token}, the running App read {message}");
        }
        // An old second start sends only 1; anything else is ignored.
        using var unknown = new MemoryStream([7, 1]);
        Check(await InstanceMessage.Read(unknown, CancellationToken.None) == null, "unknown message was accepted");
        using var empty = new MemoryStream();
        Check(await InstanceMessage.Read(empty, CancellationToken.None) == null, "closed pipe was read as a message");
    }

    private static void RollingBack()
    {
        var parent = Path.Combine(Path.GetTempPath(), "cage-rollback-test-" + Guid.NewGuid().ToString("N"));
        var target = Path.Combine(parent, "Cage");
        Directory.CreateDirectory(target);
        try
        {
            Check(!UpdateWait.RollingBack(target), "an installation without a rollback was rolling back");
            // The customer's machine kept the restore folder of the failed
            // rollback; a later update is not a rollback because of it.
            var stale = Directory.CreateDirectory(target + ".restore-" + Guid.NewGuid()).FullName;
            Directory.SetCreationTimeUtc(stale, DateTime.UtcNow.AddHours(-2));
            Check(!UpdateWait.RollingBack(target), "an old restore folder was read as a rollback");
            Directory.CreateDirectory(target + "-other.restore-" + Guid.NewGuid());
            Check(!UpdateWait.RollingBack(target), "another installation's restore folder was read as this one's");
            var copying = Directory.CreateDirectory(target + ".restore-" + Guid.NewGuid()).FullName;
            Check(UpdateWait.RollingBack(target), "the rollback copying the previous App was missed");
            Directory.Delete(copying);
            File.WriteAllText(target + ".cage-installation.json", "{}");
            Check(UpdateWait.RollingBack(target), "the rollback receipt was missed");
        }
        finally { Directory.Delete(parent, true); }
    }

    private static void Confirmation()
    {
        var saved = Environment.GetEnvironmentVariable("CAGE_UPDATE_CONFIRMATION");
        try
        {
            Environment.SetEnvironmentVariable("CAGE_UPDATE_CONFIRMATION", null);
            Check(AppUpdater.PendingConfirmation() == null, "a start without a token had one");
            Environment.SetEnvironmentVariable("CAGE_UPDATE_CONFIRMATION", "not-a-token");
            Check(AppUpdater.PendingConfirmation() == null, "an invalid token was accepted");
            var token = Guid.NewGuid();
            Environment.SetEnvironmentVariable("CAGE_UPDATE_CONFIRMATION", token.ToString());
            Check(AppUpdater.PendingConfirmation() == token, "the helper's token was lost");
        }
        finally { Environment.SetEnvironmentVariable("CAGE_UPDATE_CONFIRMATION", saved); }

        // Helpers before 0.2.3 read the confirmation only while the start
        // they made runs: it stays past the confirmation, then ends.
        Directory.CreateDirectory(AppUpdater.Root);
        var confirmed = Guid.NewGuid();
        var marker = Path.Combine(AppUpdater.Root, "launch-" + confirmed + ".json");
        var clock = Stopwatch.StartNew();
        var written = TimeSpan.Zero;
        var writer = Task.Run(async () => { await Task.Delay(400); File.WriteAllText(marker, "{}"); written = clock.Elapsed; });
        try
        {
            AppUpdater.AwaitConfirmation(confirmed, TimeSpan.FromSeconds(20));
            writer.Wait();
            Check(clock.Elapsed - written >= TimeSpan.FromSeconds(1.5), $"ended {clock.Elapsed - written} after the confirmation");
            Check(clock.Elapsed < TimeSpan.FromSeconds(10), "waited on after the confirmation");
        }
        finally { File.Delete(marker); }
        clock.Restart();
        AppUpdater.AwaitConfirmation(Guid.NewGuid(), TimeSpan.FromMilliseconds(600));
        Check(clock.Elapsed < TimeSpan.FromSeconds(3), "an update that never confirmed held the start");
    }

    // The result of the last update is shown once and kept: in the customer
    // report the next start had already deleted the only record of it.
    private static void Results()
    {
        Directory.CreateDirectory(AppUpdater.Root);
        var kept = Path.Combine(AppUpdater.Root, "results");
        Directory.CreateDirectory(kept);
        for (var i = 0; i < 12; i++) File.WriteAllText(Path.Combine(kept, $"20200101-0000{i:00}-000.json"), "{}");
        File.WriteAllText(Path.Combine(AppUpdater.Root, "last-result.json"), "{\"success\":false,\"message\":\"更新未完成：fixture\"}");
        var reported = new List<string>();
        using (var updater = new AppUpdater(_ => { }, () => { }, () => { }, () => "fixture-publisher", reported.Add))
        {
            var state = updater.Handle("status");
            Check(state.GetValueOrDefault("phase") as string == "failed" && state.GetValueOrDefault("message") as string == "更新未完成：fixture", "the failed update was not shown");
        }
        Check(reported.Count == 1 && reported[0] == "Update failed: 更新未完成：fixture", "the failed update did not reach the event log: " + string.Join("|", reported));
        Check(!File.Exists(Path.Combine(AppUpdater.Root, "last-result.json")), "the result was shown again at the next start");
        var files = Directory.GetFiles(kept, "*.json").Select(Path.GetFileName).Order(StringComparer.Ordinal).ToArray();
        Check(files.Length == 10 && !files.Contains("20200101-000000-000.json") && !files[^1]!.StartsWith("2020", StringComparison.Ordinal), "kept results: " + string.Join(",", files));
        Check(File.ReadAllText(Path.Combine(kept, files[^1]!)).Contains("fixture"), "the kept result lost its message");
    }

    private static readonly System.Reflection.BindingFlags Private = System.Reflection.BindingFlags.NonPublic | System.Reflection.BindingFlags.Static;

    private static Task Preserve(string operation, string source, string target, string receipt, string? backup = null) =>
        (Task)typeof(UpdateInstaller).GetMethod("PreserveFiles", Private)!.Invoke(null, [operation, source, target, receipt, backup])!;

    // The rollback of the customer report: the previous App copied beside the
    // installation, the receipt written, detach failing on a held file.
    private static async Task HelperRecovery()
    {
        var parent = Path.Combine(Path.GetTempPath(), "cage-helper-recovery-" + Guid.NewGuid().ToString("N"));
        var target = Path.Combine(parent, "Cage");
        var id = Guid.NewGuid().ToString();
        var restored = target + ".restore-" + id; var failed = target + ".failed-" + id; var receipt = target + ".cage-installation.json";
        void App(string path, string version)
        {
            Directory.CreateDirectory(Path.Combine(path, "resources"));
            File.WriteAllText(Path.Combine(path, "Cage.install-id"), "io.github.lee-xeo.claude-cage");
            File.WriteAllText(Path.Combine(path, "program.txt"), version);
            File.WriteAllText(Path.Combine(path, "resources", "tool.txt"), version);
            File.WriteAllText(Path.Combine(path, "Uninstall.exe"), "uninstaller " + version);
            var files = Directory.EnumerateFiles(path, "*", SearchOption.AllDirectories).Select(file => new {
                path = Path.GetRelativePath(path, file).Replace('\\', '/'),
                sha256 = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(File.ReadAllBytes(file))).ToLowerInvariant(),
            }).ToArray();
            File.WriteAllText(Path.Combine(path, "Cage.install-files.json"), System.Text.Json.JsonSerializer.Serialize(new { version = 1, files }));
        }
        App(target, "candidate"); App(restored, "previous");
        Directory.CreateDirectory(Path.Combine(target, "CageData"));
        File.WriteAllText(Path.Combine(target, "CageData", "disk"), "vm disk");
        var backup = Path.Combine(parent, "previous-backup");
        Directory.CreateDirectory(backup);
        var detach = typeof(UpdateInstaller).GetMethod("Detach", Private)!;
        try
        {
            await Preserve("prepare", target, restored, receipt, failed);
            bool detached;
            using (new FileStream(Path.Combine(target, "program.txt"), FileMode.Open, FileAccess.Read, FileShare.Read))
                detached = await (Task<bool>)detach.Invoke(null, [target, backup, failed, receipt])!;
            Check(!detached, "a held program file let detach succeed");
            Check(File.Exists(receipt) && !Directory.Exists(failed) && File.ReadAllText(Path.Combine(target, "program.txt")) == "candidate", "a failed detach did not put every entry back");
            // A new start of the helper finds the receipt that moved nothing
            // and archives it, as the installer does.
            await UpdateInstaller.HealPendingTransaction(target);
            Check(!File.Exists(receipt) && Directory.GetFiles(parent, "Cage.cage-installation-archived-*.json").Length == 1, "the unmoved rollback receipt was not archived");
            Check(Directory.Exists(restored), "healing removed the restore copy");
            // The same failure inside a rollback: completing the receipt keeps
            // the new version and blocks nothing.
            File.Delete(Directory.GetFiles(parent, "Cage.cage-installation-archived-*.json")[0]);
            await Preserve("prepare", target, restored, receipt, failed);
            using (new FileStream(Path.Combine(target, "program.txt"), FileMode.Open, FileAccess.Read, FileShare.Read))
                Check(!await (Task<bool>)detach.Invoke(null, [target, backup, failed, receipt])!, "a held program file let detach succeed");
            await Preserve("complete", "", target, receipt);
            Check(!File.Exists(receipt), "completing after failed detaches kept the receipt");
            // Nothing held: detach moves the program and leaves CageData.
            await Preserve("prepare", target, restored, receipt, failed);
            Check(await (Task<bool>)detach.Invoke(null, [target, backup, failed, receipt])!, "detach failed without a held file");
            Check(Directory.GetFileSystemEntries(target).Select(Path.GetFileName).SequenceEqual(["CageData"]), "detach left program entries or moved CageData");
            // A receipt that moved entries stays: the update refuses to start.
            var moved = target + "-moved";
            App(moved, "candidate");
            var movedReceipt = moved + ".cage-installation.json";
            File.WriteAllText(movedReceipt, System.Text.Json.JsonSerializer.Serialize(new {
                kind = "cage-installation", version = 1, installPath = moved, stagePath = moved + ".restore-" + id, backupPath = "",
                files = Array.Empty<object>(), directories = Array.Empty<string>(), keepInPlace = new[] { "CageData" },
                moves = new[] { new { path = "my-project", destination = "my-project", directory = true, state = "moved" } },
            }));
            try { await UpdateInstaller.HealPendingTransaction(moved); Check(false, "a receipt with moved entries was released"); }
            catch (IOException) { }
            Check(File.Exists(movedReceipt), "a receipt with moved entries was archived");
        }
        finally { Directory.Delete(parent, true); }
    }

    // The window follows the installer's phases in their order.
    private static void InstallerPhases()
    {
        var phases = new[] { "acquiring_installation_lock", "healing_transaction_record", "checking_destination", "unpacking_application", "detecting_webview_runtime",
            "installing_webview_runtime", "preparing_file_transaction", "moving_previous_application", "moving_personal_files", "publishing_application",
            "cleaning_previous_application", "registering_installation", "trusting_publisher", "adding_user_path", "completed" };
        var percents = phases.Select(UpdateInstaller.InstallPercent).ToArray();
        Check(percents.All(percent => percent is >= 0 and <= 100), "an installer phase has no percent: " + string.Join(",", percents));
        Check(percents.Zip(percents.Skip(1)).All(pair => pair.First <= pair.Second), "installer percents go back: " + string.Join(",", percents));
        Check(UpdateInstaller.InstallPercent("unknown_phase") == null && percents[^1] == 100, "unknown or final phase misread");
    }

    private static void Waiting()
    {
        ApplicationConfiguration();
        var parent = Path.Combine(Path.GetTempPath(), "cage-wait-test-" + Guid.NewGuid().ToString("N"));
        var target = Path.Combine(parent, "Cage");
        Directory.CreateDirectory(target);
        try
        {
            // The App the update starts is there: hand over to it.
            var name = @"Local\Cage-test-" + Guid.NewGuid().ToString("N");
            var clock = Stopwatch.StartNew();
            using (new Mutex(true, name))
                Check(UpdateWait.Wait(target, name, TimeSpan.FromSeconds(20)), "the update's App was not handed over to");
            Check(clock.Elapsed < TimeSpan.FromSeconds(5), "the handover waited too long");
            // No helper runs in this test: the update is over, start as usual.
            clock.Restart();
            Check(UpdateWait.Wait(target, name, TimeSpan.FromSeconds(20)), "start was refused after the update ended");
            Check(clock.Elapsed < TimeSpan.FromSeconds(5), "start waited after the update ended");
            // A rollback began: end at once.
            File.WriteAllText(target + ".cage-installation.json", "{}");
            clock.Restart();
            Check(!UpdateWait.Wait(target, name, TimeSpan.FromSeconds(20)), "start continued into a rollback");
            Check(clock.Elapsed < TimeSpan.FromSeconds(5), "start stayed during a rollback");
        }
        finally { Directory.Delete(parent, true); }
    }

    public static async Task Demo(string scenario, string target)
    {
        ApplicationConfiguration();
        using var progress = UpdateProgress.Open("0.2.2  →  0.2.3", UpdateWait.InstanceKey()) ?? throw new InvalidOperationException("no window");
        Console.WriteLine("update window shown; scenario " + scenario);
        progress.Step(UpdateSteps.Backup); await Task.Delay(4000);
        progress.Step(UpdateSteps.StopEnvironment); await Task.Delay(4000);
        foreach (var phase in new[] { "acquiring_installation_lock", "unpacking_application", "preparing_file_transaction", "moving_previous_application", "publishing_application", "cleaning_previous_application", "registering_installation", "completed" })
        {
            progress.Step(UpdateSteps.Install, UpdateInstaller.InstallPercent(phase)); await Task.Delay(2500);
        }
        progress.Step(UpdateSteps.StartNew); await Task.Delay(5000);
        const string detail = "更新未完成：新版工作台未能完成启动确认（演示）";
        switch (scenario)
        {
            case "success": Console.WriteLine("confirmed; the window closes"); return;
            case "restored":
                progress.Status("正在恢复 {0}", "0.2.2"); await Task.Delay(5000);
                progress.Fail("已恢复 {0}", "0.2.2", detail, target, true); break;
            case "kept":
                progress.Status("正在恢复 {0}", "0.2.2"); await Task.Delay(5000);
                progress.Fail("已保留 {0}", "0.2.3", detail + "。原应用无法恢复，已保留新版", target, true); break;
            case "incomplete":
                progress.Status("正在恢复 {0}", "0.2.2"); await Task.Delay(5000);
                progress.Fail("恢复未完成", "", detail + "。自动恢复未完成。恢复目录记录在 " + target + ".cage-installation.json", target, false); break;
            default: throw new ArgumentException("scenario: success, restored, kept or incomplete");
        }
        Console.WriteLine("result shown; waiting for the window to close");
        await progress.Closed;
    }

    // For a look at the window: each state drawn to a PNG in directory.
    public static void Render(string directory)
    {
        ApplicationConfiguration();
        Directory.CreateDirectory(directory);
        void Save(UpdateWindow window, string name)
        {
            window.StartPosition = FormStartPosition.Manual; window.Location = new Point(-4000, -4000);
            window.Show(); window.PerformLayout(); Application.DoEvents();
            using var image = new Bitmap(window.Width, window.Height);
            window.DrawToBitmap(image, new Rectangle(Point.Empty, window.Size));
            image.Save(Path.Combine(directory, name + ".png"), System.Drawing.Imaging.ImageFormat.Png);
            window.Close();
        }
        string[] steps = ["备份", "停止环境", "安装", "启动新版"];
        using (var running = new UpdateWindow("0.2.3  →  0.2.4", steps)) { running.SetStep(2, 62); Save(running, "1-installing"); }
        using (var restoring = new UpdateWindow("0.2.3  →  0.2.4", steps)) { restoring.SetStep(3, null); restoring.SetStatus("正在恢复 0.2.3"); Save(restoring, "2-restoring"); }
        using (var failed = new UpdateWindow("0.2.3  →  0.2.4", steps))
        {
            failed.SetStep(3, null);
            failed.Fail(3, "已恢复 0.2.3", ("打开 Cage lite", _ => { }), ("导出诊断包", _ => { }));
            Save(failed, "3-restored");
        }
        using (var waiting = new UpdateWindow(null, ["正在更新"])) { waiting.SetStep(0, null); Save(waiting, "4-waiting"); }
    }

    private static void ApplicationConfiguration()
    {
        try { Application.EnableVisualStyles(); Application.SetCompatibleTextRenderingDefault(false); }
        catch (InvalidOperationException) { }
    }
}
