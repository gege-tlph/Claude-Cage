using System.Diagnostics;
using System.Drawing.Drawing2D;
using System.Runtime.InteropServices;

namespace Cage.Windows;

// The window of an update in progress: the product and versions, then one row
// per step on a timeline with its state, and the result when it fails. With
// no steps it only shows that an update runs. Names and states only. Drawn by
// hand in the workbench's colors; changes ease in over a fifth of a second.
internal sealed class UpdateWindow : Form
{
    private enum State { Waiting, Running, Done, Failed }

    private sealed class Step(string name)
    {
        public readonly string Name = name;
        public State State = State.Waiting;
        public State Previous = State.Waiting;
        public double Since = double.NegativeInfinity;
    }

    private sealed record Palette(Color Paper, Color Strong, Color Ink, Color Muted, Color Inactive, Color Line, Color Track,
        Color Accent, Color AccentInk, Color Danger, Color Button, Color ButtonInk, Color Hover, Color Pressed, Color Border, bool Dark);

    // Set only to draw pictures of both themes.
    internal static bool? DarkOverride { get; set; }

    private static Palette CurrentPalette() => SystemInformation.HighContrast
        ? new(Paper: SystemColors.Window, Strong: SystemColors.WindowText, Ink: SystemColors.WindowText, Muted: SystemColors.GrayText, Inactive: SystemColors.GrayText,
            Line: SystemColors.GrayText, Track: SystemColors.Control, Accent: SystemColors.Highlight, AccentInk: SystemColors.HighlightText, Danger: SystemColors.Highlight,
            Button: SystemColors.Highlight, ButtonInk: SystemColors.HighlightText, Hover: SystemColors.Control, Pressed: SystemColors.ControlDark, Border: SystemColors.WindowText, Dark: false)
        : DarkOverride ?? WindowChrome.IsSystemDarkTheme()
        ? new(Paper: Hex("#252525"), Strong: Hex("#f4f4f4"), Ink: Hex("#e2e2e2"), Muted: Hex("#a2a2a2"), Inactive: Hex("#6e6e6e"),
            Line: Hex("#3f3f3f"), Track: Hex("#363636"), Accent: Hex("#ededed"), AccentInk: Hex("#252525"), Danger: Hex("#f26d78"),
            Button: Hex("#efefef"), ButtonInk: Hex("#1c1c1c"), Hover: Hex("#2f2f2f"), Pressed: Hex("#383838"), Border: Hex("#4a4a4a"), Dark: true)
        : new(Paper: Hex("#ffffff"), Strong: Hex("#202020"), Ink: Hex("#363636"), Muted: Hex("#6c6e70"), Inactive: Hex("#a3a3a3"),
            Line: Hex("#e3e3e3"), Track: Hex("#efefef"), Accent: Hex("#343434"), AccentInk: Hex("#ffffff"), Danger: Hex("#c43d3d"),
            Button: Hex("#242424"), ButtonInk: Hex("#ffffff"), Hover: Hex("#f2f2f2"), Pressed: Hex("#e9e9e9"), Border: Hex("#dcdcdc"), Dark: false);

    private static Color Hex(string value) => ColorTranslator.FromHtml(value);

    // Logical sizes, at 96 DPI.
    private const int WindowWidth = 420, Pad = 28, IconSize = 40, TextLeft = Pad + IconSize + 14, HeaderTop = 24,
        StepsTop = HeaderTop + IconSize + 22, RowHeight = 38, Dot = 18, ButtonHeight = 32, BottomPad = 24;

    private readonly Palette colors = CurrentPalette();
    private readonly string? subtitle;
    private readonly List<Step> steps = new();
    private readonly Stopwatch clock = Stopwatch.StartNew();
    private readonly System.Windows.Forms.Timer frames = new() { Interval = 15 };
    private string statusText = "";
    private string? detail;
    private Rectangle statusBounds;
    private double statusSince = double.NegativeInfinity;
    private bool tipShown;
    private readonly ToolTip tip = new();
    private readonly List<RoundButton> actions = new();
    private Icon? productIcon;
    private Font? titleFont, subtitleFont, stepFont, percentFont, statusFont, resultFont, buttonFont;
    private float scale;
    private int barLeft, barRight;
    private int active = -1;
    private int? target;
    private double shown, lastFrame;
    private double opening = double.NaN, closing = double.NaN, settled = double.NegativeInfinity;
    private (int From, int To, double Since)? growth;
    private bool result, faded, hasStatus;
    private ITaskbarList3? taskbar;

