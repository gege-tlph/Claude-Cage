using System.IO.Pipes;
using System.Security.Cryptography;
using System.Security.Principal;
using System.Text;
using System.Text.Json;
using System.Runtime.InteropServices;

namespace Cage.Windows;

internal static class Program
{
    public static string? SmokeOutput { get; private set; }
    [STAThread]
    private static int Main(string[] args)
    {
        if (args is ["--install-update"])
        {
            ApplicationConfiguration.Initialize();
            var updateKey = UpdateWait.InstanceKey();
            // The helper runs from a copy in the updates folder: the installed
            // backend gives the interface language, before anything changes.
            return UpdateInstaller.Run(openProgress: (versions, target) =>
            {
                InterfaceLanguage.Refresh(Path.Combine(target, "resources", "cage.exe")).GetAwaiter().GetResult();
                return UpdateProgress.Open(versions, updateKey);
            }).GetAwaiter().GetResult();
        }
        if (args is ["--smoke-test", var output] && Path.IsPathFullyQualified(output))
        {
            SmokeOutput = output;
            if (File.Exists(output)) File.Delete(output);
        }
        else if (args.Length != 0) return 2;
        // The update helper's own start carries its token. Any other start
        // during its rollback must not run from the folder that it moves.
        var confirmation = AppUpdater.PendingConfirmation();
        var helper = confirmation == null && SmokeOutput == null ? UpdateWait.RunningHelper() : null;
        if (helper != null && (UpdateProgress.BringForward(UpdateWait.InstanceKey()) || UpdateWait.RollingBack(UpdateWait.InstallPath))) return 0;
        AppDomain.CurrentDomain.UnhandledException += (_, e) => Backend.Report(e.ExceptionObject is Exception error ? error.ToString() : "Unhandled native exception").GetAwaiter().GetResult();
        Application.ThreadException += (_, e) => {
            Backend.Report(e.Exception.ToString()).GetAwaiter().GetResult();
            using var dialog = new ThreadExceptionDialog(e.Exception);
            if (dialog.ShowDialog() == DialogResult.Abort) Application.Exit();
        };
        TaskScheduler.UnobservedTaskException += (_, e) => _ = Backend.Report(e.Exception.ToString());
        ApplicationConfiguration.Initialize();
        InterfaceLanguage.Refresh().GetAwaiter().GetResult();
        if (RuntimeInformation.OSArchitecture != Architecture.X64 || !SupportedWindowsBuild())
        {
            MessageBox.Show(InterfaceLanguage.Text("Cage lite 需要 Windows 10 22H2（19045）、Windows 10 LTSC 2021 或 Windows 11，x64（Intel/AMD）系统。"), "Cage lite");
            return 1;
        }
        var key = UpdateWait.InstanceKey();
        var instanceMutex = @"Local\Cage-" + key;
        // Helpers before 0.2.3 show no window: show that an update runs until
        // the App it starts is there; this start then hands over to it below,
        // as to any App already running.
        if (helper != null && UpdateWait.Legacy(helper) && !UpdateWait.Wait(UpdateWait.InstallPath, instanceMutex, TimeSpan.FromMinutes(5))) return 0;
        using var mutex = new Mutex(true, instanceMutex, out var owner);
        if (!owner)
        {
            if (SmokeOutput != null) return 3;
            try
            {
                using var client = new NamedPipeClientStream(".", "Cage-" + key, PipeDirection.Out, PipeOptions.CurrentUserOnly);
                client.Connect(3000);
                InstanceMessage.Write(client, confirmation);
            }
            // The update helper reports an App that could not take its token.
            catch (IOException) when (confirmation != null) { }
            catch (IOException) { MessageBox.Show(InterfaceLanguage.Text("Cage lite 已在运行，但窗口暂时无法唤起。请从任务栏打开 Cage lite。"), "Cage lite"); }
            catch (TimeoutException) { MessageBox.Show(InterfaceLanguage.Text("Cage lite 正在启动，请稍候。"), "Cage lite"); }
            if (confirmation is { } handed) AppUpdater.AwaitConfirmation(handed, TimeSpan.FromSeconds(115));
            return 0;
        }
        using var form = new MainWindow();
        using var cancellation = new CancellationTokenSource();
        _ = Task.Run(async () =>
        {
            while (!cancellation.IsCancellationRequested)
            {
                try
                {
                    await using var pipe = new NamedPipeServerStream("Cage-" + key, PipeDirection.In, 1,
                        PipeTransmissionMode.Byte, PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);
                    await pipe.WaitForConnectionAsync(cancellation.Token);
                    if (await InstanceMessage.Read(pipe, cancellation.Token) is not { } message) continue;
                    if (message.Confirmation is { } token) form.AcceptUpdateConfirmation(token);
                    if (message.Restore && form.IsHandleCreated) form.BeginInvoke(form.RestoreWindow);
                }
                catch (OperationCanceledException) { break; }
                catch (IOException) { }
            }
        });
        Application.Run(form); cancellation.Cancel();
        if (SmokeOutput == null) return 0;
        try
        {
            using var report = JsonDocument.Parse(File.ReadAllText(SmokeOutput));
            return report.RootElement.TryGetProperty("success", out var success) && success.ValueKind == JsonValueKind.True ? 0 : 1;
        }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException or JsonException) { return 1; }
    }

    // Keep in step with host.WindowsBuildSupported: Windows 10 22H2 (19045)
    // or later, or build 19044 for Enterprise and IoT Enterprise LTSC 2021.
    private static bool SupportedWindowsBuild()
    {
        var build = Environment.OSVersion.Version.Build;
        if (build >= 19045) return true;
        if (build != 19044) return false;
        using var key = Microsoft.Win32.Registry.LocalMachine.OpenSubKey(@"SOFTWARE\Microsoft\Windows NT\CurrentVersion");
        return key?.GetValue("EditionID") is "EnterpriseS" or "EnterpriseSN" or "IoTEnterpriseS";
    }
}
