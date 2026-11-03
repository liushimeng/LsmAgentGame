// 虚拟城市 批次81「💬 系统对话」i18n（zh-CN）。
// 拆成独立文件：zh-CN.ts 已逼近 §4 的 1800 行硬上限。
// 键集合与 virtualCityKeys.ts 的 VirtualCityDict 完全对齐（三语同步，§12）。
const virtualCitySystemZH = {
  'virtualCity.systemDialog.title': '系统对话',
  'virtualCity.systemDialog.subtitle': '与城市系统主 Agent 对话 —— 用自然语言查看状态、注入场景、控制模拟与视角',
  'virtualCity.systemDialog.open': '系统对话',
  'virtualCity.systemDialog.close': '关闭',
  'virtualCity.systemDialog.minimize': '最小化',
  'virtualCity.systemDialog.restore': '恢复对话',
  'virtualCity.systemDialog.inputPlaceholder': '输入指令，例如：查看城市状态 / 注入一个强度 3 的地震…（Enter 发送，Shift+Enter 换行）',
  'virtualCity.systemDialog.send': '发送',
  'virtualCity.systemDialog.clear': '清空',
  'virtualCity.systemDialog.busy': '系统正在处理…',
  'virtualCity.systemDialog.emptyTitle': '开始与城市系统对话',
  'virtualCity.systemDialog.emptyHint': '试试：查看城市状态 / 注入一个强度 3 的地震 / 把视角切到市中心',
  'virtualCity.systemDialog.errorPrefix': '系统错误：',
  'virtualCity.systemDialog.toolPrefix': '工具',
  'virtualCity.systemDialog.showCommandBadge': '已调整视角',
  'virtualCity.systemDialog.reportBadge': '预测报告',
  'virtualCity.systemDialog.minimizedBar': '系统对话',
  'virtualCity.systemDialog.connected': '系统 Agent 已连接',
  'virtualCity.systemDialog.disconnected': '系统 Agent 未连接',
  'virtualCity.systemDialog.disabled': '系统对话已停用（服务端 [city_system] enabled=false），暂不可发送指令。',
  'virtualCity.systemDialog.toolPending': '进行中',
  'virtualCity.systemDialog.toolFailed': '失败',
  'virtualCity.systemDialog.toolOk': '成功',
  'virtualCity.systemDialog.toolInput': '参数',
  'virtualCity.systemDialog.toolOutput': '输出',
  'virtualCity.systemDialog.toolRunning': '正在调用…',
  // 建房弹窗「系统 Agent 线路」（与既有「采集 Agent 线路」同构）。
  'virtualCity.citySystemLine': '系统 Agent 线路',
  'virtualCity.citySystemLineRandom': '只随机',
  'virtualCity.citySystemLineHint':
    'LsmAgentGame-City-System 主 Agent 每轮对话从 LLM 线路池随机取一条线路；也可固定到某个模型线路。',
};

export default virtualCitySystemZH;