    // False while an update runs: closing then only minimizes, since a start
    // of the App brings this window back rather than starting.
    [System.ComponentModel.DesignerSerializationVisibility(System.ComponentModel.DesignerSerializationVisibility.Hidden)]
    public bool Closable { get; set; } = true;

    public UpdateWindow(string? subtitle, IReadOnlyList<string> stepNames)
    {
        this.subtitle = subtitle;
        foreach (var name in stepNames) steps.Add(new Step(InterfaceLanguage.Text(name)));
        SetStyle(ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer | ControlStyles.UserPaint, true);
        Text = InterfaceLanguage.Text("Cage lite 更新");
        Icon = Icon.ExtractAssociatedIcon(Environment.ProcessPath!) ?? SystemIcons.Application;
        AutoScaleMode = AutoScaleMode.None;
        FormBorderStyle = FormBorderStyle.FixedSingle; MaximizeBox = false;
        StartPosition = FormStartPosition.CenterScreen;
        BackColor = colors.Paper; ForeColor = colors.Ink;
        frames.Tick += (_, _) => Frame();
        FormClosing += CloseRequested;
        FormClosed += (_, _) => { frames.Stop(); SetTaskbar(TaskbarState.None, 0); };
        Relayout(false);
    }

    // Steps before index are done; index runs, with its percent when known;
    // the rest wait.
    public void SetStep(int index, int? percent)
    {
        var now = Now;
        for (var i = 0; i < steps.Count; i++)
            Advance(steps[i], i < index ? State.Done : i == index ? State.Running : State.Waiting, now);
        if (index != active || percent == null) shown = 0;
        active = index;
        target = percent is { } value ? Math.Clamp(value, 0, 100) : null;
        UpdateTaskbar(); Wake();
    }

    public void SetStatus(string text)
    {
        if (text != statusText) statusSince = Now;
        statusText = text;
        hasStatus = text.Length > 0;
        AccessibleDescription = text;
        Relayout(true);
    }

    // The full message behind the result, for whoever diagnoses it.
    public void SetDetail(string? detail) => this.detail = string.IsNullOrWhiteSpace(detail) ? null : detail;

    // The step at index failed: the result line and what can be done next.
    // The first action is the rightmost, the default one.
    public void Fail(int index, string result, params (string Text, Action<Button> Click)[] next)
    {
        var now = Now;
        for (var i = 0; i < steps.Count; i++)
            Advance(steps[i], i < index ? State.Done : i == index ? State.Failed : State.Waiting, now);
        active = -1; target = null;
        this.result = true;
        foreach (var old in actions) { Controls.Remove(old); old.Dispose(); }
        actions.Clear();
        Closable = true;
        foreach (var (text, click) in next)
        {
            var button = new RoundButton(colors, primary: actions.Count == 0) { Text = InterfaceLanguage.Text(text) };
            button.Click += (_, _) => click(button);
            button.TextChanged += (_, _) => Relayout(false);
            actions.Add(button);
            Controls.Add(button);
        }
        if (actions.Count > 0) AcceptButton = actions[0];
        statusText = result; statusSince = now; hasStatus = result.Length > 0; AccessibleDescription = result;
        Relayout(true);
        actions.FirstOrDefault()?.Focus();
        SetTaskbar(TaskbarState.Error, 1);
        Wake();
    }

    // Every transition at its end, for a still picture of the window.
    internal void Settle()
    {
        settled = Now;
        if (target is { } value) shown = value;
        opening = double.NaN; growth = null; Opacity = 1;
        Relayout(false);
    }

