// Virtual City batch 81 "System dialog" i18n (en).
// Split into its own file: en.ts is near the §4 1800-line cap.
// Key set mirrors virtualCitySystem-zh.ts exactly (three-language sync, §12).
const virtualCitySystemEN = {
  'virtualCity.systemDialog.title': 'System dialog',
  'virtualCity.systemDialog.subtitle': 'Chat with the City System agent — check status, inject scenarios, control the simulation and camera in natural language',
  'virtualCity.systemDialog.open': 'System dialog',
  'virtualCity.systemDialog.close': 'Close',
  'virtualCity.systemDialog.minimize': 'Minimize',
  'virtualCity.systemDialog.restore': 'Restore dialog',
  'virtualCity.systemDialog.inputPlaceholder': 'Type a command, e.g. "show city status" / "inject a magnitude 3 earthquake"… (Enter to send, Shift+Enter for newline)',
  'virtualCity.systemDialog.send': 'Send',
  'virtualCity.systemDialog.clear': 'Clear',
  'virtualCity.systemDialog.busy': 'System is working…',
  'virtualCity.systemDialog.emptyTitle': 'Start chatting with the City System',
  'virtualCity.systemDialog.emptyHint': 'Try: show city status / inject a magnitude 3 earthquake / move the camera to the city center',
  'virtualCity.systemDialog.errorPrefix': 'System error: ',
  'virtualCity.systemDialog.toolPrefix': 'Tool',
  'virtualCity.systemDialog.showCommandBadge': 'Camera adjusted',
  'virtualCity.systemDialog.reportBadge': 'Prediction report',
  'virtualCity.systemDialog.minimizedBar': 'System dialog',
  'virtualCity.systemDialog.connected': 'System agent connected',
  'virtualCity.systemDialog.disconnected': 'System agent not connected',
  'virtualCity.systemDialog.disabled': 'System dialog is disabled on the server ([city_system] enabled=false); sending commands is unavailable.',
  // Batch 83 §3.1: header drag-handle hint (title + aria-label).
  'virtualCity.systemDialog.dragHint': 'Drag to move; double-click header to reset',
  'virtualCity.systemDialog.toolPending': 'running',
  'virtualCity.systemDialog.toolFailed': 'failed',
  'virtualCity.systemDialog.toolOk': 'ok',
  'virtualCity.systemDialog.toolInput': 'Input',
  'virtualCity.systemDialog.toolOutput': 'Output',
  'virtualCity.systemDialog.toolRunning': 'Calling…',
  // Create-room modal "System agent line" (mirrors the collector agent line).
  'virtualCity.citySystemLine': 'System agent line',
  'virtualCity.citySystemLineRandom': 'Random only',
  'virtualCity.citySystemLineHint':
    'LsmAgentGame-City-System picks a random line from the LLM line pool for each turn; you can also pin a specific model line.',
};

export default virtualCitySystemEN;
