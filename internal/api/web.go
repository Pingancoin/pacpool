package api

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Pingancoin/pacpool/internal/service"
)

type dashboardCopy struct {
	Lang              string
	Title             string
	Subtitle          string
	StatusHealthy     string
	StatusDegraded    string
	Ready             string
	Waiting           string
	Height            string
	Peers             string
	NetworkDifficulty string
	NetworkHashrate   string
	Miners            string
	Accepted          string
	Solved            string
	Fee               string
	PendingPayout     string
	PayoutThreshold   string
	CurrentRound      string
	Stratum           string
	Template          string
	Workers           string
	NoWorkers         string
	Worker            string
	Difficulty        string
	Rejected          string
	LastShare         string
	RecentRounds      string
	NoRounds          string
	Round             string
	State             string
	Work              string
	Block             string
	Updated           string
	API               string
	Explorer          string
	PoolStatus        string
	Payouts           string
	PaymentRecords    string
	NoPayments        string
	PaymentTime       string
	PaymentAmount     string
	PaymentTxID       string
	PaymentRecipients string
	MiningGuide       string
	MiningURL         string
	MiningUsername    string
	MiningPassword    string
	MiningExample     string
	MiningPasswordAny string
	MinerDownload     string
	WalletDownload    string
	ViewWorkers       string
	HomeNav           string
	BlocksNav         string
	MinerLookup       string
	MinerAddress      string
	Lookup            string
	OnlineMachines    string
	TotalEarned       string
	TotalPaid         string
	TodayEarned       string
	Unpaid            string
	NoMinerData       string
	LastPayment       string
	LangEnglish       string
	LangChinese       string
	LangJapanese      string
	LangKorean        string
	ThemeBlack        string
	ThemeWhite        string
	StratumClosedNote string
	TemplateReady     string
	TemplateWaiting   string
	DayMode           string
	NightMode         string
}

type dashboardView struct {
	Copy           dashboardCopy
	Status         service.State
	Updated        string
	HealthLabel    string
	StratumLabel   string
	StratumNote    string
	TemplateLabel  string
	Height         string
	Peers          string
	NetworkDiff    string
	NetworkHash    string
	Miners         string
	Accepted       string
	Rejected       string
	Solved         string
	Fee            string
	PendingPayout  string
	PayoutMin      string
	CurrentRoundID string
	StratumHost    string
	ExplorerURL    string
	APIURL         string
	StatusURL      string
	PayoutsURL     string
	MiningURL      string
	UsernameSample string
	WorkersURL     string
	BlocksURL      string
	MinerURL       string
	WalletURL      string
	MinerQuery     string
	MinerSearched  bool
	MinerFound     bool
	MinerStats     service.MinerStats
	MinerUnpaid    string
	MinerPaid      string
	MinerTotal     string
	MinerToday     string
	MinerLastPay   string
}

