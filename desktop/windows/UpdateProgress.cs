using System.Diagnostics;
using System.Runtime.InteropServices;

namespace Cage.Windows;

// The update helper's window, from the backup until the new version
// confirms, or until the result of a failure is closed. The helper's thread
// does the work; the window has a thread of its own. A start of the App
// while the update runs only brings it forward (BringForward), so that start
// does not hold the program folder.
internal sealed class UpdateProgress : IUpdateProgressView
{
    private static readonly string[] Steps = ["备份", "停止环境", "安装", "启动新版"];

    public static string ShowEvent(string instanceKey) => @"Local\Cage-update-" + instanceKey;

    [DllImport("user32.dll")] private static extern bool AllowSetForegroundWindow(int processId);
    [DllImport("user32.dll")] private static extern bool SetForegroundWindow(IntPtr window);

    // A start of the App asks the update window to come forward. False when
    // no update runs with a window (also once its result shows).
    public static bool BringForward(string instanceKey)
    {
        try
        {
            if (!EventWaitHandle.TryOpenExisting(ShowEvent(instanceKey), out var show)) return false;
            using (show)
            {
                AllowSetForegroundWindow(-1);
                show.Set();
            }
            return true;
        }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException or WaitHandleCannotBeOpenedException) { return false; }
    }

    private readonly UpdateWindow window;
    private readonly Thread thread;
    private EventWaitHandle? show;
    private RegisteredWaitHandle? showWait;
    private readonly TaskCompletionSource closed = new(TaskCreationOptions.RunContinuationsAsynchronously);
    private int current = -1;

    private UpdateProgress(string versions, string instanceKey)
    {
        UpdateWindow? created = null;
        Exception? failure = null;
        // Not disposed: the window may raise HandleCreated again later.
        var ready = new ManualResetEventSlim();
        thread = new Thread(() =>
        {
            try
            {
                created = new UpdateWindow(versions, Steps) { Closable = false };
                created.FormClosed += (_, _) => closed.TrySetResult();
                created.HandleCreated += (_, _) => ready.Set();
                Application.Run(created);
            }
            catch (Exception error) { failure = error; ready.Set(); }
            finally { closed.TrySetResult(); }
        }) { IsBackground = true, Name = "Cage update window" };
        thread.SetApartmentState(ApartmentState.STA);
        thread.Start();
        if (!ready.Wait(TimeSpan.FromSeconds(10)) || failure != null || created == null)
            throw new InvalidOperationException("更新窗口无法打开", failure);
        window = created;
        show = new EventWaitHandle(false, EventResetMode.AutoReset, ShowEvent(instanceKey));
        showWait = ThreadPool.RegisterWaitForSingleObject(show, (_, _) => Post(Restore), null, Timeout.Infinite, false);
    }

    // Null when no window can be shown: the update then runs without one.
    public static UpdateProgress? Open(string versions, string instanceKey)
    {
        try { return new UpdateProgress(versions, instanceKey); }
        catch (Exception error) when (error is InvalidOperationException or IOException or UnauthorizedAccessException or WaitHandleCannotBeOpenedException) { return null; }
    }

    public Task Closed => closed.Task;

    public void Step(int index, int? percent = null)
    {
        current = index;
        Post(() => window.SetStep(index, percent));
    }

    public void Status(string format, string version) => Post(() => window.SetStatus(InterfaceLanguage.Format(format, version)));

    // Starts of the App are no longer held from here: they run as usual
    // while the result stays until it is closed.
    public void Fail(string format, string version, string? detail, string target, bool canOpen)
    {
        StopShowing();
        var index = Math.Max(current, 0);
        Post(() =>
        {
            var next = new List<(string, Action<Button>)>();
            if (canOpen) next.Add(("打开 Cage lite", _ => { StartApp(target); window.Close(); }));
            next.Add(("导出诊断包", button => Export(target, button)));
            window.Fail(index, InterfaceLanguage.Format(format, version), next.ToArray());
            window.SetDetail(detail);
            Restore();
        });
    }

    private static void StartApp(string target)
    {
        try { Process.Start(new ProcessStartInfo(Path.Combine(target, "Cage.exe")) { UseShellExecute = false, WorkingDirectory = target })?.Dispose(); }
        catch (Exception error) when (error is System.ComponentModel.Win32Exception or IOException) { }
    }

    // The installation's own backend writes the bundle, as the App does.
    private void Export(string target, Button button)
    {
        using var picker = new SaveFileDialog { Title = InterfaceLanguage.Text("导出诊断包"), Filter = InterfaceLanguage.Text("诊断包 (*.zip)") + "|*.zip", FileName = "cage-diagnostics.zip", DefaultExt = "zip", AddExtension = true };
        if (picker.ShowDialog(window) != DialogResult.OK) return;
        var file = picker.FileName;
        button.Enabled = false;
        _ = Task.Run(async () =>
        {
            var exported = false;
            try
            {
                var (exit, _) = await Backend.JsonAt(Path.Combine(target, "resources", "cage.exe"), ["diagnostics", "--json", "--connection", "local", file], deadline: TimeSpan.FromSeconds(60));
                exported = exit == 0;
            }
            catch (Exception error) when (error is IOException or UnauthorizedAccessException or OperationCanceledException or System.ComponentModel.Win32Exception or System.Text.Json.JsonException) { }
            Post(() =>
            {
                button.Text = InterfaceLanguage.Text(exported ? "已导出诊断包" : "导出失败");
                button.Enabled = !exported;
            });
        });
    }

    private void Close() => Post(() => { window.Closable = true; window.Close(); });

    private void Restore()
    {
        if (window.WindowState == FormWindowState.Minimized) window.WindowState = FormWindowState.Normal;
        window.Show(); window.Activate(); SetForegroundWindow(window.Handle);
    }

    private void Post(Action action)
    {
        try { if (!window.IsDisposed && window.IsHandleCreated) window.BeginInvoke(action); }
        catch (InvalidOperationException) { }
    }

    private void StopShowing()
    {
        showWait?.Unregister(null); showWait = null;
        show?.Dispose(); show = null;
    }

    public void Dispose()
    {
        StopShowing();
        Close();
        thread.Join(TimeSpan.FromSeconds(5));
    }
}
