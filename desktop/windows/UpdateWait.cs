using System.ComponentModel;
using System.Diagnostics;

namespace Cage.Windows;

// A start of the App while an update replaces it, from a shortcut, the Start
// menu or a pinned taskbar button. It must neither become the App that the
// update then waits for in vain nor hold the program folder that a rollback
// moves.
internal static class UpdateWait
{
    public static string InstallPath => AppContext.BaseDirectory.TrimEnd(Path.DirectorySeparatorChar);

    // The helper runs from a copy in the private updates folder.
    public static bool HelperRunning()
    {
        try
        {
            if (!Directory.Exists(AppUpdater.Root) || !Directory.EnumerateFiles(AppUpdater.Root, "helper-*.exe").Any()) return false;
        }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException) { }
        var prefix = Path.GetFullPath(AppUpdater.Root).TrimEnd(Path.DirectorySeparatorChar) + Path.DirectorySeparatorChar;
        foreach (var process in Process.GetProcesses())
            using (process)
            {
                if (!process.ProcessName.StartsWith("helper-", StringComparison.OrdinalIgnoreCase)) continue;
                try
                {
                    if (process.MainModule?.FileName is { } path && path.StartsWith(prefix, StringComparison.OrdinalIgnoreCase)) return true;
                }
                catch (Exception error) when (error is Win32Exception or InvalidOperationException or NotSupportedException) { }
            }
        return false;
    }

    // A rollback copies the previous App beside the installation, then writes
    // its receipt and moves the program folders. A folder left by an earlier
    // rollback is older than this one could be.
    public static bool RollingBack(string target)
    {
        try
        {
            if (File.Exists(target + ".cage-installation.json")) return true;
            var parent = Path.GetDirectoryName(target);
            if (parent == null) return false;
            var recent = DateTime.UtcNow - TimeSpan.FromMinutes(30);
            return Directory.EnumerateDirectories(parent, Path.GetFileName(target) + ".restore-*")
                .Any(path => Directory.GetCreationTimeUtc(path) > recent);
        }
        catch (Exception error) when (error is IOException or UnauthorizedAccessException) { return false; }
    }

    // Shows that an update runs until the App it starts is there (the caller
    // then hands over to it), the helper ended, or a rollback began. False
    // when this start must end at once.
    public static bool Wait(string target, string instanceMutex, TimeSpan timeout)
    {
        var proceed = true;
        var finished = false;
        using var window = new UpdateWindow(null, ["正在更新"]);
        window.SetStep(0, null);
        var deadline = DateTime.UtcNow + timeout;
        var nextHelperCheck = DateTime.UtcNow;
        using var timer = new System.Windows.Forms.Timer { Interval = 250 };
        void Finish(bool next) { proceed = next; finished = true; timer.Stop(); window.Close(); }
        timer.Tick += (_, _) =>
        {
            if (RollingBack(target)) { Finish(false); return; }
            if (Mutex.TryOpenExisting(instanceMutex, out var other)) { other.Dispose(); Finish(true); return; }
            if (DateTime.UtcNow >= deadline) { Finish(true); return; }
            if (DateTime.UtcNow < nextHelperCheck) return;
            nextHelperCheck = DateTime.UtcNow.AddSeconds(1);
            if (!HelperRunning()) Finish(true);
        };
        // Closing the window ends this start; the update continues.
        window.FormClosing += (_, _) => { if (!finished) { proceed = false; timer.Stop(); } };
        timer.Start();
        Application.Run(window);
        return proceed;
    }
}