var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{"timeText": timeText, "formatPAC": formatPAC, "shortText": shortText, "hasOnlineWorkers": hasOnlineWorkers}).Parse(`<!doctype html>
<html lang="{{.Copy.Lang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Copy.Title}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f4;
      --panel: #ffffff;
      --panel-soft: #eef3ee;
      --text: #1a201a;
      --muted: #5f6b60;
      --line: #d9dfd6;
      --accent: #2e7d5b;
      --accent-strong: #15553e;
      --warn: #a05a13;
      --shadow: 0 12px 30px rgba(20, 34, 26, .08);
    }
    body[data-theme="dark"] {
      color-scheme: dark;
      --bg: #101411;
      --panel: #171d18;
      --panel-soft: #1d271f;
      --text: #edf3ed;
      --muted: #a9b6aa;
      --line: #2b362e;
      --accent: #6bd49e;
      --accent-strong: #9df0bd;
      --warn: #f0b35a;
      --shadow: none;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font: 15px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    a { color: var(--accent-strong); text-decoration: none; }
    a:hover { text-decoration: underline; }
    .page { width: min(1180px, calc(100% - 32px)); margin: 0 auto; padding: 28px 0 44px; }
    header { display: grid; grid-template-columns: minmax(360px, 1fr) auto minmax(300px, .75fr); align-items: flex-start; gap: 28px; margin-bottom: 22px; }
    h1 { margin: 0 0 6px; font-size: clamp(28px, 5vw, 46px); line-height: 1.05; letter-spacing: 0; }
    .subtitle { margin: 0; color: var(--muted); max-width: 720px; font-size: 16px; }
    .header-tools { display: grid; justify-items: end; gap: 10px; }
    .page-nav { display: flex; gap: 18px; flex-wrap: wrap; justify-content: center; margin-top: 48px; }
    .controls { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
    .theme-toggle, .language-select {
      min-height: 32px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--panel);
      color: var(--text);
      padding: 4px 11px;
      font-size: 13px;
    }
    .language-select { padding-right: 28px; }
    .page-nav a, .pill {
      min-height: 32px;
      display: inline-flex;
      align-items: center;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 4px 11px;
      background: var(--panel);
      color: var(--text);
      white-space: nowrap;
    }
    .page-nav a {
      min-height: 42px;
      padding: 7px 20px;
      font-size: 17px;
      font-weight: 700;
    }
    .pill.ok { border-color: rgba(46, 125, 91, .45); color: var(--accent-strong); }
    .pill.warn { border-color: rgba(160, 90, 19, .45); color: var(--warn); }
    .hero { margin-bottom: 16px; }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
      padding: 18px;
    }
    .metrics {
      display: grid;
      grid-template-columns: repeat(4, minmax(140px, 1fr));
      gap: 12px;
    }
    .metric {
      background: var(--panel-soft);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 14px;
      min-height: 92px;
    }
    .label { color: var(--muted); font-size: 13px; margin-bottom: 7px; }
    .value { font-size: 24px; font-weight: 700; line-height: 1.15; word-break: break-word; }
    h2 { margin: 0 0 12px; font-size: 19px; line-height: 1.2; }
    .note { margin: 10px 0 0; color: var(--muted); }
    .links { display: grid; gap: 9px; }
    .linkrow { display: flex; justify-content: space-between; gap: 12px; border-bottom: 1px solid var(--line); padding-bottom: 8px; }
    .linkrow:last-child { border-bottom: 0; padding-bottom: 0; }
    .grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 1fr); gap: 16px; }
    .stack { display: grid; gap: 16px; }
    .guide { display: grid; grid-template-columns: 1fr; gap: 7px; }
    .guide-item { display: grid; grid-template-columns: 104px minmax(0, 1fr); gap: 10px; align-items: baseline; background: var(--panel-soft); border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px; min-width: 0; }
    .guide-panel { padding: 12px 14px; }
    .guide-panel h2 { margin-bottom: 8px; }
    .guide-panel .label { margin-bottom: 4px; }
    .guide-panel .note { margin-top: 7px; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; word-break: break-all; }
    .miner-search { display: grid; grid-template-columns: minmax(220px, 1fr) auto; gap: 10px; margin-bottom: 14px; }
    .lookup-panel { margin-bottom: 16px; padding: 14px 16px; }
    .lookup-bar { display: grid; grid-template-columns: auto minmax(260px, 1fr); gap: 14px; align-items: center; }
    .lookup-bar h2 { margin: 0; white-space: nowrap; }
    .lookup-bar .miner-search { margin: 0; }
    .lookup-result { margin-top: 14px; }
    input, button {
      min-height: 38px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: var(--panel);
      color: var(--text);
      font: inherit;
    }
    input { padding: 7px 10px; }
    button {
      padding: 7px 15px;
      background: var(--accent);
      border-color: var(--accent);
      color: #fff;
      font-weight: 650;
      cursor: pointer;
    }
    .miner-metrics { grid-template-columns: repeat(5, minmax(120px, 1fr)); }
    table { width: 100%; border-collapse: collapse; }
    th, td { padding: 10px 8px; border-bottom: 1px solid var(--line); text-align: left; vertical-align: top; }
    th { color: var(--muted); font-size: 13px; font-weight: 600; }
    .empty { color: var(--muted); padding: 18px 0 4px; }
    footer { margin-top: 18px; color: var(--muted); font-size: 13px; }
    @media (max-width: 860px) {
      header, .grid { grid-template-columns: 1fr; display: grid; }
      header { gap: 12px; }
      .header-tools { justify-items: start; }
      .page-nav { justify-content: flex-start; margin-top: 0; gap: 8px; }
      .page-nav a { min-height: 34px; padding: 4px 11px; font-size: 14px; }
      .controls { justify-content: flex-start; }
      .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .lookup-bar { grid-template-columns: 1fr; gap: 10px; }
      .lookup-bar h2 { white-space: normal; }
    }
    @media (max-width: 520px) {
      .page { width: min(100% - 22px, 1180px); padding-top: 18px; }
      .metrics, .miner-metrics, .miner-search, .guide-item { grid-template-columns: 1fr; }
      .panel { padding: 14px; }
      th, td { padding: 8px 5px; font-size: 13px; }
    }
  </style>
</head>
<body>
  <main class="page">
    <header>
      <div>
        <h1>{{.Copy.Title}}</h1>
        <p class="subtitle">{{.Copy.Subtitle}}</p>
      </div>
      <nav class="page-nav" aria-label="Pages">
        <a href="/?lang={{.Copy.Lang}}">{{.Copy.HomeNav}}</a>
        <a href="{{.WorkersURL}}">{{.Copy.ViewWorkers}}</a>
        <a href="{{.BlocksURL}}">{{.Copy.BlocksNav}}</a>
      </nav>
      <div class="header-tools">
        <div class="controls">
          <button class="theme-toggle" type="button" data-dark-label="{{.Copy.ThemeBlack}}" data-light-label="{{.Copy.ThemeWhite}}">{{.Copy.ThemeBlack}}</button>
          <select class="language-select" data-path="/">
            <option value="en" {{if eq .Copy.Lang "en"}}selected{{end}}>English</option>
            <option value="zh-CN" {{if eq .Copy.Lang "zh-CN"}}selected{{end}}>简体中文</option>
            <option value="ja" {{if eq .Copy.Lang "ja"}}selected{{end}}>日本語</option>
            <option value="ko" {{if eq .Copy.Lang "ko"}}selected{{end}}>한국어</option>
          </select>
        </div>
      </div>
    </header>

    <section class="hero">
      <div class="panel">
        <div class="metrics">
          <div class="metric"><div class="label">{{.Copy.Height}}</div><div class="value">{{.Height}}</div></div>
          <div class="metric"><div class="label">{{.Copy.NetworkDifficulty}}</div><div class="value">{{.NetworkDiff}}</div></div>
          <div class="metric"><div class="label">{{.Copy.NetworkHashrate}}</div><div class="value">{{.NetworkHash}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Miners}}</div><div class="value">{{.Miners}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Solved}}</div><div class="value">{{.Solved}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Fee}}</div><div class="value">{{.Fee}}</div></div>
          <div class="metric"><div class="label">{{.Copy.PendingPayout}}</div><div class="value">{{.PendingPayout}}</div></div>
          <div class="metric"><div class="label">{{.Copy.PayoutThreshold}}</div><div class="value">{{.PayoutMin}}</div></div>
        </div>
      </div>
    </section>

    <section class="panel lookup-panel">
      <div class="lookup-bar">
        <h2>{{.Copy.MinerLookup}}</h2>
        <form method="get" class="miner-search">
          <input type="hidden" name="lang" value="{{.Copy.Lang}}">
          <input name="miner" value="{{.MinerQuery}}" placeholder="{{.Copy.MinerAddress}}" autocomplete="off">
          <button type="submit">{{.Copy.Lookup}}</button>
        </form>
      </div>
      {{if .MinerSearched}}
        <div class="lookup-result">
        {{if .MinerFound}}
          <div class="metrics miner-metrics">
            <div class="metric"><div class="label">{{.Copy.OnlineMachines}}</div><div class="value">{{.MinerStats.OnlineMachines}}</div></div>
            <div class="metric"><div class="label">{{.Copy.TotalEarned}}</div><div class="value">{{.MinerTotal}}</div></div>
            <div class="metric"><div class="label">{{.Copy.TotalPaid}}</div><div class="value">{{.MinerPaid}}</div></div>
            <div class="metric"><div class="label">{{.Copy.TodayEarned}}</div><div class="value">{{.MinerToday}}</div></div>
            <div class="metric"><div class="label">{{.Copy.Unpaid}}</div><div class="value">{{.MinerUnpaid}}</div></div>
          </div>
          <p class="note">{{.Copy.LastPayment}} {{.MinerLastPay}}</p>
        {{else}}
          <div class="empty">{{.Copy.NoMinerData}}</div>
        {{end}}
        </div>
      {{end}}
    </section>

    <section class="grid">
      <div class="stack">
        <section class="panel guide-panel">
          <h2>{{.Copy.MiningGuide}}</h2>
          <div class="guide">
            <div class="guide-item"><div class="label">{{.Copy.MiningURL}}</div><div class="mono">{{.MiningURL}}</div></div>
            <div class="guide-item"><div class="label">{{.Copy.MiningUsername}}</div><div class="mono">{{.UsernameSample}}</div></div>
            <div class="guide-item"><div class="label">{{.Copy.MiningPassword}}</div><div>{{.Copy.MiningPasswordAny}}</div></div>
            <div class="guide-item"><div class="label">{{.Copy.MinerDownload}}</div><div><a href="{{.MinerURL}}">github.com/Pingancoin/pacminer</a></div></div>
            <div class="guide-item"><div class="label">{{.Copy.WalletDownload}}</div><div><a href="{{.WalletURL}}">pingancoin.org/#wallet</a></div></div>
          </div>
          <p class="note">{{.Copy.MiningExample}}</p>
        </section>
      </div>

      <div class="stack">
        <section class="panel">
          <h2>{{.Copy.PaymentRecords}}</h2>
          {{if .Status.Pool.Payments}}
          <table>
            <thead><tr><th>{{.Copy.PaymentTime}}</th><th>{{.Copy.PaymentAmount}}</th><th>{{.Copy.PaymentRecipients}}</th><th>{{.Copy.PaymentTxID}}</th></tr></thead>
            <tbody>
            {{range .Status.Pool.Payments}}
              <tr><td>{{timeText .CreatedAt}}</td><td>{{formatPAC .Total}}</td><td>{{len .Payouts}}</td><td class="mono">{{shortText .TxID 18}}</td></tr>
            {{end}}
            </tbody>
          </table>
          {{else}}<div class="empty">{{.Copy.NoPayments}}</div>{{end}}
        </section>
      </div>
    </section>

    <section class="panel" style="margin-top:16px">
      <h2>{{.Copy.API}}</h2>
      <div class="links">
        <div class="linkrow"><span>{{.Copy.Explorer}}</span><a href="{{.ExplorerURL}}">{{.ExplorerURL}}</a></div>
        <div class="linkrow"><span>{{.Copy.PoolStatus}}</span><a href="{{.StatusURL}}">{{.StatusURL}}</a></div>
        <div class="linkrow"><span>{{.Copy.Payouts}}</span><a href="{{.PayoutsURL}}">{{.PayoutsURL}}</a></div>
      </div>
    </section>

    <footer>{{.Copy.Updated}} {{.Updated}}</footer>
  </main>
  <script>
    (function () {
      var key = "pacpool-theme";
      var button = document.querySelector(".theme-toggle");
      var lang = document.querySelector(".language-select");
      function apply(theme) {
        if (theme !== "dark") theme = "light";
        document.body.setAttribute("data-theme", theme);
        try { localStorage.setItem(key, theme); } catch (error) {}
        if (button) button.textContent = theme === "dark" ? button.getAttribute("data-light-label") : button.getAttribute("data-dark-label");
      }
      var saved = "light";
      try { saved = localStorage.getItem(key) || saved; } catch (error) {}
      if (button) button.addEventListener("click", function () {
        apply(document.body.getAttribute("data-theme") === "dark" ? "light" : "dark");
      });
      if (lang) lang.addEventListener("change", function () {
        window.location.href = (lang.getAttribute("data-path") || "/") + "?lang=" + encodeURIComponent(lang.value);
      });
      apply(saved);
    })();
  </script>
</body>
</html>`))

