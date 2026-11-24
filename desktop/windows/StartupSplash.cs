using System.Text.Json;

namespace Cage.Windows;

// The window shows this page while the backend starts, so startup is one
// animation instead of a line of text. It draws the same splash as the
// workbench (ui/src/startup-splash.jsx, cage-logo.jsx and their CSS) at the
// same place; the workbench then continues it without replaying the intro.
// Keep the geometry and timings in step with those files.
internal static class StartupSplash
{
    internal static bool IsDark(Color background) => background.GetBrightness() < 0.5f;

    internal static string Hex(Color color) => $"#{color.R:x2}{color.G:x2}{color.B:x2}";

    // Read by the workbench's splash on its first page load; `shown` (Unix ms)
    // lets it carry on from the same moment of the animation.
    internal static string HandoffScript(Color background, long shown) =>
        "window.cageStartupSplash = Object.freeze(" + JsonSerializer.Serialize(new { background = Hex(background), dark = IsDark(background), shown }) + ");";

    internal static string Html(Color background)
    {
        var dark = IsDark(background);
        return Template
            .Replace("{{background}}", Hex(background))
            .Replace("{{bars}}", dark ? "#ece7df" : "#282828");
    }

    private const string Template = """
        <!doctype html><html><head><meta charset="utf-8"><style>
        html, body { margin: 0; height: 100%; overflow: hidden; background: {{background}}; }
        body { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 22px; font-size: 14px; }
        .stage { position: relative; width: 132px; height: 132px; display: grid; place-items: center; }
        .glow { position: absolute; inset: -34px; border-radius: 50%; background: radial-gradient(circle, color-mix(in srgb, #ee6e45 42%, transparent) 0%, transparent 68%); animation: glow 2.4s ease-in-out infinite; }
        svg { position: relative; width: 132px; height: 132px; overflow: visible; shape-rendering: crispEdges; }
        .crab rect { fill: #ee6e45; } .crab .eyes rect { fill: #282828; }
        .crab { animation: bob 1.1s steps(1, end) infinite; }
        .legs-a { animation: legs 0.55s steps(1, end) infinite; }
        .legs-b { animation: legs 0.55s steps(1, end) infinite reverse; }
        .eyes { transform-box: fill-box; transform-origin: center; animation: blink 3.2s ease-in-out infinite; }
        .frame { fill: none; stroke: {{bars}}; stroke-width: 7.2; stroke-linecap: square; stroke-linejoin: miter; stroke-dasharray: 100; stroke-dashoffset: 100; animation: frame 0.7s cubic-bezier(.6, 0, .3, 1) 0.05s forwards; }
        .bar { fill: {{bars}}; transform: translateY(-110px); animation: bar 0.45s cubic-bezier(.3, 1.4, .5, 1) forwards; }
        .bar-1 { animation-delay: 0.55s; } .bar-2 { animation-delay: 0.72s; }
        .status { height: 19.5px; }
        .dots { display: flex; gap: 6px; height: 6px; }
        .dots i { width: 6px; height: 6px; background: #ee6e45; opacity: 0.25; animation: dot 1.2s steps(1, end) infinite; }
        .dots i:nth-child(2) { animation-delay: 0.4s; } .dots i:nth-child(3) { animation-delay: 0.8s; }
        @keyframes glow { 0%, 100% { transform: scale(0.88); opacity: 0.55; } 50% { transform: scale(1.08); opacity: 1; } }
        @keyframes bob { 0% { transform: translateY(0); } 50% { transform: translateY(-2.5px); } }
        @keyframes legs { 0% { transform: translateY(0); } 50% { transform: translateY(-1.5px); } }
        @keyframes blink { 0%, 92%, 100% { transform: scaleY(1); } 95% { transform: scaleY(0.1); } }
        @keyframes frame { to { stroke-dashoffset: 0; } }
        @keyframes bar { to { transform: translateY(0); } }
        @keyframes dot { 0% { opacity: 1; } 33% { opacity: 0.25; } }
        @media (prefers-reduced-motion: reduce) {
          *, *::before, *::after { animation: none !important; }
          .frame { stroke-dashoffset: 0; } .bar { transform: none; }
        }
        </style></head><body>
        <div class="stage"><div class="glow"></div>
        <svg viewBox="0 0 100 100" aria-hidden="true">
          <g class="crab">
            <rect x="20.5" y="29.5" width="59.2" height="37" />
            <rect x="12.5" y="47.5" width="8.5" height="10" />
            <rect x="79.2" y="47.5" width="8.3" height="10" />
            <g class="legs-a"><rect x="24.8" y="66" width="4.6" height="9" /><rect x="57.3" y="66" width="5.9" height="9" /></g>
            <g class="legs-b"><rect x="36.5" y="66" width="5.8" height="9" /><rect x="70.6" y="66" width="4.6" height="9" /></g>
            <g class="eyes"><rect x="36.5" y="37.2" width="5.6" height="10.3" /><rect x="58" y="37.2" width="5.6" height="10.3" /></g>
          </g>
          <rect class="frame" x="7.3" y="12.3" width="85.4" height="73.6" pathLength="100" />
          <rect class="bar bar-1" x="29" y="12" width="5.8" height="74" />
          <rect class="bar bar-2" x="65.2" y="12" width="5.8" height="74" />
        </svg></div>
        <div class="status"></div>
        <div class="dots"><i></i><i></i><i></i></div>
        </body></html>
        """;
}
