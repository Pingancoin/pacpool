package api

import (
	"fmt"
	"html/template"
	"net/http"
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
	Miners            string
	Accepted          string
	Solved            string
	Fee               string
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
	LangEnglish       string
	LangChinese       string
	LangJapanese      string
	LangKorean        string
	StratumClosedNote string
	TemplateReady     string
	TemplateWaiting   string
}

type dashboardView struct {
	Copy           dashboardCopy
	Status         service.State
	Updated        string
	HealthLabel    string
	StratumLabel   string
	TemplateLabel  string
	Height         string
	Peers          string
	Miners         string
	Accepted       string
	Rejected       string
	Solved         string
	Fee            string
	CurrentRoundID string
	StratumHost    string
	ExplorerURL    string
	APIURL         string
	StatusURL      string
	PayoutsURL     string
}

var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{"timeText": timeText}).Parse(`<!doctype html>
<html lang="{{.Copy.Lang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Copy.Title}}</title>
  <style>
    :root {
      color-scheme: light dark;
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
    @media (prefers-color-scheme: dark) {
      :root {
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
    header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 22px; }
    h1 { margin: 0 0 6px; font-size: clamp(28px, 5vw, 46px); line-height: 1.05; letter-spacing: 0; }
    .subtitle { margin: 0; color: var(--muted); max-width: 720px; font-size: 16px; }
    .langs { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
    .langs a, .pill {
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
    .pill.ok { border-color: rgba(46, 125, 91, .45); color: var(--accent-strong); }
    .pill.warn { border-color: rgba(160, 90, 19, .45); color: var(--warn); }
    .hero {
      display: grid;
      grid-template-columns: minmax(0, 1.4fr) minmax(280px, .6fr);
      gap: 16px;
      margin-bottom: 16px;
    }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
      padding: 18px;
    }
    .metrics {
      display: grid;
      grid-template-columns: repeat(4, minmax(130px, 1fr));
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
    .side { display: grid; gap: 12px; align-content: start; }
    h2 { margin: 0 0 12px; font-size: 19px; line-height: 1.2; }
    .note { margin: 10px 0 0; color: var(--muted); }
    .links { display: grid; gap: 9px; }
    .linkrow { display: flex; justify-content: space-between; gap: 12px; border-bottom: 1px solid var(--line); padding-bottom: 8px; }
    .linkrow:last-child { border-bottom: 0; padding-bottom: 0; }
    .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
    table { width: 100%; border-collapse: collapse; }
    th, td { padding: 10px 8px; border-bottom: 1px solid var(--line); text-align: left; vertical-align: top; }
    th { color: var(--muted); font-size: 13px; font-weight: 600; }
    .empty { color: var(--muted); padding: 18px 0 4px; }
    footer { margin-top: 18px; color: var(--muted); font-size: 13px; }
    @media (max-width: 860px) {
      header, .hero, .grid { grid-template-columns: 1fr; display: grid; }
      header { gap: 12px; }
      .langs { justify-content: flex-start; }
      .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    }
    @media (max-width: 520px) {
      .page { width: min(100% - 22px, 1180px); padding-top: 18px; }
      .metrics { grid-template-columns: 1fr; }
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
      <nav class="langs" aria-label="Language">
        <a href="/?lang=en">{{.Copy.LangEnglish}}</a>
        <a href="/?lang=zh-CN">{{.Copy.LangChinese}}</a>
        <a href="/?lang=ja">{{.Copy.LangJapanese}}</a>
        <a href="/?lang=ko">{{.Copy.LangKorean}}</a>
      </nav>
    </header>

    <section class="hero">
      <div class="panel">
        <div class="metrics">
          <div class="metric"><div class="label">{{.Copy.StatusHealthy}}</div><div class="value">{{.HealthLabel}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Height}}</div><div class="value">{{.Height}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Peers}}</div><div class="value">{{.Peers}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Miners}}</div><div class="value">{{.Miners}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Accepted}}</div><div class="value">{{.Accepted}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Solved}}</div><div class="value">{{.Solved}}</div></div>
          <div class="metric"><div class="label">{{.Copy.Fee}}</div><div class="value">{{.Fee}}</div></div>
          <div class="metric"><div class="label">{{.Copy.CurrentRound}}</div><div class="value">{{.CurrentRoundID}}</div></div>
        </div>
      </div>
      <aside class="side">
        <div class="panel">
          <h2>{{.Copy.Stratum}}</h2>
          <span class="pill {{if .Status.Pool.ReadyForStratum}}ok{{else}}warn{{end}}">{{.StratumLabel}}</span>
          <p class="note">{{.StratumHost}}</p>
          {{if not .Status.Pool.ReadyForStratum}}<p class="note">{{.Copy.StratumClosedNote}}</p>{{end}}
        </div>
        <div class="panel">
          <h2>{{.Copy.Template}}</h2>
          <span class="pill {{if .Status.Pool.Template.Available}}ok{{else}}warn{{end}}">{{.TemplateLabel}}</span>
          <p class="note">{{.Copy.Updated}} {{.Updated}}</p>
        </div>
      </aside>
    </section>

    <section class="grid">
      <div class="panel">
        <h2>{{.Copy.Workers}}</h2>
        {{if .Status.Pool.Workers}}
        <table>
          <thead><tr><th>{{.Copy.Worker}}</th><th>{{.Copy.Difficulty}}</th><th>{{.Copy.Accepted}}</th><th>{{.Copy.Rejected}}</th><th>{{.Copy.LastShare}}</th></tr></thead>
          <tbody>
          {{range .Status.Pool.Workers}}
            <tr><td>{{.Name}}</td><td>{{printf "%.2f" .Difficulty}}</td><td>{{.Accepted}}</td><td>{{.Rejected}}</td><td>{{timeText .LastShareAt}}</td></tr>
          {{end}}
          </tbody>
        </table>
        {{else}}<div class="empty">{{.Copy.NoWorkers}}</div>{{end}}
      </div>

      <div class="panel">
        <h2>{{.Copy.RecentRounds}}</h2>
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
</body>
</html>`))

