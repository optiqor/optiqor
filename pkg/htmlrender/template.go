package htmlrender

// documentTemplate is the single self-contained HTML document. Inline
// CSS only — must render identically from file:// and over HTTP.
// Brand mirror: optiqor-cli/brand/tokens.json — update in lockstep.
const documentTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="robots" content="noindex">
  <meta property="og:title" content="{{.Title}}">
  <meta property="og:description" content="{{.AccuracyDisclosure}}">
  <meta property="og:type" content="article">
  <meta name="twitter:card" content="summary">
  <title>{{.Title}}</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500&display=swap">
  <style>
    :root {
      --ink-0:#0A0A0B; --ink-1:#111114; --ink-2:#16161A; --ink-3:#1C1C22;
      --ink-4:#24242B; --ink-5:#33333D; --ink-6:#5C5C68; --ink-7:#9C9CA8;
      --ink-8:#C8C8D0; --ink-9:#EDEDEE;
      --accent:#22D3EE; --accent-dim:#0E7490;
      --high:#FF6B6B; --med:#F59E0B; --low:#22D3EE; --ok:#34D399;
      --rule:#1F1F25; --rule-default:#2A2A33;
      --radius-sm:3px; --radius-md:6px; --radius-lg:10px;
      --font-sans: Geist, ui-sans-serif, system-ui, -apple-system, "Segoe UI", Helvetica, Arial, sans-serif;
      --font-mono: "Geist Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    }
    *,*::before,*::after { box-sizing:border-box; }
    html,body { margin:0; padding:0; background:var(--ink-0); color:var(--ink-9); font-family:var(--font-sans); -webkit-font-smoothing:antialiased; }
    body {
      background-image:
        linear-gradient(to right, rgba(255,255,255,.015) 1px, transparent 1px),
        linear-gradient(to bottom, rgba(255,255,255,.015) 1px, transparent 1px);
      background-size: 64px 64px;
      min-height:100vh;
    }
    ::selection { background:rgba(34,211,238,.25); color:var(--ink-9); }
    a { color:var(--accent); text-decoration:none; }
    a:hover { text-decoration:underline; text-underline-offset:2px; }
    .wrap { max-width: 1120px; margin: 0 auto; padding: 56px 32px 96px; }
    .topline {
      display:flex; align-items:center; justify-content:space-between;
      padding-bottom:18px; border-bottom:1px solid var(--rule); margin-bottom:32px;
      color:var(--ink-7);
    }
    .brand { display:inline-flex; align-items:center; gap:10px; color:var(--ink-9); }
    .brand svg { width:22px; height:22px; }
    .brand-text {
      font-family:var(--font-mono); font-size:12px; letter-spacing:.18em;
      text-transform:uppercase;
    }
    .pill {
      display:inline-flex; align-items:center; gap:6px;
      font-family:var(--font-mono); font-size:11px; letter-spacing:.08em;
      padding:3px 10px; border-radius:999px;
      box-shadow: inset 0 0 0 1px var(--rule-default);
      color:var(--ink-7);
    }
    .pill .dot { width:6px; height:6px; border-radius:999px; background:var(--accent); }
    .eyebrow {
      font-family:var(--font-mono); font-size:11px; letter-spacing:.14em;
      text-transform:uppercase; color:var(--ink-7);
    }
    h1 {
      font-size: clamp(28px, 4.4vw, 44px); line-height:1.06; letter-spacing:-.02em;
      font-weight:500; margin:14px 0 8px;
    }
    h1 .muted { color:var(--ink-7); }
    .meta { color:var(--ink-7); font-size:14px; }
    .summary {
      margin-top:28px; display:grid; grid-template-columns: repeat(4, 1fr);
      gap:1px; background:var(--rule);
      box-shadow: inset 0 0 0 1px var(--rule-default);
      border-radius:var(--radius-lg); overflow:hidden;
    }
    @media (max-width:720px) { .summary { grid-template-columns: repeat(2, 1fr); } }
    .stat { background:var(--ink-1); padding:18px 20px; }
    .stat .v { font-family:var(--font-mono); font-variant-numeric:tabular-nums; font-size:24px; letter-spacing:-.02em; }
    .stat .v.accent { color:var(--accent); }
    .stat .v.ok { color:var(--ok); }
    .stat .l { margin-top:4px; font-family:var(--font-mono); font-size:10px;
               letter-spacing:.12em; text-transform:uppercase; color:var(--ink-6); }
    .stat .sub { color:var(--ink-7); margin-left:6px; }

    .share {
      margin-top:16px; display:flex; align-items:center; gap:10px;
      background:var(--ink-2); padding:10px 12px; border-radius:var(--radius-md);
      box-shadow: inset 0 0 0 1px var(--rule-default);
    }
    .share .label { font-family:var(--font-mono); font-size:11px; color:var(--ink-6); }
    .share code {
      flex:1; min-width:0; white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
      font-family:var(--font-mono); font-size:12px; color:var(--ink-8);
    }
    .share button {
      appearance:none; background:transparent; color:var(--ink-9);
      border:none; padding:4px 8px; font-family:var(--font-mono); font-size:11px;
      cursor:pointer; border-radius:var(--radius-sm);
    }
    .share button:hover { background:var(--ink-3); }

    .section-head {
      margin: 56px 0 16px; display:flex; align-items:center; gap:14px;
    }
    .section-head .label {
      font-family:var(--font-mono); font-size:11px; letter-spacing:.12em;
      text-transform:uppercase;
    }
    .section-head .label.cost { color:var(--accent); }
    .section-head .label.bonus { color:var(--med); }
    .section-head .rule { flex:1; height:1px; background:linear-gradient(to right, transparent, var(--rule-default) 8%, var(--rule-default) 92%, transparent); }

    .findings { display:grid; gap:10px; }
    .f {
      background:var(--ink-1); padding:16px 18px; border-radius:var(--radius-md);
      box-shadow: inset 0 0 0 1px var(--rule-default);
    }
    .f-row { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
    .sev {
      min-width:46px; text-align:center;
      font-family:var(--font-mono); font-size:11px; letter-spacing:.08em;
      padding:2px 8px; border-radius:var(--radius-sm);
    }
    .sev-high { background:rgba(255,107,107,.12); color:var(--high); }
    .sev-med  { background:rgba(245,158,11,.12); color:var(--med); }
    .sev-low  { background:rgba(34,211,238,.12); color:var(--low); }
    .sev-info { background:rgba(156,156,168,.18); color:var(--ink-8); }
    .wl { font-family:var(--font-mono); font-size:12px; letter-spacing:.04em; color:var(--accent); }
    .save { margin-left:auto; font-family:var(--font-mono); font-variant-numeric:tabular-nums; font-size:13px; color:var(--ok); }
    .f-title { margin-top:10px; font-size:14.5px; color:var(--ink-9); }
    .f-detail { margin-top:6px; font-size:13px; line-height:1.6; color:var(--ink-7); }
    .conf {
      margin-top:8px; display:inline-flex; gap:6px;
      font-family:var(--font-mono); font-size:11px; color:var(--ink-6);
    }
    .conf .dots .a { color:var(--accent); }
    .conf .dots .o { color:var(--ink-6); }

    .clean {
      background:var(--ink-1); padding:24px; border-radius:var(--radius-md);
      box-shadow: inset 0 0 0 1px var(--rule-default); margin-top:32px;
    }
    .clean .ok { color:var(--ok); font-size:14px; }
    .clean p { color:var(--ink-7); margin-top:8px; font-size:13px; }

    .accuracy {
      margin-top:64px; padding-top:24px; border-top:1px solid var(--rule);
      display:flex; gap:14px; align-items:flex-start;
      font-family:var(--font-mono); font-size:12px; line-height:1.6; color:var(--ink-7);
    }
    .accuracy svg { width:16px; height:16px; flex-shrink:0; margin-top:2px; opacity:.6; }
    .accuracy strong { color:var(--ink-9); font-weight:500; }

    .footer-meta {
      margin-top:32px; font-family:var(--font-mono); font-size:11px; letter-spacing:.08em;
      color:var(--ink-6); display:flex; justify-content:space-between; flex-wrap:wrap; gap:8px;
    }
    .footer-meta a { color:inherit; border-bottom:1px solid transparent; }
    .footer-meta a:hover { color:var(--ink-9); border-bottom-color:var(--accent); text-decoration:none; }
  </style>
</head>
<body>
  <main class="wrap">
    <div class="topline">
      <span class="brand">
        {{ template "logo" . }}
        <span class="brand-text">Optiqor</span>
      </span>
      <span class="pill"><span class="dot"></span>{{.Mode}} · ±40%</span>
    </div>

    <p class="eyebrow">Analysis</p>
    <h1>
      {{.Source}}<br>
      <span class="muted">
        {{- if .Totals.HasSavings -}}
          potential savings {{.Totals.MonthlyUSD}}<span style="color:var(--ink-7)"> /mo</span>
        {{- else -}}
          no cost optimisations detected
        {{- end -}}
      </span>
    </h1>
    <p class="meta">{{.Totals.WorkloadsLabel}} analysed · generated {{.GeneratedAtISO}}</p>

    <div class="summary">
      <div class="stat">
        <div class="v">{{.Workloads}}</div>
        <div class="l">Workloads</div>
      </div>
      <div class="stat">
        <div class="v accent">{{len .Cost}}</div>
        <div class="l">Cost opts</div>
      </div>
      <div class="stat">
        <div class="v">{{len .Security}}<span class="sub" style="font-family:var(--font-mono); font-size:10px;"> · bonus</span></div>
        <div class="l">Security</div>
      </div>
      <div class="stat">
        <div class="v ok">{{ if .Totals.HasSavings }}{{.Totals.MonthlyUSD}}{{ else }}—{{ end }}</div>
        <div class="l">/mo {{ if .Totals.HasSavings }}<span class="sub">· ~{{.Totals.AnnualUSD}}/yr</span>{{ end }}</div>
      </div>
    </div>

    {{ if .HasShareURL }}
    <div class="share">
      <span class="label">share</span>
      <code id="share-url">{{.ShareURL}}</code>
      <button type="button" id="copy-btn" data-copy="{{.ShareURL}}">copy</button>
    </div>
    {{ end }}

    {{ if .HasCost }}
    <div class="section-head">
      <span class="label cost">━━ Cost optimizations</span>
      <span class="rule"></span>
    </div>
    <div class="findings">
      {{ range .Cost }}{{ template "finding" . }}{{ end }}
    </div>
    {{ end }}

    {{ if .HasSecurity }}
    <div class="section-head">
      <span class="label bonus">━━ Security findings · bonus · {{len .Security}}</span>
      <span class="rule"></span>
    </div>
    <p class="meta" style="margin-bottom:14px; font-style:italic;">
      Spotted while parsing your chart. Cost is the headline; this is a bonus.
    </p>
    <div class="findings">
      {{ range .Security }}{{ template "finding" . }}{{ end }}
    </div>
    {{ end }}

    {{ if .Clean }}
    <div class="clean">
      <div class="ok">✓ Clean. No findings.</div>
      <p>Either your chart is already optimised, or the detectors didn&apos;t see enough signal in the values you supplied.</p>
    </div>
    {{ end }}

    <div class="accuracy">
      {{ template "logo" . }}
      <span><strong>{{.AccuracyDisclosure}}</strong></span>
    </div>

    <div class="footer-meta">
      <span>{{.GeneratedAtISO}} · generated by <a href="https://optiqor.dev">optiqor.dev</a></span>
      <span>Apache-2.0 · github.com/optiqor/optiqor-cli</span>
    </div>
  </main>

  {{ if .HasShareURL }}
  <script>
    (function () {
      var btn = document.getElementById('copy-btn');
      if (!btn) return;
      btn.addEventListener('click', function () {
        var url = btn.getAttribute('data-copy') || '';
        if (!url) return;
        navigator.clipboard.writeText(url).then(function () {
          var old = btn.textContent;
          btn.textContent = '✓ copied';
          setTimeout(function () { btn.textContent = old; }, 1500);
        });
      });
    })();
  </script>
  {{ end }}
</body>
</html>

{{ define "logo" }}
<svg viewBox="0 0 64 64" fill="none" aria-hidden="true">
  <path d="M32 6 a26 26 0 1 1 -18.4 44.4" stroke="currentColor" stroke-width="4" stroke-linecap="round"/>
  <line x1="32" y1="32" x2="50" y2="50" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
  <circle cx="22" cy="32" r="4.5" fill="#22D3EE"/>
  <circle cx="50" cy="50" r="2.5" fill="#22D3EE"/>
</svg>
{{ end }}

{{ define "finding" }}
<div class="f">
  <div class="f-row">
    <span class="sev {{.SeverityCls}}">{{.Severity}}</span>
    <span class="wl">{{.Workload}}</span>
    {{ if .HasSavings }}<span class="save">save ~{{.SavingsUSD}}/mo</span>{{ end }}
  </div>
  <div class="f-title">{{.Title}}</div>
  {{ if .Detail }}<div class="f-detail">{{.Detail}}</div>{{ end }}
  <div class="conf">
    confidence:
    <span class="dots">
      {{ if eq .Confidence "high" }}<span class="a">●●●</span>{{ end }}
      {{ if eq .Confidence "medium" }}<span class="a">●●</span><span class="o">○</span>{{ end }}
      {{ if eq .Confidence "low" }}<span class="a">●</span><span class="o">○○</span>{{ end }}
    </span>
    <span>{{.Confidence}}</span>
  </div>
</div>
{{ end }}
`
