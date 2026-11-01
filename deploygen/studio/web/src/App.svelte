<script lang="ts">
  import { onMount } from 'svelte';

  type Field = { key: string; type: string; required: boolean; label: string; validation?: string; default_value?: unknown };
  type ActionMeta = {
    name: string; kind: 'action' | 'nflow'; description: string; method: string; path: string; req_type: string; res_type: string;
    source_file?: string; source_line?: number; success_status?: number; flow_schema_driven?: boolean;
    request_schema?: string; response_schema?: string;
    tags: string[]; node?: string; category?: string; is_system: boolean; requires_auth: boolean;
    required_roles?: string[]; required_permissions?: string[]; required_features?: string[];
    idempotent: boolean; timeout_ms?: number; retry_max?: number; rate_limit?: string;
    cache_ttl_ms?: number; concurrency_limit?: number; deduplicated?: boolean; coalesced?: boolean;
    req_fields: Field[]; res_fields: Field[];
  };
  type Span = { name: string; status: string; start_offset_us: number; duration_us: number; duration_ns?: number; duration: string; detail?: string };
  type Execution = {
    id: string; action_name: string; timestamp: string; status: number; success: boolean;
    duration_ms: number; duration_formatted: string; duration_us?: number; total_trace_us?: number;
    payload?: unknown; response?: unknown; error?: string | { kind?: string; message?: string; cause?: string; validation?: unknown };
    waterfall?: Span[];
  };
  type Identity = { user_id: string; tenant_id: string; roles: string[]; permissions: string[]; features: string[] };
  type SecurityUniverse = { permissions: string[]; roles: string[]; features: string[]; actions: Record<string, { roles: string[]; permissions: string[]; features: string[] }> };
  type FlatRow = { path: string; value: unknown; type: string; depth: number };

  let actions = $state<ActionMeta[]>([]);
  let selected = $state<ActionMeta | null>(null);
  let query = $state('');
  let selectedTag = $state('ALL');
  let sourceFilter = $state<'ALL' | 'ACTIONS' | 'NFLOW'>('ALL');
  let showSystemActions = $state(false);
  let mode = $state<'form' | 'json'>('form');
  let payload = $state<Record<string, unknown>>({});
  let payloadText = $state('{}');
  let activeSnippet = $state<'curl' | 'ts' | 'go'>('curl');
  let execution = $state<Execution | null>(null);
  let rightTab = $state<'response' | 'trace' | 'history'>('response');
  let responseMode = $state<'tree' | 'json'>('tree');
  let responseFilter = $state('');
  let history = $state<Execution[]>([]);
  let routeHistory = $state<Execution[]>([]);
  let busy = $state('');
  let errorMessage = $state('');
  let statusMessage = $state('');
  let security = $state<SecurityUniverse>({ permissions: [], roles: [], features: [], actions: {} });
  let persona = $state<'super' | 'match' | 'anon' | 'custom'>('super');
  let claimsOpen = $state(false);
  let customIdentity = $state<Identity>({ user_id: 'dev-admin', tenant_id: 'ws-dev', roles: [], permissions: [], features: [] });
  let theme = $state<'dark' | 'light'>('dark');
  let mobileView = $state<'workbench' | 'response'>('workbench');
  let schemaOpen = $state(false);
  let schemaSide = $state<'request' | 'response'>('request');
  let schemaMode = $state<'table' | 'ts' | 'go'>('table');
  let leftWidth = $state(254);
  let rightWidth = $state(420);

  const visibleActions = $derived(actions.filter((a) => {
    if (!showSystemActions && a.is_system) return false;
    if (sourceFilter === 'ACTIONS' && a.kind === 'nflow') return false;
    if (sourceFilter === 'NFLOW' && a.kind !== 'nflow') return false;
    if (selectedTag !== 'ALL' && !(a.tags || []).includes(selectedTag)) return false;
    const needle = query.trim().toLowerCase();
    return !needle || `${a.name} ${a.path} ${a.description} ${(a.tags || []).join(' ')}`.toLowerCase().includes(needle);
  }));
  const tags = $derived(Array.from(new Set(actions.filter((a) => showSystemActions || !a.is_system).flatMap((a) => a.tags || []))).sort());
  const businessCount = $derived(actions.filter((a) => !a.is_system).length);
  const systemCount = $derived(actions.filter((a) => a.is_system).length);
  const goActionCount = $derived(actions.filter((a) => a.kind !== 'nflow' && !a.is_system).length);
  const nflowRouteCount = $derived(actions.filter((a) => a.kind === 'nflow').length);
  const schemaFields = $derived(selected ? (schemaSide === 'request' ? selected.req_fields : selected.res_fields) || [] : []);
  const schemaType = $derived(selected ? (schemaSide === 'request' ? selected.req_type : selected.res_type) || 'any' : 'any');
  const resultValue = $derived(execution ? (execution.success ? execution.response : execution.error) : null);
  const flatResponse = $derived(flatten(resultValue).filter((row) => {
    const needle = responseFilter.trim().toLowerCase();
    return !needle || `${row.path} ${stringify(row.value)}`.toLowerCase().includes(needle);
  }));
  const responseSize = $derived(resultValue === null ? 0 : new TextEncoder().encode(JSON.stringify(resultValue)).length);
  const payloadSize = $derived((mode === 'form' ? JSON.stringify(payload) : payloadText).length);
  const currentIdentity = $derived(makeIdentity());

  onMount(() => {
    try {
      const savedTheme = localStorage.getItem('nexss_studio_theme');
      if (savedTheme === 'light' || savedTheme === 'dark') theme = savedTheme;
    } catch { /* Private browsing may disable local storage. */ }
    applyTheme(theme);
    void loadStudio();
  });

  async function api<T = any>(path: string, options: RequestInit = {}): Promise<T> {
    let response: Response;
    try {
      response = await fetch(path, {
        ...options,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', ...(options.headers || {}) }
      });
    } catch (error) {
      throw new Error(`Could not reach the Studio backend: ${error instanceof Error ? error.message : String(error)}`);
    }
    let data: any;
    try { data = await response.json(); }
    catch { throw new Error(`Studio backend returned an unreadable response (HTTP ${response.status}).`); }
    if (!response.ok) throw new Error(typeof data?.error === 'string' ? data.error : data?.message || `Request failed (HTTP ${response.status}).`);
    return data as T;
  }

  async function loadStudio() {
    busy = 'load'; errorMessage = '';
    try {
      const [catalog, claims] = await Promise.all([
        api<ActionMeta[]>('/studio/api/actions'),
        api<SecurityUniverse>('/system/security').catch(() => ({ permissions: [], roles: [], features: [], actions: {} }))
      ]);
      actions = Array.isArray(catalog) ? catalog : [];
      security = claims || { permissions: [], roles: [], features: [], actions: {} };
      customIdentity = { ...customIdentity, roles: [...(security.roles || [])], permissions: [...(security.permissions || [])], features: [...(security.features || [])] };
      const first = actions.find((a) => !a.is_system) || actions[0];
      if (first) selectAction(first);
      statusMessage = actions.length ? `${goActionCount} Go actions · ${nflowRouteCount} NFlow routes ready` : 'No registered actions or NFlow routes found';
      await loadHistory();
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : String(error);
    } finally { busy = ''; }
  }

  function defaultPayload(action: ActionMeta): Record<string, unknown> {
    const initial: Record<string, unknown> = {};
    for (const field of action.req_fields || []) {
      if (field.default_value !== undefined && field.default_value !== null) initial[field.key] = field.default_value;
      else if (field.type === 'number') initial[field.key] = 0;
      else if (field.type === 'boolean') initial[field.key] = false;
      else if (field.type === 'array') initial[field.key] = [];
      else initial[field.key] = '';
    }
    return initial;
  }

  function selectAction(action: ActionMeta) {
    selected = action; execution = null; errorMessage = ''; statusMessage = '';
    mode = 'form'; responseFilter = ''; rightTab = 'response';
    payload = defaultPayload(action);
    payloadText = JSON.stringify(payload, null, 2);
    if (persona === 'match') customIdentity = identityForAction(action);
  }

  function updateField(field: Field, raw: string | boolean) {
    if (field.type === 'boolean') payload[field.key] = Boolean(raw);
    else if (field.type === 'number') payload[field.key] = raw === '' ? '' : Number(raw);
    else if (field.type === 'array' || field.type === 'object') {
      try { payload[field.key] = JSON.parse(String(raw)); }
      catch { payload[field.key] = String(raw); }
    } else payload[field.key] = String(raw);
    payloadText = JSON.stringify(payload, null, 2);
  }

  function buildRouteURL(action: ActionMeta, body: Record<string, unknown>): { url: URL; payload: Record<string, unknown> } {
    const pathFields = new Set<string>();
    const path = action.path.replace(/\{([^}]+)\}/g, (_match, key: string) => {
      pathFields.add(key);
      const value = body[key];
      if (value === undefined || value === null || String(value) === '') throw new Error(`Enter a value for path field "${key}".`);
      return encodeURIComponent(String(value));
    });
    const url = new URL(path, window.location.origin);
    const requestPayload = { ...body };
    for (const key of pathFields) delete requestPayload[key];
    if (['GET', 'HEAD'].includes((action.method || 'POST').toUpperCase())) {
      for (const [key, value] of Object.entries(requestPayload)) {
        if (value === undefined || value === null || value === '') continue;
        if (Array.isArray(value)) value.forEach((item) => url.searchParams.append(key, String(item)));
        else url.searchParams.set(key, typeof value === 'object' ? JSON.stringify(value) : String(value));
      }
    }
    return { url, payload: requestPayload };
  }

  async function executeNFlowRoute(action: ActionMeta, body: Record<string, unknown>): Promise<Execution> {
    const method = (action.method || 'POST').toUpperCase();
    const { url, payload: routePayload } = buildRouteURL(action, body);
    const started = performance.now();
    let response: Response | undefined;
    let responseBody: unknown = null;
    let networkError = '';
    try {
      response = await fetch(`${url.pathname}${url.search}`, {
        method,
        credentials: 'same-origin',
        headers: ['GET', 'HEAD'].includes(method) ? { Accept: 'application/json' } : { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: ['GET', 'HEAD'].includes(method) ? undefined : JSON.stringify(routePayload)
      });
      const raw = await response.text();
      if (raw) {
        try { responseBody = JSON.parse(raw); }
        catch { responseBody = raw; }
      }
    } catch (error) {
      networkError = error instanceof Error ? error.message : String(error);
    }
    const elapsedUs = Math.round((performance.now() - started) * 1000);
    const success = Boolean(response?.ok);
    const status = response?.status || 0;
    const error = networkError
      ? { kind: 'NETWORK_ERROR', message: `Could not reach ${method} ${action.path}: ${networkError}` }
      : success ? undefined : {
        kind: 'HTTP_ERROR',
        message: typeof responseBody === 'object' && responseBody !== null
          ? String((responseBody as { error?: string; message?: string }).error || (responseBody as { message?: string }).message || `Route returned HTTP ${status}.`)
          : `Route returned HTTP ${status}.`,
        cause: responseBody === null ? undefined : typeof responseBody === 'string' ? responseBody : JSON.stringify(responseBody)
      };
    return {
      id: `route_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      action_name: action.name,
      timestamp: new Date().toISOString(),
      status,
      success,
      duration_ms: Math.round(elapsedUs / 1000),
      duration_us: elapsedUs,
      total_trace_us: elapsedUs,
      duration_formatted: `${(elapsedUs / 1000).toFixed(elapsedUs < 10000 ? 2 : 0)}ms`,
      payload: body,
      response: responseBody,
      error,
      waterfall: []
    };
  }

  async function executeAction() {
    if (!selected || busy) return;
    errorMessage = ''; statusMessage = ''; busy = 'execute';
    let body: unknown;
    try {
      body = mode === 'form' ? payload : JSON.parse(payloadText);
      if (body === null || Array.isArray(body) || typeof body !== 'object') throw new Error('Request payload must be a JSON object.');
      const missing = (selected.req_fields || []).filter((field) => field.required && ((body as Record<string, unknown>)[field.key] === undefined || (body as Record<string, unknown>)[field.key] === null || (body as Record<string, unknown>)[field.key] === ''));
      if (missing.length) throw new Error(`Required fields missing: ${missing.map((field) => field.key).join(', ')}.`);
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : 'Request JSON is invalid.';
      busy = ''; return;
    }
    try {
      if (selected.kind === 'nflow') {
        execution = await executeNFlowRoute(selected, body as Record<string, unknown>);
        routeHistory = [execution, ...routeHistory.filter((record) => record.id !== execution?.id)].slice(0, 50);
        history = [...routeHistory, ...history.filter((record) => !routeHistory.some((routeRecord) => routeRecord.id === record.id))]
          .sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp)).slice(0, 50);
        statusMessage = execution.success ? 'NFlow route completed over HTTP' : 'NFlow route returned an error';
        if (persona !== 'super') statusMessage += ' · route used the normal same-origin session; Studio identity overrides apply only to Go action runs';
      } else {
        execution = await api<Execution>('/studio/api/execute', {
          method: 'POST', body: JSON.stringify({ action_name: selected.name, payload: body, identity: currentIdentity })
        });
        statusMessage = execution.success ? 'Go action completed' : 'Go action returned an error';
        await loadHistory();
      }
      rightTab = 'response';
      if (window.innerWidth < 1280) mobileView = 'response';
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : String(error);
    } finally { busy = ''; }
  }

  async function loadHistory() {
    try {
      const stored = await api<Execution[]>('/studio/api/history?limit=50');
      history = [...routeHistory, ...stored.filter((record) => !routeHistory.some((routeRecord) => routeRecord.id === record.id))]
        .sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp)).slice(0, 50);
    }
    catch (error) { if (!errorMessage) errorMessage = error instanceof Error ? error.message : String(error); }
  }

  async function generateScenario(scenario: string) {
    if (!selected || busy) return;
    errorMessage = ''; busy = 'scenario';
    try {
      const result = await api<{ payload: Record<string, unknown> }>('/studio/api/ai/scenario', {
        method: 'POST', body: JSON.stringify({ action_name: selected.name, scenario })
      });
      payload = result.payload || {};
      payloadText = JSON.stringify(payload, null, 2);
      mode = 'form'; statusMessage = `${scenarioLabel(scenario)} payload loaded`;
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : String(error);
    } finally { busy = ''; }
  }

  function replay(record: Execution) {
    const action = actions.find((entry) => entry.name === record.action_name);
    if (action) selectAction(action);
    if (record.payload && typeof record.payload === 'object' && !Array.isArray(record.payload)) {
      payload = record.payload as Record<string, unknown>;
      payloadText = JSON.stringify(payload, null, 2);
    }
    mode = 'form'; rightTab = 'response'; mobileView = 'workbench'; statusMessage = `Loaded ${record.action_name} from history`;
  }

  function setTheme(next: 'dark' | 'light') { theme = next; applyTheme(next); }
  function startResize(event: PointerEvent, pane: 'left' | 'right') {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = pane === 'left' ? leftWidth : rightWidth;
    document.body.style.cursor = 'col-resize';
    const move = (moveEvent: PointerEvent) => {
      const delta = moveEvent.clientX - startX;
      if (pane === 'left') leftWidth = Math.max(180, Math.min(500, startWidth + delta));
      else rightWidth = Math.max(300, Math.min(650, startWidth - delta));
    };
    const stop = () => {
      document.body.style.cursor = '';
      document.removeEventListener('pointermove', move);
    };
    document.addEventListener('pointermove', move);
    document.addEventListener('pointerup', stop, { once: true });
  }
  function applyTheme(next: 'dark' | 'light') {
    if (typeof document === 'undefined') return;
    document.documentElement.classList.toggle('light', next === 'light');
    try { localStorage.setItem('nexss_studio_theme', next); } catch { /* Ignore unavailable storage. */ }
  }

  function makeIdentity(): Identity {
    if (persona === 'anon') return { user_id: '', tenant_id: '', roles: [], permissions: [], features: [] };
    if (persona === 'custom') return structuredClone(customIdentity);
    if (persona === 'match') return identityForAction(selected);
    return { user_id: 'dev-admin', tenant_id: 'ws-dev', roles: [...(security.roles || [])], permissions: [...(security.permissions || [])], features: [...(security.features || [])] };
  }
  function identityForAction(action: ActionMeta | null): Identity {
    return {
      user_id: 'dev-matched-user', tenant_id: 'ws-dev',
      roles: [...(action?.required_roles || [])], permissions: [...(action?.required_permissions || [])], features: [...(action?.required_features || [])]
    };
  }
  function addCustomRole(role: string) {
    const value = role.trim();
    if (value && !customIdentity.roles.includes(value)) customIdentity.roles = [...customIdentity.roles, value];
  }
  function toggleClaim(field: 'roles' | 'permissions' | 'features', value: string, checked: boolean) {
    const current = new Set(customIdentity[field]);
    if (checked) current.add(value); else current.delete(value);
    customIdentity[field] = [...current];
  }
  function scenarioLabel(scenario: string) {
    return ({ happy: 'Happy path', edge: 'Edge limits', fuzz: 'Validation breach', injection: 'Injection test' } as Record<string, string>)[scenario] || scenario;
  }

  function schemaCode(language: 'ts' | 'go'): string {
    if (!schemaFields.length) return language === 'ts' ? `export type ${schemaType} = Record<string, never>;` : `type ${schemaType} struct {}`;
    if (language === 'ts') {
      const lines = schemaFields.map((field) => `  ${field.key}${field.required ? '' : '?'}: ${field.type === 'number' ? 'number' : field.type === 'boolean' ? 'boolean' : field.type === 'array' ? 'unknown[]' : field.type === 'object' ? 'Record<string, unknown>' : 'string'};`);
      return `export interface ${schemaType} {\n${lines.join('\n')}\n}`;
    }
    const lines = schemaFields.map((field) => {
      const name = field.key.split(/[-_]/).map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join('');
      const type = field.type === 'number' ? 'int64' : field.type === 'boolean' ? 'bool' : field.type === 'date' ? 'time.Time' : field.type === 'array' ? '[]any' : field.type === 'object' ? 'map[string]any' : 'string';
      return `\t${name} ${type} \`json:"${field.key}"\``;
    });
    return `type ${schemaType} struct {\n${lines.join('\n')}\n}`;
  }

  function snippet(): string {
    if (!selected) return '';
    const value = mode === 'form' ? payload : safeJSON(payloadText);
    const body = value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
    const { url: routeURL, payload: requestPayload } = buildRouteURL(selected, body);
    const method = (selected.method || 'POST').toUpperCase();
    const hasBody = !['GET', 'HEAD'].includes(method);
    const path = `${routeURL.pathname}${routeURL.search}`;
    if (activeSnippet === 'curl') {
      return hasBody
        ? `curl -X ${method} '${window.location.origin}${path}' -H 'Content-Type: application/json' -d '${JSON.stringify(requestPayload)}'`
        : `curl -X ${method} '${window.location.origin}${path}' -H 'Accept: application/json'`;
    }
    if (activeSnippet === 'ts') {
      const lines = [
        `const response = await fetch('${path}', {`,
        `  method: '${method}',`,
        `  headers: { Accept: 'application/json'${hasBody ? ", 'Content-Type': 'application/json'" : ''} }${hasBody ? ',' : ''}`
      ];
      if (hasBody) lines.push(`  body: JSON.stringify(${JSON.stringify(requestPayload, null, 2)})`);
      lines.push('});', 'const result = await response.json();');
      return lines.join('\n');
    }
    const lines = hasBody
      ? [`req, err := http.NewRequestWithContext(ctx, "${method}", "${path}", strings.NewReader(${JSON.stringify(JSON.stringify(requestPayload))}))`, 'req.Header.Set("Content-Type", "application/json")']
      : [`req, err := http.NewRequestWithContext(ctx, "${method}", "${path}", nil)`, 'req.Header.Set("Accept", "application/json")'];
    lines.push('if err != nil { return err }', 'resp, err := http.DefaultClient.Do(req)');
    return lines.join('\n');
  }
  function safeJSON(raw: string): unknown { try { return JSON.parse(raw); } catch { return {}; } }
  async function copyText(value: string) {
    try { await navigator.clipboard.writeText(value); statusMessage = 'Copied to clipboard'; }
    catch { errorMessage = 'Clipboard access is unavailable in this browser.'; }
  }

  function flatten(value: unknown, prefix = '', depth = 0): FlatRow[] {
    if (value === null || typeof value !== 'object') return prefix ? [{ path: prefix, value, type: value === null ? 'null' : typeof value, depth }] : [];
    const entries = Array.isArray(value) ? value.map((item, index) => [String(index), item] as const) : Object.entries(value as Record<string, unknown>);
    if (!entries.length) return prefix ? [{ path: prefix, value: Array.isArray(value) ? '[]' : '{}', type: Array.isArray(value) ? 'array' : 'object', depth }] : [];
    return entries.flatMap(([key, child]) => flatten(child, prefix ? `${prefix}.${key}` : key, depth));
  }
  function stringify(value: unknown): string { return typeof value === 'string' ? value : JSON.stringify(value); }
  function formatBytes(bytes: number): string { return bytes < 1024 ? `${bytes} B` : bytes < 1048576 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1048576).toFixed(2)} MB`; }
  function formatTime(value?: string): string { return value ? new Date(value).toLocaleTimeString() : '—'; }
  function errorText(value: Execution['error']): string { return typeof value === 'string' ? value : value?.message || JSON.stringify(value || {}); }
</script>

<svelte:head>
  <title>Nexss Studio — Action Workbench</title>
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</svelte:head>

<div class="app-shell">
  <header class="topbar">
    <div class="brand-line"><span class="brand">⚡ NEXSS // STUDIO</span><span class="counts">GO: <strong class="positive">{goActionCount}</strong> <i></i> NFLOW: <strong class="accent-text">{nflowRouteCount}</strong> <i></i> SYSTEM: <strong>{systemCount}</strong></span></div>
    <div class="principal-line">
      <label for="persona">Principal:</label>
      <select id="persona" bind:value={persona} onchange={() => { if (persona === 'match' && selected) customIdentity = identityForAction(selected); }}>
        <option value="super">Superuser (all app permissions)</option><option value="match">Match current action</option><option value="anon">Anonymous</option><option value="custom">Custom claims matrix</option>
      </select>
      <button class="identity-badge" type="button" onclick={() => claimsOpen = true}>User: <b>{currentIdentity.user_id || 'anonymous'}</b><i></i> Perms: <b>{currentIdentity.permissions.length}</b></button>
    </div>
    <div class="header-tools">
      <div class="mobile-tabs"><button class:active={mobileView === 'workbench'} onclick={() => mobileView = 'workbench'}>Workbench</button><button class:active={mobileView === 'response'} onclick={() => { mobileView = 'response'; void loadHistory(); }}>Response</button></div>
      <button class:active={showSystemActions} class="tool-button" type="button" onclick={() => { showSystemActions = !showSystemActions; selectedTag = 'ALL'; }}>{showSystemActions ? '●' : '○'} <span>{showSystemActions ? 'Hide System Actions' : 'Show System Actions'}</span></button>
      <div class="theme-switch"><button class:active={theme === 'dark'} onclick={() => setTheme('dark')}>DARK</button><button class:active={theme === 'light'} onclick={() => setTheme('light')}>LIGHT</button></div>
    </div>
  </header>

  {#if errorMessage}<div class="global-message error-message" role="alert"><strong>Studio error</strong><span>{errorMessage}</span><button onclick={() => errorMessage = ''} aria-label="Dismiss">×</button></div>{/if}
  {#if statusMessage}<div class="global-message status-message" role="status">{statusMessage}<button onclick={() => statusMessage = ''} aria-label="Dismiss">×</button></div>{/if}

  <main class="workbench" style={`--left-width:${leftWidth}px;--right-width:${rightWidth}px`} class:response-mode={mobileView === 'response'} class:workbench-mode={mobileView === 'workbench'}>
    <aside class="left-pane">
      <div class="search-area"><input aria-label="Search actions and routes" type="search" placeholder="Search actions & routes…" bind:value={query} /><div class="tag-list"><button class:tag-active={selectedTag === 'ALL'} onclick={() => selectedTag = 'ALL'}>ALL TAGS</button>{#each tags as tag}<button class:tag-active={selectedTag === tag} onclick={() => selectedTag = tag}>{tag}</button>{/each}</div></div>
      <div class="list-heading"><span>{showSystemActions ? 'All Operations' : 'Operations'}</span><div class="source-tabs"><button class:active={sourceFilter === 'ALL'} onclick={() => sourceFilter = 'ALL'}>ALL {businessCount}</button><button class:active={sourceFilter === 'ACTIONS'} onclick={() => sourceFilter = 'ACTIONS'}>GO {goActionCount}</button><button class:active={sourceFilter === 'NFLOW'} onclick={() => sourceFilter = 'NFLOW'}>NFLOW {nflowRouteCount}</button></div><b>{visibleActions.length}</b></div>
      <nav class="action-list" aria-label="Available actions">
        {#each visibleActions as action (action.name)}
          <button class="action-item" class:chosen={selected?.name === action.name} onclick={() => selectAction(action)}>
            <span class="action-name">{action.name}<small class="kind-marker" class:nflow-kind={action.kind === 'nflow'}>{action.kind === 'nflow' ? 'NFLOW' : 'GO ACTION'}</small></span><span class="action-route"><b class:method-get={action.method === 'GET'} class:method-other={action.method !== 'POST' && action.method !== 'GET'}>{action.method || 'POST'}</b><span>{action.path}</span></span>
          </button>
        {:else}<p class="empty-list">No actions match this filter.</p>{/each}
      </nav>
      <div class="left-footer">{actions.length} operations · <span>{goActionCount} Go / {nflowRouteCount} NFlow</span></div>
    </aside>
    <button class="resizer" type="button" aria-label="Resize action directory" onpointerdown={(event) => startResize(event, 'left')}></button>

    <section class="center-pane">
      {#if selected}
        <section class="action-card">
          <div class="action-card-top"><div class="action-heading"><div class="action-title"><span class="pill" class:method-post={selected.method === 'POST'} class:method-get={selected.method === 'GET'} class:method-other={selected.method !== 'POST' && selected.method !== 'GET'}>{selected.method || 'POST'}</span><h1>{selected.name}</h1><span class="kind-badge" class:nflow-kind={selected.kind === 'nflow'}>{selected.kind === 'nflow' ? 'NFLOW ROUTE' : 'REGISTERED GO ACTION'}</span><span class="node-badge">{selected.kind === 'nflow' ? `${selected.source_file || 'NFlow'}${selected.source_line ? `:${selected.source_line}` : ''}` : `NODE: ${selected.node || 'gateway'}`}</span></div><p>{selected.description || 'No description supplied.'}</p></div>
            <div class="schema-buttons"><button onclick={() => { schemaSide = 'request'; schemaMode = 'table'; schemaOpen = true; }}>REQ <strong>{selected.req_type || 'any'}</strong></button><button onclick={() => { schemaSide = 'response'; schemaMode = 'table'; schemaOpen = true; }}>RES <strong>{selected.res_type || 'any'}</strong></button></div></div>
          <div class="capabilities">{#if selected.kind === 'nflow'}<div><span>OPERATION</span><b>HTTP ROUTE</b></div><div><span>PIPELINE</span><b class="accent-text">{selected.name}</b></div><div><span>SUCCESS</span><b>{selected.success_status || 200}</b></div><div><span>REQ SCHEMA</span><b>{selected.request_schema || 'not declared'}</b></div><div><span>RES SCHEMA</span><b>{selected.response_schema || 'not declared'}</b></div><div><span>AUTH</span><b>SESSION</b></div>{:else}<div><span>IDEMPOTENT</span><b class:good={selected.idempotent}>{selected.idempotent ? 'YES' : 'NO'}</b></div><div><span>TIMEOUT</span><b>{selected.timeout_ms || 0}ms</b></div><div><span>RETRIES</span><b>{selected.retry_max || 0}x</b></div><div><span>RATE LIMIT</span><b class="accent-text">{selected.rate_limit || 'none'}</b></div><div><span>AUTH</span><b class:bad={selected.requires_auth} class:good={!selected.requires_auth}>{selected.requires_auth ? 'REQUIRED' : 'PUBLIC'}</b></div><div><span>DEDUP</span><b class:good={selected.deduplicated}>{selected.deduplicated ? 'YES' : 'NO'}</b></div>{/if}</div>
        </section>

        <section class="scenario-card"><div class="scenario-buttons">{#if selected.kind === 'nflow'}<span class="nflow-label">HTTP ROUTE</span><small>Calls the declared endpoint with the current same-origin session.</small>{:else}<span>TEST SCENARIOS</span><button disabled={!!busy} onclick={() => generateScenario('happy')}>Happy path</button><button disabled={!!busy} onclick={() => generateScenario('edge')}>Edge limits</button><button disabled={!!busy} onclick={() => generateScenario('fuzz')}>Validation breach</button><button disabled={!!busy} onclick={() => generateScenario('injection')}>Injection</button>{/if}</div><div class="mode-switch"><button class:active={mode === 'form'} onclick={() => mode = 'form'}>FORM VIEW</button><button class:active={mode === 'json'} onclick={() => { payloadText = JSON.stringify(payload, null, 2); mode = 'json'; }}>RAW JSON</button></div></section>

        <section class="payload-card">
          {#if mode === 'form'}
            <div class="form-grid">
              {#each selected.req_fields || [] as field (field.key)}
                <label class="field-control"><span>{field.label || field.key} <small>({field.key}) · {field.type}</small>{#if field.required}<b class="required-star">*</b>{/if}</span>
                  {#if field.type === 'boolean'}<select value={String(Boolean(payload[field.key]))} onchange={(event) => updateField(field, event.currentTarget.value === 'true')}><option value="false">false</option><option value="true">true</option></select>
                  {:else if field.type === 'array' || field.type === 'object'}<textarea rows="2" value={JSON.stringify(payload[field.key] ?? (field.type === 'array' ? [] : {}))} oninput={(event) => updateField(field, event.currentTarget.value)}></textarea>
                  {:else}<input type={field.type === 'number' ? 'number' : 'text'} value={String(payload[field.key] ?? '')} oninput={(event) => updateField(field, event.currentTarget.value)} placeholder={field.type === 'date' ? 'RFC3339 date/time' : field.validation || ''} />{/if}
                </label>
              {:else}<p class="no-fields">No input fields required for this action.</p>{/each}
            </div>
          {:else}<textarea class="json-editor" aria-label="Raw JSON payload" spellcheck="false" bind:value={payloadText}></textarea>{/if}
          <div class="payload-footer"><span>{payloadSize} B request payload</span><button class="execute-button" type="button" disabled={!!busy} onclick={executeAction}>{busy === 'execute' ? 'EXECUTING…' : selected.kind === 'nflow' ? '▶ CALL NFLOW ROUTE' : '▶ EXECUTE GO ACTION'}</button></div>
        </section>

        <section class="snippet-card"><div class="snippet-header"><div class="snippet-tabs"><button class:active={activeSnippet === 'curl'} onclick={() => activeSnippet = 'curl'}>cURL</button><button class:active={activeSnippet === 'ts'} onclick={() => activeSnippet = 'ts'}>TypeScript</button><button class:active={activeSnippet === 'go'} onclick={() => activeSnippet = 'go'}>Go</button></div><button class="copy-button" onclick={() => copyText(snippet())}>[ COPY SNIPPET ]</button></div><pre>{snippet()}</pre></section>
      {:else}
        <div class="select-empty"><strong>Action Workbench</strong><p>Select a Go action or NFlow-declared route to inspect its contract, send a request, and inspect the result.</p></div>
      {/if}
      <div class="center-footer">Go actions run through the Studio executor. NFlow entries call the analyzed HTTP route directly; routes and schemas come from the NFlow source.</div>
    </section>
    <button class="resizer" type="button" aria-label="Resize response panel" onpointerdown={(event) => startResize(event, 'right')}></button>

    <aside class="right-pane" class:show-mobile={mobileView === 'response'}>
      <div class="right-toolbar"><div class="right-tabs"><button class:active={rightTab === 'response'} onclick={() => rightTab = 'response'}>RESPONSE</button><button class:active={rightTab === 'trace'} onclick={() => rightTab = 'trace'}>TRACE</button><button class:active={rightTab === 'history'} onclick={() => { rightTab = 'history'; void loadHistory(); }}>HISTORY</button></div>{#if execution}<div class="exec-meta"><span class="status-pill" class:success={execution.success} class:failure={!execution.success}>HTTP {execution.status}</span><b>{execution.duration_formatted || `${execution.duration_ms}ms`}</b></div>{/if}</div>
      {#if execution && !execution.success}<div class="error-taxonomy"><strong>[{typeof execution.error === 'object' ? execution.error?.kind || 'ERROR' : 'ERROR'}]</strong><p>{errorText(execution.error)}</p>{#if typeof execution.error === 'object' && execution.error?.cause}<small>{execution.error.cause}</small>{/if}</div>{/if}
      {#if rightTab === 'response'}
        <div class="response-subtoolbar"><div class="response-switch"><button class:active={responseMode === 'tree'} onclick={() => responseMode = 'tree'}>FIELDS</button><button class:active={responseMode === 'json'} onclick={() => responseMode = 'json'}>RAW JSON</button></div><input type="search" placeholder="Filter fields…" bind:value={responseFilter} /><span>{formatBytes(responseSize)}</span><button class="copy-button" onclick={() => copyText(JSON.stringify(resultValue, null, 2))}>COPY</button></div>
        <div class="response-content">
          {#if execution}
            {#if responseMode === 'tree'}<div class="field-tree">{#each flatResponse as row (row.path)}<div class="tree-row"><span class="tree-key">{row.path}</span><span class="tree-type">{row.type}</span><span class="tree-value">{stringify(row.value)}</span></div>{:else}<p class="muted">No response fields match the filter.</p>{/each}</div>
            {:else}<pre class="json-response">{JSON.stringify(resultValue, null, 2)}</pre>{/if}
          {:else}<p class="muted">Ready for execution. Select a Go action or NFlow route and send a request.</p>{/if}
        </div>
      {:else if rightTab === 'trace'}
        <div class="trace-view"><div class="trace-ruler"><span>0µs</span><span>{Math.round((execution?.total_trace_us || 500) / 2)}µs</span><span>{execution?.total_trace_us || 500}µs</span></div>
          {#if selected?.kind === 'nflow' && execution}<div class="trace-span"><div class="trace-title"><span class:trace-error={!execution.success}>{execution.success ? 'HTTP' : 'ERROR'}</span><strong>{selected.method} {selected.path}</strong><small>{execution.duration_formatted}</small></div><div class="trace-track"><i class:trace-error-bar={!execution.success} style="left:0;width:100%"></i></div><p>Direct same-origin HTTP request · status {execution.status} · NFlow pipeline {selected.name}</p></div>
          {:else if execution?.waterfall?.length}{#each execution.waterfall as span (span.name)}<div class="trace-span"><div class="trace-title"><span class:trace-error={span.status === 'ERROR'}>{span.status}</span><strong>{span.name}</strong><small>{span.duration}</small></div><div class="trace-track"><i class:trace-error-bar={span.status === 'ERROR'} style={`left:${Math.min(98, ((span.start_offset_us || 0) / Math.max(execution.total_trace_us || 1, 1)) * 100)}%;width:${Math.max(1, Math.min(100, ((span.duration_us || 1) / Math.max(execution.total_trace_us || 1, 1)) * 100))}%`}></i></div>{#if span.detail}<p>{span.detail}</p>{/if}</div>{/each}
          {:else}<p class="muted">Execute a Go action to inspect its timeline spans.</p>{/if}
        </div>
      {:else}
        <div class="history-heading"><strong>Past executions ({history.length})</strong><button onclick={loadHistory} disabled={!!busy}>REFRESH FEED</button></div>
        <div class="history-list">{#each history as record (record.id)}<div class="history-row"><div class="history-main"><span class="status-dot" class:good={record.success} class:bad={!record.success}></span><div><strong>{record.action_name}</strong><small>{formatTime(record.timestamp)} · {record.duration_formatted || `${record.duration_ms}ms`}</small></div></div><div class="history-buttons"><span>{record.status}</span><button title="Load request into workbench" onclick={() => replay(record)}>↻</button></div></div>{:else}<p class="muted">No executions yet.</p>{/each}</div>
      {/if}
    </aside>
  </main>

  {#if schemaOpen && selected}
    <div class="modal-backdrop" role="presentation" onclick={(event) => { if (event.target === event.currentTarget) schemaOpen = false; }}>
      <div class="schema-modal" role="dialog" aria-modal="true" aria-label={`${schemaSide} schema`} tabindex="-1">
        <header><strong>SCHEMA: <span>{schemaType}</span></strong><div class="schema-mode"><button class:active={schemaMode === 'table'} onclick={() => schemaMode = 'table'}>TABLE</button><button class:active={schemaMode === 'ts'} onclick={() => schemaMode = 'ts'}>TYPESCRIPT</button><button class:active={schemaMode === 'go'} onclick={() => schemaMode = 'go'}>GO</button></div><button class="modal-close" onclick={() => schemaOpen = false}>[ ESC / CLOSE ]</button></header>
        {#if schemaMode === 'table'}<div class="schema-table-wrap"><table><thead><tr><th>FIELD</th><th>TYPE</th><th>REQUIRED</th><th>VALIDATION</th></tr></thead><tbody>{#each schemaFields as field (field.key)}<tr><td>{field.key}</td><td>{field.type}</td><td>{field.required ? 'YES' : 'NO'}</td><td>{field.validation || '—'}</td></tr>{:else}<tr><td colspan="4">No fields declared.</td></tr>{/each}</tbody></table></div>{:else}<pre class="schema-code">{schemaCode(schemaMode)}</pre>{/if}
      </div>
    </div>
  {/if}

  {#if claimsOpen}
    <div class="modal-backdrop" role="presentation" onclick={(event) => { if (event.target === event.currentTarget) claimsOpen = false; }}>
      <div class="claims-modal" role="dialog" aria-modal="true" aria-label="Studio claims matrix" tabindex="-1">
        <header><strong>LIVE SECURITY CLAIMS MATRIX</strong><button class="modal-close" onclick={() => claimsOpen = false}>[ ESC / CLOSE ]</button></header>
        <div class="claims-grid"><label>ACTOR USER ID<input bind:value={customIdentity.user_id} /></label><label>TENANT ID<input bind:value={customIdentity.tenant_id} /></label></div>
        <div class="claim-section"><strong>ROLES</strong><div class="claim-list">{#each security.roles as role}<label><input type="checkbox" checked={customIdentity.roles.includes(role)} onchange={(event) => toggleClaim('roles', role, event.currentTarget.checked)} />{role}</label>{:else}<span>No role requirements declared.</span>{/each}</div><div class="claim-add"><input id="new-role" placeholder="Add custom role…" /><button onclick={() => { const input = document.getElementById('new-role') as HTMLInputElement; addCustomRole(input.value); input.value = ''; }}>+ ADD</button></div></div>
        <div class="claim-section"><strong>PERMISSIONS</strong><div class="claim-list">{#each security.permissions as permission}<label><input type="checkbox" checked={customIdentity.permissions.includes(permission)} onchange={(event) => toggleClaim('permissions', permission, event.currentTarget.checked)} />{permission}</label>{:else}<span>No permissions declared.</span>{/each}</div></div>
        <div class="claim-section"><strong>FEATURES</strong><div class="claim-list">{#each security.features as feature}<label><input type="checkbox" checked={customIdentity.features.includes(feature)} onchange={(event) => toggleClaim('features', feature, event.currentTarget.checked)} />{feature}</label>{:else}<span>No features declared.</span>{/each}</div></div>
        <p class="claim-note">Identity overrides are honored only when the backend explicitly enables them for local development.</p>
        <footer><button class="execute-button" onclick={() => { persona = 'custom'; claimsOpen = false; }}>APPLY IDENTITY</button></footer>
      </div>
    </div>
  {/if}
</div>

<style>
  :global(*) { box-sizing: border-box; }
  :global(html) { background: var(--bg); }
  :global(body) { margin: 0; height: 100vh; overflow: hidden; color: var(--ink); background: var(--bg); font: 13px/1.5 Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
  :global(button), :global(input), :global(select), :global(textarea) { font: inherit; }
  :global(button) { color: inherit; }
  :global(::-webkit-scrollbar) { width: 5px; height: 5px; }
  :global(::-webkit-scrollbar-thumb) { background: var(--border-strong); border-radius: 3px; }
  :global(:root) { --bg:#0c0c0e; --bg-raised:#141416; --bg-elevated:#1c1c1f; --bg-hover:#242428; --ink:#f0f0f5; --ink-secondary:#a0a0b0; --ink-tertiary:#606070; --ink-quaternary:#404050; --accent:#c9a96e; --accent-dim:rgba(201,169,110,.1); --positive:#4ade80; --positive-dim:rgba(74,222,128,.1); --negative:#f87171; --negative-dim:rgba(248,113,113,.1); --border:rgba(255,255,255,.06); --border-strong:rgba(255,255,255,.14); --field:#0a0a0c; --mono:"JetBrains Mono", "SFMono-Regular", Consolas, monospace; }
  :global(html.light) { --bg:#f6f5f2; --bg-raised:#fff; --bg-elevated:#eceae4; --bg-hover:#e4e1d8; --ink:#111114; --ink-secondary:#4a4a58; --ink-tertiary:#757588; --ink-quaternary:#a0a0b0; --accent:#9e7a36; --accent-dim:rgba(158,122,54,.12); --positive:#16a34a; --positive-dim:rgba(22,163,74,.12); --negative:#dc2626; --negative-dim:rgba(220,38,38,.12); --border:rgba(0,0,0,.08); --border-strong:rgba(0,0,0,.16); --field:#f7f6f3; }
  .source-tabs { display:flex; flex-shrink:0; gap:2px; margin:0 5px 0 auto; letter-spacing:0; }
  .source-tabs button { border:1px solid var(--border); border-radius:3px; padding:2px 4px; background:var(--bg); color:var(--ink-tertiary); cursor:pointer; font:8px var(--mono); white-space:nowrap; }
  .source-tabs button.active { border-color:rgba(201,169,110,.32); background:var(--accent-dim); color:var(--accent); }
  .kind-marker { display:inline-block; margin-left:6px; color:var(--ink-tertiary); font:8px var(--mono); }
  .kind-marker.nflow-kind,.kind-badge.nflow-kind { color:var(--accent)!important; border-color:rgba(201,169,110,.32)!important; background:var(--accent-dim); }
  .kind-badge { flex-shrink:0; border:1px solid var(--border); border-radius:3px; padding:2px 5px; color:var(--positive); font:8px var(--mono); white-space:nowrap; }
  .scenario-buttons>small { color:var(--ink-tertiary); font:9px var(--mono); }
  .scenario-buttons>.nflow-label { color:var(--accent); }
  .app-shell { height:100vh; display:flex; flex-direction:column; min-width:0; background:var(--bg); }
  .topbar { height:42px; min-height:42px; display:flex; align-items:center; justify-content:space-between; gap:12px; padding:0 15px; border-bottom:1px solid var(--border); background:var(--bg-raised); font:10px var(--mono); z-index:3; }
  .brand-line,.principal-line,.header-tools,.counts,.identity-badge { display:flex; align-items:center; }
  .brand-line { gap:24px; min-width:220px; }.brand { color:var(--accent); font-weight:800; letter-spacing:.08em; font-size:11px; white-space:nowrap; }.counts { gap:7px; color:var(--ink-tertiary); white-space:nowrap; }.counts strong { color:var(--ink-secondary); }.counts .positive { color:var(--positive); }.counts i,.identity-badge i { width:1px; height:12px; margin:0 3px; background:var(--border-strong); }
  .principal-line { gap:7px; }.principal-line label { color:var(--ink-tertiary); text-transform:uppercase; font-weight:700; }.principal-line select { max-width:190px; border:1px solid var(--border); border-radius:4px; padding:4px 6px; color:var(--accent); background:var(--bg); font:10px var(--mono); }.identity-badge { gap:5px; border:1px solid var(--border); border-radius:4px; padding:4px 7px; background:var(--bg-elevated); cursor:pointer; white-space:nowrap; }.identity-badge b:first-of-type { color:var(--positive); }.identity-badge b:last-of-type { color:var(--accent); }
  .header-tools { gap:10px; }.tool-button,.theme-switch button,.mobile-tabs button { border:1px solid var(--border); border-radius:4px; padding:4px 7px; background:var(--bg); color:var(--ink-secondary); cursor:pointer; font:9px var(--mono); }.tool-button.active { color:var(--accent); }.theme-switch { display:flex; gap:2px; padding:2px; border:1px solid var(--border); border-radius:4px; background:var(--bg); }.theme-switch button { border:0; padding:3px 5px; color:var(--ink-tertiary); }.theme-switch button.active,.mobile-tabs button.active { background:var(--bg-elevated); color:var(--accent); font-weight:700; }.mobile-tabs { display:none; }
  .global-message { min-height:28px; display:flex; align-items:center; gap:10px; padding:4px 14px; border-bottom:1px solid var(--border); font-size:11px; }.global-message span { overflow-wrap:anywhere; }.global-message button { margin-left:auto; border:0; background:none; color:inherit; cursor:pointer; }.error-message { color:var(--negative); background:var(--negative-dim); }.status-message { color:var(--positive); background:var(--positive-dim); }
  .workbench { flex:1; min-height:0; display:grid; grid-template-columns:var(--left-width) 5px minmax(420px,1fr) 5px var(--right-width); overflow:hidden; }.resizer { width:5px; min-width:5px; border:0; padding:0; background:var(--border); cursor:col-resize; touch-action:none; }.resizer:hover,.resizer:active { background:var(--accent); } .left-pane { display:flex; min-width:0; flex-direction:column; border-right:1px solid var(--border); background:var(--bg-raised); overflow:hidden; }.search-area { padding:12px 11px 7px; border-bottom:1px solid var(--border); }.search-area>input,.response-subtoolbar input { width:100%; outline:none; border:1px solid var(--border); border-radius:4px; padding:7px 8px; color:var(--ink); background:var(--bg); font:11px var(--mono); }.search-area>input:focus,.field-control input:focus,.field-control textarea:focus,.field-control select:focus,.json-editor:focus,.claims-grid input:focus { border-color:var(--accent); }.tag-list { display:flex; gap:4px; overflow-x:auto; padding-top:7px; }.tag-list button { flex-shrink:0; border:1px solid var(--border); border-radius:3px; padding:2px 6px; background:var(--bg); color:var(--ink-tertiary); cursor:pointer; font:9px var(--mono); text-transform:uppercase; }.tag-list button.tag-active { border-color:rgba(201,169,110,.32); background:var(--accent-dim); color:var(--accent); }.list-heading { display:flex; justify-content:space-between; padding:10px 12px 7px; color:var(--ink-tertiary); font:9px var(--mono); letter-spacing:.12em; text-transform:uppercase; }.list-heading b { color:var(--ink-secondary); }.action-list { flex:1; overflow:auto; padding:0 7px 10px; }.action-item { width:100%; display:block; border:0; border-left:2px solid transparent; border-radius:2px; padding:8px 8px; background:transparent; text-align:left; cursor:pointer; }.action-item:hover { background:var(--bg-elevated); }.action-item.chosen { border-left-color:var(--accent); background:var(--accent-dim); }.action-name { display:block; overflow:hidden; color:var(--ink); font:10px var(--mono); text-overflow:ellipsis; white-space:nowrap; }.action-route { display:flex; gap:8px; margin-top:3px; color:var(--ink-tertiary); font:9px var(--mono); }.action-route b { color:var(--positive); }.action-route b.method-get,.pill.method-get { color:#60a5fa; }.action-route b.method-other,.pill.method-other { color:var(--accent); }.action-route span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.empty-list,.muted { padding:14px; color:var(--ink-tertiary); font:11px var(--mono); font-style:italic; }.left-footer { padding:7px 11px; border-top:1px solid var(--border); color:var(--ink-tertiary); font:9px var(--mono); }.left-footer span { color:var(--ink-secondary); }
  .center-pane { min-width:0; display:flex; flex-direction:column; gap:10px; overflow:auto; padding:14px; background:var(--bg); }.action-card,.scenario-card,.payload-card,.snippet-card { flex-shrink:0; border:1px solid var(--border); background:var(--bg-raised); }.action-card-top { min-height:75px; display:flex; align-items:center; justify-content:space-between; gap:12px; padding:13px 14px; }.action-title { display:flex; align-items:center; gap:9px; min-width:0; }.action-title h1 { margin:0; overflow:hidden; font:700 13px var(--mono); text-overflow:ellipsis; white-space:nowrap; }.pill { display:inline-flex; align-items:center; border:1px solid rgba(74,222,128,.2); border-radius:3px; padding:2px 7px; background:var(--positive-dim); color:var(--positive); font:600 9px var(--mono); }.pill.method-get { border-color:rgba(59,130,246,.3); background:rgba(59,130,246,.15); color:#60a5fa; }.pill.method-other { border-color:rgba(201,169,110,.2); background:var(--accent-dim); color:var(--accent); }.node-badge { flex-shrink:0; border:1px solid var(--border); border-radius:3px; padding:2px 5px; color:var(--ink-tertiary); font:9px var(--mono); }.action-heading p { margin:5px 0 0; color:var(--ink-secondary); font-size:11px; }.schema-buttons { display:flex; gap:6px; }.schema-buttons button { border:1px solid var(--border); border-radius:3px; padding:6px 7px; background:var(--bg-elevated); color:var(--accent); cursor:pointer; font:9px var(--mono); }.schema-buttons button+button { color:var(--positive); }.schema-buttons strong { font-size:9px; }.capabilities { display:grid; grid-template-columns:repeat(6,minmax(0,1fr)); border-top:1px solid var(--border); background:var(--bg-elevated); text-align:center; }.capabilities div { min-width:0; padding:5px 2px; border-right:1px solid var(--border); font:8px var(--mono); }.capabilities div:last-child { border:0; }.capabilities span,.capabilities b { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.capabilities span { color:var(--ink-tertiary); }.capabilities b { margin-top:2px; color:var(--ink); font-size:9px; }.good { color:var(--positive)!important; }.bad { color:var(--negative)!important; }.accent-text { color:var(--accent)!important; }
  .scenario-card { display:flex; justify-content:space-between; align-items:center; gap:9px; padding:8px 10px; }.scenario-buttons,.mode-switch,.snippet-tabs { display:flex; align-items:center; gap:5px; }.scenario-buttons>span { margin-right:3px; color:#a78bfa; font:700 9px var(--mono); }.scenario-buttons button,.mode-switch button { border:1px solid var(--border); border-radius:3px; padding:5px 7px; background:var(--bg); color:var(--ink-secondary); cursor:pointer; font:9px var(--mono); }.scenario-buttons button:hover,.mode-switch button.active { border-color:var(--accent); color:var(--accent); }.scenario-buttons button:disabled { opacity:.55; }.mode-switch { flex-shrink:0; padding:2px; border:1px solid var(--border); border-radius:4px; background:var(--bg); }.mode-switch button { border:0; padding:4px 6px; color:var(--ink-tertiary); }.mode-switch button.active { background:var(--bg-elevated); font-weight:700; }
  .payload-card { display:flex; min-height:210px; flex-direction:column; }.form-grid { flex:1; display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:14px 12px; padding:14px; }.field-control { min-width:0; display:flex; flex-direction:column; gap:5px; color:var(--ink-secondary); font:10px var(--mono); }.field-control>span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.field-control small { color:var(--ink-tertiary); font-size:9px; }.required-star { margin-left:3px; color:var(--negative); }.field-control input,.field-control select,.field-control textarea,.json-editor { width:100%; min-height:30px; outline:none; border:1px solid var(--border); border-radius:3px; padding:6px 8px; color:var(--ink); background:var(--field); font:10px var(--mono); }.field-control textarea { resize:vertical; }.no-fields { grid-column:1/-1; margin:0; align-self:start; color:var(--ink-tertiary); font:10px var(--mono); font-style:italic; }.json-editor { flex:1; min-height:180px; resize:vertical; padding:12px; border:0; border-radius:0; line-height:1.6; }.payload-footer { display:flex; align-items:center; justify-content:space-between; gap:8px; border-top:1px solid var(--border); padding:8px 10px; background:var(--bg-elevated); color:var(--ink-tertiary); font:9px var(--mono); }.execute-button { border:1px solid var(--accent); border-radius:3px; padding:8px 13px; background:var(--accent); color:#0c0c0e; cursor:pointer; font:700 10px var(--mono); letter-spacing:.06em; }.execute-button:disabled { opacity:.55; cursor:wait; }
  .snippet-header { display:flex; align-items:center; justify-content:space-between; border-bottom:1px solid var(--border); padding:7px 11px; background:var(--bg-elevated); }.snippet-tabs button,.copy-button { border:0; background:transparent; color:var(--ink-tertiary); cursor:pointer; font:9px var(--mono); }.snippet-tabs button.active { border-bottom:1px solid var(--accent); color:var(--accent); }.copy-button { color:var(--accent); white-space:nowrap; }.snippet-card pre { max-height:130px; overflow:auto; margin:0; padding:10px 12px; color:var(--ink-secondary); background:var(--field); font:9px/1.6 var(--mono); white-space:pre-wrap; overflow-wrap:anywhere; }.center-footer { flex-shrink:0; padding:2px 2px 0; color:var(--ink-tertiary); font:9px var(--mono); }.select-empty { margin:auto; max-width:360px; text-align:center; }.select-empty strong { color:var(--accent); font:700 16px var(--mono); }.select-empty p { color:var(--ink-tertiary); font-size:12px; }
  .right-pane { min-width:0; display:flex; flex-direction:column; overflow:hidden; border-left:1px solid var(--border); background:var(--bg-raised); }.right-toolbar { min-height:43px; display:flex; align-items:center; justify-content:space-between; gap:6px; border-bottom:1px solid var(--border); padding:6px 8px; background:var(--bg-elevated); }.right-tabs { display:flex; gap:3px; }.right-tabs button { border:1px solid transparent; border-radius:3px; padding:6px 7px; background:transparent; color:var(--ink-tertiary); cursor:pointer; font:9px var(--mono); }.right-tabs button.active { border-color:var(--border); background:var(--bg); color:var(--accent); font-weight:700; }.exec-meta { display:flex; align-items:center; gap:8px; color:var(--accent); font:9px var(--mono); white-space:nowrap; }.status-pill { border-radius:3px; padding:3px 5px; background:var(--positive-dim); color:var(--positive); }.status-pill.failure { background:var(--negative-dim); color:var(--negative); }.error-taxonomy { border-bottom:1px solid var(--border); padding:9px 11px; background:var(--negative-dim); font:10px var(--mono); }.error-taxonomy strong { color:var(--negative); }.error-taxonomy p { margin:4px 0 0; color:var(--ink); overflow-wrap:anywhere; }.error-taxonomy small { display:block; margin-top:4px; color:var(--negative); overflow-wrap:anywhere; }.response-subtoolbar { display:flex; align-items:center; gap:6px; min-height:36px; border-bottom:1px solid var(--border); padding:5px 7px; background:var(--bg-raised); }.response-subtoolbar input { min-width:70px; padding:4px 5px; font-size:9px; }.response-subtoolbar>span { flex-shrink:0; color:var(--ink-tertiary); font:8px var(--mono); }.response-switch { display:flex; flex-shrink:0; padding:2px; border:1px solid var(--border); border-radius:3px; background:var(--bg); }.response-switch button { border:0; border-radius:2px; padding:4px 5px; background:transparent; color:var(--ink-tertiary); cursor:pointer; font:8px var(--mono); white-space:nowrap; }.response-switch button.active { background:var(--bg-elevated); color:var(--accent); }.response-content { flex:1; min-height:0; overflow:auto; background:var(--bg); }.field-tree { padding:8px; font:9px var(--mono); }.tree-row { display:grid; grid-template-columns:minmax(90px,1fr) auto minmax(90px,1.2fr); gap:7px; padding:5px 3px; border-bottom:1px solid var(--border); }.tree-key { overflow-wrap:anywhere; color:#60a5fa; }.tree-type { color:var(--ink-tertiary); }.tree-value { overflow-wrap:anywhere; color:#4ade80; }.json-response { margin:0; padding:11px; color:var(--ink-secondary); font:9px/1.6 var(--mono); white-space:pre-wrap; overflow-wrap:anywhere; }.trace-view { flex:1; overflow:auto; padding:11px; font:9px var(--mono); }.trace-ruler { display:flex; justify-content:space-between; border-bottom:1px solid var(--border); padding-bottom:5px; color:var(--ink-tertiary); font-size:8px; }.trace-span { padding:9px 2px; border-bottom:1px solid var(--border); }.trace-title { display:flex; align-items:center; gap:7px; }.trace-title>span { color:var(--positive); font-size:8px; }.trace-title>span.trace-error { color:var(--negative); }.trace-title strong { flex:1; color:var(--ink-secondary); }.trace-title small { color:var(--accent); }.trace-track { position:relative; height:5px; margin:7px 0 4px; border-radius:3px; background:var(--bg-elevated); }.trace-track i { position:absolute; top:0; height:5px; min-width:3px; border-radius:3px; background:var(--accent); }.trace-track i.trace-error-bar { background:var(--negative); }.trace-span p { margin:0; color:var(--ink-tertiary); overflow-wrap:anywhere; }.history-heading { display:flex; justify-content:space-between; border-bottom:1px solid var(--border); padding:9px 10px; background:var(--bg-elevated); color:var(--ink-tertiary); font:9px var(--mono); text-transform:uppercase; }.history-heading button { border:0; background:none; color:var(--accent); cursor:pointer; font:9px var(--mono); }.history-list { flex:1; overflow:auto; padding:6px; }.history-row { display:flex; align-items:center; justify-content:space-between; gap:8px; border-bottom:1px solid var(--border); padding:8px 3px; }.history-main { display:flex; align-items:center; gap:7px; min-width:0; }.history-main>div { min-width:0; }.history-main strong,.history-main small { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.history-main strong { color:var(--ink-secondary); font:9px var(--mono); }.history-main small { margin-top:2px; color:var(--ink-tertiary); font:8px var(--mono); }.status-dot { width:7px; height:7px; flex-shrink:0; border-radius:50%; }.status-dot.good { background:var(--positive); }.status-dot.bad { background:var(--negative); }.history-buttons { display:flex; align-items:center; gap:6px; color:var(--ink-tertiary); font:8px var(--mono); }.history-buttons button { border:0; background:none; color:var(--accent); cursor:pointer; font-size:15px; }
  .modal-backdrop { position:fixed; inset:0; z-index:10; display:flex; align-items:center; justify-content:center; padding:18px; background:rgba(0,0,0,.72); }.schema-modal,.claims-modal { width:min(700px,100%); max-height:90vh; overflow:auto; border:1px solid var(--border-strong); border-radius:7px; padding:15px; background:var(--bg-raised); box-shadow:0 20px 80px rgba(0,0,0,.5); }.schema-modal>header,.claims-modal>header { display:flex; align-items:center; gap:10px; border-bottom:1px solid var(--border); padding-bottom:10px; color:var(--accent); font:700 10px var(--mono); }.schema-modal>header>strong { white-space:nowrap; }.schema-modal>header>strong span { color:var(--ink); }.schema-mode { display:flex; gap:2px; margin-left:auto; padding:2px; border:1px solid var(--border); border-radius:3px; }.schema-mode button,.modal-close { border:0; padding:4px 5px; background:transparent; color:var(--ink-tertiary); cursor:pointer; font:8px var(--mono); white-space:nowrap; }.schema-mode button.active { background:var(--bg-elevated); color:var(--accent); }.modal-close { margin-left:auto; }.schema-table-wrap { max-height:60vh; overflow:auto; margin-top:12px; border:1px solid var(--border); border-radius:4px; }.schema-modal table { width:100%; border-collapse:collapse; text-align:left; font:9px var(--mono); }.schema-modal th { position:sticky; top:0; background:var(--bg-elevated); color:var(--ink-tertiary); }.schema-modal th,.schema-modal td { padding:8px; border-bottom:1px solid var(--border); }.schema-modal td:first-child { color:var(--accent); }.schema-code { max-height:60vh; overflow:auto; margin:12px 0 0; padding:12px; background:var(--field); color:var(--ink-secondary); font:10px/1.6 var(--mono); }.claims-modal { width:min(650px,100%); }.claims-modal>header { justify-content:space-between; }.claims-modal>header .modal-close { margin:0; }.claims-grid { display:grid; grid-template-columns:repeat(2,1fr); gap:10px; margin-top:13px; }.claims-grid label,.claim-section { color:var(--ink-tertiary); font:9px var(--mono); }.claims-grid input,.claim-add input { display:block; width:100%; margin-top:5px; border:1px solid var(--border); border-radius:3px; padding:7px; outline:0; color:var(--ink); background:var(--field); font:10px var(--mono); }.claim-section { margin-top:13px; }.claim-section>strong { display:block; margin-bottom:5px; color:var(--ink-tertiary); }.claim-list { display:flex; flex-wrap:wrap; gap:8px 12px; max-height:100px; overflow:auto; border:1px solid var(--border); border-radius:3px; padding:8px; background:var(--field); }.claim-list label { display:flex; align-items:center; gap:4px; color:var(--ink-secondary); }.claim-list input { accent-color:var(--accent); }.claim-list span { color:var(--ink-tertiary); }.claim-add { display:flex; gap:6px; margin-top:6px; }.claim-add input { flex:1; margin:0; }.claim-add button { border:1px solid var(--border); border-radius:3px; padding:4px 8px; background:var(--bg-elevated); color:var(--accent); cursor:pointer; font:9px var(--mono); }.claim-note { color:var(--ink-tertiary); font:9px var(--mono); }.claims-modal footer { display:flex; justify-content:flex-end; border-top:1px solid var(--border); padding-top:10px; }
  @media(max-width:1400px) { .workbench { grid-template-columns:230px 5px minmax(380px,1fr) 5px 370px; }.scenario-card { align-items:flex-start; flex-direction:column; }.scenario-buttons { flex-wrap:wrap; }.mode-switch { align-self:flex-end; margin-top:-30px; } }
  @media(max-width:1100px) { .workbench { grid-template-columns:230px minmax(0,1fr); }.resizer { display:none; }.right-pane { display:none; }.right-pane.show-mobile { position:absolute; z-index:2; inset:0 0 0 230px; display:flex; border-left:1px solid var(--border); }.mobile-tabs { display:flex; gap:3px; }.header-tools .tool-button span { display:none; }.principal-line label { display:none; } }
  @media(max-width:700px) { .topbar { height:auto; min-height:46px; flex-wrap:wrap; gap:6px; padding:6px 8px; }.brand-line { flex:1; min-width:0; gap:7px; }.counts { font-size:8px; }.principal-line { order:3; width:100%; }.principal-line select { flex:1; max-width:none; }.identity-badge { margin-left:auto; }.header-tools { gap:5px; }.tool-button { display:none; }.workbench { display:block; position:relative; }.left-pane { display:none; }.center-pane { height:100%; padding:8px; }.workbench.response-mode .center-pane { display:none; }.workbench.response-mode .right-pane.show-mobile { inset:0; }.workbench.workbench-mode .right-pane { display:none; }.action-card-top { align-items:flex-start; flex-direction:column; }.action-title { flex-wrap:wrap; }.schema-buttons { align-self:flex-end; margin-top:-26px; }.capabilities { grid-template-columns:repeat(3,1fr); }.capabilities div:nth-child(3) { border-right:0; }.scenario-card { padding:7px; }.scenario-buttons { gap:4px; }.scenario-buttons>span { width:100%; }.mode-switch { margin-top:0; align-self:flex-start; }.form-grid { grid-template-columns:1fr; }.payload-card { min-height:230px; }.right-pane.show-mobile { inset:0; }.response-subtoolbar { flex-wrap:wrap; }.claims-grid { grid-template-columns:1fr; }.schema-modal>header { flex-wrap:wrap; }.schema-mode { margin-left:0; }.modal-close { margin-left:auto; } }
</style>
