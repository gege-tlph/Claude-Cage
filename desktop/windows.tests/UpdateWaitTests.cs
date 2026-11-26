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

    private static void ApplicationConfiguration()
    {
        try { Application.EnableVisualStyles(); Application.SetCompatibleTextRenderingDefault(false); }
        catch (InvalidOperationException) { }
    }
}
