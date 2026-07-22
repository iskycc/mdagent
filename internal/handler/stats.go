package handler

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"

	"miaodi-agent/internal/service"
)

// StatsProvider 统计服务接口
type StatsProvider interface {
	GetStats() (*service.StatsData, error)
	ToJSON(*service.StatsData) (string, error)
}

// StatsHandler 统计页面处理器
type StatsHandler struct {
	statsSvc StatsProvider
	token    string
}

// NewStatsHandler 创建统计处理器
func NewStatsHandler(statsSvc StatsProvider, token string) *StatsHandler {
	return &StatsHandler{statsSvc: statsSvc, token: token}
}

// RegisterRoutes 注册路由
func (h *StatsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/stats", h.requireToken(h.handleStatsPage))
	mux.HandleFunc("/api/stats", h.requireToken(h.handleStatsAPI))
}

// requireToken 验证 Bearer Token 或 URL 查询参数 token。
func (h *StatsHandler) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.authenticate(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="stats"`)
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (h *StatsHandler) authenticate(r *http.Request) bool {
	// 优先 Authorization: Bearer <token>
	if auth := r.Header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
			return h.compareToken(auth[len(prefix):])
		}
	}
	// 兼容 URL 查询参数 token=<token>
	if tok := r.URL.Query().Get("token"); tok != "" {
		return h.compareToken(tok)
	}
	return false
}

func (h *StatsHandler) compareToken(tok string) bool {
	if h.token == "" || tok == "" {
		return false
	}
	if len(tok) != len(h.token) {
		return false
	}
	var same byte
	for i := 0; i < len(tok); i++ {
		same |= tok[i] ^ h.token[i]
	}
	return same == 0
}

func (h *StatsHandler) handleStatsAPI(w http.ResponseWriter, r *http.Request) {
	data, err := h.statsSvc.GetStats()
	if err != nil {
		log.Printf("get stats failed: %v", err)
		http.Error(w, `{"error":"获取统计数据失败"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (h *StatsHandler) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.statsSvc.GetStats()
	if err != nil {
		log.Printf("get stats failed: %v", err)
		http.Error(w, "获取统计数据失败", http.StatusInternalServerError)
		return
	}

	jsonStr, err := h.statsSvc.ToJSON(data)
	if err != nil {
		log.Printf("marshal stats failed: %v", err)
		http.Error(w, "序列化统计数据失败", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("stats").Parse(statsTemplate)
	if err != nil {
		log.Printf("parse stats template failed: %v", err)
		http.Error(w, "模板解析失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, map[string]interface{}{
		"Data": template.JS(jsonStr),
	}); err != nil {
		log.Printf("execute stats template failed: %v", err)
	}
}

const statsTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>喵滴 Agent 统计看板</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script src="https://cdn.jsdelivr.net/npm/echarts@5.4.3/dist/echarts.min.js"></script>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; }
  </style>
</head>
<body class="bg-slate-50 text-slate-800 min-h-screen">
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="mb-8 flex flex-col sm:flex-row sm:items-end sm:justify-between gap-2">
      <div>
        <h1 class="text-3xl font-bold text-slate-900">喵滴 Agent 统计看板</h1>
        <p class="text-slate-500 mt-1">实时掌握 Bot 运行与用户活跃情况</p>
      </div>
      <div class="text-sm text-slate-400">数据生成时间：<span id="generated-at"></span></div>
    </div>

    <!-- Tab 导航 -->
    <div class="mb-6 border-b border-slate-200">
      <nav class="-mb-px flex space-x-8" aria-label="Tabs">
        <button onclick="switchTab('overview')" id="tab-overview" class="tab-btn border-indigo-500 text-indigo-600 whitespace-nowrap py-4 px-1 border-b-2 font-medium text-sm">
          概览
        </button>
        <button onclick="switchTab('performance')" id="tab-performance" class="tab-btn border-transparent text-slate-500 hover:text-slate-700 hover:border-slate-300 whitespace-nowrap py-4 px-1 border-b-2 font-medium text-sm">
          性能统计
        </button>
      </nav>
    </div>

    <!-- 概览 Tab -->
    <div id="panel-overview" class="tab-panel">
    <!-- 核心指标卡片 -->
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-6 mb-8">
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <div class="text-sm font-medium text-slate-500 mb-1">总用户数</div>
        <div class="text-3xl font-bold text-indigo-600" id="total-users">0</div>
      </div>
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <div class="text-sm font-medium text-slate-500 mb-1">已绑定用户</div>
        <div class="text-3xl font-bold text-emerald-600" id="bound-users">0</div>
      </div>
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <div class="text-sm font-medium text-slate-500 mb-1">总会话数</div>
        <div class="text-3xl font-bold text-blue-600" id="total-conversations">0</div>
      </div>
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <div class="text-sm font-medium text-slate-500 mb-1">近 7 天喵滴请求</div>
        <div class="text-3xl font-bold text-amber-600" id="calls-7">0</div>
      </div>
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <div class="text-sm font-medium text-slate-500 mb-1">近 7 天 LLM 调用</div>
        <div class="text-3xl font-bold text-purple-600" id="llm-calls-7">0</div>
      </div>
    </div>

    <!-- 30 天趋势 -->
    <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100 mb-6">
      <h2 class="text-lg font-semibold text-slate-800 mb-4">近 30 天喵滴请求趋势</h2>
      <div id="chart-30" class="w-full h-80"></div>
    </div>

    <!-- LLM 调用与 Token 趋势 -->
    <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100 mb-6">
      <h2 class="text-lg font-semibold text-slate-800 mb-4">近 30 天 LLM 调用次数与 Token 消耗</h2>
      <div id="chart-llm-30" class="w-full h-80"></div>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
      <!-- 7 天趋势 -->
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <h2 class="text-lg font-semibold text-slate-800 mb-4">近 7 天请求趋势</h2>
        <div id="chart-7" class="w-full h-72"></div>
      </div>
      <!-- 接口调用占比 -->
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <h2 class="text-lg font-semibold text-slate-800 mb-4">近 30 天接口调用占比</h2>
        <div id="chart-pie" class="w-full h-72"></div>
      </div>
    </div>

    <!-- 活跃用户卡片 -->
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-6">
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100 flex items-center justify-between">
        <div>
          <div class="text-sm font-medium text-slate-500">近 30 天喵滴请求</div>
          <div class="text-2xl font-bold text-slate-800 mt-1" id="calls-30">0</div>
        </div>
        <div class="w-10 h-10 rounded-full bg-indigo-50 flex items-center justify-center text-indigo-600">📈</div>
      </div>
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100 flex items-center justify-between">
        <div>
          <div class="text-sm font-medium text-slate-500">近 30 天 LLM 调用</div>
          <div class="text-2xl font-bold text-slate-800 mt-1" id="llm-calls-30">0</div>
        </div>
        <div class="w-10 h-10 rounded-full bg-purple-50 flex items-center justify-center text-purple-600">🤖</div>
      </div>
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100 flex items-center justify-between">
        <div>
          <div class="text-sm font-medium text-slate-500">近 7 天活跃用户</div>
          <div class="text-2xl font-bold text-slate-800 mt-1" id="active-7">0</div>
        </div>
        <div class="w-10 h-10 rounded-full bg-emerald-50 flex items-center justify-center text-emerald-600">👥</div>
      </div>
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100 flex items-center justify-between">
        <div>
          <div class="text-sm font-medium text-slate-500">近 30 天活跃用户</div>
          <div class="text-2xl font-bold text-slate-800 mt-1" id="active-30">0</div>
        </div>
        <div class="w-10 h-10 rounded-full bg-blue-50 flex items-center justify-center text-blue-600">🌍</div>
      </div>
    </div>
    </div>

    <!-- 性能统计 Tab -->
    <div id="panel-performance" class="tab-panel hidden">
      <div class="bg-white rounded-2xl shadow-sm p-6 border border-slate-100">
        <h2 class="text-lg font-semibold text-slate-800 mb-4">接口性能统计</h2>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-slate-200 text-sm">
            <thead class="bg-slate-50">
              <tr>
                <th class="px-4 py-3 text-left font-medium text-slate-500">接口</th>
                <th class="px-4 py-3 text-right font-medium text-slate-500">调用次数</th>
                <th class="px-4 py-3 text-right font-medium text-slate-500">成功率</th>
                <th class="px-4 py-3 text-right font-medium text-slate-500">平均 (ms)</th>
                <th class="px-4 py-3 text-right font-medium text-slate-500">P50</th>
                <th class="px-4 py-3 text-right font-medium text-slate-500">P90</th>
                <th class="px-4 py-3 text-right font-medium text-slate-500">P95</th>
                <th class="px-4 py-3 text-right font-medium text-slate-500">P99</th>
              </tr>
            </thead>
            <tbody id="performance-table-body" class="divide-y divide-slate-200"></tbody>
          </table>
        </div>
        <p class="text-xs text-slate-400 mt-4">* 数据自进程启动开始累积，页面刷新后重置。</p>
      </div>
    </div>

    <div class="mt-8 text-center text-xs text-slate-400">
      喵滴 Agent · 统计看板
    </div>
  </div>

  <script>
    const stats = {{.Data}};

    document.getElementById('generated-at').textContent = stats.generated_at;
    document.getElementById('total-users').textContent = stats.total_users.toLocaleString();
    document.getElementById('bound-users').textContent = stats.bound_users.toLocaleString();
    document.getElementById('total-conversations').textContent = stats.total_conversations.toLocaleString();
    document.getElementById('calls-7').textContent = stats.calls_7_days.toLocaleString();
    document.getElementById('calls-30').textContent = stats.calls_30_days.toLocaleString();
    document.getElementById('llm-calls-7').textContent = stats.llm_calls_7_days.toLocaleString();
    document.getElementById('llm-calls-30').textContent = stats.llm_calls_30_days.toLocaleString();
    document.getElementById('active-7').textContent = stats.active_users_7_days.toLocaleString();
    document.getElementById('active-30').textContent = stats.active_users_30_days.toLocaleString();

    function renderLineChart(domId, data, color) {
      const chart = echarts.init(document.getElementById(domId));
      const dates = data.map(d => d.date);
      const counts = data.map(d => d.count);
      chart.setOption({
        tooltip: { trigger: 'axis' },
        grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
        xAxis: { type: 'category', boundaryGap: false, data: dates, axisLine: { lineStyle: { color: '#94a3b8' } } },
        yAxis: { type: 'value', splitLine: { lineStyle: { color: '#f1f5f9' } }, axisLine: { lineStyle: { color: '#94a3b8' } } },
        series: [{
          data: counts,
          type: 'line',
          smooth: true,
          symbol: 'circle',
          symbolSize: 8,
          itemStyle: { color: color },
          areaStyle: {
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: color + '40' },
              { offset: 1, color: color + '05' }
            ])
          },
          lineStyle: { width: 3 }
        }]
      });
      window.addEventListener('resize', () => chart.resize());
    }

    renderLineChart('chart-30', stats.daily_30_days, '#4f46e5');
    renderLineChart('chart-7', stats.daily_7_days, '#10b981');

    const llmChart = echarts.init(document.getElementById('chart-llm-30'));
    const llmDates = stats.llm_daily_30_days.map(d => d.date);
    const llmCounts = stats.llm_daily_30_days.map(d => d.count);
    const llmTokens = stats.llm_daily_30_days.map(d => d.total_tokens);
    llmChart.setOption({
      tooltip: { trigger: 'axis' },
      legend: { data: ['调用次数', 'Token 消耗'], bottom: '0%' },
      grid: { left: '3%', right: '4%', bottom: '12%', containLabel: true },
      xAxis: { type: 'category', boundaryGap: false, data: llmDates, axisLine: { lineStyle: { color: '#94a3b8' } } },
      yAxis: [
        { type: 'value', name: '次数', position: 'left', splitLine: { lineStyle: { color: '#f1f5f9' } }, axisLine: { lineStyle: { color: '#94a3b8' } } },
        { type: 'value', name: 'Token', position: 'right', splitLine: { show: false }, axisLine: { lineStyle: { color: '#94a3b8' } } }
      ],
      series: [
        {
          name: '调用次数',
          data: llmCounts,
          type: 'line',
          smooth: true,
          symbol: 'circle',
          symbolSize: 8,
          itemStyle: { color: '#7c3aed' },
          lineStyle: { width: 3 }
        },
        {
          name: 'Token 消耗',
          data: llmTokens,
          type: 'line',
          smooth: true,
          symbol: 'circle',
          symbolSize: 8,
          yAxisIndex: 1,
          itemStyle: { color: '#f59e0b' },
          lineStyle: { width: 3, type: 'dashed' }
        }
      ]
    });
    window.addEventListener('resize', () => llmChart.resize());

    const pieChart = echarts.init(document.getElementById('chart-pie'));
    pieChart.setOption({
      tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
      legend: { bottom: '0%' },
      series: [{
        type: 'pie',
        radius: ['40%', '70%'],
        avoidLabelOverlap: false,
        itemStyle: { borderRadius: 8, borderColor: '#fff', borderWidth: 2 },
        label: { show: false },
        emphasis: { label: { show: true, fontSize: 16, fontWeight: 'bold' } },
        data: stats.action_stats.map(s => ({ value: s.count, name: s.action }))
      }]
    });
    window.addEventListener('resize', () => pieChart.resize());

    function switchTab(name) {
      document.querySelectorAll('.tab-panel').forEach(el => el.classList.add('hidden'));
      document.getElementById('panel-' + name).classList.remove('hidden');
      document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.classList.remove('border-indigo-500', 'text-indigo-600');
        btn.classList.add('border-transparent', 'text-slate-500');
      });
      const active = document.getElementById('tab-' + name);
      active.classList.remove('border-transparent', 'text-slate-500');
      active.classList.add('border-indigo-500', 'text-indigo-600');
    }

    function renderPerformanceTable() {
      const tbody = document.getElementById('performance-table-body');
      const rows = (stats.performance || []).map(p => {
        const successRate = (p.success_rate * 100).toFixed(1) + '%';
        const successClass = p.success_rate >= 0.99 ? 'text-emerald-600' : p.success_rate >= 0.9 ? 'text-amber-600' : 'text-rose-600';
        return '<tr class="hover:bg-slate-50">' +
          '<td class="px-4 py-3 font-medium text-slate-700">' + p.name + '</td>' +
          '<td class="px-4 py-3 text-right text-slate-600">' + p.count.toLocaleString() + '</td>' +
          '<td class="px-4 py-3 text-right ' + successClass + '">' + successRate + '</td>' +
          '<td class="px-4 py-3 text-right text-slate-600">' + p.avg_ms.toFixed(1) + '</td>' +
          '<td class="px-4 py-3 text-right text-slate-600">' + p.p50_ms.toFixed(1) + '</td>' +
          '<td class="px-4 py-3 text-right text-slate-600">' + p.p90_ms.toFixed(1) + '</td>' +
          '<td class="px-4 py-3 text-right text-slate-600">' + p.p95_ms.toFixed(1) + '</td>' +
          '<td class="px-4 py-3 text-right text-slate-600">' + p.p99_ms.toFixed(1) + '</td>' +
          '</tr>';
      }).join('');
      tbody.innerHTML = rows || '<tr><td colspan="8" class="px-4 py-6 text-center text-slate-400">暂无性能数据</td></tr>';
    }

    renderPerformanceTable();
  </script>
</body>
</html>
`
