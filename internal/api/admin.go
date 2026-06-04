package api

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Pingancoin/pacpool/internal/service"
)

type adminView struct {
	Settings      service.AdminSettings
	PayoutMinPAC  string
	Saved         bool
	Error         string
	Authenticated bool
}

var adminTemplate = template.Must(template.New("admin").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Pingancoin Pool Admin</title>
  <style>
    :root { color-scheme: light dark; --bg:#f6f7f4; --panel:#fff; --text:#1a201a; --muted:#5f6b60; --line:#d9dfd6; --accent:#2e7d5b; --warn:#a05a13; }
    @media (prefers-color-scheme: dark) { :root { --bg:#101411; --panel:#171d18; --text:#edf3ed; --muted:#a9b6aa; --line:#2b362e; --accent:#6bd49e; --warn:#f0b35a; } }
    * { box-sizing: border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    .page { width:min(760px, calc(100% - 32px)); margin:0 auto; padding:32px 0 46px; }
    h1 { margin:0 0 6px; font-size:32px; line-height:1.1; }
    p { color:var(--muted); margin:0 0 18px; }
    .panel { background:var(--panel); border:1px solid var(--line); border-radius:8px; padding:18px; }
    .grid { display:grid; gap:14px; }
    label { display:grid; gap:6px; color:var(--muted); font-size:13px; }
    input, textarea { width:100%; min-height:38px; border:1px solid var(--line); border-radius:6px; padding:7px 10px; background:var(--panel); color:var(--text); font:inherit; }
    textarea { min-height:120px; resize:vertical; }
    .check { display:flex; align-items:center; gap:10px; color:var(--text); font-size:15px; }
    .check input { width:auto; min-height:auto; }
    button { min-height:38px; border:1px solid var(--accent); border-radius:6px; padding:7px 15px; background:var(--accent); color:#fff; font-weight:650; cursor:pointer; }
    .notice { border:1px solid rgba(46,125,91,.45); color:var(--accent); border-radius:6px; padding:10px 12px; margin-bottom:14px; }
    .error { border:1px solid rgba(160,90,19,.45); color:var(--warn); border-radius:6px; padding:10px 12px; margin-bottom:14px; }
    .actions { display:flex; justify-content:flex-end; margin-top:4px; }
    .mono { font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }
    a { color:var(--accent); }
  </style>
</head>
<body>
  <main class="page">
    <h1>矿池后台</h1>
    <p>管理自动付款、矿池手续费和起付额度。保存后立即生效，并写入运行时配置。</p>
    {{if .Saved}}<div class="notice">设置已保存。</div>{{end}}
    {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    {{if .Authenticated}}
    <form class="panel grid" method="post" action="/admin/settings">
      <label class="check"><input type="checkbox" name="auto_payout" value="1" {{if .Settings.AutoPayoutEnabled}}checked{{end}}> 打开自动付款</label>
      <label>矿池手续费百分比
        <input name="fee_percent" inputmode="decimal" value="{{printf "%.2f" .Settings.FeePercent}}">
      </label>
      <label>起付额度 PAC
        <input name="payout_min_pac" inputmode="decimal" value="{{.PayoutMinPAC}}">
      </label>
      <label>中文公告
        <textarea name="announcement_zh_cn" maxlength="2000" placeholder="中文页面显示，留空则不显示公告">{{.Settings.Announcements.ZhCN}}</textarea>
      </label>
      <label>English notice
        <textarea name="announcement_en" maxlength="2000" placeholder="Shown on English pages; falls back to Chinese when empty">{{.Settings.Announcements.En}}</textarea>
      </label>
      <label>日本語公告
        <textarea name="announcement_ja" maxlength="2000" placeholder="日本語ページに表示。空欄の場合は中文公告に戻ります">{{.Settings.Announcements.Ja}}</textarea>
      </label>
      <label>한국어 공지
        <textarea name="announcement_ko" maxlength="2000" placeholder="한국어 페이지에 표시됩니다. 비워두면 중국어 공지로 대체됩니다">{{.Settings.Announcements.Ko}}</textarea>
      </label>
      <div class="actions"><button type="submit">保存设置</button></div>
    </form>
    {{else}}
    <form class="panel grid" method="post" action="/admin/login">
      <label>管理员 Token
        <input class="mono" name="token" type="password" autocomplete="current-password">
      </label>
      <div class="actions"><button type="submit">进入后台</button></div>
    </form>
    {{end}}
  </main>
</body>
</html>`))

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	authenticated := s.authorizedAdmin(r)
	if authenticated {
		s.setAdminCookie(w, r)
	}
	settings := s.service.AdminSettings()
	status := http.StatusOK
	if !authenticated {
		status = http.StatusUnauthorized
	}
	renderAdmin(w, status, adminView{
		Settings:      settings,
		PayoutMinPAC:  formatAdminPAC(settings.PayoutMin),
		Saved:         r.URL.Query().Get("saved") == "1",
		Error:         r.URL.Query().Get("error"),
		Authenticated: authenticated,
	})
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		renderAdmin(w, http.StatusBadRequest, adminView{Error: "invalid form"})
		return
	}
	if !s.authorizedAdmin(r) {
		renderAdmin(w, http.StatusUnauthorized, adminView{Error: "管理员 Token 不正确"})
		return
	}
	s.setAdminCookie(w, r)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.authorizedAdmin(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "admin token required"})
		return
	}
	s.setAdminCookie(w, r)
	if err := r.ParseForm(); err != nil {
		redirectAdminError(w, r, "invalid form")
		return
	}
	autoPayout := r.FormValue("auto_payout") == "1"
	feeBPS, err := parseFeePercent(r.FormValue("fee_percent"))
	if err != nil {
		redirectAdminError(w, r, err.Error())
		return
	}
	payoutMin, err := parseAdminPAC(r.FormValue("payout_min_pac"))
	if err != nil {
		redirectAdminError(w, r, err.Error())
		return
	}
	announcements, err := parseAnnouncementSet(service.AnnouncementSet{
		ZhCN: r.FormValue("announcement_zh_cn"),
		En:   r.FormValue("announcement_en"),
		Ja:   r.FormValue("announcement_ja"),
		Ko:   r.FormValue("announcement_ko"),
	})
	if err != nil {
		redirectAdminError(w, r, err.Error())
		return
	}
	if _, err := s.service.UpdateAdminSettings(service.AdminSettingsUpdate{
		AutoPayoutEnabled: &autoPayout,
		FeeBPS:            &feeBPS,
		PayoutMin:         &payoutMin,
		Announcements:     &announcements,
	}); err != nil {
		redirectAdminError(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/admin?saved=1", http.StatusSeeOther)
}

func renderAdmin(w http.ResponseWriter, status int, view adminView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = adminTemplate.Execute(w, view)
}

func redirectAdminError(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/admin?error="+urlQueryEscape(message), http.StatusSeeOther)
}

func parseFeePercent(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("矿池手续费不能为空")
	}
	fee, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("矿池手续费格式不正确")
	}
	bps := int(fee*100 + 0.5)
	if bps < 0 || bps > 5000 {
		return 0, fmt.Errorf("矿池手续费必须在 0 到 50%% 之间")
	}
	return bps, nil
}

func parseAdminPAC(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("起付额度不能为空")
	}
	wholePart, fracPart, ok := strings.Cut(value, ".")
	if !ok {
		fracPart = ""
	}
	whole, err := strconv.ParseInt(strings.TrimSpace(wholePart), 10, 64)
	if err != nil || whole < 0 {
		return 0, fmt.Errorf("起付额度格式不正确")
	}
	if len(fracPart) > 8 {
		return 0, fmt.Errorf("起付额度最多支持 8 位小数")
	}
	for len(fracPart) < 8 {
		fracPart += "0"
	}
	frac := int64(0)
	if fracPart != "" {
		parsed, err := strconv.ParseInt(fracPart, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("起付额度格式不正确")
		}
		frac = parsed
	}
	return whole*100_000_000 + frac, nil
}

func formatAdminPAC(atoms int64) string {
	formatted := strings.TrimSuffix(strings.TrimSuffix(formatPAC(atoms), " PAC"), "0")
	return strings.TrimSuffix(formatted, ".")
}

func parseAnnouncementSet(values service.AnnouncementSet) (service.AnnouncementSet, error) {
	parsed := service.AnnouncementSet{
		ZhCN: normalizeAnnouncement(values.ZhCN),
		En:   normalizeAnnouncement(values.En),
		Ja:   normalizeAnnouncement(values.Ja),
		Ko:   normalizeAnnouncement(values.Ko),
	}
	if len([]rune(parsed.ZhCN)) > 2000 || len([]rune(parsed.En)) > 2000 ||
		len([]rune(parsed.Ja)) > 2000 || len([]rune(parsed.Ko)) > 2000 {
		return service.AnnouncementSet{}, fmt.Errorf("每条首页公告最多支持 2000 个字符")
	}
	return parsed, nil
}

func normalizeAnnouncement(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}