    private double Now => clock.Elapsed.TotalSeconds;

    private static void Advance(Step step, State state, double now)
    {
        if (step.State == state) return;
        step.Previous = step.State; step.State = state; step.Since = now;
    }

    // Progress of a transition that began at since and lasts duration, eased
    // like the workbench's (fast start, long soft landing).
    private double Ease(double since, double duration, double delay = 0)
    {
        var now = Now;
        if (since <= settled) return 1;
        var t = Math.Clamp((now - since - delay) / duration, 0, 1);
        return 1 - Math.Pow(1 - t, 4);
    }

    private int S(float value) => (int)Math.Round(value * scale);

    private void Fonts()
    {
        var family = SystemFonts.MessageBoxFont?.FontFamily.Name ?? "Segoe UI";
        Font Make(string name, float px, FontStyle style = FontStyle.Regular) => new(name, px * scale, style, GraphicsUnit.Pixel);
        foreach (var old in new[] { titleFont, subtitleFont, stepFont, percentFont, statusFont, resultFont, buttonFont }) old?.Dispose();
        titleFont = Make("Segoe UI Semibold", 17); subtitleFont = Make(family, 12); stepFont = Make(family, 13);
        percentFont = Make("Segoe UI", 12); statusFont = Make(family, 13); resultFont = Make(family, 13.5f, FontStyle.Bold); buttonFont = Make(family, 13);
        productIcon?.Dispose();
        productIcon = TryIcon(S(IconSize));
    }

    private static Icon? TryIcon(int size)
    {
        try { return Icon.ExtractIcon(Environment.ProcessPath!, 0, size); }
        catch (Exception error) when (error is IOException or ArgumentException or UnauthorizedAccessException) { return null; }
    }

    private void Relayout(bool animate)
    {
        var dpiScale = DeviceDpi / 96f;
        if (dpiScale != scale || titleFont == null) { scale = dpiScale; Fonts(); }
        var flags = TextFormatFlags.NoPadding | TextFormatFlags.SingleLine;
        var names = steps.Count == 0 ? 0 : steps.Max(step => TextRenderer.MeasureText(step.Name, stepFont, Size.Empty, flags).Width);
        var width = S(WindowWidth);
        barLeft = S(TextLeft) + names + S(24);
        barRight = width - S(Pad) - S(44);
        if (barRight - barLeft < S(100)) { width += S(100) - (barRight - barLeft); barRight = width - S(Pad) - S(44); }
        var y = steps.Count == 0 ? S(StepsTop + 12) : S(StepsTop) + steps.Count * S(RowHeight);
        if (hasStatus)
        {
            var line = TextRenderer.MeasureText("Ag", result ? resultFont : statusFont, Size.Empty, flags).Height + S(6);
            var left = steps.Count == 0 ? S(Pad) : S(TextLeft);
            statusBounds = new Rectangle(left, y + S(8), width - S(Pad) - left, line);
            y = statusBounds.Bottom;
        }
        if (actions.Count > 0)
        {
            y += S(18);
            var right = width - S(Pad);
            foreach (var button in actions)
            {
                button.Font = buttonFont; button.Zoom = scale;
                var w = Math.Max(S(88), TextRenderer.MeasureText(button.Text, buttonFont, Size.Empty, flags).Width + S(32));
                button.SetBounds(right - w, y, w, S(ButtonHeight));
                right -= w + S(8);
            }
            y += S(ButtonHeight);
        }
        var height = y + S(BottomPad);
        if (animate && Visible && IsHandleCreated && height != ClientSize.Height && WindowState == FormWindowState.Normal)
        {
            growth = (ClientSize.Height, height, Now);
            ClientSize = new Size(width, ClientSize.Height);
            Wake();
        }
        else { growth = null; ClientSize = new Size(width, height); }
        Invalidate();
    }

    protected override void OnHandleCreated(EventArgs e)
    {
        base.OnHandleCreated(e);
        WindowChrome.FromAppearance(colors.Paper, colors.Strong, colors.Dark).Apply(Handle, null);
        if (DeviceDpi / 96f != scale) Relayout(false);
    }

