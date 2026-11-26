namespace Cage.Windows;

// What a second start of the App sends to the App already running: 1 brings
// its window up; 2 and the update helper's token also let it confirm the
// update that started the second one.
internal static class InstanceMessage
{
    public static void Write(Stream pipe, Guid? confirmation)
    {
        // Unbuffered: no Flush, which on a pipe waits until the reader has
        // read everything, and an App before 0.2.3 reads one byte only.
        if (confirmation is { } token) pipe.Write([2, .. token.ToByteArray()]);
        else pipe.WriteByte(1);
    }

    // Null for anything else, which is ignored.
    public static async Task<(bool Restore, Guid? Confirmation)?> Read(Stream pipe, CancellationToken cancellation)
    {
        var data = new byte[17];
        if (await pipe.ReadAsync(data.AsMemory(0, 1), cancellation) != 1) return null;
        switch (data[0])
        {
            case 1: return (true, null);
            case 2:
                await pipe.ReadExactlyAsync(data.AsMemory(1, 16), cancellation);
                return (true, new Guid(data.AsSpan(1, 16)));
            default: return null;
        }
    }
}