var workersTemplate = template.Must(template.New("workers").Funcs(template.FuncMap{"timeText": timeText, "hasOnlineWorkers": hasOnlineWorkers}).Parse(`<!doctype html>
<html lang="{{.Copy.Lang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Copy.Workers}} | {{.Copy.Title}}</title>
  <style>
    :root { color-scheme: light; --bg:#f6f7f4; --panel:#fff; --text:#1a201a; --muted:#5f6b60; --line:#d9dfd6; --accent:#15553e; --shadow:0 12px 30px rgba(20,34,26,.08); }
    body[data-theme="dark"] { color-scheme: dark; --bg:#101411; --panel:#171d18; --text:#edf3ed; --muted:#a9b6aa; --line:#2b362e; --accent:#9df0bd; --shadow:none; }
    * { box-sizing: border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    a { color:var(--accent); text-decoration:none; }
    a:hover { text-decoration:underline; }
    .page { width:min(1180px, calc(100% - 32px)); margin:0 auto; padding:28px 0 44px; }
    header { display:flex; justify-content:space-between; gap:18px; align-items:flex-start; margin-bottom:18px; }
    h1 { margin:0 0 6px; font-size:clamp(28px, 5vw, 42px); line-height:1.08; }
    .subtitle, .empty { color:var(--muted); }
    .panel { background:var(--panel); border:1px solid var(--line); border-radius:8px; box-shadow:var(--shadow); padding:18px; }
    .toolbar { display:flex; gap:8px; flex-wrap:wrap; justify-content:flex-end; }
    .toolbar a, .toolbar button, .toolbar select { min-height:32px; border:1px solid var(--line); border-radius:999px; padding:4px 11px; background:var(--panel); color:var(--text); font:inherit; cursor:pointer; }
    table { width:100%; border-collapse:collapse; }
    th, td { padding:10px 8px; border-bottom:1px solid var(--line); text-align:left; vertical-align:top; }
    th { color:var(--muted); font-size:13px; font-weight:600; }
    .mono { font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; word-break:break-all; }
    @media (max-width:720px) { header { display:grid; } .toolbar { justify-content:flex-start; } th, td { padding:8px 5px; font-size:13px; } }
  </style>
</head>
<body>
  <main class="page">
    <header>
      <div>
        <h1>{{.Copy.Workers}}</h1>
        <div class="subtitle">{{.Copy.Subtitle}}</div>
      </div>
      <nav class="toolbar">
        <a href="/?lang={{.Copy.Lang}}">{{.Copy.HomeNav}}</a>
        <a href="/workers?lang={{.Copy.Lang}}">{{.Copy.ViewWorkers}}</a>
        <a href="/blocks?lang={{.Copy.Lang}}">{{.Copy.BlocksNav}}</a>
        <button class="theme-toggle" type="button" data-dark-label="{{.Copy.ThemeBlack}}" data-light-label="{{.Copy.ThemeWhite}}">{{.Copy.ThemeBlack}}</button>
        <select class="language-select" data-path="/workers">
          <option value="en" {{if eq .Copy.Lang "en"}}selected{{end}}>English</option>
          <option value="zh-CN" {{if eq .Copy.Lang "zh-CN"}}selected{{end}}>简体中文</option>
          <option value="ja" {{if eq .Copy.Lang "ja"}}selected{{end}}>日本語</option>
          <option value="ko" {{if eq .Copy.Lang "ko"}}selected{{end}}>한국어</option>
        </select>
      </nav>
    </header>
    <section class="panel">
      {{if hasOnlineWorkers .Status.Pool.Workers}}
      <table>
        <thead><tr><th>{{.Copy.Worker}}</th><th>{{.Copy.Difficulty}}</th><th>{{.Copy.Accepted}}</th><th>{{.Copy.Rejected}}</th><th>{{.Copy.LastShare}}</th></tr></thead>
        <tbody>
        {{range .Status.Pool.Workers}}
          {{if .Online}}
          <tr><td class="mono">{{.Name}}</td><td>{{printf "%.2f" .Difficulty}}</td><td>{{.Accepted}}</td><td>{{.Rejected}}</td><td>{{timeText .LastShareAt}}</td></tr>
          {{end}}
        {{end}}
        </tbody>
      </table>
      {{else}}<div class="empty">{{.Copy.NoWorkers}}</div>{{end}}
    </section>
  </main>
  <script>
    (function () {
      var key = "pacpool-theme";
      var button = document.querySelector(".theme-toggle");
      var lang = document.querySelector(".language-select");
      function apply(theme) {
        if (theme !== "dark") theme = "light";
        document.body.setAttribute("data-theme", theme);
        try { localStorage.setItem(key, theme); } catch (error) {}
        if (button) button.textContent = theme === "dark" ? button.getAttribute("data-light-label") : button.getAttribute("data-dark-label");
      }
      var saved = "light";
      try { saved = localStorage.getItem(key) || saved; } catch (error) {}
      if (button) button.addEventListener("click", function () {
        apply(document.body.getAttribute("data-theme") === "dark" ? "light" : "dark");
      });
      if (lang) lang.addEventListener("change", function () {
        window.location.href = (lang.getAttribute("data-path") || "/") + "?lang=" + encodeURIComponent(lang.value);
      });
      apply(saved);
    })();
  </script>
</body>
</html>`))