func renderDashboard(w http.ResponseWriter, r *http.Request, status service.State) error {
	lang := dashboardLang(r)
	copy := dashboardCopyFor(lang)
	view := dashboardView{
		Copy:           copy,
		Status:         status,
		Updated:        timeText(status.UpdatedAt),
		HealthLabel:    healthyText(copy, status.Healthy),
		StratumLabel:   readyText(copy, status.Pool.ReadyForStratum),
		TemplateLabel:  templateText(copy, status.Pool.Template.Available),
		Height:         fmt.Sprint(status.Network.BestHeight),
		Peers:          fmt.Sprint(status.Network.PeerCount),
		Miners:         fmt.Sprint(status.Pool.ConnectedMiners),
		Accepted:       fmt.Sprint(status.Pool.Shares.Accepted),
		Rejected:       fmt.Sprint(status.Pool.Shares.Rejected),
		Solved:         fmt.Sprint(status.Pool.Shares.SolvedBlocks),
		Fee:            fmt.Sprintf("%.2f%%", status.Pool.FeePercent),
		CurrentRoundID: fmt.Sprintf("#%d", status.Pool.CurrentRound.ID),
		StratumHost:    "stratum.pingancoin.org:3333",
		ExplorerURL:    "https://explorer.pingancoin.org",
		APIURL:         "https://api.pingancoin.org/status",
		StatusURL:      "/status",
		PayoutsURL:     "/payouts",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return dashboardTemplate.Execute(w, view)
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
		Miners:            "Miners",
		Accepted:          "Accepted",
		Solved:            "Solved",
		Fee:               "Pool fee",
		CurrentRound:      "Current round",
		Stratum:           "Stratum",
		Template:          "Block template",
		Workers:           "Workers",
		NoWorkers:         "No connected workers yet.",
		Worker:            "Worker",
		Difficulty:        "Difficulty",
		Rejected:          "Rejected",
		LastShare:         "Last share",
		RecentRounds:      "Recent rounds",
		NoRounds:          "No completed rounds yet.",
		Round:             "Round",
		State:             "State",
		Work:              "Work",
		Block:             "Block",
		Updated:           "Updated",
		API:               "Public API",
		Explorer:          "Explorer",
		PoolStatus:        "Pool status",
		Payouts:           "Payouts",
		LangEnglish:       "English",
		LangChinese:       "简体中文",
		LangJapanese:      "日本語",
		LangKorean:        "한국어",
		StratumClosedNote: "Miner connections stay closed until the official pool mining address is configured.",
		TemplateReady:     "Available",
		TemplateWaiting:   "Not available",
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
		base.Miners = "矿工数"
		base.Accepted = "有效份额"
		base.Solved = "已出块"
		base.Fee = "矿池费率"
		base.CurrentRound = "当前轮次"
		base.Stratum = "Stratum"
		base.Template = "区块模板"
		base.Workers = "矿工"
		base.NoWorkers = "暂无矿工连接。"
		base.Worker = "矿工"
		base.Difficulty = "难度"
		base.Rejected = "拒绝"
		base.LastShare = "最近份额"
		base.RecentRounds = "最近轮次"
		base.NoRounds = "暂无完成轮次。"
		base.Round = "轮次"
		base.State = "状态"
		base.Work = "工作量"
		base.Block = "区块"
		base.Updated = "更新时间"
		base.API = "公开 API"
		base.Explorer = "区块浏览器"
		base.PoolStatus = "矿池状态"
		base.Payouts = "结算信息"
		base.StratumClosedNote = "矿池收币地址配置完成前，矿工连接入口保持关闭。"
		base.TemplateReady = "可用"
		base.TemplateWaiting = "不可用"
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
		base.Miners = "マイナー"
		base.Accepted = "承認シェア"
		base.Solved = "発見ブロック"
		base.Fee = "プール手数料"
		base.CurrentRound = "現在のラウンド"
		base.Stratum = "Stratum"
		base.Template = "ブロックテンプレート"
		base.Workers = "ワーカー"
		base.NoWorkers = "接続中のワーカーはありません。"
		base.Worker = "ワーカー"
		base.Difficulty = "難易度"
		base.Rejected = "拒否"
		base.LastShare = "最終シェア"
		base.RecentRounds = "最近のラウンド"
		base.NoRounds = "完了したラウンドはまだありません。"
		base.Round = "ラウンド"
		base.State = "状態"
		base.Work = "作業量"
		base.Block = "ブロック"
		base.Updated = "更新"
		base.API = "公開 API"
		base.Explorer = "エクスプローラー"
		base.PoolStatus = "プール状態"
		base.Payouts = "支払い"
		base.StratumClosedNote = "公式プール採掘アドレスが設定されるまで、マイナー接続は閉じたままです。"
		base.TemplateReady = "利用可能"
		base.TemplateWaiting = "利用不可"
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
		base.Miners = "채굴자"
		base.Accepted = "승인 공유"
		base.Solved = "발견 블록"
		base.Fee = "풀 수수료"
		base.CurrentRound = "현재 라운드"
		base.Stratum = "Stratum"
		base.Template = "블록 템플릿"
		base.Workers = "워커"
		base.NoWorkers = "연결된 워커가 없습니다."
		base.Worker = "워커"
		base.Difficulty = "난이도"
		base.Rejected = "거부"
		base.LastShare = "마지막 공유"
		base.RecentRounds = "최근 라운드"
		base.NoRounds = "완료된 라운드가 아직 없습니다."
		base.Round = "라운드"
		base.State = "상태"
		base.Work = "작업량"
		base.Block = "블록"
		base.Updated = "업데이트"
		base.API = "공개 API"
		base.Explorer = "탐색기"
		base.PoolStatus = "풀 상태"
		base.Payouts = "지급"
		base.StratumClosedNote = "공식 풀 채굴 주소가 설정될 때까지 채굴자 연결은 닫혀 있습니다."
		base.TemplateReady = "사용 가능"
		base.TemplateWaiting = "사용 불가"
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

func timeText(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