    protected override void OnDpiChanged(DpiChangedEventArgs e)
    {
        base.OnDpiChanged(e);
        Relayout(false);
    }

    protected override void OnLoad(EventArgs e)
    {
        base.OnLoad(e);
        if (double.IsNegativeInfinity(settled)) { Opacity = 0; opening = Now; }
    }

    protected override void OnShown(EventArgs e)
    {
        base.OnShown(e);
        Lift();
        UpdateTaskbar();
        Wake();
    }

    [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();

    // In front of the other windows. A process started from the background
    // may not take the foreground: the window then rises without the focus
    // instead of opening behind the active window.
    public void Lift()
    {
        if (WindowState == FormWindowState.Minimized) WindowState = FormWindowState.Normal;
        Activate();
        if (GetForegroundWindow() == Handle) return;
        TopMost = true; TopMost = false;
    }

    protected override void OnResize(EventArgs e)
    {
        base.OnResize(e);
        if (WindowState != FormWindowState.Minimized) Wake();
    }

    // A close from the user or the update fades out first; one that is not
    // allowed yet minimizes.
    private void CloseRequested(object? sender, FormClosingEventArgs e)
    {
        if (!Closable && e.CloseReason == CloseReason.UserClosing)
        {
            e.Cancel = true; WindowState = FormWindowState.Minimized; return;
        }
        if (faded || e.CloseReason != CloseReason.UserClosing || !Visible || WindowState == FormWindowState.Minimized || !IsHandleCreated) return;
        e.Cancel = true;
        if (double.IsNaN(closing)) { closing = Now; opening = double.NaN; Wake(); }
    }

    private void Wake()
    {
        Invalidate();
        if (IsHandleCreated && !frames.Enabled) { lastFrame = Now; frames.Start(); }
    }

    private void Frame()
    {
        var now = Now;
        var elapsed = Math.Max(0, now - lastFrame); lastFrame = now;
        if (target is { } value) shown += (value - shown) * (1 - Math.Exp(-elapsed / 0.22));
        if (!double.IsNaN(opening))
        {
            var f = Ease(opening, 0.2);
            Opacity = f;
            if (f >= 1) opening = double.NaN;
        }
        if (!double.IsNaN(closing))
        {
            var f = Ease(closing, 0.16);
            Opacity = Math.Max(0, 1 - f);
            if (f >= 1) { faded = true; closing = double.NaN; frames.Stop(); Close(); return; }
        }
        if (growth is { } grow)
        {
            var (from, to, since) = grow;
            var f = Ease(since, 0.26);
            ClientSize = new Size(ClientSize.Width, (int)Math.Round(from + (to - from) * f));
            if (f >= 1) growth = null;
        }
        if (WindowState != FormWindowState.Minimized) Invalidate();
        if (!Animating(now)) frames.Stop();
    }

    // The spinner, the indeterminate bar and the running bar move all the
    // time; everything else settles within a second.
    private bool Animating(double now)
    {
        if (!double.IsNaN(opening) || !double.IsNaN(closing) || growth != null) return true;
        if (!result && (steps.Count == 0 || active >= 0)) return true;
        return now - statusSince < 1 || steps.Any(step => now - step.Since < 1);
    }

    protected override void OnPaint(PaintEventArgs e)
    {
        var g = e.Graphics;
        g.SmoothingMode = SmoothingMode.AntiAlias;
        g.PixelOffsetMode = PixelOffsetMode.HighQuality;
        var now = Now;
        var flags = TextFormatFlags.NoPadding | TextFormatFlags.SingleLine | TextFormatFlags.NoPrefix;

        if (productIcon != null) g.DrawIcon(productIcon, new Rectangle(S(Pad), S(HeaderTop), S(IconSize), S(IconSize)));
        var titleHeight = TextRenderer.MeasureText("Cage lite", titleFont, Size.Empty, flags).Height;
        var subtitleHeight = subtitle == null ? 0 : TextRenderer.MeasureText(subtitle, subtitleFont, Size.Empty, flags).Height + S(2);
        var top = S(HeaderTop) + (S(IconSize) - titleHeight - subtitleHeight) / 2;
        TextRenderer.DrawText(g, "Cage lite", titleFont, new Point(S(TextLeft), top), colors.Strong, flags);
        if (subtitle != null)
            TextRenderer.DrawText(g, subtitle, subtitleFont, new Point(S(TextLeft), top + titleHeight + S(2)), colors.Muted, flags);

        if (hasStatus)
        {
            var fade = Ease(statusSince, 0.25);
            var font = result ? resultFont : statusFont;
            var shade = Blend(colors.Paper, result ? colors.Strong : colors.Muted, fade);
            var shift = (int)Math.Round((1 - fade) * S(4));
            TextRenderer.DrawText(g, statusText, font, new Rectangle(statusBounds.X, statusBounds.Y + shift, statusBounds.Width, statusBounds.Height), shade,
                flags | TextFormatFlags.VerticalCenter | TextFormatFlags.EndEllipsis);
        }
        if (steps.Count == 0) { Indeterminate(g, now); return; }
        for (var i = 0; i < steps.Count; i++)
        {
            var step = steps[i];
            var rowTop = S(StepsTop) + i * S(RowHeight);
            var cy = rowTop + S(RowHeight) / 2f;
            var cx = S(Pad) + S(IconSize) / 2f;
            if (i < steps.Count - 1)
            {
                float y1 = cy + S(Dot) / 2f + S(4), y2 = cy + S(RowHeight) - S(Dot) / 2f - S(4);
                using var line = new Pen(colors.Line, 1.5f * scale) { StartCap = LineCap.Round, EndCap = LineCap.Round };
                g.DrawLine(line, cx, y1, cx, y2);
                if (step.State == State.Done)
                {
                    var f = (float)Ease(step.Since, 0.3, 0.12);
                    using var fill = new Pen(colors.Accent, 1.5f * scale) { StartCap = LineCap.Round, EndCap = LineCap.Round };
                    if (f > 0) g.DrawLine(fill, cx, y1, cx, y1 + (y2 - y1) * f);
                }
            }
            Indicator(g, step, cx, cy, now);
            var shade = Blend(TextColor(step.Previous), TextColor(step.State), Ease(step.Since, 0.2));
            TextRenderer.DrawText(g, step.Name, stepFont, new Rectangle(S(TextLeft), rowTop, barLeft - S(TextLeft), S(RowHeight)), shade, flags | TextFormatFlags.VerticalCenter | TextFormatFlags.EndEllipsis);
            if (i == active && step.State == State.Running && target != null) Bar(g, cy, now);
        }
    }

    // The full message shows while the pointer rests on the result line.
    protected override void OnMouseMove(MouseEventArgs e)
    {
        base.OnMouseMove(e);
        var over = hasStatus && detail != null && statusBounds.Contains(e.Location);
        if (over == tipShown) return;
        tipShown = over;
        if (over) tip.Show(detail, this, statusBounds.Left, statusBounds.Bottom + S(4), 30000);
        else tip.Hide(this);
    }

    protected override void OnMouseLeave(EventArgs e)
    {
        base.OnMouseLeave(e);
        if (tipShown) { tipShown = false; tip.Hide(this); }
    }

    private Color TextColor(State state) => state switch
    {
        State.Running => colors.Strong,
        State.Done => colors.Ink,
        State.Failed => colors.Danger,
        _ => colors.Inactive,
    };

    private static Color Blend(Color from, Color to, double f) => WindowChrome.Mix(from, to, f);

    private void Indicator(Graphics g, Step step, float cx, float cy, double now)
    {
        var r = S(Dot) / 2f;
        var stroke = 1.6f * scale;
        switch (step.State)
        {
            case State.Waiting:
            {
                using var ring = new Pen(Blend(colors.Accent, colors.Line, Ease(step.Since, 0.2)), stroke);
                g.DrawEllipse(ring, cx - r + stroke / 2, cy - r + stroke / 2, 2 * r - stroke, 2 * r - stroke);
                break;
            }
            case State.Running:
            {
                var box = new RectangleF(cx - r + stroke, cy - r + stroke, 2 * r - 2 * stroke, 2 * r - 2 * stroke);
                using var track = new Pen(Blend(colors.Line, colors.Track, Ease(step.Since, 0.2)), stroke * 1.2f);
                g.DrawEllipse(track, box);
                // A head that runs ahead and a tail that catches up.
                var turn = now * 300 % 360;
                var sweep = 50 + 200 * (0.5 - 0.5 * Math.Cos(now * Math.PI * 2 / 1.4));
                using var arc = new Pen(colors.Accent, stroke * 1.2f) { StartCap = LineCap.Round, EndCap = LineCap.Round };
                g.DrawArc(arc, box, (float)turn, (float)(sweep * Ease(step.Since, 0.25)) + 1);
                break;
            }
            case State.Done:
            case State.Failed:
            {
                var pop = Ease(step.Since, 0.24);
                var radius = r * (float)(0.55 + 0.45 * pop);
                using (var fill = new SolidBrush(Color.FromArgb((int)(255 * Math.Min(1, pop * 1.6)), step.State == State.Done ? colors.Accent : colors.Danger)))
                    g.FillEllipse(fill, cx - radius, cy - radius, 2 * radius, 2 * radius);
                var draw = (float)Ease(step.Since, 0.28, 0.1);
                using var mark = new Pen(step.State == State.Done ? colors.AccentInk : Color.White, 1.7f * scale) { StartCap = LineCap.Round, EndCap = LineCap.Round, LineJoin = LineJoin.Round };
                if (step.State == State.Done)
                    Partial(g, mark, draw, new PointF(cx - 3.8f * scale, cy + 0.2f * scale), new PointF(cx - 1.1f * scale, cy + 2.9f * scale), new PointF(cx + 4f * scale, cy - 2.6f * scale));
                else
                {
                    var d = 3.1f * scale;
                    Partial(g, mark, Math.Min(1, draw * 2), new PointF(cx - d, cy - d), new PointF(cx + d, cy + d));
                    Partial(g, mark, Math.Max(0, draw * 2 - 1), new PointF(cx + d, cy - d), new PointF(cx - d, cy + d));
                }
                break;
            }
        }
    }

    // The first fraction of a polyline, by length: a mark drawn in one stroke.
    private static void Partial(Graphics g, Pen pen, float fraction, params PointF[] points)
    {
        if (fraction <= 0) return;
        var lengths = new float[points.Length - 1];
        for (var i = 0; i < lengths.Length; i++)
            lengths[i] = MathF.Sqrt(MathF.Pow(points[i + 1].X - points[i].X, 2) + MathF.Pow(points[i + 1].Y - points[i].Y, 2));
        var left = lengths.Sum() * fraction;
        var drawn = new List<PointF> { points[0] };
        for (var i = 0; i < lengths.Length && left > 0; i++)
        {
            var part = Math.Min(1, left / lengths[i]);
            drawn.Add(new PointF(points[i].X + (points[i + 1].X - points[i].X) * part, points[i].Y + (points[i + 1].Y - points[i].Y) * part));
            left -= lengths[i];
        }
        if (drawn.Count > 1) g.DrawLines(pen, drawn.ToArray());
    }

    private void Bar(Graphics g, float cy, double now)
    {
        var height = 4f * scale;
        var track = new RectangleF(barLeft, cy - height / 2, barRight - barLeft, height);
        using (var path = WindowChrome.CreateRoundedRectPath(track, height / 2))
        using (var brush = new SolidBrush(colors.Track))
            g.FillPath(brush, path);
        var filled = (float)(track.Width * Math.Clamp(shown, 0, 100) / 100);
        if (filled >= height)
        {
            var fill = new RectangleF(track.X, track.Y, filled, height);
            using var path = WindowChrome.CreateRoundedRectPath(fill, height / 2);
            using (var brush = new SolidBrush(colors.Accent)) g.FillPath(brush, path);
            // A soft light that passes along the filled part.
            var glow = 48f * scale;
            var x = fill.X - glow + (float)(now % 1.8 / 1.8) * (fill.Width + 2 * glow);
            using var light = new LinearGradientBrush(new RectangleF(x, fill.Y, glow, height), Color.Transparent, Color.Transparent, LinearGradientMode.Horizontal);
            var shine = Color.FromArgb(colors.Dark ? 70 : 90, colors.Dark ? Color.Black : Color.White);
            light.InterpolationColors = new ColorBlend { Colors = [Color.FromArgb(0, shine), shine, Color.FromArgb(0, shine)], Positions = [0f, 0.5f, 1f] };
            var clip = g.Clip;
            g.SetClip(path);
            g.FillRectangle(light, x, fill.Y, glow, height);
            g.Clip = clip;
        }
        var flags = TextFormatFlags.NoPadding | TextFormatFlags.SingleLine | TextFormatFlags.Right | TextFormatFlags.VerticalCenter;
        TextRenderer.DrawText(g, $"{Math.Round(shown):0}%", percentFont, new Rectangle(barRight, (int)(cy - S(10)), ClientSize.Width - S(Pad) - barRight, S(20)), colors.Muted, flags);
    }

    // No steps: a short segment that glides along a thin track.
    private void Indeterminate(Graphics g, double now)
    {
        var height = 3f * scale;
        var track = new RectangleF(S(Pad), S(StepsTop), ClientSize.Width - 2 * S(Pad), height);
        using var path = WindowChrome.CreateRoundedRectPath(track, height / 2);
        using (var brush = new SolidBrush(colors.Track)) g.FillPath(brush, path);
        var t = now % 1.6 / 1.6;
        var eased = t < 0.5 ? 4 * t * t * t : 1 - Math.Pow(-2 * t + 2, 3) / 2;
        var width = track.Width * 0.3f;
        var x = track.X - width + (float)eased * (track.Width + width);
        var clip = g.Clip;
        g.SetClip(path);
        using (var segment = WindowChrome.CreateRoundedRectPath(new RectangleF(x, track.Y, width, height), height / 2))
        using (var brush = new SolidBrush(colors.Accent))
            g.FillPath(brush, segment);
        g.Clip = clip;
    }

    private enum TaskbarState { None = 0, Indeterminate = 1, Normal = 2, Error = 4 }

    // The taskbar button carries the progress too: the window is often
    // minimized while the update runs.
    private void UpdateTaskbar()
    {
        if (result) return;
        if (steps.Count == 0) { SetTaskbar(TaskbarState.Indeterminate, 0); return; }
        if (active < 0) { SetTaskbar(TaskbarState.Normal, 0); return; }
        SetTaskbar(TaskbarState.Normal, (active + (target ?? 0) / 100.0) / steps.Count);
    }

    private void SetTaskbar(TaskbarState state, double fraction)
    {
        if (!IsHandleCreated) return;
        try
        {
            if (taskbar == null)
            {
                taskbar = (ITaskbarList3)new TaskbarList();
                taskbar.HrInit();
            }
            taskbar.SetProgressState(Handle, (int)state);
            if (state is TaskbarState.Normal or TaskbarState.Error)
                taskbar.SetProgressValue(Handle, (ulong)Math.Round(Math.Clamp(fraction, 0, 1) * 1000), 1000);
        }
        catch (Exception error) when (error is COMException or InvalidCastException or NotSupportedException) { taskbar = null; }
    }

    protected override void Dispose(bool disposing)
    {
        if (disposing)
        {
            frames.Dispose(); tip.Dispose(); productIcon?.Dispose();
            foreach (var font in new[] { titleFont, subtitleFont, stepFont, percentFont, statusFont, resultFont, buttonFont }) font?.Dispose();
        }
        base.Dispose(disposing);
    }

    [ComImport, Guid("ea1afb91-9e28-4b86-90e9-9e9f8a5eefaf"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface ITaskbarList3
    {
        void HrInit();
        void AddTab(IntPtr window);
        void DeleteTab(IntPtr window);
        void ActivateTab(IntPtr window);
        void SetActiveAlt(IntPtr window);
        void MarkFullscreenWindow(IntPtr window, [MarshalAs(UnmanagedType.Bool)] bool fullscreen);
        void SetProgressValue(IntPtr window, ulong completed, ulong total);
        void SetProgressState(IntPtr window, int state);
    }

    [ComImport, Guid("56FDF344-FD6D-11d0-958A-006097C9A090"), ClassInterface(ClassInterfaceType.None)]
    private class TaskbarList { }

    // A flat button with round corners: filled for the default action,
    // outlined for the others.
    private sealed class RoundButton : Button
    {
        private readonly Palette colors;
        private readonly bool primary;
        private bool hover, down;

        [System.ComponentModel.DesignerSerializationVisibility(System.ComponentModel.DesignerSerializationVisibility.Hidden)]
        public float Zoom { get; set; } = 1;

        public RoundButton(Palette colors, bool primary)
        {
            this.colors = colors; this.primary = primary;
            SetStyle(ControlStyles.AllPaintingInWmPaint | ControlStyles.OptimizedDoubleBuffer | ControlStyles.UserPaint, true);
            FlatStyle = FlatStyle.Flat; FlatAppearance.BorderSize = 0;
            BackColor = colors.Paper; Cursor = Cursors.Hand; UseMnemonic = false;
        }

        protected override void OnMouseEnter(EventArgs e) { hover = true; Invalidate(); base.OnMouseEnter(e); }
        protected override void OnMouseLeave(EventArgs e) { hover = false; down = false; Invalidate(); base.OnMouseLeave(e); }
        protected override void OnMouseDown(MouseEventArgs e) { down = e.Button == MouseButtons.Left; Invalidate(); base.OnMouseDown(e); }
        protected override void OnMouseUp(MouseEventArgs e) { down = false; Invalidate(); base.OnMouseUp(e); }
        protected override void OnEnabledChanged(EventArgs e) { Invalidate(); base.OnEnabledChanged(e); }

        protected override void OnPaint(PaintEventArgs e)
        {
            var g = e.Graphics;
            g.Clear(colors.Paper);
            g.SmoothingMode = SmoothingMode.AntiAlias;
            g.PixelOffsetMode = PixelOffsetMode.HighQuality;
            var bounds = new RectangleF(0.5f, 0.5f, Width - 1f, Height - 1f);
            using var path = WindowChrome.CreateRoundedRectPath(bounds, 7 * Zoom);
            Color fill, ink;
            if (primary)
            {
                fill = !Enabled ? WindowChrome.Mix(colors.Button, colors.Paper, 0.55) : down ? WindowChrome.Mix(colors.Button, colors.Paper, 0.22) : hover ? WindowChrome.Mix(colors.Button, colors.Paper, 0.12) : colors.Button;
                ink = colors.ButtonInk;
            }
            else
            {
                fill = down ? colors.Pressed : hover && Enabled ? colors.Hover : colors.Paper;
                ink = Enabled ? colors.Ink : colors.Muted;
            }
            using (var brush = new SolidBrush(fill)) g.FillPath(brush, path);
            if (!primary)
                using (var border = new Pen(colors.Border, 1f)) g.DrawPath(border, path);
            if (Focused && ShowFocusCues)
            {
                var inset = 2.5f * Zoom;
                using var ring = WindowChrome.CreateRoundedRectPath(RectangleF.Inflate(bounds, -inset, -inset), 5 * Zoom);
                using var pen = new Pen(Color.FromArgb(150, primary ? colors.ButtonInk : colors.Muted), 1f) { DashStyle = DashStyle.Dot };
                g.DrawPath(pen, ring);
            }
            TextRenderer.DrawText(g, Text, Font, ClientRectangle, ink, fill,
                TextFormatFlags.HorizontalCenter | TextFormatFlags.VerticalCenter | TextFormatFlags.SingleLine | TextFormatFlags.NoPrefix | TextFormatFlags.NoPadding);
        }
    }
}