var blocksTemplate = template.Must(template.New("blocks").Funcs(template.FuncMap{"timeText": timeText}).Parse(`<!doctype html>
<html lang="{{.Copy.Lang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Copy.BlocksNav}} | {{.Copy.Title}}</title>
  <style>
    :root { color-scheme: light; --bg:#f6f7f4; --panel:#fff; --text:#1a201a; --muted:#5f6b60; --line:#d9dfd6; --accent:#15553e; --shadow:0 12px 30px rgba(20,34,26,.08); }
    body[data-theme="dark"] { color-scheme: dark; --bg:#101411; --panel:#171d18; --text:#edf3ed; --muted:#a9b6aa; --line:#2b362e; --accent:#9df0bd; --shadow:none; }
    * { box-sizing: border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    a { color:var(--accent); text-decoration:none; }
    a:hover { text-decoration:underline; }
    .page { width:min(1180px, calc(100% - 32px)); margin:0 auto; padding:28px 0 44px; }
    header { display:flex; justify-content:space-between; gap:18px; align-items:flex-start; margin-bottom:18px; }
    h1 { margin:0 0 6px; font-size:clamp(28px, 5vw, 42px); line-height:1.08; }
    .subtitle, .empty { color:var(--muted); }
    .panel { background:var(--panel); border:1px solid var(--line); border-radius:8px; box-shadow:var(--shadow); padding:18px; }
    .toolbar { display:flex; gap:8px; flex-wrap:wrap; justify-content:flex-end; }
    .toolbar a, .toolbar button, .toolbar select { min-height:32px; border:1px solid var(--line); border-radius:999px; padding:4px 11px; background:var(--panel); color:var(--text); font:inherit; cursor:pointer; }
    table { width:100%; border-collapse:collapse; }
    th, td { padding:10px 8px; border-bottom:1px solid var(--line); text-align:left; vertical-align:top; }
    th { color:var(--muted); font-size:13px; font-weight:600; }
    @media (max-width:720px) { header { display:grid; } .toolbar { justify-content:flex-start; } th, td { padding:8px 5px; font-size:13px; } }
  </style>
</head>
<body>
  <main class="page">
    <header>
      <div>
        <h1>{{.Copy.BlocksNav}}</h1>
        <div class="subtitle">{{.Copy.Subtitle}}</div>
      </div>
      <nav class="toolbar">
        <a href="/?lang={{.Copy.Lang}}">{{.Copy.HomeNav}}</a>
        <a href="/workers?lang={{.Copy.Lang}}">{{.Copy.ViewWorkers}}</a>
        <a href="/blocks?lang={{.Copy.Lang}}">{{.Copy.BlocksNav}}</a>
        <button class="theme-toggle" type="button" data-dark-label="{{.Copy.ThemeBlack}}" data-light-label="{{.Copy.ThemeWhite}}">{{.Copy.ThemeBlack}}</button>
        <select class="language-select" data-path="/blocks">
          <option value="en" {{if eq .Copy.Lang "en"}}selected{{end}}>English</option>
          <option value="zh-CN" {{if eq .Copy.Lang "zh-CN"}}selected{{end}}>简体中文</option>
          <option value="ja" {{if eq .Copy.Lang "ja"}}selected{{end}}>日本語</option>
          <option value="ko" {{if eq .Copy.Lang "ko"}}selected{{end}}>한국어</option>
        </select>
      </nav>
    </header>
    <section class="panel">
      {{if .Status.Pool.RecentRounds}}
      <table>
        <thead><tr><th>{{.Copy.Round}}</th><th>{{.Copy.State}}</th><th>{{.Copy.Work}}</th><th>{{.Copy.Block}}</th></tr></thead>
        <tbody>
        {{range .Status.Pool.RecentRounds}}
          <tr><td>#{{.ID}}</td><td>{{if .Solved}}solved{{else}}open{{end}}</td><td>{{printf "%.2f" .AcceptedWork}}</td><td>{{if .BlockHash}}{{.BlockHeight}}{{else}}-{{end}}</td></tr>
        {{end}}
        </tbody>
      </table>
      {{else}}<div class="empty">{{.Copy.NoRounds}}</div>{{end}}
    </section>
  </main>
  <script>
    (function () {
      var key = "pacpool-theme";
      var button = document.querySelector(".theme-toggle");
      var lang = document.querySelector(".language-select");
      function apply(theme) {
        if (theme !== "dark") theme = "light";
        document.body.setAttribute("data-theme", theme);
        try { localStorage.setItem(key, theme); } catch (error) {}
        if (button) button.textContent = theme === "dark" ? button.getAttribute("data-light-label") : button.getAttribute("data-dark-label");
      }
      var saved = "light";
      try { saved = localStorage.getItem(key) || saved; } catch (error) {}
      if (button) button.addEventListener("click", function () {
        apply(document.body.getAttribute("data-theme") === "dark" ? "light" : "dark");
      });
      if (lang) lang.addEventListener("change", function () {
        window.location.href = (lang.getAttribute("data-path") || "/") + "?lang=" + encodeURIComponent(lang.value);
      });
      apply(saved);
    })();
  </script>
</body>
</html>`))

