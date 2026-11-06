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
  // Batch 84 §5.1: scenario catalog browser (dimension tabs + search + item list).
  'virtualCity.systemDialog.catalogTitle': 'Scenario catalog',
  'virtualCity.systemDialog.catalogSearch': 'Search scenarios (name / kind / description)',
  'virtualCity.systemDialog.catalogAll': 'All',
  'virtualCity.systemDialog.catalogLoading': 'Loading catalog…',
  'virtualCity.systemDialog.catalogEmpty': 'No matching scenario',
  'virtualCity.systemDialog.catalogError': 'Failed to load the scenario catalog',
  'virtualCity.systemDialog.catalogTotal': '{n} in total',
  'virtualCity.systemDialog.catalogHint': 'Clicking an item only fills the input box; you still have to confirm and send it.',
  'virtualCity.systemDialog.catalogInject': 'Inject',
  // Batch 84 §5.1: system info panel (city summary + agent read-back).
  'virtualCity.systemDialog.infoTitle': 'System info',
  'virtualCity.systemDialog.infoEmpty': 'No city state data yet',
  'virtualCity.systemDialog.infoMonth': 'Month',
  'virtualCity.systemDialog.infoPopulation': 'Population',
  'virtualCity.systemDialog.infoCpi': 'CPI',
  'virtualCity.systemDialog.infoUnemployment': 'Unemployment',
  'virtualCity.systemDialog.infoStock': 'Stock index',
  'virtualCity.systemDialog.infoTreasury': 'Treasury',
  'virtualCity.systemDialog.infoPolicyRate': 'Policy rate',
  'virtualCity.systemDialog.infoScenarios': 'Active scenarios',
  'virtualCity.systemDialog.infoAgentState': 'Agent read-back',
  // Batch 84 §4.3: incoming agent letter card.
  'virtualCity.systemDialog.agentMessageFrom': 'From',
  'virtualCity.systemDialog.agentMessageUnknown': 'Unknown agent',
  // Batch 84: display-command notices (unsupported / unwired / invalid).
  'virtualCity.systemDialog.noticeUnsupportedType': 'Unsupported display command: {type}',
  'virtualCity.systemDialog.noticeUnwired': 'This display capability is not wired to the engine in this batch: {type}',
  'virtualCity.systemDialog.noticeInvalidParams': 'Invalid display command parameters: {type}',
  'virtualCity.systemDialog.noticeEntityNotFound': 'Entity not found: {detail}',
  'virtualCity.systemDialog.noticeUnknownCategory': 'Unknown category: {detail}',
  // Batch 85 §3.2: object-level display control (degradation disclosure / vehicle not found).
  'virtualCity.systemDialog.noticeFilterShowhide': 'Current implementation shows/hides by category (no per-instance tinting): {detail}',
  'virtualCity.systemDialog.noticeVehicleNotFound': 'Vehicle not found: {detail}',
  'virtualCity.systemDialog.noticeDismiss': 'Dismiss',
};

export default virtualCitySystemEN;