func renderDashboard(w http.ResponseWriter, r *http.Request, svc *service.Service) error {
	lang := dashboardLang(r)
	copy := dashboardCopyFor(lang)
	status := svc.Snapshot()
	minerQuery := strings.TrimSpace(r.URL.Query().Get("miner"))
	minerStats, minerFound := service.MinerStats{}, false
	if minerQuery != "" {
		minerStats, minerFound = svc.MinerStats(minerQuery)
	}
	view := dashboardView{
		Copy:           copy,
		Status:         status,
		Updated:        timeText(status.UpdatedAt),
		HealthLabel:    healthyText(copy, status.Healthy),
		StratumLabel:   readyText(copy, status.Pool.ReadyForStratum),
		StratumNote:    stratumNote(copy, status.Pool),
		TemplateLabel:  templateText(copy, status.Pool.Template.Available),
		Height:         fmt.Sprint(status.Network.BestHeight),
		Peers:          fmt.Sprint(status.Network.PeerCount),
		NetworkDiff:    formatDifficulty(status.PACD.Difficulty, status.Pool.Template.Difficulty),
		NetworkHash:    formatNetworkHashrate(status.PACD.Difficulty, status.Pool.Template.Difficulty, status.Network.TargetSpacingSec, status.PACD.TargetSpacingSec),
		Miners:         fmt.Sprint(status.Pool.ConnectedMiners),
		Accepted:       fmt.Sprint(status.Pool.Shares.Accepted),
		Rejected:       fmt.Sprint(status.Pool.Shares.Rejected),
		Solved:         fmt.Sprint(status.Pool.Shares.SolvedBlocks),
		Fee:            fmt.Sprintf("%.2f%%", status.Pool.FeePercent),
		PendingPayout:  formatPACCompact(totalPendingPayout(status.Pool.PendingPayouts)),
		PayoutMin:      formatPACCompact(status.Pool.AutoPayout.MinAmount),
		CurrentRoundID: fmt.Sprintf("#%d", status.Pool.CurrentRound.ID),
		StratumHost:    "stratum.pingancoin.org:3333",
		ExplorerURL:    "https://explorer.pingancoin.org",
		APIURL:         "https://api.pingancoin.org/status",
		StatusURL:      "/status",
		PayoutsURL:     "/payouts",
		MiningURL:      "stratum+tcp://stratum.pingancoin.org:3333",
		UsernameSample: "PYourWalletAddress.rig01",
		WorkersURL:     "/workers?lang=" + copy.Lang,
		BlocksURL:      "/blocks?lang=" + copy.Lang,
		MinerURL:       "https://github.com/Pingancoin/pacminer",
		WalletURL:      "https://www.pingancoin.org/#wallet",
		MinerQuery:     minerQuery,
		MinerSearched:  minerQuery != "",
		MinerFound:     minerFound,
		MinerStats:     minerStats,
		MinerUnpaid:    formatPAC(minerStats.Unpaid),
		MinerPaid:      formatPAC(minerStats.Paid),
		MinerTotal:     formatPAC(minerStats.Total),
		MinerToday:     formatPAC(minerStats.TodayEarned),
		MinerLastPay:   timeText(minerStats.LastPaymentAt),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return dashboardTemplate.Execute(w, view)
}

func renderWorkers(w http.ResponseWriter, r *http.Request, svc *service.Service) error {
	lang := dashboardLang(r)
	copy := dashboardCopyFor(lang)
	view := dashboardView{
		Copy:   copy,
		Status: svc.Snapshot(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return workersTemplate.Execute(w, view)
}

func renderBlocks(w http.ResponseWriter, r *http.Request, svc *service.Service) error {
	lang := dashboardLang(r)
	copy := dashboardCopyFor(lang)
	view := dashboardView{
		Copy:   copy,
		Status: svc.Snapshot(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return blocksTemplate.Execute(w, view)
}

func dashboardLang(r *http.Request) string {
	lang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if lang == "" {
		lang = r.Header.Get("Accept-Language")
	}
	lang = strings.ToLower(lang)
	switch {
	case strings.HasPrefix(lang, "zh"):
		return "zh-CN"
	case strings.HasPrefix(lang, "ja"):
		return "ja"
	case strings.HasPrefix(lang, "ko"):
		return "ko"
	default:
		return "en"
	}
}

func dashboardCopyFor(lang string) dashboardCopy {
	base := dashboardCopy{
		Lang:              "en",
		Title:             "Pingancoin Pool",
		Subtitle:          "Live pool operations for the Pingancoin pure PoW network.",
		StatusHealthy:     "Health",
		StatusDegraded:    "Degraded",
		Ready:             "Ready",
		Waiting:           "Waiting",
		Height:            "Height",
		Peers:             "Peers",
		NetworkDifficulty: "Network difficulty",
		NetworkHashrate:   "Network hashrate",
		Miners:            "Miners",
		Accepted:          "Accepted",
		Solved:            "Solved",
		Fee:               "Pool fee",
		PendingPayout:     "Pending payout",
		PayoutThreshold:   "Payout threshold",
		CurrentRound:      "Current round",
		Stratum:           "Stratum",
		Template:          "Block template",
		Workers:           "Workers",
		NoWorkers:         "No connected workers yet.",
		Worker:            "Worker",
		Difficulty:        "Difficulty",
		Rejected:          "Rejected",
		LastShare:         "Last share",
		RecentRounds:      "Block records",
		NoRounds:          "No block records yet.",
		Round:             "Round",
		State:             "State",
		Work:              "Work",
		Block:             "Block",
		Updated:           "Updated",
		API:               "Public API",
		Explorer:          "Explorer",
		PoolStatus:        "Pool status",
		Payouts:           "Payouts",
		PaymentRecords:    "Payment records",
		NoPayments:        "No payments have been executed yet.",
		PaymentTime:       "Time",
		PaymentAmount:     "Amount",
		PaymentTxID:       "TxID",
		PaymentRecipients: "Recipients",
		MiningGuide:       "How to connect miners",
		MiningURL:         "Stratum URL",
		MiningUsername:    "Username",
		MiningPassword:    "Password",
		MiningExample:     "Use your own PAC wallet address as the username. Add a dot and rig name to distinguish machines.",
		MiningPasswordAny: "Any value is accepted.",
		MinerDownload:     "Miner download",
		WalletDownload:    "Wallet download",
		ViewWorkers:       "Miner ranking",
		HomeNav:           "Home",
		BlocksNav:         "Block records",
		MinerLookup:       "Miner lookup",
		MinerAddress:      "Enter payout address",
		Lookup:            "Lookup",
		OnlineMachines:    "Online machines",
		TotalEarned:       "Total earned",
		TotalPaid:         "Total paid",
		TodayEarned:       "Today earned",
		Unpaid:            "Unpaid",
		NoMinerData:       "No miner records found for this address.",
		LastPayment:       "Last payment:",
		LangEnglish:       "English",
		LangChinese:       "简体中文",
		LangJapanese:      "日本語",
		LangKorean:        "한국어",
		ThemeBlack:        "Black",
		ThemeWhite:        "White",
		StratumClosedNote: "Miner connections stay closed until the official pool mining address is configured.",
		TemplateReady:     "Available",
		TemplateWaiting:   "Not available",
		DayMode:           "Day",
		NightMode:         "Night",
	}
	switch lang {
	case "zh-CN":
		base.Lang = "zh-CN"
		base.Title = "Pingancoin 矿池"
		base.Subtitle = "Pingancoin 纯 PoW 网络的官方矿池运行状态。"
		base.StatusHealthy = "健康状态"
		base.StatusDegraded = "异常"
		base.Ready = "已就绪"
		base.Waiting = "等待中"
		base.Height = "区块高度"
		base.Peers = "节点连接"
		base.NetworkDifficulty = "全网难度"
		base.NetworkHashrate = "全网算力"
		base.Miners = "矿工数"
		base.Accepted = "有效份额"
		base.Solved = "已出块"
		base.Fee = "矿池费率"
		base.PendingPayout = "待付款"
		base.PayoutThreshold = "起付额度"
		base.CurrentRound = "当前轮次"
		base.Stratum = "Stratum"
		base.Template = "区块模板"
		base.Workers = "矿工"
		base.NoWorkers = "暂无矿工连接。"
		base.Worker = "矿工"
		base.Difficulty = "难度"
		base.Rejected = "拒绝"
		base.LastShare = "最近份额"
		base.RecentRounds = "出块记录"
		base.NoRounds = "暂无出块记录。"
		base.Round = "轮次"
		base.State = "状态"
		base.Work = "工作量"
		base.Block = "区块"
		base.Updated = "更新时间"
		base.API = "公开 API"
		base.Explorer = "区块浏览器"
		base.PoolStatus = "矿池状态"
		base.Payouts = "结算信息"
		base.PaymentRecords = "付款记录"
		base.NoPayments = "暂无付款记录。"
		base.PaymentTime = "时间"
		base.PaymentAmount = "金额"
		base.PaymentTxID = "交易 ID"
		base.PaymentRecipients = "收款地址数"
		base.MiningGuide = "矿工接入方式"
		base.MiningURL = "接入地址"
		base.MiningUsername = "用户名"
		base.MiningPassword = "密码"
		base.MiningExample = "用户名填写自己的 PAC 钱包地址；多台矿机可在地址后加点号和矿工名区分。"
		base.MiningPasswordAny = "任意填写即可。"
		base.MinerDownload = "矿工下载"
		base.WalletDownload = "钱包下载"
		base.ViewWorkers = "矿工排行"
		base.HomeNav = "首页"
		base.BlocksNav = "出块记录"
		base.MinerLookup = "矿工查询"
		base.MinerAddress = "输入收款钱包地址"
		base.Lookup = "查询"
		base.OnlineMachines = "在线机器"
		base.TotalEarned = "累计收益"
		base.TotalPaid = "已付款"
		base.TodayEarned = "今日收益"
		base.Unpaid = "待结算"
		base.NoMinerData = "没有找到这个地址的矿工记录。"
		base.LastPayment = "最近付款："
		base.ThemeBlack = "黑色"
		base.ThemeWhite = "白色"
		base.StratumClosedNote = "矿池收币地址配置完成前，矿工连接入口保持关闭。"
		base.TemplateReady = "可用"
		base.TemplateWaiting = "不可用"
		base.DayMode = "白天"
		base.NightMode = "夜晚"
	case "ja":
		base.Lang = "ja"
		base.Title = "Pingancoin プール"
		base.Subtitle = "Pingancoin pure PoW ネットワークの公式プール稼働状況。"
		base.StatusHealthy = "状態"
		base.StatusDegraded = "低下"
		base.Ready = "準備完了"
		base.Waiting = "待機中"
		base.Height = "ブロック高"
		base.Peers = "ピア"
		base.NetworkDifficulty = "ネットワーク難易度"
		base.NetworkHashrate = "ネットワークハッシュレート"
		base.Miners = "マイナー"
		base.Accepted = "承認シェア"
		base.Solved = "発見ブロック"
		base.Fee = "プール手数料"
		base.PendingPayout = "未払い"
		base.PayoutThreshold = "支払い基準"
		base.CurrentRound = "現在のラウンド"
		base.Stratum = "Stratum"
		base.Template = "ブロックテンプレート"
		base.Workers = "ワーカー"
		base.NoWorkers = "接続中のワーカーはありません。"
		base.Worker = "ワーカー"
		base.Difficulty = "難易度"
		base.Rejected = "拒否"
		base.LastShare = "最終シェア"
		base.RecentRounds = "ブロック記録"
		base.NoRounds = "ブロック記録はまだありません。"
		base.Round = "ラウンド"
		base.State = "状態"
		base.Work = "作業量"
		base.Block = "ブロック"
		base.Updated = "更新"
		base.API = "公開 API"
		base.Explorer = "エクスプローラー"
		base.PoolStatus = "プール状態"
		base.Payouts = "支払い"
		base.PaymentRecords = "支払い記録"
		base.NoPayments = "支払い記録はまだありません。"
		base.PaymentTime = "時刻"
		base.PaymentAmount = "金額"
		base.PaymentTxID = "TxID"
		base.PaymentRecipients = "受取数"
		base.MiningGuide = "マイナー接続方法"
		base.MiningURL = "Stratum URL"
		base.MiningUsername = "ユーザー名"
		base.MiningPassword = "パスワード"
		base.MiningExample = "ユーザー名には自分の PAC ウォレットアドレスを使い、ドットとリグ名で機器を区別できます。"
		base.MiningPasswordAny = "任意の値で構いません。"
		base.MinerDownload = "マイナーDL"
		base.WalletDownload = "ウォレットDL"
		base.ViewWorkers = "マイナーランキング"
		base.HomeNav = "ホーム"
		base.BlocksNav = "ブロック記録"
		base.MinerLookup = "マイナー検索"
		base.MinerAddress = "支払い先アドレスを入力"
		base.Lookup = "検索"
		base.OnlineMachines = "オンライン台数"
		base.TotalEarned = "総報酬"
		base.TotalPaid = "支払済み"
		base.TodayEarned = "本日の報酬"
		base.Unpaid = "未払い"
		base.NoMinerData = "このアドレスのマイナー記録はありません。"
		base.LastPayment = "最終支払い:"
		base.ThemeBlack = "黒"
		base.ThemeWhite = "白"
		base.StratumClosedNote = "公式プール採掘アドレスが設定されるまで、マイナー接続は閉じたままです。"
		base.TemplateReady = "利用可能"
		base.TemplateWaiting = "利用不可"
		base.DayMode = "昼"
		base.NightMode = "夜"
	case "ko":
		base.Lang = "ko"
		base.Title = "Pingancoin 풀"
		base.Subtitle = "Pingancoin 순수 PoW 네트워크의 공식 풀 운영 상태입니다."
		base.StatusHealthy = "상태"
		base.StatusDegraded = "저하"
		base.Ready = "준비됨"
		base.Waiting = "대기 중"
		base.Height = "블록 높이"
		base.Peers = "피어"
		base.NetworkDifficulty = "네트워크 난이도"
		base.NetworkHashrate = "네트워크 해시레이트"
		base.Miners = "채굴자"
		base.Accepted = "승인 공유"
		base.Solved = "발견 블록"
		base.Fee = "풀 수수료"
		base.PendingPayout = "미지급"
		base.PayoutThreshold = "지급 기준"
		base.CurrentRound = "현재 라운드"
		base.Stratum = "Stratum"
		base.Template = "블록 템플릿"
		base.Workers = "워커"
		base.NoWorkers = "연결된 워커가 없습니다."
		base.Worker = "워커"
		base.Difficulty = "난이도"
		base.Rejected = "거부"
		base.LastShare = "마지막 공유"
		base.RecentRounds = "블록 기록"
		base.NoRounds = "아직 블록 기록이 없습니다."
		base.Round = "라운드"
		base.State = "상태"
		base.Work = "작업량"
		base.Block = "블록"
		base.Updated = "업데이트"
		base.API = "공개 API"
		base.Explorer = "탐색기"
		base.PoolStatus = "풀 상태"
		base.Payouts = "지급"
		base.PaymentRecords = "지급 기록"
		base.NoPayments = "아직 지급 기록이 없습니다."
		base.PaymentTime = "시간"
		base.PaymentAmount = "금액"
		base.PaymentTxID = "TxID"
		base.PaymentRecipients = "수신자 수"
		base.MiningGuide = "채굴기 접속 방법"
		base.MiningURL = "Stratum URL"
		base.MiningUsername = "사용자 이름"
		base.MiningPassword = "비밀번호"
		base.MiningExample = "사용자 이름은 본인의 PAC 지갑 주소를 사용하고, 점과 장비 이름을 붙여 구분할 수 있습니다."
		base.MiningPasswordAny = "아무 값이나 사용할 수 있습니다."
		base.MinerDownload = "채굴기 다운로드"
		base.WalletDownload = "지갑 다운로드"
		base.ViewWorkers = "채굴자 순위"
		base.HomeNav = "홈"
		base.BlocksNav = "블록 기록"
		base.MinerLookup = "채굴자 조회"
		base.MinerAddress = "지급 주소 입력"
		base.Lookup = "조회"
		base.OnlineMachines = "온라인 장비"
		base.TotalEarned = "총 수익"
		base.TotalPaid = "총 지급"
		base.TodayEarned = "오늘 수익"
		base.Unpaid = "미지급"
		base.NoMinerData = "이 주소의 채굴자 기록이 없습니다."
		base.LastPayment = "마지막 지급:"
		base.ThemeBlack = "검정"
		base.ThemeWhite = "흰색"
		base.StratumClosedNote = "공식 풀 채굴 주소가 설정될 때까지 채굴자 연결은 닫혀 있습니다."
		base.TemplateReady = "사용 가능"
		base.TemplateWaiting = "사용 불가"
		base.DayMode = "낮"
		base.NightMode = "밤"
	}
	return base
}

func healthyText(copy dashboardCopy, ok bool) string {
	if ok {
		return copy.Ready
	}
	return copy.StatusDegraded
}

func readyText(copy dashboardCopy, ok bool) string {
	if ok {
		return copy.Ready
	}
	return copy.Waiting
}

func templateText(copy dashboardCopy, ok bool) string {
	if ok {
		return copy.TemplateReady
	}
	return copy.TemplateWaiting
}

func stratumNote(copy dashboardCopy, pool service.PoolState) string {
	if !pool.MiningOpen && pool.MiningStartTime != "" {
		return fmt.Sprintf("%s Mining opens at %s UTC.", copy.StratumClosedNote, pool.MiningStartTime)
	}
	if strings.TrimSpace(pool.NotReadyReason) != "" {
		return fmt.Sprintf("%s %s.", copy.StratumClosedNote, pool.NotReadyReason)
	}
	return copy.StratumClosedNote
}

func timeText(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func formatPAC(atoms int64) string {
	sign := ""
	if atoms < 0 {
		sign = "-"
		atoms = -atoms
	}
	whole := atoms / 100_000_000
	frac := atoms % 100_000_000
	return fmt.Sprintf("%s%d.%08d PAC", sign, whole, frac)
}

func formatPACCompact(atoms int64) string {
	sign := ""
	if atoms < 0 {
		sign = "-"
		atoms = -atoms
	}
	whole := atoms / 100_000_000
	frac := atoms % 100_000_000
	if frac == 0 {
		return fmt.Sprintf("%s%d PAC", sign, whole)
	}
	value := fmt.Sprintf("%s%d.%08d", sign, whole, frac)
	value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	return value + " PAC"
}

func totalPendingPayout(payouts []service.PayoutEntry) int64 {
	var total int64
	for _, payout := range payouts {
		if payout.Amount > 0 {
			total += payout.Amount
		}
	}
	return total
}

func hasOnlineWorkers(workers []service.WorkerState) bool {
	for _, worker := range workers {
		if worker.Online {
			return true
		}
	}
	return false
}

func formatDifficulty(values ...string) string {
	difficulty := firstFloat(values...)
	if difficulty <= 0 {
		return "-"
	}
	if difficulty >= 1_000_000 {
		return fmt.Sprintf("%.2fM", difficulty/1_000_000)
	}
	if difficulty >= 1_000 {
		return fmt.Sprintf("%.2fK", difficulty/1_000)
	}
	if difficulty >= 10 {
		return fmt.Sprintf("%.2f", difficulty)
	}
	return fmt.Sprintf("%.4f", difficulty)
}

func formatNetworkHashrate(difficulty string, fallbackDifficulty string, spacingCandidates ...int64) string {
	diff := firstFloat(difficulty, fallbackDifficulty)
	if diff <= 0 {
		return "-"
	}
	spacing := int64(150)
	for _, candidate := range spacingCandidates {
		if candidate > 0 {
			spacing = candidate
			break
		}
	}
	hashrate := diff * 4_294_967_296 / float64(spacing)
	switch {
	case hashrate >= 1e18:
		return fmt.Sprintf("%.2f EH/s", hashrate/1e18)
	case hashrate >= 1e15:
		return fmt.Sprintf("%.2f PH/s", hashrate/1e15)
	case hashrate >= 1e12:
		return fmt.Sprintf("%.2f TH/s", hashrate/1e12)
	case hashrate >= 1e9:
		return fmt.Sprintf("%.2f GH/s", hashrate/1e9)
	case hashrate >= 1e6:
		return fmt.Sprintf("%.2f MH/s", hashrate/1e6)
	case hashrate >= 1e3:
		return fmt.Sprintf("%.2f KH/s", hashrate/1e3)
	default:
		return fmt.Sprintf("%.2f H/s", hashrate)
	}
}

func firstFloat(values ...string) float64 {
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func shortText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	head := (limit - 3) / 2
	tail := limit - 3 - head
	return value[:head] + "..." + value[len(value)-tail:]
}
